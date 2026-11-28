package gitsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type translatedError string

func (e translatedError) Error() string { return i18n.T(string(e)) }

var ErrChanged error = translatedError("仓库或文件已改变，请刷新后重试")
var ErrConflict error = translatedError("存在合并冲突，请先处理冲突")

type Git struct {
	LFS            bool
	Progress       func(RuntimeProgress)
	Path, StateDir string
	Env            []string
	AllowLocal     bool
	Trash          func(string) error
	Guard          func([]string) error
	mu             sync.Mutex
}
type Change struct {
	Path, Original, Code string
	Staged, Conflict     bool
	ExcludedRemoval      bool
}
type Repository struct {
	Root, Branch, Head, Fingerprint string
	Remotes                         map[string]string
	Changes                         []Change
	Ahead, Behind                   int
	Pending                         *Merge
}

// Commands are direct process arguments, never shell text. Hooks and external
// diff programs are disabled for imported note repositories.
func (g *Git) run(ctx context.Context, root string, stdin []byte, extra []string, args ...string) ([]byte, error) {
	p := g.Path
	if p == "" {
		p = "git"
	}
	base := []string{"-C", root, "-c", "core.quotepath=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false"}
	if g.LFS {
		base = append(base, "-c", "filter.lfs.clean=git-lfs clean -- %f", "-c", "filter.lfs.smudge=git-lfs smudge -- %f", "-c", "filter.lfs.process=git-lfs filter-process", "-c", "filter.lfs.required=true")
	}
	cmd := exec.CommandContext(ctx, p, append(base, args...)...)
	hideCommand(cmd)
	env := os.Environ()
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		for i := len(env) - 1; i >= 0; i-- {
			if strings.HasPrefix(env[i], key+"=") {
				env = append(env[:i], env[i+1:]...)
			}
		}
	}
	cmd.Env = append(env, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_MERGE_AUTOEDIT=no", "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS=1")
	if filepath.IsAbs(p) {
		cmd.Env = append(cmd.Env, "PATH="+filepath.Dir(p)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	cmd.Env = append(cmd.Env, g.Env...)
	cmd.Env = append(cmd.Env, extra...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs
	e := cmd.Run()
	if e != nil {
		if ctx.Err() != nil {
			return out.Bytes(), ctx.Err()
		}
		msg := strings.TrimSpace(errs.String())
		if len(msg) > 3000 {
			msg = msg[:3000]
		}
		return out.Bytes(), fmt.Errorf("Git %s: %s", args[0], msg)
	}
	return out.Bytes(), nil
}
func (g *Git) text(ctx context.Context, root string, args ...string) (string, error) {
	b, e := g.run(ctx, root, nil, nil, args...)
	return strings.TrimSpace(string(b)), e
}
func validPath(root, rel string) (string, error) {
	if rel == "" || strings.ContainsAny(rel, "\x00\r\n") || filepath.IsAbs(rel) || strings.Contains(rel, "\\") || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", errors.New(i18n.T("仓库文件路径无效"))
	}
	for _, p := range strings.Split(rel, "/") {
		if strings.EqualFold(p, ".git") || p == ".." {
			return "", errors.New(i18n.T("仓库文件路径无效"))
		}
	}
	dest, resolveErr := vault.Resolve(root, rel)
	if resolveErr != nil {
		return "", resolveErr
	}
	cur := dest
	for cur != root {
		info, e := os.Lstat(cur)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New(i18n.T("同步不支持符号链接"))
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return "", e
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", errors.New(i18n.T("仓库文件超出空间"))
		}
		cur = parent
	}
	return dest, nil
}
func (g *Git) inspect(ctx context.Context, root string) (Repository, error) {
	s := Repository{Remotes: map[string]string{}}
	top, e := g.text(ctx, root, "rev-parse", "--show-toplevel")
	if e != nil {
		return s, errors.New(i18n.T("此空间不是 Git 仓库"))
	}
	abs, e := filepath.Abs(root)
	if e != nil {
		return s, e
	}
	if !strings.EqualFold(filepath.Clean(top), filepath.Clean(abs)) {
		return s, errors.New(i18n.T("请选择 Git 仓库根目录作为空间"))
	}
	s.Root = abs
	s.Branch, e = g.text(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if e != nil {
		return s, errors.New(i18n.T("仓库未处于分支，请先选择分支"))
	}
	s.Head, _ = g.text(ctx, root, "rev-parse", "--verify", "HEAD")
	names, e := g.text(ctx, root, "remote")
	if e != nil {
		return s, e
	}
	for _, name := range strings.Fields(names) {
		u, e := g.text(ctx, root, "remote", "get-url", name)
		if e != nil {
			return s, e
		}
		s.Remotes[name] = u
	}
	raw, e := g.run(ctx, root, nil, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if e != nil {
		return s, e
	}
	_, exclusions, _, _, e := readExclusions(root)
	if e != nil {
		return s, e
	}
	h := sha256.New()
	h.Write([]byte(s.Head))
	h.Write(raw)
	parts := bytes.Split(raw, []byte{0})
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if len(p) < 4 {
			continue
		}
		c := Change{Code: string(p[:2]), Path: string(p[3:])}
		c.Staged = p[0] != ' ' && p[0] != '?'
		c.Conflict = strings.Contains(c.Code, "U") || c.Code == "AA" || c.Code == "DD"
		if p[0] == 'R' || p[0] == 'C' || p[1] == 'R' || p[1] == 'C' {
			i++
			if i < len(parts) {
				c.Original = string(parts[i])
			}
		}
		path, e := validPath(abs, c.Path)
		if e != nil {
			return s, e
		}
		if c.Code == "D " && exclusionMatches(c.Path, exclusions) {
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				c.ExcludedRemoval = true
			}
		}
		f, e := os.Open(path)
		if e == nil {
			_, e = io.Copy(h, f)
			f.Close()
			if e != nil {
				return s, e
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return s, e
		}
		s.Changes = append(s.Changes, c)
	}
	index, e := g.run(ctx, root, nil, nil, "ls-files", "--stage", "-z")
	if e != nil {
		return s, e
	}
	h.Write(index)
	s.Fingerprint = hex.EncodeToString(h.Sum(nil))
	if counts, e := g.text(ctx, root, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); e == nil {
		fmt.Sscanf(counts, "%d %d", &s.Ahead, &s.Behind)
	}
	s.Pending, e = g.loadMerge(root)
	if e != nil {
		return s, e
	}
	return s, nil
}
func (g *Git) Inspect(ctx context.Context, root string) (Repository, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inspect(ctx, root)
}
func (g *Git) check(ctx context.Context, root string, b Binding) (Repository, error) {
	s, e := g.inspect(ctx, root)
	if e != nil {
		return s, e
	}
	if s.Branch != b.Branch || s.Remotes[b.Remote] != b.URL || b.Remote == "" || b.Branch == "" {
		return s, ErrChanged
	}
	if b.Root != "" && filepath.Clean(b.Root) != filepath.Clean(root) {
		return s, ErrChanged
	}
	if !g.AllowLocal {
		if _, e = GitHubRepository(b.URL); e != nil {
			return s, e
		}
	}
	return s, nil
}
func GitHubRepository(remote string) (string, error) {
	p := ""
	if strings.HasPrefix(remote, "git@github.com:") {
		p = strings.TrimPrefix(remote, "git@github.com:")
	} else {
		u, e := url.Parse(remote)
		if e != nil || !strings.EqualFold(u.Hostname(), "github.com") || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
			return "", errors.New(i18n.T("请选择 GitHub 仓库远端"))
		}
		if u.Scheme == "https" && u.User == nil || u.Scheme == "ssh" && u.User != nil && u.User.Username() == "git" {
			p = strings.TrimPrefix(u.Path, "/")
		} else {
			return "", errors.New(i18n.T("GitHub 远端地址无效"))
		}
	}
	p = strings.TrimSuffix(p, ".git")
	parts := strings.Split(p, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(p, "\r\n \x00") {
		return "", errors.New(i18n.T("GitHub 仓库地址无效"))
	}
	return p, nil
}
func (g *Git) Commit(ctx context.Context, root string, b Binding, paths []string, message, fingerprint string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, e := g.operationLock(ctx, root)
	if e != nil {
		return e
	}
	defer release()
	index, indexLock, releaseIndex, e := g.claimIndex(ctx, root)
	if e != nil {
		return e
	}
	defer releaseIndex()
	s, e := g.check(ctx, root, b)
	if e != nil {
		return e
	}
	if s.Fingerprint != fingerprint {
		return ErrChanged
	}
	if s.Pending != nil {
		return ErrConflict
	}
	if strings.TrimSpace(message) == "" || len(message) > 16000 {
		return errors.New(i18n.T("请填写有效的提交说明"))
	}
	if len(paths) == 0 {
		return errors.New(i18n.T("请选择提交文件"))
	}
	selected := map[string]bool{}
	for _, p := range paths {
		if _, e := validPath(root, p); e != nil {
			return e
		}
		selected[p] = true
	}
	var list, add, remove []string
	for _, c := range s.Changes {
		if c.Conflict {
			return ErrConflict
		}
		if selected[c.Path] {
			list = append(list, c.Path)
			if c.ExcludedRemoval {
				remove = append(remove, c.Path)
			} else {
				add = append(add, c.Path)
			}
			if c.Original != "" {
				list = append(list, c.Original)
				add = append(add, c.Original)
			}
		}
	}
	if len(list) == 0 {
		return errors.New(i18n.T("没有可提交的改动"))
	}
	if e = g.prepareLFS(ctx, root); e != nil {
		return e
	}
	sort.Strings(list)
	if e = os.MkdirAll(g.StateDir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(g.StateDir, "index-")
	if e != nil {
		return e
	}
	idx := f.Name()
	f.Close()
	os.Remove(idx)
	defer os.Remove(idx)
	env := []string{"GIT_INDEX_FILE=" + idx}
	args := []string{"read-tree", "HEAD"}
	if s.Head == "" {
		args = []string{"read-tree", "--empty"}
	}
	if _, e = g.run(ctx, root, nil, env, args...); e != nil {
		return e
	}
	// Exclusions remove tracked entries while deliberately retaining their
	// local files; git add must not put those files back into the commit.
	if len(remove) > 0 {
		if _, e = g.run(ctx, root, []byte(strings.Join(remove, "\x00")+"\x00"), env, "update-index", "--force-remove", "-z", "--stdin"); e != nil {
			return e
		}
	}
	if len(add) > 0 {
		if _, e = g.run(ctx, root, nil, env, append([]string{"add", "-A", "--"}, add...)...); e != nil {
			return e
		}
	}
	tree, e := g.run(ctx, root, nil, env, "write-tree")
	if e != nil {
		return e
	}
	args = []string{"commit-tree", strings.TrimSpace(string(tree))}
	if s.Head != "" {
		args = append(args, "-p", s.Head)
	}
	commit, e := g.run(ctx, root, []byte(message+"\n"), env, args...)
	if e != nil {
		return e
	}
	current, e := g.inspect(ctx, root)
	if e != nil {
		return e
	}
	if current.Fingerprint != s.Fingerprint {
		return ErrChanged
	}
	old := s.Head
	if old == "" {
		old = strings.Repeat("0", 40)
	}
	// Start with the user's index and replace only paths included in this commit.
	realIndex, readErr := os.ReadFile(index)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if readErr == nil {
		if e = os.WriteFile(idx, realIndex, 0600); e != nil {
			return e
		}
	} else {
		if _, e = g.run(ctx, root, nil, env, "read-tree", "--empty"); e != nil {
			return e
		}
	}
	if _, e = g.run(ctx, root, nil, env, append([]string{"reset", "-q", strings.TrimSpace(string(commit)), "--"}, list...)...); e != nil {
		return e
	}
	prepared, e := os.ReadFile(idx)
	if e != nil {
		return e
	}
	if e = os.WriteFile(indexLock, prepared, 0600); e != nil {
		return e
	}
	if _, e = g.run(ctx, root, nil, nil, "update-ref", "-m", "FreeFolio commit", "HEAD", strings.TrimSpace(string(commit)), old); e != nil {
		return e
	}
	if e = os.Rename(indexLock, index); e != nil {
		g.run(context.WithoutCancel(ctx), root, nil, nil, "update-ref", "HEAD", old, strings.TrimSpace(string(commit)))
		return e
	}
	return nil
}
func (g *Git) Push(ctx context.Context, root string, b Binding) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, e := g.operationLock(ctx, root)
	if e != nil {
		return e
	}
	defer release()
	s, e := g.check(ctx, root, b)
	if e != nil {
		return e
	}
	if s.Pending != nil {
		return ErrConflict
	}
	pushURL, e := g.text(ctx, root, "remote", "get-url", "--push", "--all", b.Remote)
	if e != nil {
		return e
	}
	if pushURL != b.URL {
		return ErrChanged
	}
	if e = g.prepareLFS(ctx, root); e != nil {
		return e
	}
	if g.LFS {
		if _, e = g.run(ctx, root, nil, nil, "lfs", "push", b.Remote, b.Branch); e != nil {
			return e
		}
	}
	_, e = g.run(ctx, root, nil, nil, "-c", "remote."+b.Remote+".mirror=false", "push", "--porcelain", b.URL, "HEAD:refs/heads/"+b.Branch)
	return e
}
