package app

import (
	"context"
	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/gitsync"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitSaveSyncSwitchControlsDelay(t *testing.T) {
	a, _ := newTestApp(t)
	a.gitSync.initGitSync()
	a.gitSync.loaded = true
	cfg := a.gitSync.gitSpaceConfig(a.state.Active)
	cfg.Automatic = true
	// Render the real settings page with all rows visible for pointer testing.
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.gitSync.settingsGitSync(c) }) }, 1100, 2400)
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
	a.gitSync.initGitSync()
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
	if !tt.HasText("octo") || a.gitSync.failed || a.gitSync.gitSpaceConfig(a.state.Active).Account != "42" {
		t.Fatal(a.gitSync.message, tt.Texts())
	}
}

func TestGitSyncSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to render Git sync settings")
	savePNG := func(name string, tt *ui.Tester) {
		t.Helper()
		f, e := os.Create(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if e = png.Encode(f, tt.Image()); e != nil {
			t.Fatal(e)
		}
	}

	// Default view (1240x760).
	a, tt := newTestApp(t)
	tt.SetSize(1240, 760)
	a.gitSync.initGitSync()
	a.gitSync.loaded = true
	a.openSettings(tabGit)
	c := a.gitSync.gitSpaceConfig(a.state.Active)
	c.Automatic = true
	c.Binding = gitsync.Binding{Remote: "origin", URL: "https://github.com/example/notes.git", Branch: "main"}
	tt.Frame()
	tt.Advance(time.Second)
	savePNG("github-sync.png", tt)

	// Active populated view (connected account, inspected repo, changes, AI mode).
	a2, tt2 := newTestApp(t)
	tt2.SetSize(1240, 2100)
	a2.gitSync.initGitSync()
	a2.gitSync.loaded = true
	a2.gitSync.accounts = []gitsync.Account{{ID: "42", Login: "octocat", Email: "octocat@example.com"}}
	a2.prefs.AI.Connections = []prefs.AIConnection{{
		ID: "openai", Name: "OpenAI", Protocol: agent.Chat,
		Models: []agent.ModelInfo{{ID: "gpt-4.1-mini", Enabled: true}},
	}}
	a2.openSettings(tabGit)
	id := a2.state.Active
	c2 := a2.gitSync.gitSpaceConfig(id)
	c2.Enabled = true
	c2.Account = "42"
	c2.Automatic = true
	c2.MessageMode = "ai"
	c2.Connection, c2.Model = "openai", "gpt-4.1-mini"
	c2.Binding = gitsync.Binding{Remote: "origin", URL: "https://github.com/example/notes.git", Branch: "main"}
	a2.gitSync.applyGitRepo(id, gitsync.Repository{
		Root:    a2.state.ActiveSpace().Root,
		Branch:  "main",
		Ahead:   2,
		Behind:  1,
		Remotes: map[string]string{"origin": "https://github.com/example/notes.git"},
		Changes: []gitsync.Change{
			{Path: "Welcome.md", Code: "M"},
			{Path: "notes/idea.md", Code: "??"},
			{Path: "archive/old.md", Code: "D", Staged: true},
		},
	})
	tt2.Frame()
	tt2.Advance(time.Second)
	savePNG("github-sync-active.png", tt2)

	// Dark theme active view.
	tt2.SetDark(true)
	tt2.Frame()
	savePNG("github-sync-dark.png", tt2)

	// Narrow responsive view (640x760).
	tt2.SetDark(false)
	tt2.SetSize(640, 760)
	tt2.Frame()
	savePNG("github-sync-narrow.png", tt2)
}

func TestGitRefreshPreservesFileSelection(t *testing.T) {
	a, _ := newTestApp(t)
	a.gitSync.initGitSync()
	id := a.state.Active
	s := gitsync.Repository{Changes: []gitsync.Change{{Path: "a.md"}, {Path: "b.md"}}}
	a.gitSync.applyGitRepo(id, s)
	a.gitSync.checked[id]["b.md"] = false
	a.gitSync.applyGitRepo(id, s)
	if a.gitSync.checked[id]["b.md"] || !a.gitSync.checked[id]["a.md"] {
		t.Fatal("preview changed selected commit files")
	}
}

func TestGitGuardRejectsLockedDocument(t *testing.T) {
	a, tt := newTestApp(t)
	a.gitSync.initGitSync()
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
	a.gitSync.initGitSync()
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
	a.gitSync.initGitSync()
	a.gitSync.loaded = true
	a.openSettings(tabGit)
	c := a.gitSync.gitSpaceConfig(a.state.Active)
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
	a.gitSync.initGitSync()
	a.gitSync.loaded = true
	c := a.gitSync.gitSpaceConfig(a.state.Active)
	c.Enabled, c.Automatic = true, true
	c.Binding = gitsync.Binding{Remote: "origin", URL: "https://github.com/o/r.git", Branch: "main"}
	if !a.gitSync.saveGitSync() {
		t.Fatal(a.gitSync.message)
	}
	tt.Frame()
	tt.Advance(time.Second)
	if a.gitSync.busy {
		t.Fatal("enable started sync")
	}
	a.cloud.config.Enabled = true
	a.cloud.config.Spaces[a.state.Active] = "notes"
	if a.gitSync.saveGitSync() {
		t.Fatal("overlapping sync accepted")
	}
}

func TestGitWorkspaceChangesNoDuplicateFilenameAndBatchSelect(t *testing.T) {
	a, _ := newTestApp(t)
	a.gitSync.initGitSync()
	a.gitSync.loaded = true
	id := a.state.Active
	a.gitSync.applyGitRepo(id, gitsync.Repository{
		Root:    a.state.ActiveSpace().Root,
		Branch:  "main",
		Remotes: map[string]string{"origin": "https://github.com/o/r.git"},
		Changes: []gitsync.Change{
			{Path: "alpha.md", Code: "M"},
			{Path: "beta.md", Code: "??"},
		},
	})
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.gitSync.settingsGitSync(c) }) }, 1100, 2400)
	tt.Frame()
	cb, ok := tt.AccessByRole(ui.RoleCheckBox, "alpha.md")
	if !ok {
		t.Fatalf("checkbox accessibility label missing for alpha.md: %v", tt.Texts())
	}
	if cb.Bounds.W > 36 {
		t.Fatalf("checkbox unexpectedly wide (%v), may render duplicate visible label", cb.Bounds.W)
	}
	if err := tt.Click("清空选择"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.gitSync.checked[id]["alpha.md"] || a.gitSync.checked[id]["beta.md"] {
		t.Fatalf("clear selection failed: %+v", a.gitSync.checked[id])
	}
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.gitSync.checked[id]["alpha.md"] || !a.gitSync.checked[id]["beta.md"] {
		t.Fatalf("select all failed: %+v", a.gitSync.checked[id])
	}
}

func TestGitDirtyBadgeClearsOnSave(t *testing.T) {
	a, _ := newTestApp(t)
	a.gitSync.initGitSync()
	a.gitSync.loaded = true
	cfg := a.gitSync.gitSpaceConfig(a.state.Active)
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.gitSync.settingsGitSync(c) }) }, 1100, 2400)
	tt.Frame()
	if tt.HasText("有未保存更改") {
		t.Fatal("dirty badge shown before modification")
	}
	cfg.Mode = "pull"
	tt.Frame()
	if !tt.HasText("有未保存更改") || !tt.HasText("未保存") {
		t.Fatalf("dirty badge missing after modification: %v", tt.Texts())
	}
	if err := tt.Click("保存 Git 同步设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("有未保存更改") || tt.HasText("未保存") {
		t.Fatalf("dirty badge remained after save: %v", tt.Texts())
	}
}

func TestGitConflictsRenderBeforeSettings(t *testing.T) {
	a, _ := newTestApp(t)
	a.gitSync.initGitSync()
	a.gitSync.loaded = true
	a.gitSync.message = "存在合并冲突，请先处理冲突"
	a.gitSync.failed = true
	id := a.state.Active
	a.gitSync.applyGitRepo(id, gitsync.Repository{
		Root:    a.state.ActiveSpace().Root,
		Branch:  "main",
		Remotes: map[string]string{"origin": "https://github.com/o/r.git"},
		Pending: &gitsync.Merge{
			Conflicts: []gitsync.Conflict{
				{Path: "note-a.md", Resolved: false},
				{Path: "note-b.md", Resolved: true},
			},
		},
	})
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.gitSync.settingsGitSync(c) }) }, 1100, 2400)
	tt.Frame()
	texts := tt.Texts()
	if !tt.HasText("合并冲突（1/2 已解决）") || !tt.HasText("待解决") || !tt.HasText("已解决") {
		t.Fatalf("conflict section missing expected status: %v", texts)
	}
	joined := strings.Join(texts, "\n")
	if strings.Index(joined, "合并冲突（1/2 已解决）") > strings.Index(joined, "同步配置") {
		t.Fatalf("conflicts should appear before sync configuration: %v", texts)
	}
	dismiss, ok := tt.AccessByRole(ui.RoleButton, "关闭提示")
	if !ok {
		t.Fatal("dismiss button missing on status banner")
	}
	tt.ClickAt(dismiss.Bounds.X+dismiss.Bounds.W/2, dismiss.Bounds.Y+dismiss.Bounds.H/2)
	tt.Frame()
	if a.gitSync.message != "" || a.gitSync.failed {
		t.Fatal("dismiss did not clear status banner")
	}
}
