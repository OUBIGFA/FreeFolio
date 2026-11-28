package app

import (
	"context"
	"github.com/OUBIGFA/FreeFolio/internal/gitsync"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGitSaveSyncSwitchControlsDelay(t *testing.T) {
	a, _ := newTestApp(t)
	a.initGitSync()
	a.gitSync.loaded = true
	cfg := a.gitSpaceConfig(a.state.Active)
	cfg.Automatic = true
	// Render the real settings page with all rows visible for pointer testing.
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.settingsGitSync(c) }) }, 1100, 2400)
	tt.Frame()
	switcher, ok := tt.AccessByRole(ui.RoleSwitch, "保存后同步")
	if !ok || cfg.OnSave {
		t.Fatal("save sync opt-in missing or enabled by default")
	}
	input, ok := tt.AccessByRole(ui.RoleGroup, "保存后等待（秒）")
	if !ok || !input.Disabled {
		t.Fatalf("delay must stay visible and disabled until enabled: found=%v input=%+v", ok, input)
	}
	tt.ClickAt(switcher.Bounds.X+switcher.Bounds.W/2, switcher.Bounds.Y+switcher.Bounds.H/2)
	tt.Frame()
	input, ok = tt.AccessByRole(ui.RoleGroup, "保存后等待（秒）")
	if !ok || !cfg.OnSave || input.Disabled {
		t.Fatal("enabling did not unlock delay")
	}
	tt.ClickAt(switcher.Bounds.X+switcher.Bounds.W/2, switcher.Bounds.Y+switcher.Bounds.H/2)
	tt.Frame()
	input, _ = tt.AccessByRole(ui.RoleGroup, "保存后等待（秒）")
	if cfg.OnSave || !input.Disabled {
		t.Fatal("disabling left delay editable")
	}
}

func TestGitLoginStartsBrowserFlowWithoutClientID(t *testing.T) {
	a, tt := newTestApp(t)
	a.initGitSync()
	a.gitSync.loaded = true
	a.openSettings(tabGit)
	a.gitSync.signIn = func(context.Context, func(gitsync.RuntimeProgress)) (gitsync.Account, []gitsync.Account, error) {
		account := gitsync.Account{ID: "42", Login: "octo"}
		return account, []gitsync.Account{account}, nil
	}
	tt.Frame()
	if tt.HasText("此构建尚未配置 GitHub 应用，维护者需完成一次性注册") {
		t.Fatal("login still requires registration")
	}
	if e := tt.Click("连接 GitHub"); e != nil {
		t.Fatal(e)
	}
	if a.gitSync.busy {
		select {
		case done := <-a.gitSync.done:
			a.gitSync.done <- done
		case <-time.After(time.Second):
			t.Fatal("login worker did not complete")
		}
	}
	tt.Frame()
	if !tt.HasText("octo") || a.gitSync.failed || a.gitSpaceConfig(a.state.Active).Account != "42" {
		t.Fatal(a.gitSync.message, tt.Texts())
	}
}

func TestGitSyncSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to render Git sync settings")
	a, tt := newTestApp(t)
	a.initGitSync()
	a.gitSync.loaded = true
	a.openSettings(tabGit)
	c := a.gitSpaceConfig(a.state.Active)
	c.Automatic = true
	c.Binding = gitsync.Binding{Remote: "origin", URL: "https://github.com/example/notes.git", Branch: "main"}
	tt.Frame()
	tt.Advance(time.Second)
	f, e := os.Create(filepath.Join(dir, "github-sync.png"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = png.Encode(f, tt.Image()); e != nil {
		t.Fatal(e)
	}
}

func TestGitRefreshPreservesFileSelection(t *testing.T) {
	a, _ := newTestApp(t)
	a.initGitSync()
	id := a.state.Active
	s := gitsync.Repository{Changes: []gitsync.Change{{Path: "a.md"}, {Path: "b.md"}}}
	a.applyGitRepo(id, s)
	a.gitSync.checked[id]["b.md"] = false
	a.applyGitRepo(id, s)
	if a.gitSync.checked[id]["b.md"] || !a.gitSync.checked[id]["a.md"] {
		t.Fatal("preview changed selected commit files")
	}
}

func TestGitGuardRejectsLockedDocument(t *testing.T) {
	a, tt := newTestApp(t)
	a.initGitSync()
	s := &a.gitSync
	s.busy = true
	s.guard = make(chan gitGuard, 1)
	s.done = make(chan func(), 1)
	reply := make(chan error, 1)
	a.doc.ed.SetReadOnly(true)
	s.guard <- gitGuard{a.doc.space.ID, []string{a.doc.rel}, reply}
	tt.Frame()
	select {
	case e := <-reply:
		if e == nil {
			t.Fatal("locked document accepted")
		}
	default:
		t.Fatal("guard was not consumed")
	}
}

func TestGitSettingsEnglish(t *testing.T) {
	a, tt := newTestApp(t)
	defer i18n.SetLanguage("zh-Hans")
	a.initGitSync()
	a.gitSync.loaded = true
	a.openSettings(tabGit)
	i18n.SetLanguage("en")
	tt.Frame()
	if !tt.HasText("GitHub Repository Sync") || !tt.HasText("Connect GitHub") {
		t.Fatal(tt.Texts())
	}
}

func TestGitSyncSettingsExposeCompleteControls(t *testing.T) {
	a, tt := newTestApp(t)
	a.initGitSync()
	a.gitSync.loaded = true
	a.openSettings(tabGit)
	c := a.gitSpaceConfig(a.state.Active)
	c.Automatic = true
	c.Binding = gitsync.Binding{Remote: "origin", URL: "https://github.com/o/r.git", Branch: "main"}
	tt.Frame()
	for _, label := range []string{"GitHub 仓库同步", "连接 GitHub", "检查仓库", "同步动作", "同步计划", "提交说明方式", "保存 Git 同步设置"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %s: %v", label, tt.Texts())
		}
	}
}
func TestGitSyncEnableDoesNotRunAndCloudOverlapRejected(t *testing.T) {
	a, tt := newTestApp(t)
	a.initGitSync()
	a.gitSync.loaded = true
	c := a.gitSpaceConfig(a.state.Active)
	c.Enabled, c.Automatic = true, true
	c.Binding = gitsync.Binding{Remote: "origin", URL: "https://github.com/o/r.git", Branch: "main"}
	if !a.saveGitSync() {
		t.Fatal(a.gitSync.message)
	}
	tt.Frame()
	tt.Advance(time.Second)
	if a.gitSync.busy {
		t.Fatal("enable started sync")
	}
	a.cloud.config.Enabled = true
	a.cloud.config.Spaces[a.state.Active] = "notes"
	if a.saveGitSync() {
		t.Fatal("overlapping sync accepted")
	}
}
