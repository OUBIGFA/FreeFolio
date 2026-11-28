package gitsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

const ignoreBegin = "# FreeFolio sync exclusions"
const ignoreEnd = "# End FreeFolio sync exclusions"

type ExclusionEntry struct {
	Path      string
	Directory bool
}

type ExclusionState struct {
	Entries     []ExclusionEntry
	Paths       []string
	Fingerprint string
}

func exclusionMatches(path string, rules []string) bool {
	for _, rule := range rules {
		if path == strings.TrimSuffix(rule, "/") || strings.HasSuffix(rule, "/") && strings.HasPrefix(path, rule) {
			return true
		}
	}
	return false
}

func exclusionPath(root, path string) error {
	path = strings.TrimSuffix(path, "/")
	if path == ".gitignore" || strings.ContainsAny(path, "\r\n") {
		return errors.New(i18n.T("不能排除 Git 配置文件或无效路径"))
	}
	_, err := validPath(root, path)
	return err
}

// Managed rules are root-relative literal paths, not user-entered patterns.
// All text outside this marked block is kept byte for byte.
func readExclusions(root string) ([]byte, []string, int, int, error) {
	path, err := validPath(root, ".gitignore")
	if err != nil {
		return nil, nil, 0, 0, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil, -1, -1, nil
	}
	if err != nil {
		return nil, nil, 0, 0, err
	}
	if !utf8.Valid(data) || len(data) > 1<<20 {
		return nil, nil, 0, 0, errors.New(i18n.T("无法读取忽略规则，请检查 .gitignore 的编码和大小"))
	}
	start, end, offset := -1, -1, 0
	var paths []string
	for _, line := range strings.SplitAfter(string(data), "\n") {
		text := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		switch text {
		case ignoreBegin:
			if start != -1 {
				return nil, nil, 0, 0, errors.New(i18n.T("同步排除规则已被修改，请先检查 .gitignore"))
			}
			start = offset
		case ignoreEnd:
			if start == -1 || end != -1 {
				return nil, nil, 0, 0, errors.New(i18n.T("同步排除规则已被修改，请先检查 .gitignore"))
			}
			end = offset + len(line)
		default:
			if start != -1 && end == -1 && text != "" {
				if !strings.HasPrefix(text, "/") {
					return nil, nil, 0, 0, errors.New(i18n.T("同步排除规则已被修改，请先检查 .gitignore"))
				}
				var decoded strings.Builder
				for i := 1; i < len(text); i++ {
					if text[i] == '\\' {
						i++
						if i == len(text) {
							return nil, nil, 0, 0, errors.New(i18n.T("同步排除规则已被修改，请先检查 .gitignore"))
						}
					}
					decoded.WriteByte(text[i])
				}
				path := decoded.String()
				if err := exclusionPath(root, path); err != nil {
					return nil, nil, 0, 0, err
				}
				paths = append(paths, path)
			}
		}
		offset += len(line)
	}
	if start != -1 && end == -1 {
		return nil, nil, 0, 0, errors.New(i18n.T("同步排除规则已被修改，请先检查 .gitignore"))
	}
	return data, paths, start, end, nil
}

func exclusionFingerprint(repo Repository, data []byte) string {
	h := sha256.New()
	h.Write([]byte(repo.Fingerprint))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// Exclusions lists actual files and folders, including already ignored paths,
// so the selection can be changed without editing pattern syntax.
func (g *Git) Exclusions(ctx context.Context, root string) (ExclusionState, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var result ExclusionState
	repo, err := g.inspect(ctx, root)
	if err != nil {
		return result, err
	}
	data, rules, _, _, err := readExclusions(root)
	if err != nil {
		return result, err
	}
	result.Paths, result.Fingerprint = rules, exclusionFingerprint(repo, data)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == ".gitignore" || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			rel += "/"
		}
		result.Entries = append(result.Entries, ExclusionEntry{Path: rel, Directory: entry.IsDir()})
		return nil
	})
	return result, err
}

// SetExclusions preserves local content and unrelated staged changes. Index
// removals stay visible as pending changes until the next selected commit.
func (g *Git) SetExclusions(ctx context.Context, root string, paths []string, fingerprint string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, err := g.operationLock(ctx, root)
	if err != nil {
		return err
	}
	defer release()
	index, indexLock, releaseIndex, err := g.claimIndex(ctx, root)
	if err != nil {
		return err
	}
	defer releaseIndex()
	repo, err := g.inspect(ctx, root)
	if err != nil {
		return err
	}
	if repo.Pending != nil {
		return ErrConflict
	}
	data, _, start, end, err := readExclusions(root)
	if err != nil {
		return err
	}
	if exclusionFingerprint(repo, data) != fingerprint {
		return ErrChanged
	}
	paths = slices.Clone(paths)
	slices.Sort(paths)
	paths = slices.Compact(paths)
	for _, path := range paths {
		if err := exclusionPath(root, path); err != nil {
			return err
		}
	}
	for _, change := range repo.Changes {
		if change.Conflict {
			return ErrConflict
		}
		if change.Staged && !change.ExcludedRemoval && (exclusionMatches(change.Path, paths) || change.Path == ".gitignore") {
			return errors.New(i18n.T("排除的文件有已暂存改动，请先提交或取消暂存"))
		}
	}
	tracked, err := g.run(ctx, root, nil, nil, "ls-files", "-z")
	if err != nil {
		return err
	}
	var remove, restore []string
	for _, path := range strings.Split(string(tracked), "\x00") {
		if path != "" && exclusionMatches(path, paths) {
			remove = append(remove, path)
		}
	}
	for _, change := range repo.Changes {
		if change.ExcludedRemoval && !exclusionMatches(change.Path, paths) {
			restore = append(restore, change.Path)
		}
	}
	beforeIndex, err := os.ReadFile(index)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	temp, err := os.CreateTemp(g.StateDir, "exclude-index-")
	if err != nil {
		return err
	}
	idx := temp.Name()
	temp.Close()
	defer os.Remove(idx)
	if len(beforeIndex) > 0 {
		if err := os.WriteFile(idx, beforeIndex, 0600); err != nil {
			return err
		}
	} else {
		os.Remove(idx)
	}
	env := []string{"GIT_INDEX_FILE=" + idx}
	if len(remove) > 0 {
		if _, err := g.run(ctx, root, []byte(strings.Join(remove, "\x00")+"\x00"), env, "update-index", "--force-remove", "-z", "--stdin"); err != nil {
			return err
		}
	}
	if len(restore) > 0 {
		if _, err := g.run(ctx, root, nil, env, append([]string{"reset", "-q", "HEAD", "--"}, restore...)...); err != nil {
			return err
		}
	}
	var block strings.Builder
	if len(paths) > 0 {
		block.WriteString(ignoreBegin + "\n")
		for _, path := range paths {
			block.WriteByte('/')
			for _, r := range path {
				if strings.ContainsRune("\\[]*?!# ", r) {
					block.WriteByte('\\')
				}
				block.WriteRune(r)
			}
			block.WriteByte('\n')
		}
		block.WriteString(ignoreEnd + "\n")
	}
	var next []byte
	if start >= 0 {
		next = append(append(append([]byte{}, data[:start]...), block.String()...), data[end:]...)
	} else {
		next = append([]byte{}, data...)
		if block.Len() > 0 {
			if len(next) > 0 && next[len(next)-1] != '\n' {
				next = append(next, '\n')
			}
			next = append(next, block.String()...)
		}
	}
	current, err := g.inspect(ctx, root)
	if err != nil {
		return err
	}
	currentData, _, _, _, err := readExclusions(root)
	if err != nil {
		return err
	}
	if exclusionFingerprint(current, currentData) != fingerprint {
		return ErrChanged
	}
	ignorePath, err := validPath(root, ".gitignore")
	if err != nil {
		return err
	}
	if !bytes.Equal(data, next) {
		if err := fsutil.WriteFileAtomic(ignorePath, next, 0600); err != nil {
			return err
		}
	}
	if len(remove)+len(restore) > 0 {
		prepared, err := os.ReadFile(idx)
		if err == nil {
			err = os.WriteFile(indexLock, prepared, 0600)
		}
		if err == nil {
			err = os.Rename(indexLock, index)
		}
		if err != nil {
			if rollbackErr := fsutil.WriteFileAtomic(ignorePath, data, 0600); rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
	}
	return nil
}
