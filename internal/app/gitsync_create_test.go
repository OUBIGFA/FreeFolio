package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/gitsync"
	"github.com/egoist/mygo/ui"
)

func finishGitCreationWork(t *testing.T, gs *gitUIState) {
	t.Helper()
	if !gs.busy {
		t.Fatalf("Git worker did not start: failed=%t message=%q", gs.failed, gs.message)
	}
	select {
	case apply := <-gs.done:
		apply()
		gs.busy = false
	case <-time.After(60 * time.Second):
		gs.cancel()
		select {
		case <-gs.done:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("Git preparation did not finish")
	}
}

func TestGitPrivateCreationRequiresFileReviewAndRejectsChangedPreview(t *testing.T) {
	a, _ := newTestApp(t)
	gs := &a.gitSync
	gs.initGitSync()
	gs.loaded = true
	id, spaceRoot := a.state.Active, root(a)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(spaceRoot))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	gs.selected = id
	gs.accounts = []gitsync.Account{{ID: "42", Login: "octo"}}
	gs.createRemote = func(context.Context, string, string) (gitsync.RemoteRepository, error) {
		t.Error("remote creation must not occur before confirmed review")
		return gitsync.RemoteRepository{}, nil
	}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { gs.settingsGitSync(c) }) }, 1100, 2400)
	tt.Frame()
	entry, ok := tt.AccessByRole(ui.RoleButton, "创建私有仓库")
	if !ok || entry.Disabled {
		t.Fatal("create action unavailable")
	}
	tt.ClickAt(entry.Bounds.X+entry.Bounds.W/2, entry.Bounds.Y+entry.Bounds.H/2)
	finishGitCreationWork(t, gs)
	tt.Frame()
	if gs.failed || gs.creation == nil || !tt.HasText("创建私有仓库并上传") || !tt.HasText("Welcome.md") {
		t.Fatal("file review missing", gs.message, tt.Texts())
	}
	if gs.gitSpaceConfig(id).Enabled || gs.gitSpaceConfig(id).Automatic || gs.repos[id].Head != "" || len(gs.repos[id].Remotes) != 0 {
		t.Fatal("review enabled sync or published content")
	}
	if err := tt.Click("清空选择"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	confirm, ok := tt.AccessByRole(ui.RoleButton, "创建私有仓库并上传")
	if !ok || !confirm.Disabled {
		t.Fatal("empty selection permits upload")
	}
	gs.checked[id]["Welcome.md"] = true
	if err := os.WriteFile(filepath.Join(spaceRoot, "new.md"), []byte("not reviewed"), 0600); err != nil {
		t.Fatal(err)
	}
	gs.createGitRepository(id)
	finishGitCreationWork(t, gs)
	if !gs.failed || gs.message != gitsync.ErrChanged.Error() {
		t.Fatal("changed files were not rejected", gs.message)
	}
}

func TestGitPrivateCreationEntryRequiresAccount(t *testing.T) {
	a, _ := newTestApp(t)
	gs := &a.gitSync
	gs.initGitSync()
	gs.loaded = true
	gs.selected = a.state.Active
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { gs.settingsGitSync(c) }) }, 1100, 2400)
	tt.Frame()
	button, ok := tt.AccessByRole(ui.RoleButton, "创建私有仓库")
	if !ok || !button.Disabled {
		t.Fatal("private repository creation needs a connected account")
	}
}

func TestGitPrivateCreationSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect private repository review")
	a, _ := newTestApp(t)
	gs := &a.gitSync
	gs.initGitSync()
	gs.loaded = true
	id := a.state.Active
	gs.selected = id
	gs.accounts = []gitsync.Account{{ID: "42", Login: "octocat"}}
	gs.gitSpaceConfig(id).Account = "42"
	gs.creation = &gitRepositoryDraft{space: id, name: "my-notes"}
	gs.applyGitRepo(id, gitsync.Repository{Root: root(a), Branch: "main", Changes: []gitsync.Change{
		{Path: "Welcome.md", Code: "??"}, {Path: "notes/idea.md", Code: "??"}, {Path: ".env", Code: "??"},
	}})
	for _, width := range []int{1100, 640} {
		tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { gs.settingsGitSync(c) }) }, width, 1200)
		tt.Frame()
		tt.Advance(time.Second)
		name := "github-create.png"
		if width == 640 {
			name = "github-create-narrow.png"
		}
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tt.Image())
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
