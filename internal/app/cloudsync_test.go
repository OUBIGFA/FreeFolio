package app

import (
	"context"
	"errors"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/cloudsync"
	"github.com/egoist/mygo/ui"
	"golang.org/x/net/webdav"
)

// Load the saved settings before the first frame, as a real launch does.
func newCloudScheduleTestApp(t *testing.T, configure func(*cloudsync.Config)) (*App, *ui.Tester) {
	t.Helper()
	a, _ := newTestApp(t)
	server := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(t.TempDir()), LockSystem: webdav.NewMemLS()})
	t.Cleanup(server.Close)
	cfg := cloudsync.DefaultConfig()
	cfg.Enabled, cfg.Automatic = true, true
	cfg.WebDAVURL = server.URL
	cfg.Spaces[a.state.Active] = "Notes"
	configure(&cfg)
	if err := cloudsync.SaveConfig(a.cfg.ConfigDir, cfg); err != nil {
		t.Fatal(err)
	}
	a, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.cancelCloudSync()
		if a.cloud.busy {
			select {
			case <-a.cloud.done:
			case <-time.After(3 * time.Second):
				t.Error("sync worker did not stop")
			}
		}
	})
	tt := ui.NewTester(a.View, 1200, 760)
	return a, tt
}

func TestCloudEnablingWaitsForFutureSchedule(t *testing.T) {
	for _, toggle := range []string{"automatic", "enabled"} {
		for _, schedule := range []string{"interval", "daily"} {
			t.Run(toggle+"/"+schedule, func(t *testing.T) {
				a, tt := newCloudScheduleTestApp(t, func(cfg *cloudsync.Config) {
					cfg.Automatic = toggle != "automatic"
					cfg.Enabled = toggle != "enabled"
					cfg.Schedule, cfg.DailyTime = schedule, "00:00"
				})
				tt.Advance(24 * time.Hour)
				a.cloud.config.Enabled, a.cloud.config.Automatic = true, true
				tt.Frame()
				want := a.now.Add(30 * time.Minute)
				if schedule == "daily" {
					want = time.Date(a.now.Year(), a.now.Month(), a.now.Day()+1, 0, 0, 0, 0, a.now.Location())
				}
				tt.Advance(3 * time.Second)
				if a.cloud.busy || !a.cloud.due.IsZero() || !a.cloud.next.Equal(want) {
					t.Fatalf("enabling sync ran or queued an immediate job: busy=%v due=%v next=%v want=%v", a.cloud.busy, a.cloud.due, a.cloud.next, want)
				}
				tt.Advance(want.Sub(a.now))
				if a.cloud.count != 1 {
					t.Fatalf("the next scheduled sync did not start: now=%v next=%v count=%d message=%q", a.now, a.cloud.next, a.cloud.count, a.cloud.message)
				}
			})
		}
	}
}

func TestCloudDisablingDiscardsPendingSync(t *testing.T) {
	for _, pending := range []string{"startup", "save", "retry", "interval"} {
		for _, toggle := range []string{"automatic", "enabled"} {
			t.Run(pending+"/"+toggle, func(t *testing.T) {
				a, tt := newCloudScheduleTestApp(t, func(cfg *cloudsync.Config) {
					cfg.SyncOnStart = pending == "startup"
				})
				switch pending {
				case "save":
					a.doc.justSaved = true
					a.cloudSaved(a.doc)
				case "retry":
					a.cloud.busy = true
					a.cloud.done = make(chan cloudOutcome, 1)
					a.cloud.done <- cloudOutcome{err: errors.New("temporary connection failure")}
					tt.Frame()
				}
				if toggle == "automatic" {
					a.cloud.config.Automatic = false
				} else {
					a.cloud.config.Enabled = false
				}
				tt.Frame()
				if !a.cloud.due.IsZero() || !a.cloud.next.IsZero() || a.cloud.retries != 0 {
					t.Fatalf("disabled sync retained pending work: due=%v next=%v retries=%d", a.cloud.due, a.cloud.next, a.cloud.retries)
				}
				tt.Advance(24 * time.Hour)
				a.cloud.config.Enabled, a.cloud.config.Automatic = true, true
				tt.Frame()
				want := a.now.Add(30 * time.Minute)
				tt.Advance(3 * time.Second)
				if a.cloud.busy || !a.cloud.due.IsZero() || !a.cloud.next.Equal(want) {
					t.Fatalf("reenabling sync replayed pending work: busy=%v due=%v next=%v", a.cloud.busy, a.cloud.due, a.cloud.next)
				}
			})
		}
	}
}

func TestCloudStartupSyncRunsOnceAtLaunch(t *testing.T) {
	a, tt := newCloudScheduleTestApp(t, func(*cloudsync.Config) {})
	tt.Advance(2*time.Second - time.Millisecond)
	if a.cloud.busy {
		t.Fatal("startup sync ran before its delay")
	}
	tt.Advance(time.Millisecond)
	if a.cloud.count != 1 {
		t.Fatal("enabled startup sync did not run at launch")
	}
	tt.Advance(3 * time.Second)
	if a.cloud.count != 1 {
		t.Fatal("startup sync ran more than once")
	}
}

func TestCloudProgressAndManualCancellation(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabCloud)
	requested := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requested <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	a.cloud.config.Enabled, a.cloud.config.Automatic = true, true
	a.cloud.config.WebDAVURL = server.URL
	a.cloud.config.Spaces[a.state.Active] = "Notes"
	tt.Frame()
	if err := tt.Click("立即同步"); err != nil {
		t.Fatal(err)
	}
	done := a.cloud.done
	defer a.cancelCloudSync()
	select {
	case <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("sync did not contact the local server")
	}
	tt.Advance(60 * time.Millisecond)
	if !strings.Contains(a.cloud.message, "正在检查云端目录") {
		t.Errorf("progress=%q", a.cloud.message)
	}
	// Live sync progress must render once in the status bar, never duplicated in the switch summary.
	a.cloud.message = "正在核对云端文件 · Personal · 1/3\nWelcome.md"
	tt.Frame()
	verifyCount := 0
	for _, text := range tt.Texts() {
		if strings.Contains(text, "正在核对云端文件") {
			verifyCount++
		}
	}
	if verifyCount != 1 {
		t.Fatalf("expected progress message once, got %d copies: %q", verifyCount, tt.Texts())
	}
	if err := tt.Click("取消同步"); err != nil {
		t.Fatal(err)
	}
	if a.cloud.busy {
		select {
		case out := <-done:
			done <- out
		case <-time.After(3 * time.Second):
			t.Fatal("cancel did not release the worker")
		}
	}
	tt.Advance(3 * time.Second)
	if a.cloud.busy || a.cloud.failed || a.cloud.retries != 0 || !a.cloud.due.IsZero() || !tt.HasText("已取消同步") {
		t.Fatalf("cancel outcome: busy=%v failed=%v retry=%d due=%v message=%q", a.cloud.busy, a.cloud.failed, a.cloud.retries, a.cloud.due, a.cloud.message)
	}
}

func TestCloudUnchangedAndCanceledOutcomes(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "canceled"}[canceled], func(t *testing.T) {
			a, tt := newTestApp(t)
			a.openSettings(tabCloud)
			a.cloud.busy = true
			a.cloud.config.Enabled, a.cloud.config.Automatic = true, true
			a.cloud.config.Spaces[a.state.Active] = "Notes"
			a.cloud.done = make(chan cloudOutcome, 1)
			a.cloud.guards = make(chan cloudGuard)
			tt.Frame()
			out := cloudOutcome{}
			if canceled {
				out.err = context.Canceled
			}
			a.cloud.done <- out
			tt.Advance(60 * time.Millisecond)
			if a.cloud.busy || a.cloud.failed {
				t.Fatalf("busy=%v failed=%v", a.cloud.busy, a.cloud.failed)
			}
			want := "无需同步"
			if canceled {
				want = "已取消同步"
			}
			if !strings.Contains(a.cloud.message, want) || !tt.HasText(a.cloud.message) {
				t.Fatalf("message=%q", a.cloud.message)
			}
			if a.cloud.retries != 0 || !a.cloud.due.IsZero() {
				t.Fatal("unchanged or canceled operation scheduled a failure retry")
			}
		})
	}
}

func TestCloudSettingsSaveAndProtectActiveEdits(t *testing.T) {
	a, tt := newTestApp(t)
	if a.cloud.config.Enabled || a.cloud.config.Automatic {
		t.Fatal("sync must default off")
	}
	a.openSettings(tabCloud)
	tt.Frame()
	for _, label := range []string{"云端同步", "启用云端同步", "WebDAV 地址", "保存同步设置", "测试连接"} {
		if _, ok := tt.Find(label); !ok && !tt.HasText(label) {
			t.Fatalf("missing sync control %q", label)
		}
	}
	a.cloud.config.Enabled = true
	a.cloud.config.WebDAVURL = "https://example.invalid/dav"
	a.cloud.config.WebDAVPassword = "test-only-secret"
	a.cloud.config.Spaces[a.state.Active] = "Notes"
	if err := tt.Click("保存同步设置"); err != nil {
		t.Fatal(err)
	}
	cfg, err := cloudsync.LoadConfig(a.cfg.ConfigDir)
	if err != nil || !cfg.Enabled || cfg.WebDAVPassword != "test-only-secret" {
		t.Fatalf("saved configuration: %v", err)
	}
	// Worker guards consult the editor on the UI thread, before changing disk.
	a.doc.ed.ReplaceText(nil, "unsaved ")
	a.cloud.busy = true
	a.cloud.guards = make(chan cloudGuard, 1)
	a.cloud.done = make(chan cloudOutcome, 1)
	reply := make(chan error, 1)
	a.cloud.guards <- cloudGuard{a.doc.space.ID, a.doc.rel, reply}
	tt.Frame()
	select {
	case err := <-reply:
		if err == nil {
			t.Fatal("unsaved document was not protected")
		}
	default:
		t.Fatal("guard was not delivered")
	}
	a.cloud.done <- cloudOutcome{err: errors.New("test cancellation")}
	tt.Frame()
	if a.cloud.busy || !a.cloud.failed {
		t.Fatal("worker completion not reflected in settings")
	}
}

func TestCloudSaveDebouncesAndHistoryCloses(t *testing.T) {
	a, tt := newTestApp(t)
	a.cloud.config.Enabled, a.cloud.config.Automatic = true, true
	a.cloud.config.Spaces[a.doc.space.ID] = "Notes"
	a.doc.justSaved = true
	a.cloudSaved(a.doc)
	if got := a.cloud.due.Sub(a.now); got != 15*time.Second {
		t.Fatalf("save delay=%v", got)
	}
	a.versions = versionsState{open: false, for_: a.doc, doc: a.doc.ed.Committed().Clone()}
	// Keep automatic networking disabled while checking the closed modal state.
	a.cloud.config.Automatic = false
	tt.Frame()
	if a.versions.for_ != nil || a.versions.doc != nil {
		t.Fatal("closed history retains document")
	}
}

func TestSidebarCloudSyncIcons(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	for _, label := range []string{"立即同步", "云端同步"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("missing sidebar cloud sync icon %q", label)
		}
	}
	// Open the sidebar cloud sync menu and toggle cloud sync + open settings.
	if err := tt.Click("云端同步"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("启用云端同步"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	tt.Frame()
	if !a.cloud.config.Enabled {
		t.Fatal("sidebar cloud menu did not enable cloud sync")
	}
	if err := tt.Click("云端同步"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("云端同步设置…"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	tt.Frame()
	if !a.settingsOpen || a.settingsTab != tabCloud {
		t.Fatalf("settings not opened on cloud tab: open=%v tab=%q", a.settingsOpen, a.settingsTab)
	}
}

func TestCloudSettingsUIStates(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabCloud)
	tt.Frame()

	// Encryption password stays hidden while encryption is off.
	if tt.HasText("内容加密密码") {
		t.Fatal("encryption password should be hidden when encryption is off")
	}
	a.cloud.config.Encrypt = true
	tt.Frame()
	if !tt.HasText("内容加密密码") {
		t.Fatal("encryption password missing when encryption is enabled")
	}

	// Switch to S3 and verify S3 fields replace WebDAV fields.
	if err := tt.Click("S3"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.cloud.config.Provider != "s3" {
		t.Fatalf("provider=%q, want s3", a.cloud.config.Provider)
	}
	if !tt.HasText("存储桶") || tt.HasText("WebDAV 地址") {
		t.Fatal("S3 fields did not replace WebDAV fields")
	}

	// Automatic schedule expands and switches between interval and daily.
	if tt.HasText("同步计划") {
		t.Fatal("schedule should stay hidden when automatic sync is off")
	}
	a.cloud.config.Automatic = true
	tt.Frame()
	if !tt.HasText("间隔（分钟）") {
		t.Fatal("interval input missing when automatic sync is enabled")
	}
	a.cloud.config.Schedule = "daily"
	tt.Frame()
	if !tt.HasText("每天同步时间") {
		t.Fatal("daily time input missing in daily schedule mode")
	}

	// Conflicts and status messages render in the settings view.
	a.cloud.failed = true
	a.cloud.message = "同步失败：网络超时"
	a.cloud.conflicts = []cloudConflict{{
		space:  a.state.Active,
		name:   "Personal",
		detail: cloudsync.Conflict{Path: "Welcome.md", RemoteExists: true, RemoteHash: "abc"},
	}}
	tt.Frame()
	for _, want := range []string{"同步失败：网络超时", "待处理冲突（1）", "保留本地版本", "采用云端版本"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in conflict/status UI", want)
		}
	}
}

func TestCloudSyncSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to render cloud sync")
	for _, dark := range []bool{false, true} {
		a, tt := newTestApp(t)
		tt.SetSize(1120, 960)
		tt.SetDark(dark)
		a.cloud.config.Enabled = true
		a.cloud.config.WebDAVURL = "https://dav.example.com/notes"
		a.cloud.config.Spaces[a.state.Active] = "Notes"
		a.openSettings(tabCloud)
		for range 3 {
			tt.Frame()
		}
		name := "cloud-sync"
		if dark {
			name += "-dark"
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// Also capture an expanded S3 + conflict + status view for visual review.
	a, tt := newTestApp(t)
	tt.SetSize(1120, 1280)
	a.cloud.config.Enabled = true
	a.cloud.config.Provider = "s3"
	a.cloud.config.S3Endpoint = "https://s3.example.com"
	a.cloud.config.S3Region = "us-east-1"
	a.cloud.config.S3Bucket = "freefolio-notes"
	a.cloud.config.Automatic = true
	a.cloud.config.Encrypt = true
	a.cloud.config.Spaces[a.state.Active] = "Personal"
	a.cloud.message = "同步完成：上传 2，下载 1，删除 0，冲突 1"
	a.cloud.conflicts = []cloudConflict{{
		space:  a.state.Active,
		name:   "Personal",
		detail: cloudsync.Conflict{Path: "Notes/Plan.md", RemoteExists: true, RemoteHash: "h1"},
	}}
	a.openSettings(tabCloud)
	for range 3 {
		tt.Frame()
	}
	f, err := os.Create(filepath.Join(dir, "cloud-sync-s3-expanded.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCacheBoundsAndCoalescesWaiters(t *testing.T) {
	r := newRemoteImages(t.TempDir())
	for i := 0; i < 100; i++ {
		url := string(rune('a'+i)) + ".mp4"
		r.resource("https://example.invalid/"+url, nil, nil)
	}
	if len(r.entries) > 64 || len(r.order) > 64 {
		t.Fatalf("unbounded cache: %d entries", len(r.entries))
	}
	url := "https://example.invalid/wait.png"
	r.entries[url] = &remoteImage{loading: true}
	for range 100 {
		r.resource(url, func(func()) {}, func() {}, "same-editor")
	}
	if got := len(r.entries[url].waiting); got != 1 {
		t.Fatalf("duplicate editor waiters: %d", got)
	}
}

// Both docking sides must route range selection through the same tree.
func TestMultiSelectSupportsEitherSidebarSide(t *testing.T) {
	for _, left := range []bool{false, true} {
		a, tt := newTestApp(t)
		a.prefs.SidebarLeft = left
		a.state.Favorites = nil
		settle(tt)
		if err := tt.ClickWith(ui.Cmd, "Ideas"); err != nil {
			t.Fatal(err)
		}
		if len(a.picks) < 2 {
			t.Fatalf("left=%v picks=%v", left, a.picks)
		}
		tt.Key(0, ui.KeyEscape)
		if len(a.picks) != 0 {
			t.Fatal("Escape did not clear selection")
		}
	}
}
