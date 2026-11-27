package gitsync

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func put(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func repository(t *testing.T) (string, string, *Git, Binding) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "space")
	remote := filepath.Join(base, "remote.git")
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, root, "init", "-b", "main")
	fixtureGit(t, root, "config", "user.name", "Fixture")
	fixtureGit(t, root, "config", "user.email", "fixture@example.test")
	put(t, root, "note.md", "first\n")
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "commit", "-m", "initial")
	fixtureGit(t, base, "init", "--bare", remote)
	fixtureGit(t, root, "remote", "add", "origin", remote)
	fixtureGit(t, root, "push", "-u", "origin", "main")
	g := &Git{Path: "git", StateDir: filepath.Join(base, "state"), AllowLocal: true}
	g.Env = []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}
	return root, remote, g, Binding{Remote: "origin", URL: remote, Branch: "main"}
}

func TestCommitSelectionPreservesOtherStagedChanges(t *testing.T) {
	root, _, g, b := repository(t)
	put(t, root, "note.md", "updated\n")
	put(t, root, "other.md", "staged\n")
	fixtureGit(t, root, "add", "other.md")
	ctx := context.Background()
	s, e := g.Inspect(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	if e = g.Commit(ctx, root, b, []string{"note.md"}, "Update note", s.Fingerprint); e != nil {
		t.Fatal(e)
	}
	if got := fixtureGit(t, root, "show", "--format=", "--name-only", "HEAD"); got != "note.md" {
		t.Fatal(got)
	}
	if got := fixtureGit(t, root, "diff", "--cached", "--name-only"); got != "other.md" {
		t.Fatal("staging lost", got)
	}
}

func TestCommitRejectsChangedFilesAndRemoteBinding(t *testing.T) {
	root, _, g, b := repository(t)
	put(t, root, "note.md", "one\n")
	ctx := context.Background()
	s, e := g.Inspect(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	put(t, root, "note.md", "two\n")
	if g.Commit(ctx, root, b, []string{"note.md"}, "stale", s.Fingerprint) == nil {
		t.Fatal("stale AI summary committed")
	}
	b.Branch = "other"
	if g.Push(ctx, root, b) == nil {
		t.Fatal("wrong branch pushed")
	}
}

func TestMergeConflictKeepsOriginalAndCanResolve(t *testing.T) {
	root, remote, g, b := repository(t)
	base := filepath.Dir(root)
	other := filepath.Join(base, "other")
	fixtureGit(t, base, "clone", "-b", "main", remote, other)
	put(t, other, "note.md", "remote\n")
	fixtureGit(t, other, "add", ".")
	fixtureGit(t, other, "commit", "-m", "remote")
	fixtureGit(t, other, "push")
	put(t, root, "note.md", "local\n")
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "commit", "-m", "local")
	p, e := g.Pull(context.Background(), root, b)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Conflicts) != 1 {
		t.Fatal(p)
	}
	data, _ := os.ReadFile(filepath.Join(root, "note.md"))
	if string(data) != "local\n" {
		t.Fatal("original changed", string(data))
	}
	if e = g.Resolve(context.Background(), root, "note.md", "remote", ""); e != nil {
		t.Fatal(e)
	}
	if e = g.FinishMerge(context.Background(), root, b); e != nil {
		t.Fatal(e)
	}
	data, _ = os.ReadFile(filepath.Join(root, "note.md"))
	if string(data) != "remote\n" {
		t.Fatal(string(data))
	}
	if e = g.Push(context.Background(), root, b); e != nil {
		t.Fatal(e)
	}
}

func TestPullFastForwardPreservesUnrelatedFilesAndDoesNotCreateCommit(t *testing.T) {
	root, remote, g, b := repository(t)
	base := filepath.Dir(root)
	other := filepath.Join(base, "other")
	fixtureGit(t, base, "clone", "-b", "main", remote, other)
	put(t, other, "note.md", "remote update\n")
	fixtureGit(t, other, "add", ".")
	fixtureGit(t, other, "commit", "-m", "remote")
	want := fixtureGit(t, other, "rev-parse", "HEAD")
	fixtureGit(t, other, "push")
	put(t, root, ".freefolio/local-state", "private local state")
	p, e := g.Pull(context.Background(), root, b)
	if e != nil || p != nil {
		t.Fatal(p, e)
	}
	if got := fixtureGit(t, root, "rev-parse", "HEAD"); got != want {
		t.Fatal("fast-forward created a merge commit", got, want)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".freefolio/local-state"))
	if string(data) != "private local state" {
		t.Fatal("local metadata changed")
	}
}

func TestPullDeletionFailureRestoresEarlierWrites(t *testing.T) {
	root, remote, g, b := repository(t)
	put(t, root, "z.md", "keep\n")
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "commit", "-m", "second")
	fixtureGit(t, root, "push")
	base := filepath.Dir(root)
	other := filepath.Join(base, "other")
	fixtureGit(t, base, "clone", "-b", "main", remote, other)
	put(t, other, "note.md", "replace\n")
	fixtureGit(t, other, "rm", "z.md")
	fixtureGit(t, other, "add", ".")
	fixtureGit(t, other, "commit", "-m", "remote")
	fixtureGit(t, other, "push")
	head := fixtureGit(t, root, "rev-parse", "HEAD")
	g.Trash = func(string) error { return errors.New("trash unavailable") }
	if _, e := g.Pull(context.Background(), root, b); e == nil {
		t.Fatal("trash failure ignored")
	}
	data, _ := os.ReadFile(filepath.Join(root, "note.md"))
	if string(data) != "first\n" {
		t.Fatal("partial write not restored", string(data))
	}
	if fixtureGit(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("HEAD moved after failure")
	}
	if e := g.CancelMerge(context.Background(), root); e != nil {
		t.Fatal(e)
	}
}

func TestPullRejectsOverlappingUntrackedFile(t *testing.T) {
	root, remote, g, b := repository(t)
	base := filepath.Dir(root)
	other := filepath.Join(base, "other")
	fixtureGit(t, base, "clone", "-b", "main", remote, other)
	put(t, other, "new.md", "remote\n")
	fixtureGit(t, other, "add", ".")
	fixtureGit(t, other, "commit", "-m", "remote")
	fixtureGit(t, other, "push")
	put(t, root, "new.md", "local\n")
	if _, e := g.Pull(context.Background(), root, b); e == nil {
		t.Fatal("untracked file overwritten")
	}
	data, _ := os.ReadFile(filepath.Join(root, "new.md"))
	if string(data) != "local\n" {
		t.Fatal(string(data))
	}
}

func TestPushRefusesDifferentPushDestination(t *testing.T) {
	root, _, g, b := repository(t)
	fixtureGit(t, root, "remote", "set-url", "--push", "origin", filepath.Join(filepath.Dir(root), "other.git"))
	if e := g.Push(context.Background(), root, b); !errors.Is(e, ErrChanged) {
		t.Fatal("push destination was not validated", e)
	}
}

func TestMutationRefusesForeignIndexLock(t *testing.T) {
	root, _, g, b := repository(t)
	put(t, root, "note.md", "changed\n")
	s, e := g.Inspect(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	head := s.Head
	put(t, root, ".git/index.lock", "foreign lock")
	if e = g.Commit(context.Background(), root, b, []string{"note.md"}, "update", s.Fingerprint); e == nil {
		t.Fatal("index lock ignored")
	}
	if fixtureGit(t, root, "rev-parse", "HEAD") != head {
		t.Fatal("HEAD changed despite index lock")
	}
	data, _ := os.ReadFile(filepath.Join(root, ".git/index.lock"))
	if string(data) != "foreign lock" {
		t.Fatal("foreign lock replaced")
	}
}

func TestCommitTreatsFilenamesAsLiteralPathspecs(t *testing.T) {
	root, _, g, b := repository(t)
	put(t, root, "[draft].md", "selected\n")
	put(t, root, "d.md", "unselected\n")
	s, e := g.Inspect(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if e = g.Commit(context.Background(), root, b, []string{"[draft].md"}, "draft", s.Fingerprint); e != nil {
		t.Fatal(e)
	}
	if got := fixtureGit(t, root, "show", "--format=", "--name-only", "HEAD"); got != "[draft].md" {
		t.Fatal(got)
	}
}
