package app

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/cloudsync"
	"github.com/egoist/mygo/ui"
)

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

func TestCloudSyncSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEMD_SNAPSHOT to render cloud sync")
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
