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

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
)

type Space struct{ ID, Root, Remote string }
type Result struct {
	Uploaded, Downloaded, Deleted int
	Merged                        int
	Conflicts                     []string
	Skipped                       []string
	Details                       []Conflict
}

// Progress reports work before blocking I/O. Total is zero while the directory
// inventory is still being built; Checked counts already examined files.
type Progress struct {
	Phase, Path    string
	Checked, Total int
}

type Conflict struct {
	Path, RemoteHash string
	RemoteExists     bool
}

// Hooks lets the native app protect active edits and send user files to the
// system recycle bin after the engine has saved a recovery copy.
type Hooks struct {
	// Reconcile is an explicit, one-run recovery direction (local or remote).
	// It preserves one-sided files and must never be enabled by a retry.
	Reconcile string
	// Recheck bypasses cached validators while keeping the common baseline,
	// backups and all conditional writes intact.
	Recheck     bool
	Progress    func(Progress)
	BeforeApply func(string) error
	Trash       func(string) error
	// Merge combines disjoint document edits against persisted common bytes.
	// False preserves the existing conflict-copy and explicit-choice behavior.
	Merge func(path string, base, local, remote []byte) ([]byte, bool)
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
func Sync(ctx context.Context, c Config, sp Space, stateDir string, h Hooks) (result Result, err error) {
	if h.Reconcile != "" && h.Reconcile != "local" && h.Reconcile != "remote" {
		return result, errors.New("无效的同步覆盖方向")
	}
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
	checked, total := 0, 0
	currentPath := ""
	report := func(phase, rel string) {
		if h.Progress != nil {
			h.Progress(Progress{phase, rel, checked, total})
		}
	}
	key := hashBytes([]byte(c.TargetID() + "\x00" + sp.ID + "\x00" + sp.Root + "\x00" + sp.Remote))[:32]
	stateFile := filepath.Join(stateDir, key+".json")
	ancestorDir := filepath.Join(stateDir, "ancestors", key)
	remember := func(rel string, data []byte) error {
		if h.Merge == nil || !mergeablePath(rel) {
			return nil
		}
		return saveAncestor(ancestorDir, data, c)
	}
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
	report("local", "")
	local, err := listLocal(sp.Root)
	if err != nil {
		return result, err
	}
	remote, err := listRemote(s, sp.Remote, func(dir string) { report("list", dir) })
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
	total = len(ordered)
	dirty := false
	checkpoint := func() error {
		if !dirty {
			return nil
		}
		raw, e := json.Marshal(b)
		if e != nil {
			return e
		}
		if e = fsutil.WriteFileAtomic(stateFile, raw, 0o600); e != nil {
			return e
		}
		dirty = false
		return nil
	}
	// Retain already confirmed files even if a later request is canceled.
	// Failed checks never advance their baseline; checkpoint failures stay visible.
	defer func() {
		if err != nil && currentPath != "" {
			err = fmt.Errorf("%s: %w", currentPath, err)
		}
		if saveErr := checkpoint(); saveErr != nil {
			// A failed checkpoint is a failure even when the network was canceled.
			// Wrap the save error so the UI cannot classify it as a clean cancel.
			if err != nil {
				err = fmt.Errorf("保存同步进度失败：%w（同步结果：%v）", saveErr, err)
			} else {
				err = saveErr
			}
		}
	}()
	for _, rel := range ordered {
		currentPath = rel
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !safeRelative(rel) || excluded(rel) {
			return result, errors.New("同步基线包含无效路径")
		}
		report("check", rel)
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
		remoteEncrypted := old.Encrypted
		var raw, r []byte
		loaded := false
		load := func(validate bool) error {
			report("verify", rel)
			var e error
			validator := ""
			if validate && known {
				validator = old.RemoteETag
			}
			raw, etag, e = readRemote(s, sp.Remote+"/"+rel, validator)
			if errors.Is(e, errRemoteNotModified) {
				rexists, rh = true, old.Hash
				return nil
			}
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
			var envelope struct {
				Format string `json:"format"`
			}
			remoteEncrypted = json.Unmarshal(raw, &envelope) == nil && envelope.Format == sealedFormat
			if int64(len(r)) > maxFileBytes {
				return errors.New("文件超过 64 MiB")
			}
			rh = hashBytes(r)
			return nil
		}
		if h.Recheck || h.Reconcile != "" {
			if err = load(false); err != nil {
				return result, err
			}
		} else if rexists && known && etag != "" && etag == old.RemoteETag {
			rh = old.Hash
		} else if rexists || known {
			if err = load(true); err != nil {
				return result, err
			}
		}

		needEncrypt := h.Reconcile != "remote" && c.Encrypt && lexists && rexists && (!known || !old.Encrypted)
		if needEncrypt && !loaded {
			if err = load(false); err != nil {
				return result, err
			}
		}
		if needEncrypt {
			needEncrypt = !remoteEncrypted
		}
		if lh == rh && !needEncrypt {
			if lexists {
				if err = remember(rel, l); err != nil {
					return result, err
				}
				row := baselineRow{Hash: lh, RemoteETag: etag, Encrypted: remoteEncrypted}
				if !known || row != old {
					b.Files[rel], dirty = row, true
				}
			} else {
				delete(b.Files, rel)
				dirty = dirty || known
			}
			checked++
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
		if h.Reconcile != "" {
			// Like rclone bisync --resync-mode path1/path2, prefer the
			// chosen side for collisions and keep the union of one-sided files.
			localChanged = lexists && (!rexists || h.Reconcile == "local")
			remoteChanged = rexists && (!lexists || h.Reconcile == "remote")
		}
		switch {
		case localChanged && remoteChanged:
			if known && lexists && rexists && h.Merge != nil && mergeablePath(rel) {
				ancestor, found, e := readAncestor(ancestorDir, old.Hash, c.EncryptPassword)
				if e != nil {
					return result, e
				}
				if found {
					if merged, clean := h.Merge(rel, ancestor, l, r); clean {
						if int64(len(merged)) > maxFileBytes {
							return result, errors.New("合并后的文件超过 64 MiB")
						}
						// Both destinations are version-checked. Keep the old baseline
						// on any partial failure so a retry cannot discard either edit.
						if h.BeforeApply != nil && h.BeforeApply(rel) != nil {
							if ctx.Err() != nil {
								return result, ctx.Err()
							}
							result.Skipped = append(result.Skipped, rel)
							checked++
							continue
						}
						current, exists, e := readLocal(sp.Root, rel)
						if e != nil {
							return result, e
						}
						if !exists || !bytes.Equal(current, l) {
							result.Skipped = append(result.Skipped, rel)
							checked++
							continue
						}
						if e = backupLocal(stateDir, key, rel, l, c.KeepSnapshots); e != nil {
							return result, e
						}
						if e = archiveRemote(s, sp.Remote, rel, raw, c.KeepSnapshots); e != nil {
							return result, e
						}
						payload := merged
						if c.Encrypt {
							payload, e = sealBackup(merged, c.EncryptPassword)
							if e != nil {
								return result, e
							}
						}
						report("merge", rel)
						newTag, e := writeRemote(s, sp.Remote+"/"+rel, payload, etag, true)
						if e != nil {
							return result, e
						}
						result.Uploaded++
						if h.BeforeApply != nil && h.BeforeApply(rel) != nil {
							result.Skipped = append(result.Skipped, rel)
							checked++
							continue
						}
						current, exists, e = readLocal(sp.Root, rel)
						if e != nil {
							return result, e
						}
						if !exists || !bytes.Equal(current, l) {
							result.Skipped = append(result.Skipped, rel)
							checked++
							continue
						}
						file, e := localFile(sp.Root, rel)
						if e != nil {
							return result, e
						}
						if e = fsutil.WriteFileAtomic(file, merged, 0o644); e != nil {
							return result, e
						}
						if e = remember(rel, merged); e != nil {
							return result, e
						}
						b.Files[rel] = baselineRow{Hash: hashBytes(merged), RemoteETag: newTag, Encrypted: c.Encrypt}
						dirty = true
						result.Downloaded++
						result.Merged++
						checked++
						if e = checkpoint(); e != nil {
							return result, e
						}
						continue
					}
				}
			}
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
			checked++
			continue // Keep the old baseline until the user resolves the conflict.
		case localChanged:
			if h.Reconcile != "" {
				if h.BeforeApply != nil && h.BeforeApply(rel) != nil {
					if ctx.Err() != nil {
						return result, ctx.Err()
					}
					result.Skipped = append(result.Skipped, rel)
					checked++
					continue
				}
				current, exists, e := readLocal(sp.Root, rel)
				if e != nil {
					return result, e
				}
				if exists != lexists || !bytes.Equal(current, l) {
					result.Skipped = append(result.Skipped, rel)
					checked++
					continue
				}
				// Keep the old remote bytes locally as well. This recovery
				// does not depend solely on the failing service's history.
				if rexists {
					if err = backupLocal(stateDir, key, rel, raw, c.KeepSnapshots); err != nil {
						return result, err
					}
				}
			}
			if rexists {
				if !loaded {
					expectedHash := rh
					if err = load(false); err != nil {
						return result, err
					}
					// Listing and GET validators can differ for identical bytes.
					// Compare content, then write against the fresh GET validator.
					if !rexists || rh != expectedHash {
						return result, errRemoteChanged
					}
				}
				if err = archiveRemote(s, sp.Remote, rel, raw, c.KeepSnapshots); err != nil {
					return result, err
				}
			}
			if !lexists {
				report("delete", rel)
				if err = deleteRemote(s, sp.Remote+"/"+rel, etag); err != nil {
					return result, err
				}
				delete(b.Files, rel)
				dirty = true
				result.Deleted++
			} else {
				report("upload", rel)
				payload := l
				if c.Encrypt {
					payload, err = sealBackup(l, c.EncryptPassword)
					if err != nil {
						return result, err
					}
				}
				if h.Reconcile != "" && rexists {
					etag, err = overwriteRemote(s, sp.Remote+"/"+rel, payload)
				} else {
					etag, err = writeRemote(s, sp.Remote+"/"+rel, payload, etag, rexists)
				}
				if err != nil {
					return result, err
				}
				if err = remember(rel, l); err != nil {
					return result, err
				}
				b.Files[rel] = baselineRow{Hash: lh, RemoteETag: etag, Encrypted: c.Encrypt}
				dirty = true
				result.Uploaded++
			}
		case remoteChanged:
			// A resolution can choose an unchanged remote version. Conditional
			// validation has no body, so fetch it before writing locally.
			if rexists && !loaded {
				expectedHash := rh
				if err = load(false); err != nil {
					return result, err
				}
				if !rexists || rh != expectedHash {
					return result, errRemoteChanged
				}
			}
			report("download", rel)
			if h.BeforeApply != nil {
				if err = h.BeforeApply(rel); err != nil {
					if ctx.Err() != nil {
						return result, ctx.Err()
					}
					result.Skipped = append(result.Skipped, rel)
					checked++
					continue
				}
			}
			current, exists, e := readLocal(sp.Root, rel)
			if e != nil {
				return result, e
			}
			if exists != lexists || !bytes.Equal(current, l) {
				result.Skipped = append(result.Skipped, rel)
				checked++
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
				dirty = true
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
				if err = remember(rel, r); err != nil {
					return result, err
				}
				b.Files[rel] = baselineRow{Hash: rh, RemoteETag: etag, Encrypted: remoteEncrypted}
				dirty = true
				result.Downloaded++
			}
		}
		checked++
		if err = checkpoint(); err != nil {
			return result, err
		}
	}
	return result, nil
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
	dir := root + "/.freefolio-history/" + hashBytes([]byte(rel))[:24]
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
