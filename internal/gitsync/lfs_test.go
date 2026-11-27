package gitsync

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLFSTrackedChangesRequireAvailableFilter(t *testing.T) {
	root, _, g, _ := repository(t)
	put(t, root, ".gitattributes", "*.bin filter=lfs diff=lfs merge=lfs -text\n")
	put(t, root, "asset.bin", "content")
	paths, e := g.LFSPaths(context.Background(), root, []string{"note.md", "asset.bin"})
	if e != nil || len(paths) != 1 || paths[0] != "asset.bin" {
		t.Fatal(paths, e)
	}
	if !isLFSPointer([]byte("version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 10\n")) {
		t.Fatal("LFS pointer not recognized")
	}
}

func TestLFSCommitPushAndPullMaterializesContent(t *testing.T) {
	root, remote, g, b := repository(t)
	ctx := context.Background()
	if _, e := g.run(ctx, root, nil, nil, "lfs", "version"); e != nil {
		t.Skip("isolated test requires installed Git LFS")
	}
	put(t, root, ".gitattributes", "*.bin filter=lfs diff=lfs merge=lfs -text\n")
	put(t, root, "asset.bin", "binary content one\x00")
	s, e := g.Inspect(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = g.Commit(ctx, root, b, []string{".gitattributes", "asset.bin"}, "asset", s.Fingerprint); e != nil {
		t.Fatal(e)
	}
	if raw := fixtureGit(t, root, "show", "HEAD:asset.bin"); !strings.HasPrefix(raw, "version https://git-lfs.github.com/spec/v1") {
		t.Fatal("large file committed without LFS")
	}
	if e = g.Push(ctx, root, b); e != nil {
		t.Fatal(e)
	}
	other := filepath.Join(filepath.Dir(root), "lfs-other")
	fixtureGit(t, filepath.Dir(root), "clone", "-b", "main", remote, other)
	put(t, other, "asset.bin", "binary content two\x00")
	g2 := &Git{Path: "git", StateDir: filepath.Join(filepath.Dir(root), "other-state"), AllowLocal: true, Env: append(slices.Clone(g.Env), "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")}
	s, e = g2.Inspect(ctx, other)
	if e != nil {
		t.Fatal(e)
	}
	if e = g2.Commit(ctx, other, b, []string{"asset.bin"}, "update asset", s.Fingerprint); e != nil {
		t.Fatal(e)
	}
	if e = g2.Push(ctx, other, b); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Pull(ctx, root, b); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(filepath.Join(root, "asset.bin"))
	if string(data) != "binary content two\x00" {
		t.Fatal("LFS content not materialized", string(data))
	}
}
