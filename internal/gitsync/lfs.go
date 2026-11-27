package gitsync

import (
	"bytes"
	"context"
	"errors"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func isLFSPointer(data []byte) bool {
	return bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/v1\n"))
}
func (g *Git) LFSPaths(ctx context.Context, root string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	for _, path := range paths {
		if _, e := validPath(root, path); e != nil {
			return nil, e
		}
	}
	b, e := g.run(ctx, root, []byte(strings.Join(paths, "\x00")+"\x00"), nil, "check-attr", "-z", "--stdin", "filter")
	if e != nil {
		return nil, e
	}
	fields := strings.Split(string(b), "\x00")
	var out []string
	for i := 0; i+2 < len(fields); i += 3 {
		if fields[i+2] == "lfs" {
			out = append(out, fields[i])
		}
	}
	return out, nil
}
func (g *Git) prepareLFS(ctx context.Context, root string) error {
	b, e := g.run(ctx, root, nil, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if e != nil {
		return e
	}
	paths := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
	if len(paths) == 1 && paths[0] == "" {
		return nil
	}
	lfs, e := g.LFSPaths(ctx, root, paths)
	if e != nil || len(lfs) == 0 {
		return e
	}
	g.LFS = true
	if _, e = g.run(ctx, root, nil, nil, "lfs", "version"); e == nil {
		return nil
	}
	p, e := ensureLFS(ctx, filepath.Join(filepath.Dir(g.StateDir), "resources"), g.Progress)
	if e != nil {
		return e
	}
	g.Env = append(g.Env, "PATH="+filepath.Dir(p)+string(os.PathListSeparator)+filepath.Dir(g.Path)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return nil
}
func ensureLFS(ctx context.Context, dir string, progress func(RuntimeProgress)) (string, error) {
	if p, e := exec.LookPath("git-lfs"); e == nil {
		return p, nil
	}
	parent := filepath.Join(dir, "git-lfs")
	dest := filepath.Join(parent, "3.8.1")
	if p := findLFS(dest); p != "" {
		return p, nil
	}
	if runtime.GOOS != "windows" {
		return "", errors.New(i18n.T("此仓库需要 Git LFS，请通过系统软件管理器安装"))
	}
	arch, digest, total := "amd64", "5bb771c12b3dacc535e912d0270a010c209d4da2e63819c5918e3547c2defed9", int64(5870805)
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
		digest = "7e70990e339efab3e793affb919355b548c9a41c5b6bb52d90267bcb84ba4784"
		total = 5270378
	} else if runtime.GOARCH != "amd64" {
		return "", errors.New(i18n.T("此平台没有可用的 Git LFS 资源"))
	}
	name := "git-lfs-windows-" + arch + "-v3.8.1.zip"
	stage, e := runtimeArchive(ctx, parent, name, "https://github.com/git-lfs/git-lfs/releases/download/v3.8.1/"+name, digest, total, progress)
	if e != nil {
		return "", e
	}
	p := findLFS(stage)
	if p == "" {
		return "", errors.New(i18n.T("压缩包中没有 Git LFS 程序"))
	}
	cmd := exec.CommandContext(ctx, p, "version")
	hideCommand(cmd)
	if e = cmd.Run(); e != nil {
		return "", e
	}
	if e = os.Rename(stage, dest); e != nil {
		return "", e
	}
	return findLFS(dest), nil
}
func findLFS(dir string) string {
	found := ""
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e == nil && !d.IsDir() && d.Name() == "git-lfs.exe" {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	return found
}
