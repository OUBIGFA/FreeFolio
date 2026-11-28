package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestGitExclusionSelectionPersistsAndCanBeReopened(t *testing.T) {
	a, _ := newTestApp(t)
	gs := &a.gitSync
	gs.initGitSync()
	gs.loaded = true
	id, spaceRoot := a.state.Active, root(a)
	gs.selected = id
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(spaceRoot))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	if err := os.MkdirAll(filepath.Join(spaceRoot, "private"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spaceRoot, "private", "note.md"), []byte("keep locally"), 0600); err != nil {
		t.Fatal(err)
	}
	gs.openGitExclusions(id)
	finishGitCreationWork(t, gs)
	if gs.exclusions == nil {
		t.Fatal("exclusion picker unavailable", gs.message)
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { gs.settingsGitSync(c) }) }, 1100, 1400)
	tt.Frame()
	check, ok := tt.AccessByRole(ui.RoleCheckBox, "private/")
	if !ok {
		t.Fatal("folder selection missing")
	}
	tt.ClickAt(check.Bounds.X+check.Bounds.W/2, check.Bounds.Y+check.Bounds.H/2)
	tt.Frame()
	child, ok := tt.AccessByRole(ui.RoleCheckBox, "private/note.md")
	if !ok || !child.Disabled {
		t.Fatal("folder exclusion did not cover its child")
	}
	if output := os.Getenv("FREEFOLIO_SNAPSHOT"); output != "" {
		if err := os.MkdirAll(output, 0755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(output, "git-exclusions.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tt.Image())
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click("保存同步排除"); err != nil {
		t.Fatal(err)
	}
	finishGitCreationWork(t, gs)
	if gs.failed || gs.exclusions != nil {
		t.Fatal("save failed", gs.message)
	}
	gs.openGitExclusions(id)
	finishGitCreationWork(t, gs)
	if !gs.exclusions.selected["private/"] {
		t.Fatal("saved selection was not restored")
	}
	g, err := gs.gitEngine(context.Background(), id, spaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := g.Inspect(context.Background(), spaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range repo.Changes {
		if change.Path == "private/note.md" {
			t.Fatal("excluded file still offered for upload")
		}
	}
	if data, err := os.ReadFile(filepath.Join(spaceRoot, "private", "note.md")); err != nil || string(data) != "keep locally" {
		t.Fatal("local file was modified", err)
	}
}
