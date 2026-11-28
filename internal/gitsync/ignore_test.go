package gitsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExclusionsKeepLocalFilesAndRemoveFromNextCommit(t *testing.T) {
	root, remote, g, binding := repository(t)
	put(t, root, "private/a.md", "private")
	put(t, root, "private/sub/b.md", "private child")
	put(t, root, ".gitignore", "# Keep this comment\nmanual.tmp\n")
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "commit", "-m", "more files")
	ctx := context.Background()
	state, err := g.Exclusions(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetExclusions(ctx, root, []string{"private/"}, state.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, root, "ls-files", "private"); got != "" {
		t.Fatal("private files still tracked", got)
	}
	if data, err := os.ReadFile(filepath.Join(root, "private/a.md")); err != nil || string(data) != "private" {
		t.Fatal("local content changed", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if !strings.HasPrefix(string(data), "# Keep this comment\nmanual.tmp\n") {
		t.Fatal("manual rules changed")
	}
	repo, err := g.Inspect(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, change := range repo.Changes {
		paths = append(paths, change.Path)
		if strings.HasPrefix(change.Path, "private/") && !change.ExcludedRemoval {
			t.Fatal("excluded removal not marked", change)
		}
	}
	if err := g.Commit(ctx, root, binding, paths, "Exclude private files", repo.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(ctx, root, binding); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, remote, "ls-tree", "-r", "--name-only", "main"); strings.Contains(got, "private/") {
		t.Fatal("excluded content was uploaded", got)
	}
	if got := fixtureGit(t, root, "show", "HEAD~1:private/a.md"); got != "private" {
		t.Fatal("history rewritten")
	}
	state, err = g.Exclusions(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetExclusions(ctx, root, nil, state.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, root, "status", "--porcelain", "--untracked-files=all"); !strings.Contains(got, "?? private/a.md") {
		t.Fatal("unexcluded files not selectable", got)
	}
}

func TestExclusionsProtectStagedContentAndExternalRules(t *testing.T) {
	root, _, g, _ := repository(t)
	ctx := context.Background()
	put(t, root, "note.md", "staged")
	fixtureGit(t, root, "add", "note.md")
	state, err := g.Exclusions(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetExclusions(ctx, root, []string{"note.md"}, state.Fingerprint); err == nil {
		t.Fatal("staged content discarded")
	}
	if got := fixtureGit(t, root, "show", ":note.md"); got != "staged" {
		t.Fatal("index altered")
	}
	fixtureGit(t, root, "reset", "HEAD", "note.md")
	state, _ = g.Exclusions(ctx, root)
	put(t, root, ".gitignore", "external-rule\n")
	if err := g.SetExclusions(ctx, root, []string{"note.md"}, state.Fingerprint); err != ErrChanged {
		t.Fatal("external rules overwritten", err)
	}
}

func TestExclusionsEscapeLiteralPaths(t *testing.T) {
	root, _, g, _ := repository(t)
	put(t, root, "[private].md", "private")
	put(t, root, "p.md", "keep")
	put(t, root, "folder/note.md", "inside")
	state, err := g.Exclusions(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetExclusions(context.Background(), root, []string{"[private].md", "folder/"}, state.Fingerprint); err != nil {
		t.Fatal(err)
	}
	got := fixtureGit(t, root, "ls-files", "--others", "--exclude-standard")
	if strings.Contains(got, "[private].md") || strings.Contains(got, "folder/") || !strings.Contains(got, "p.md") {
		t.Fatal("literal paths interpreted as patterns", got)
	}
}

func TestCancelExclusionBeforeCommitRestoresTrackingWithoutChangingFiles(t *testing.T) {
	root, _, g, _ := repository(t)
	ctx := context.Background()
	put(t, root, "note.md", "local edit")
	put(t, root, "other.md", "staged elsewhere")
	fixtureGit(t, root, "add", "other.md")
	state, err := g.Exclusions(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetExclusions(ctx, root, []string{"note.md"}, state.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, root, "show", ":other.md"); got != "staged elsewhere" {
		t.Fatal("unrelated staging lost")
	}
	state, err = g.Exclusions(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetExclusions(ctx, root, nil, state.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, root, "ls-files", "note.md"); got != "note.md" {
		t.Fatal("tracking not restored")
	}
	if data, err := os.ReadFile(filepath.Join(root, "note.md")); err != nil || string(data) != "local edit" {
		t.Fatal("local edits lost", err)
	}
	if got := fixtureGit(t, root, "show", ":other.md"); got != "staged elsewhere" {
		t.Fatal("unrelated staging lost on cancellation")
	}
}
