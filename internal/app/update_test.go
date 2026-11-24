package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/update"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// fakeUpdates answers checks with release, or err.
type fakeUpdates struct {
	disabled bool
	release  *fakeRelease
	err      error
	checks   int
	onCheck  func()
}

func (f *fakeUpdates) Enabled() bool { return !f.disabled }
func (f *fakeUpdates) Check(context.Context) (updateRelease, error) {
	f.checks++
	if f.onCheck != nil {
		f.onCheck()
	}
	if f.err != nil || f.release == nil {
		return nil, f.err
	}
	return f.release, nil
}

type fakeRelease struct {
	version, notes string
	installs       int
	err            error
	applies        int
	applyErr       error
	onDownload     func()
}

func (r *fakeRelease) Version() string { return r.version }
func (r *fakeRelease) Notes() string   { return r.notes }
func (r *fakeRelease) Download(ctx context.Context, progress func(done, total int64)) error {
	r.installs++
	if r.onDownload != nil {
		r.onDownload()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	progress(512, 1024)
	progress(1024, 1024)
	return r.err
}

func (r *fakeRelease) Apply() error {
	r.applies++
	return r.applyErr
}

// withUpdates gives a test app a fake update service, a relaunch it
// counts and pages it records.
func withUpdates(a *App, f *fakeUpdates) (relaunched *int, opened *[]string) {
	relaunched, opened = new(int), new([]string)
	a.updates.svc = f
	a.updates.relaunch = func() { *relaunched++ }
	a.updates.openURL = func(url string) { *opened = append(*opened, url) }
	return
}

func TestUpdateBackgroundDownloadsAndRestarts(t *testing.T) {
	a, tt := newTestApp(t)
	f := &fakeUpdates{release: &fakeRelease{version: "0.2.0"}}
	relaunched, _ := withUpdates(a, f)
	tt.Frame()
	if f.checks != 0 {
		t.Fatal("no check at launch: it waits", update.FirstCheck)
	}
	tt.Advance(update.FirstCheck)
	tt.Frame()
	if f.checks != 1 || f.release.installs != 1 || f.release.applies != 0 || *relaunched != 0 {
		t.Fatalf("checks %d, installs %d: a background check downloads the update", f.checks, f.release.installs)
	}
	tt.Advance(time.Second)
	if !tt.HasText("更新已准备就绪！") || !tt.HasText("点击重新启动") {
		t.Fatalf("the ready card shows above the spaces: %q", tt.Texts())
	}
	tt.Click("更新已准备就绪！")
	tt.Frame()
	if *relaunched != 1 {
		t.Fatal("clicking the card restarts")
	}
	if a.prefs.PendingUpdate != "0.2.0" {
		t.Fatalf("pending update %q", a.prefs.PendingUpdate)
	}
	// Eight hours on, a downloaded update is not checked again.
	a.updates.next = tt.Now() // the next check falls due
	tt.Frame()
	if f.checks != 1 {
		t.Fatalf("checked %d times with an update waiting", f.checks)
	}
}

func TestUpdateFoundWithoutAutomaticDownload(t *testing.T) {
	a, tt := newTestApp(t)
	a.prefs.NoAutoUpdate = true
	f := &fakeUpdates{release: &fakeRelease{version: "0.3.0", notes: "## 新功能\n\n- 自动更新"}}
	relaunched, _ := withUpdates(a, f)
	tt.Frame()
	tt.Advance(update.FirstCheck)
	tt.Frame()
	tt.Advance(time.Second)
	if f.release.installs != 0 {
		t.Fatal("automatic downloads are off")
	}
	if !tt.HasText("FreeFolio 有新版本") || !tt.HasText("FreeFolio 0.3.0") {
		t.Fatalf("the available card: %q", tt.Texts())
	}
	tt.Click("FreeFolio 有新版本")
	tt.Frame()
	if !a.settingsOpen || a.settingsTab != tabUpdates || !tt.HasText("更新说明 · v0.3.0") || !tt.HasText("• 自动更新") || !tt.HasText("下载并安装") || !tt.HasText("查看发布页") {
		t.Fatalf("the updates settings with the notes: %q", tt.Texts())
	}
	tt.Click("下载并安装")
	tt.Frame()
	if f.release.installs != 1 || *relaunched != 1 || a.prefs.PendingUpdate != "0.3.0" {
		t.Fatalf("download and install must finish from one click: downloads=%d restarts=%d pending=%q", f.release.installs, *relaunched, a.prefs.PendingUpdate)
	}
}

func TestUpdateManualCheck(t *testing.T) {
	a, tt := newTestApp(t)
	f := &fakeUpdates{}
	withUpdates(a, f)
	tt.Frame()
	a.checkForUpdates()
	tt.Frame()
	if !tt.HasText("FreeFolio 已是最新版本") {
		t.Fatalf("up to date: %q", tt.Texts())
	}
	tt.Click("返回")
	tt.Frame()
	if a.settingsOpen {
		t.Fatal("settings return to the document")
	}

	f.err = errors.New("offline")
	a.checkForUpdates()
	tt.Frame()
	if !tt.HasText("无法检查更新：offline") || !tt.HasText("手动下载") {
		t.Fatalf("failed check: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.settingsOpen {
		t.Fatal("Esc leaves settings")
	}

	f.err, f.release = nil, &fakeRelease{version: "0.2.0"}
	a.checkForUpdates()
	tt.Frame()
	if !tt.HasText("FreeFolio 0.2.0 可以更新。") || !tt.HasText("当前版本 "+Version) || !tt.HasText("下载并安装") {
		t.Fatalf("found: %q", tt.Texts())
	}
	tt.Click("返回")
	tt.Frame()
	if a.settingsOpen || a.updates.found != "0.2.0" {
		t.Fatal("leaving settings keeps the available update")
	}
	f.release = nil
	a.checkForUpdates()
	tt.Frame()
	if f.checks != 4 || !tt.HasText("FreeFolio 已是最新版本") || tt.HasText("下载并安装") {
		t.Fatal("checking again must refresh the result instead of reopening an old offer")
	}
}

func TestUpdateCheckKeepsSettingsNavigationAvailable(t *testing.T) {
	for _, test := range []struct {
		name    string
		service fakeUpdates
	}{
		{"current", fakeUpdates{}},
		{"available", fakeUpdates{release: &fakeRelease{version: "0.2.0"}}},
		{"failed", fakeUpdates{err: errors.New("offline")}},
		{"unpublished", fakeUpdates{err: update.ErrNoInstaller}},
		{"unsupported", fakeUpdates{disabled: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, tt := newTestApp(t)
			withUpdates(a, &test.service)
			a.openSettings(tabUpdates)
			tt.Frame()
			if err := tt.Click("检查更新…"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if err := tt.Click("关于"); err != nil {
				t.Fatalf("checking for updates must not put a dialog over settings: %v", err)
			}
			tt.Frame()
			if a.settingsTab != tabAbout {
				t.Fatal("settings navigation must stay available after checking")
			}
		})
	}
}

func TestUpdateManualCheckDoesNotAutoDownloadAfterLeavingSettings(t *testing.T) {
	a, tt := newTestApp(t)
	release := &fakeRelease{version: "0.2.0"}
	service := &fakeUpdates{release: release, onCheck: func() { a.settingsOpen = false }}
	withUpdates(a, service)
	tt.Frame()
	a.checkForUpdates()
	tt.Frame()
	if a.settingsOpen || release.installs != 0 || a.updates.found != "0.2.0" {
		t.Fatal("a manual check stays manual when the user leaves settings before it completes")
	}
}

func TestUpdateDownloadContinuesWhenLeavingSettingsAndCanBeCanceledInline(t *testing.T) {
	a, tt := newTestApp(t)
	withUpdates(a, &fakeUpdates{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.updates.release = &fakeRelease{version: "0.2.0"}
	a.updates.downloading, a.updates.installAfterDownload = true, true
	a.updates.phase, a.updates.cancel = phaseDownloading, cancel
	a.updates.received, a.updates.total = 512, 1024
	a.openSettings(tabUpdates)
	tt.Frame()
	if !tt.HasText("512 B / 1.0 KB") || !tt.HasText("50%") {
		t.Fatalf("progress must be visible inline: %q", tt.Texts())
	}
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsOpen || ctx.Err() != nil || !a.updates.downloading {
		t.Fatal("leaving settings must not cancel a download")
	}
	a.openSettings(tabUpdates)
	tt.Frame()
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if ctx.Err() != context.Canceled || a.updates.downloading || a.updates.installAfterDownload || !tt.HasText("下载并安装") {
		t.Fatal("inline Cancel must stop the operation and leave a download action")
	}
}

func TestUpdateInstallFailureKeepsAppOpen(t *testing.T) {
	a, tt := newTestApp(t)
	release := &fakeRelease{version: "0.2.0", applyErr: errors.New("installer blocked")}
	relaunched, _ := withUpdates(a, &fakeUpdates{release: release})
	tt.Frame()
	a.checkForUpdates()
	tt.Frame()
	if err := tt.Click("下载并安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if *relaunched != 0 || a.prefs.PendingUpdate != "" || !tt.HasText("安装更新失败：installer blocked") {
		t.Fatalf("failed launch must keep the app open with an error: %q", tt.Texts())
	}
	release.applyErr = nil
	if err := tt.Click("重试"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if *relaunched != 1 || release.applies != 2 {
		t.Fatalf("retry must apply once more: restarts=%d applies=%d", *relaunched, release.applies)
	}
	a.installUpdate()
	if release.applies != 2 {
		t.Fatal("repeated install clicks must not launch another installer")
	}
}

func TestUpdateCanceledDownloadNeedsAnotherInstallClick(t *testing.T) {
	a, tt := newTestApp(t)
	release := &fakeRelease{version: "0.2.0", onDownload: a.cancelUpdateDownload}
	relaunched, _ := withUpdates(a, &fakeUpdates{release: release})
	tt.Frame()
	a.checkForUpdates()
	tt.Frame()
	if err := tt.Click("下载并安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if *relaunched != 0 || release.applies != 0 || a.updates.ready != "" || a.updates.downloading || a.updates.installAfterDownload {
		t.Fatal("canceling a download must also cancel its installation")
	}
	release.onDownload = nil
	a.checkForUpdates()
	tt.Frame()
	if err := tt.Click("下载并安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if *relaunched != 1 || release.applies != 1 {
		t.Fatal("a new explicit click must be able to retry the canceled update")
	}
}

func TestUpdateInstallWaitsForUnsavedDocuments(t *testing.T) {
	a, tt := newTestApp(t)
	a.prefs.NoAutosave = true
	release := &fakeRelease{version: "0.2.0"}
	relaunched, _ := withUpdates(a, &fakeUpdates{release: release})
	tt.Frame()
	caretAtEnd(a, tt)
	tt.Type(" Keep this before updating.")
	tt.Frame()
	a.checkForUpdates()
	tt.Frame()
	if err := tt.Click("下载并安装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if release.applies != 0 || *relaunched != 0 || !a.unsaved.open {
		t.Fatal("installing must wait for the save decision")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if release.applies != 0 || a.prefs.PendingUpdate != "" {
		t.Fatal("canceling the save must cancel installation")
	}
	a.installUpdate()
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	text, err := vault.Read(root(a), "Welcome.md")
	if err != nil || !strings.Contains(text, "Keep this before updating.") || release.applies != 1 || *relaunched != 1 {
		t.Fatalf("save must precede installation: %q, %v applies=%d restarts=%d", text, err, release.applies, *relaunched)
	}
}

func TestUpdateNoInstallerDoesNotReportUpToDate(t *testing.T) {
	a, tt := newTestApp(t)
	withUpdates(a, &fakeUpdates{err: update.ErrNoInstaller})
	tt.Frame()
	a.checkForUpdates()
	tt.Frame()
	if !tt.HasText("暂未发布适用于此平台的安装包。") || tt.HasText("FreeFolio 已是最新版本") {
		t.Fatalf("empty release feed must stay distinct: %q", tt.Texts())
	}
}

func TestUpdateDisabledBuild(t *testing.T) {
	a, tt := newTestApp(t)
	f := &fakeUpdates{disabled: true}
	_, opened := withUpdates(a, f)
	tt.Frame()
	tt.Advance(update.FirstCheck + time.Second)
	tt.Frame()
	if f.checks != 0 {
		t.Fatal("a build that cannot update never checks")
	}
	a.checkForUpdates()
	tt.Frame()
	if !tt.HasText("前往下载页面") {
		t.Fatalf("disabled: %q", tt.Texts())
	}
	tt.Click("前往下载页面")
	tt.Frame()
	if len(*opened) != 1 || (*opened)[0] != update.ReleaseURL("") {
		t.Fatalf("opened %q", *opened)
	}
}

func TestUpdateToastWhileSidebarHidden(t *testing.T) {
	a, tt := newTestApp(t)
	relaunched, _ := withUpdates(a, &fakeUpdates{})
	a.updates.ready = "0.2.0"
	a.updates.release = &fakeRelease{version: "0.2.0"}
	tt.Frame()
	if a.updates.toast != nil {
		t.Fatal("the banner says it while the sidebar shows")
	}
	a.toggleSidebar()
	tt.Frame()
	tt.Advance(time.Second)
	if a.updates.toast == nil || !tt.HasText("重新启动") {
		t.Fatalf("the toast: %q", tt.Texts())
	}
	tt.Click("关闭")
	tt.Frame()
	if a.updates.toast != nil {
		t.Fatal("✕ closes the toast")
	}
	a.toggleSidebar()
	tt.Frame()
	a.toggleSidebar()
	tt.Frame()
	if a.updates.toast != nil {
		t.Fatal("a notice toasts once")
	}
	a.updates.ready = "0.2.1"
	a.updates.release = &fakeRelease{version: "0.2.1"}
	tt.Frame()
	tt.Advance(time.Second)
	if a.updates.toast == nil {
		t.Fatal("a newer update is news again")
	}
	tt.Click("重新启动")
	tt.Frame()
	if *relaunched != 1 {
		t.Fatal("the toast's button restarts")
	}
}

func TestUpdatedCardAfterRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{ConfigDir: filepath.Join(dir, "config"), DocumentsDir: filepath.Join(dir, "docs")}
	store := prefs.Store{Path: filepath.Join(cfg.ConfigDir, "prefs.json")}
	if err := store.Save(&prefs.Prefs{PendingUpdate: Version}); err != nil {
		t.Fatal(err)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// This launch makes the first space too, whose tour is not the point.
	a.tour = tour{}
	a.tourSeen()
	_, opened := withUpdates(a, &fakeUpdates{})
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 46, Left: 78})
	tt.Frame()
	tt.Advance(time.Second)
	if !tt.HasText("FreeFolio 已更新！") || !tt.HasText("新功能") {
		t.Fatalf("the updated card: %q", tt.Texts())
	}
	if p, _ := store.Load(); p.PendingUpdate != "" {
		t.Fatal("the pending update is cleared")
	}
	tt.Click("新功能")
	tt.Frame()
	if len(*opened) != 1 || (*opened)[0] != update.ReleaseURL(Version) {
		t.Fatalf("opened %q", *opened)
	}
	tt.Click("关闭")
	tt.Frame()
	if tt.HasText("FreeFolio 已更新！") {
		t.Fatal("✕ puts the card away")
	}

	// A restart that did not land on the new version says nothing.
	if err := store.Save(&prefs.Prefs{PendingUpdate: "9.9.9"}); err != nil {
		t.Fatal(err)
	}
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if b.updates.updatedTo != "" {
		t.Fatal("not updated")
	}
}

func TestUpdateReadyCardDismissDoesNotAct(t *testing.T) {
	a, tt := newTestApp(t)
	relaunch, _ := withUpdates(a, &fakeUpdates{release: &fakeRelease{version: "0.3.0"}})
	a.updates.ready = "0.2.0"
	tt.Frame()
	if !tt.HasText("更新已准备就绪！") || !tt.HasText("点击重新启动") {
		t.Fatal("expected ready banner to render")
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if *relaunch != 0 || !a.updates.dismissed || tt.HasText("更新已准备就绪！") {
		t.Fatalf("expected dismiss without relaunch: relaunch=%d dismissed=%v", *relaunch, a.updates.dismissed)
	}

	a.updates.ready = ""
	a.updates.dismissed = false
	a.updates.found = "0.3.0"
	a.updates.release = &fakeRelease{version: "0.3.0"}
	tt.Frame()
	if !tt.HasText("FreeFolio 有新版本") {
		t.Fatal("expected available banner to render")
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsOpen || a.updates.found != "" || a.updates.release != nil || tt.HasText("FreeFolio 有新版本") {
		t.Fatalf("expected dismiss without opening settings: settings=%v found=%q release=%v", a.settingsOpen, a.updates.found, a.updates.release)
	}
}

func TestUpdateReleasePageButtonOpensTagURL(t *testing.T) {
	a, tt := newTestApp(t)
	_, opened := withUpdates(a, &fakeUpdates{release: &fakeRelease{version: "0.3.0", notes: "## 新功能\n\n- 更清晰的更新界面"}})
	tt.Frame()
	a.checkForUpdates()
	tt.Frame()
	if err := tt.Click("查看发布页"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*opened) != 1 || (*opened)[0] != update.ReleaseURL("0.3.0") {
		t.Fatalf("opened %q", *opened)
	}
}

func TestSnapshotUpdates(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to render the update banners")
	for _, name := range []string{"ready", "available", "updated", "toast", "settings-found", "settings-ready", "settings-downloading", "settings-failed", "settings-small", "settings-checking", "settings-uptodate", "settings-found@dark", "settings-failed@dark", "ready@dark", "updated@dark", "settings"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			withUpdates(a, &fakeUpdates{})
			if base, ok := strings.CutSuffix(name, "@dark"); ok {
				name = base + "-dark"
				tt.SetDark(true)
			}
			rel := &fakeRelease{version: "0.2.0", notes: "## 新功能\n\n- 自动更新：在后台下载新版本\n- 侧边栏的更新提示\n\n## 修复\n\n- 修复了分屏时的滚动问题"}
			switch name {
			case "ready", "ready-dark":
				a.updates.ready = "0.2.0"
			case "available":
				a.updates.found, a.updates.release = "0.2.0", rel
			case "updated", "updated-dark":
				a.updates.updatedTo = Version
			case "toast":
				a.updates.ready = "0.2.0"
				a.toggleSidebar()
			case "settings-found", "settings-found-dark":
				a.updates.release, a.updates.found, a.updates.phase = rel, "0.2.0", phaseFound
				a.openSettings(tabUpdates)
			case "settings-ready":
				a.updates.release, a.updates.ready, a.updates.phase = rel, "0.2.0", phaseReady
				a.openSettings(tabUpdates)
			case "settings-downloading":
				a.updates.downloading, a.updates.phase = true, phaseDownloading
				a.updates.received, a.updates.total = 14000000, 28000000
				a.openSettings(tabUpdates)
			case "settings-checking":
				a.updates.checking, a.updates.phase = true, phaseChecking
				a.openSettings(tabUpdates)
			case "settings-uptodate":
				a.updates.phase = phaseUpToDate
				a.updates.lastCheck = time.Date(2026, 11, 21, 18, 30, 0, 0, time.Local)
				a.openSettings(tabUpdates)
			case "settings-failed", "settings-failed-dark", "settings-small":
				a.updates.phase, a.updates.failure = phaseFailed, "无法检查更新：连接超时，请稍后重试。"
				a.openSettings(tabUpdates)
				if name == "settings-small" {
					tt.SetSize(640, 420)
				}
			case "settings":
				a.openSettings(tabUpdates)
			}
			tt.Frame()
			tt.Advance(1200 * time.Millisecond)
			tt.Frame()
			savePNG(t, filepath.Join(dir, "update-"+name+".png"), tt)
		})
	}
}
