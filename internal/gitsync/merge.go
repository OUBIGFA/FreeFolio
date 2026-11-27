package gitsync

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
)

type Conflict struct {
	Path     string
	Resolved bool
}
type Merge struct {
	Applying                                          []AppliedFile
	Root, Worktree, Head, Fingerprint, Target, Backup string
	Binding                                           Binding
	Conflicts                                         []Conflict
	Applied                                           bool
}

// Applying is persisted before each replacement, allowing safe recovery after
// cancellation or process exit without discarding a concurrent external edit.
type AppliedFile struct {
	Path, Before, After string
	Existed, Deleted    bool
}
type Versions struct {
	Base, Local, Remote                           string
	BaseExists, LocalExists, RemoteExists, Binary bool
}

func (g *Git) mergeDir(root string) string {
	abs, _ := filepath.Abs(root)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return filepath.Join(g.StateDir, fmt.Sprintf("%x", sha256.Sum256([]byte(abs))))
}
func (g *Git) loadMerge(root string) (*Merge, error) {
	b, e := os.ReadFile(filepath.Join(g.mergeDir(root), "merge.json"))
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var p Merge
	e = json.Unmarshal(b, &p)
	if e != nil {
		return nil, e
	}
	if p.Root != root || p.Worktree != filepath.Join(g.mergeDir(root), "worktree") {
		return nil, errors.New(i18n.T("合并恢复记录无效"))
	}
	return &p, nil
}
func (g *Git) saveMerge(p *Merge) error {
	dir := g.mergeDir(p.Root)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, "merge.json"), b, 0600)
}
func (g *Git) Pull(ctx context.Context, root string, b Binding) (*Merge, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, e := g.operationLock(ctx, root)
	if e != nil {
		return nil, e
	}
	defer release()
	s, e := g.check(ctx, root, b)
	if e != nil {
		return nil, e
	}
	if s.Pending != nil {
		return s.Pending, nil
	}
	if e = g.prepareLFS(ctx, root); e != nil {
		return nil, e
	}
	for _, c := range s.Changes {
		if c.Staged || c.Conflict {
			return nil, errors.New(i18n.T("拉取前请先处理已暂存内容和冲突"))
		}
	}
	if s.Head == "" {
		return nil, errors.New(i18n.T("请先为本地仓库创建提交"))
	}
	if _, e = g.run(ctx, root, nil, nil, "fetch", "--no-tags", b.URL, "refs/heads/"+b.Branch); e != nil {
		return nil, e
	}
	remote, e := g.text(ctx, root, "rev-parse", "FETCH_HEAD")
	if e != nil {
		return nil, e
	}
	if _, e = g.run(ctx, root, nil, nil, "merge-base", "--is-ancestor", remote, "HEAD"); e == nil {
		return nil, nil
	}
	p := &Merge{Root: root, Head: s.Head, Fingerprint: s.Fingerprint, Binding: b, Worktree: filepath.Join(g.mergeDir(root), "worktree")}
	if _, e = g.run(ctx, root, nil, nil, "merge-base", "--is-ancestor", "HEAD", remote); e == nil {
		p.Target = remote
	}
	if e = os.MkdirAll(g.mergeDir(root), 0700); e != nil {
		return nil, e
	}
	if _, e = g.run(ctx, root, nil, nil, "worktree", "add", "--detach", p.Worktree, s.Head); e != nil {
		return nil, e
	}
	if e = g.saveMerge(p); e != nil {
		return nil, e
	}
	if p.Target != "" {
		if _, e = g.run(ctx, p.Worktree, nil, nil, "reset", "--hard", p.Target); e != nil {
			return p, e
		}
		if e = g.prepareLFS(ctx, p.Worktree); e != nil {
			return p, e
		}
		if g.LFS {
			if _, e = g.run(ctx, p.Worktree, nil, nil, "lfs", "pull", b.Remote); e != nil {
				return p, e
			}
		}
		if e = g.finish(ctx, p); e != nil {
			return p, e
		}
		return nil, nil
	}
	if _, e = g.run(ctx, p.Worktree, nil, nil, "merge", "--no-commit", "--no-ff", remote); e != nil {
		raw, readErr := g.run(ctx, p.Worktree, nil, nil, "diff", "--name-only", "--diff-filter=U", "-z")
		if readErr != nil {
			return nil, readErr
		}
		for _, path := range strings.Split(string(raw), "\x00") {
			if path != "" {
				p.Conflicts = append(p.Conflicts, Conflict{Path: path})
			}
		}
		if len(p.Conflicts) == 0 {
			g.cleanup(context.WithoutCancel(ctx), p)
			return nil, e
		}
	}
	if e = g.saveMerge(p); e != nil {
		return nil, e
	}
	if len(p.Conflicts) > 0 {
		return p, nil
	}
	if e = g.prepareLFS(ctx, p.Worktree); e != nil {
		return p, e
	}
	if g.LFS {
		if _, e = g.run(ctx, p.Worktree, nil, nil, "lfs", "pull", b.Remote); e != nil {
			return p, e
		}
	}
	if e = g.finish(ctx, p); e != nil {
		return p, e
	}
	return nil, nil
}
func (g *Git) ConflictVersions(ctx context.Context, root, path string) (Versions, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var v Versions
	p, e := g.loadMerge(root)
	if e != nil || p == nil {
		return v, errors.New(i18n.T("没有待处理的合并"))
	}
	if _, e = validPath(root, path); e != nil {
		return v, e
	}
	found := false
	for _, c := range p.Conflicts {
		if c.Path == path && !c.Resolved {
			found = true
		}
	}
	if !found {
		return v, errors.New(i18n.T("冲突已改变，请刷新"))
	}
	read := func(stage string) (string, bool) {
		b, e := g.run(ctx, p.Worktree, nil, nil, "show", stage+":"+path)
		if e != nil {
			return "", false
		}
		if len(b) > 1<<20 || !utf8.Valid(b) || strings.ContainsRune(string(b), '\x00') || isLFSPointer(b) {
			v.Binary = true
			return "", true
		}
		return string(b), true
	}
	v.Base, v.BaseExists = read(":1")
	v.Local, v.LocalExists = read(":2")
	v.Remote, v.RemoteExists = read(":3")
	return v, nil
}
func (g *Git) Resolve(ctx context.Context, root, path, choice, text string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, e := g.operationLock(ctx, root)
	if e != nil {
		return e
	}
	defer release()
	p, e := g.loadMerge(root)
	if e != nil {
		return e
	}
	if p == nil {
		return errors.New(i18n.T("没有待处理的合并"))
	}
	idx := -1
	for i, c := range p.Conflicts {
		if c.Path == path && !c.Resolved {
			idx = i
		}
	}
	if idx < 0 {
		return errors.New(i18n.T("冲突已改变，请刷新"))
	}
	dest, e := validPath(p.Worktree, path)
	if e != nil {
		return e
	}
	var data []byte
	missing := false
	switch choice {
	case "local", "remote":
		stage := ":2:"
		if choice == "remote" {
			stage = ":3:"
		}
		data, e = g.run(ctx, p.Worktree, nil, nil, "show", stage+path)
		if e != nil {
			raw, err := g.run(ctx, p.Worktree, nil, nil, "ls-files", "--unmerged", "--", path)
			if err != nil {
				return err
			}
			want := " 2\t"
			if choice == "remote" {
				want = " 3\t"
			}
			if strings.Contains(string(raw), want) {
				return e
			}
			missing = true
		}
	case "edit":
		if len(text) > 1<<20 || strings.ContainsRune(text, '\x00') {
			return errors.New(i18n.T("编辑后的冲突内容无效"))
		}
		data = []byte(text)
	default:
		return errors.New(i18n.T("请选择冲突版本"))
	}
	if missing {
		if _, e = g.run(ctx, p.Worktree, nil, nil, "rm", "-f", "--", path); e != nil {
			return e
		}
	} else {
		if choice == "edit" {
			if e = validateMergedFile(path, data); e != nil {
				return e
			}
		}
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		if e = fsutil.WriteFileAtomic(dest, data, 0600); e != nil {
			return e
		}
		if _, e = g.run(ctx, p.Worktree, nil, nil, "add", "--", path); e != nil {
			return e
		}
	}
	p.Conflicts[idx].Resolved = true
	return g.saveMerge(p)
}
func (g *Git) FinishMerge(ctx context.Context, root string, b Binding) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, e := g.operationLock(ctx, root)
	if e != nil {
		return e
	}
	defer release()
	if _, e := g.check(ctx, root, b); e != nil {
		return e
	}
	p, e := g.loadMerge(root)
	if e != nil {
		return e
	}
	if p == nil {
		return errors.New(i18n.T("没有待处理的合并"))
	}
	return g.finish(ctx, p)
}
func (g *Git) finish(ctx context.Context, p *Merge) (resultErr error) {
	if e := g.prepareLFS(ctx, p.Worktree); e != nil {
		return e
	}
	if len(p.Applying) > 0 {
		if e := g.recoverApply(ctx, p); e != nil {
			return e
		}
	}
	if p.Applied {
		return g.cleanup(ctx, p)
	}
	for _, c := range p.Conflicts {
		if !c.Resolved {
			return ErrConflict
		}
	}
	if p.Target == "" {
		if _, e := g.run(ctx, p.Worktree, nil, nil, "commit", "--no-verify", "-m", "Merge remote notes"); e != nil {
			return e
		}
		target, e := g.text(ctx, p.Worktree, "rev-parse", "HEAD")
		if e != nil {
			return e
		}
		p.Target = target
		if e = g.saveMerge(p); e != nil {
			return e
		}
	}
	if g.LFS {
		if _, e := g.run(ctx, p.Worktree, nil, nil, "lfs", "pull", p.Binding.Remote); e != nil {
			return e
		}
	}
	s, e := g.check(ctx, p.Root, p.Binding)
	if e != nil {
		return e
	}
	if s.Head != p.Head || s.Fingerprint != p.Fingerprint {
		return ErrChanged
	}
	index, indexLock, releaseIndex, e := g.claimIndex(ctx, p.Root)
	if e != nil {
		return e
	}
	defer releaseIndex()
	raw, e := g.run(ctx, p.Root, nil, nil, "diff", "--name-only", "--no-renames", "-z", p.Head, p.Target, "--")
	if e != nil {
		return e
	}
	var paths []string
	for _, path := range strings.Split(string(raw), "\x00") {
		if path == "" {
			continue
		}
		if _, e = validPath(p.Root, path); e != nil {
			return e
		}
		if _, e = validPath(p.Worktree, path); e != nil {
			return e
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		src, _ := validPath(p.Worktree, path)
		data, e := os.ReadFile(src)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		if e = validateMergedFile(path, data); e != nil {
			return e
		}
	}
	for _, change := range s.Changes {
		for _, path := range paths {
			if change.Path == path || strings.HasPrefix(path, change.Path+"/") || strings.HasPrefix(change.Path, path+"/") {
				return errors.New(i18n.T("远端改动与未提交的本地文件重叠，请先提交或移开文件"))
			}
		}
	}
	if g.Guard != nil {
		if e = g.Guard(paths); e != nil {
			return e
		}
	}
	p.Backup = filepath.Join(g.mergeDir(p.Root), "backups", time.Now().UTC().Format("20060102T150405.000000000"))
	if e = os.MkdirAll(p.Backup, 0700); e != nil {
		return e
	}
	for _, path := range paths {
		dest, _ := validPath(p.Root, path)
		data, e := os.ReadFile(dest)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		backup := filepath.Join(p.Backup, filepath.FromSlash(path))
		if e = os.MkdirAll(filepath.Dir(backup), 0700); e != nil {
			return e
		}
		if e = fsutil.WriteFileAtomic(backup, data, 0600); e != nil {
			return e
		}
	}
	if e = g.saveMerge(p); e != nil {
		return e
	}
	defer func() {
		if resultErr != nil && len(p.Applying) > 0 {
			recoverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if e := g.recoverApply(recoverCtx, p); e != nil {
				resultErr = fmt.Errorf(i18n.T("%w；恢复未完成：%v"), resultErr, e)
			}
		}
	}()
	for _, path := range paths {
		src, _ := validPath(p.Worktree, path)
		dest, _ := validPath(p.Root, path)
		data, e := os.ReadFile(src)
		deleted := errors.Is(e, os.ErrNotExist)
		if e != nil && !deleted {
			return e
		}
		before, readErr := os.ReadFile(dest)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		item := AppliedFile{Path: path, Existed: readErr == nil, Deleted: deleted, Before: fmt.Sprintf("%x", sha256.Sum256(before)), After: fmt.Sprintf("%x", sha256.Sum256(data))}
		// Backups describe the preflight snapshot. Recheck before each write.
		backup, backupErr := os.ReadFile(filepath.Join(p.Backup, filepath.FromSlash(path)))
		if (backupErr == nil) != item.Existed || item.Existed && sha256.Sum256(backup) != sha256.Sum256(before) {
			return ErrChanged
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		p.Applying = append(p.Applying, item)
		if e = g.saveMerge(p); e != nil {
			return e
		}
		if deleted {
			if _, e = os.Stat(dest); errors.Is(e, os.ErrNotExist) {
				continue
			}
			if g.Trash == nil {
				return errors.New(i18n.T("无法使用系统回收站，原文件已保留"))
			}
			if e = g.Trash(dest); e != nil {
				return e
			}
			continue
		}
		if e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		if e = fsutil.WriteFileAtomic(dest, data, 0600); e != nil {
			return e
		}
	}
	// Prepare an index outside the space while the native index lock keeps
	// other Git writers from changing staging during this transaction.
	stagedIndex := filepath.Join(g.mergeDir(p.Root), "next-index")
	os.Remove(stagedIndex)
	defer os.Remove(stagedIndex)
	if _, e = g.run(ctx, p.Root, nil, []string{"GIT_INDEX_FILE=" + stagedIndex}, "read-tree", p.Target); e != nil {
		return e
	}
	indexBytes, e := os.ReadFile(stagedIndex)
	if e != nil {
		return e
	}
	if e = os.WriteFile(indexLock, indexBytes, 0600); e != nil {
		return e
	}
	if _, e = g.run(ctx, p.Root, nil, nil, "update-ref", "-m", "FreeFolio merge", "HEAD", p.Target, p.Head); e != nil {
		return e
	}
	if e = os.Rename(indexLock, index); e != nil {
		g.run(context.WithoutCancel(ctx), p.Root, nil, nil, "update-ref", "HEAD", p.Head, p.Target)
		return e
	}
	p.Applied = true
	p.Applying = nil
	if e = g.saveMerge(p); e != nil {
		return e
	}
	return g.cleanup(ctx, p)
}
func (g *Git) recoverApply(ctx context.Context, p *Merge) error {
	head, e := g.text(ctx, p.Root, "rev-parse", "HEAD")
	if e != nil {
		return e
	}
	if head == p.Target {
		p.Applied = true
		p.Applying = nil
		return g.saveMerge(p)
	}
	if head != p.Head {
		return ErrChanged
	}
	for i := len(p.Applying) - 1; i >= 0; i-- {
		item := p.Applying[i]
		dest, e := validPath(p.Root, item.Path)
		if e != nil {
			return e
		}
		data, readErr := os.ReadFile(dest)
		exists := readErr == nil
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		if exists == item.Existed && (!exists || hash == item.Before) {
			continue
		}
		if exists == item.Deleted || exists && hash != item.After {
			return ErrChanged
		}
		if item.Existed {
			backup, e := os.ReadFile(filepath.Join(p.Backup, filepath.FromSlash(item.Path)))
			if e != nil {
				return e
			}
			if e = fsutil.WriteFileAtomic(dest, backup, 0600); e != nil {
				return e
			}
		} else if exists {
			if g.Trash == nil {
				return errors.New(i18n.T("恢复新增文件需要系统回收站"))
			}
			if e = g.Trash(dest); e != nil {
				return e
			}
		}
	}
	p.Applying = nil
	return g.saveMerge(p)
}
func (g *Git) cleanup(ctx context.Context, p *Merge) error {
	if _, e := g.run(ctx, p.Root, nil, nil, "worktree", "remove", "--force", p.Worktree); e != nil {
		return e
	}
	return os.Remove(filepath.Join(g.mergeDir(p.Root), "merge.json"))
}
func (g *Git) CancelMerge(ctx context.Context, root string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, e := g.operationLock(ctx, root)
	if e != nil {
		return e
	}
	defer release()
	p, e := g.loadMerge(root)
	if e != nil || p == nil {
		return e
	}
	if len(p.Applying) > 0 {
		if e = g.recoverApply(ctx, p); e != nil {
			return e
		}
	}
	return g.cleanup(ctx, p)
}
