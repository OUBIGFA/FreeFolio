package cloudsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/fsutil"
	"github.com/OUBIGFA/FreeMD/internal/vault"
)

type Space struct{ ID, Root, Remote string }
type Result struct {
	Uploaded, Downloaded, Deleted int
	Conflicts                     []string
	Skipped                       []string
	Details                       []Conflict
}

type Conflict struct {
	Path, RemoteHash string
	RemoteExists     bool
}

// Hooks lets the native app protect active edits and send user files to the
// system recycle bin after the engine has saved a recovery copy.
type Hooks struct {
	BeforeApply func(string) error
	Trash       func(string) error
	// Resolve names explicit user choices for this run. The remote digest
	// binds a choice to the version the user saw, so a newer version conflicts.
	Resolve map[string]Resolution
}

type Resolution struct {
	UseLocal   bool
	RemoteHash string
}

type baselineRow struct {
	Hash       string `json:"hash"`
	RemoteETag string `json:"remoteETag,omitempty"`
	Encrypted  bool   `json:"encrypted,omitempty"`
}
type baseline struct {
	Version int                    `json:"version"`
	Files   map[string]baselineRow `json:"files"`
}

func safeRelative(p string) bool {
	if p == "" || p == "." || path.IsAbs(p) || strings.ContainsAny(p, "\\:\x00") || path.Clean(p) != p {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == ".." || s == "" || strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") {
			return false
		}
	}
	return true
}

func excluded(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") || part == "node_modules" || part == "credentials" || part == "_temp" {
			return true
		}
	}
	base := strings.ToLower(path.Base(rel))
	return base == "spaces.json" || base == "prefs.json" || base == "sync.json" || strings.HasSuffix(base, ".tmp") || strings.Contains(base, " (同步冲突 ")
}

func hashBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

func localFile(root, rel string) (string, error) {
	if !safeRelative(rel) {
		return "", errors.New("invalid local sync path")
	}
	// Reject links at every segment; resolution alone would permit a link into
	// another directory inside the same space and synchronize it twice.
	cur := root
	for _, part := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("同步路径包含符号链接")
		}
	}
	return vault.Resolve(root, rel)
}

func readLocal(root, rel string) ([]byte, bool, error) {
	file, err := localFile(root, rel)
	if err != nil {
		return nil, false, err
	}
	info, err := os.Stat(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, false, fmt.Errorf("不是可同步的普通文件或超过 64 MiB：%s", rel)
	}
	b, err := os.ReadFile(file)
	return b, true, err
}

func listLocal(root string) (map[string]bool, error) {
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(file string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if file == root {
			return nil
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if excluded(rel) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("同步空间包含符号链接：%s", rel)
		}
		if e.IsDir() {
			return nil
		}
		if !safeRelative(rel) {
			return fmt.Errorf("同步路径无效：%s", rel)
		}
		out[rel] = true
		if len(out) > 100000 {
			return errors.New("同步文件数量超过限制")
		}
		return nil
	})
	return out, err
}

// Sync uses a per-file common baseline. Concurrent changes preserve the local
// file and save the remote side as an excluded, visible conflict copy.
func Sync(ctx context.Context, c Config, sp Space, stateDir string, h Hooks) (Result, error) {
	var result Result
	if !c.Enabled {
		return result, errors.New("请先启用云端同步")
	}
	if sp.ID == "" || !safeRelative(sp.Remote) {
		return result, errors.New("请选择同步空间和远端目录")
	}
	s, err := openStore(ctx, c)
	if err != nil {
		return result, err
	}
	key := hashBytes([]byte(c.TargetID() + "\x00" + sp.ID + "\x00" + sp.Root + "\x00" + sp.Remote))[:32]
	stateFile := filepath.Join(stateDir, key+".json")
	b := baseline{Version: 1, Files: map[string]baselineRow{}}
	if raw, e := os.ReadFile(stateFile); e == nil {
		if e = json.Unmarshal(raw, &b); e != nil {
			return result, e
		}
		if b.Version != 1 || b.Files == nil {
			return result, errors.New("同步基线格式无效")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return result, e
	}
	local, err := listLocal(sp.Root)
	if err != nil {
		return result, err
	}
	remote, err := listRemote(s, sp.Remote)
	if err != nil {
		return result, err
	}
	paths := map[string]bool{}
	for p := range local {
		paths[p] = true
	}
	for p := range remote {
		paths[p] = true
	}
	for p := range b.Files {
		paths[p] = true
	}
	ordered := make([]string, 0, len(paths))
	for p := range paths {
		ordered = append(ordered, p)
	}
	slices.Sort(ordered)
	checkpoint := func() error {
		raw, e := json.Marshal(b)
		if e != nil {
			return e
		}
		return fsutil.WriteFileAtomic(stateFile, raw, 0o600)
	}
	for _, rel := range ordered {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !safeRelative(rel) || excluded(rel) {
			return result, errors.New("同步基线包含无效路径")
		}
		l, lexists, err := readLocal(sp.Root, rel)
		if err != nil {
			return result, err
		}
		old, known := b.Files[rel]
		lh := ""
		if lexists {
			lh = hashBytes(l)
		}
		entry, rexists := remote[rel]
		etag, rh := entry.ETag, ""
		var raw, r []byte
		loaded := false
		load := func() error {
			var e error
			raw, etag, e = readRemote(s, sp.Remote+"/"+rel)
			if errors.Is(e, errRemoteMissing) {
				rexists = false
				rh = ""
				loaded = true
				return nil
			}
			if e != nil {
				return e
			}
			rexists = true
			loaded = true
			r, e = openBackup(raw, c.EncryptPassword)
			if e != nil {
				return e
			}
			if int64(len(r)) > maxFileBytes {
				return errors.New("文件超过 64 MiB")
			}
			rh = hashBytes(r)
			return nil
		}
		if rexists && known && etag != "" && etag == old.RemoteETag {
			rh = old.Hash
		} else if rexists || known {
			if err = load(); err != nil {
				return result, err
			}
		}

		needEncrypt := c.Encrypt && lexists && rexists && (!known || !old.Encrypted)
		if needEncrypt && !loaded {
			if err = load(); err != nil {
				return result, err
			}
		}
		if needEncrypt {
			var envelope sealedBackup
			needEncrypt = json.Unmarshal(raw, &envelope) != nil || envelope.Format != sealedFormat
		}
		if lh == rh && !needEncrypt {
			if lexists {
				b.Files[rel] = baselineRow{Hash: lh, RemoteETag: etag, Encrypted: c.Encrypt || old.Encrypted}
			} else {
				delete(b.Files, rel)
			}
			continue
		}
		localChanged := !known || lh != old.Hash
		remoteChanged := !known || rh != old.Hash
		// First contact merges disjoint files without treating absence as deletion.
		if !known {
			localChanged = lexists
			remoteChanged = rexists
		}
		if needEncrypt && lh == rh {
			localChanged, remoteChanged = true, false
		}
		if choice, ok := h.Resolve[rel]; ok {
			if choice.RemoteHash != rh {
				return result, errRemoteChanged
			}
			localChanged, remoteChanged = choice.UseLocal, !choice.UseLocal
		}
		switch {
		case localChanged && remoteChanged:
			if rexists {
				conflict := strings.TrimSuffix(rel, path.Ext(rel)) + " (同步冲突 " + rh[:12] + ")" + path.Ext(rel)
				file, e := localFile(sp.Root, conflict)
				if e != nil {
					return result, e
				}
				if e = fsutil.CreateFileAtomic(file, r, 0o600); e != nil && !errors.Is(e, os.ErrExist) {
					return result, e
				}
			}
			result.Conflicts = append(result.Conflicts, rel)
			result.Details = append(result.Details, Conflict{rel, rh, rexists})
			continue // Keep the old baseline until the user resolves the conflict.
		case localChanged:
			if rexists {
				if !loaded {
					expected := etag
					if err = load(); err != nil {
						return result, err
					}
					if !rexists || etag != expected {
						return result, errRemoteChanged
					}
				}
				if err = archiveRemote(s, sp.Remote, rel, raw, c.KeepSnapshots); err != nil {
					return result, err
				}
			}
			if !lexists {
				if err = deleteRemote(s, sp.Remote+"/"+rel, etag); err != nil {
					return result, err
				}
				delete(b.Files, rel)
				result.Deleted++
			} else {
				payload := l
				if c.Encrypt {
					payload, err = sealBackup(l, c.EncryptPassword)
					if err != nil {
						return result, err
					}
				}
				if err = writeRemote(s, sp.Remote+"/"+rel, payload, etag, rexists); err != nil {
					return result, err
				}
				b.Files[rel] = baselineRow{Hash: lh, Encrypted: c.Encrypt}
				result.Uploaded++
			}
		case remoteChanged:
			if h.BeforeApply != nil {
				if err = h.BeforeApply(rel); err != nil {
					result.Skipped = append(result.Skipped, rel)
					continue
				}
			}
			current, exists, e := readLocal(sp.Root, rel)
			if e != nil {
				return result, e
			}
			if exists != lexists || !bytes.Equal(current, l) {
				result.Skipped = append(result.Skipped, rel)
				continue
			}
			if lexists {
				if err = backupLocal(stateDir, key, rel, l, c.KeepSnapshots); err != nil {
					return result, err
				}
			}
			file, e := localFile(sp.Root, rel)
			if e != nil {
				return result, e
			}
			if !rexists {
				if h.Trash == nil {
					return result, errors.New("系统回收站不可用，保留本地文件")
				}
				if err = h.Trash(file); err != nil {
					return result, err
				}
				delete(b.Files, rel)
				result.Deleted++
			} else {
				if lexists {
					err = fsutil.WriteFileAtomic(file, r, 0o644)
				} else {
					err = fsutil.CreateFileAtomic(file, r, 0o644)
				}
				if err != nil {
					return result, err
				}
				b.Files[rel] = baselineRow{Hash: rh, RemoteETag: etag, Encrypted: c.Encrypt}
				result.Downloaded++
			}
		}
		if err = checkpoint(); err != nil {
			return result, err
		}
	}
	return result, checkpoint()
}

func backupLocal(stateDir, key, rel string, data []byte, keep int) error {
	dir := filepath.Join(stateDir, "backups", key, hashBytes([]byte(rel))[:24])
	file := filepath.Join(dir, time.Now().UTC().Format("20060102T150405.000000000")+path.Ext(rel))
	meta, _ := json.Marshal(map[string]string{"path": rel})
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, "source.json"), meta, 0o600); err != nil {
		return err
	}
	if err := fsutil.CreateFileAtomic(file, data, 0o600); err != nil {
		return err
	}
	// Backups are retained without automatic local deletion. The app exposes
	// their directory, allowing the user to inspect or recycle them explicitly.
	return nil
}

func archiveRemote(s remoteStore, root, rel string, data []byte, keep int) error {
	dir := root + "/.freemd-history/" + hashBytes([]byte(rel))[:24]
	key := dir + "/" + time.Now().UTC().Format("20060102T150405.000000000")
	if err := s.PutNew(key, bytes.NewReader(data), int64(len(data))); err != nil {
		return err
	}
	entries, err := s.List(dir)
	if err != nil {
		return err
	}
	slices.SortFunc(entries, func(a, b remoteEntry) int { return strings.Compare(b.Name, a.Name) })
	for i, e := range entries {
		if i >= keep && !e.Dir && safeRelative(e.Name) && !strings.Contains(e.Name, "/") {
			if err := s.Delete(dir + "/" + e.Name); err != nil {
				return err
			}
		}
	}
	return nil
}
