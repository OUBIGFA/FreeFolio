package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/plugin"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
)

// The window's icon and the tray icon wait for the first frame to show,
// and are then set once.
func TestLaunchFinishesAfterTheFirstFrame(t *testing.T) {
	a, tt := newTestApp(t)
	finished := 0
	a.launch = launch{pending: true, finish: func() { finished++ }}
	tt.Frame()
	tt.Frame()
	if finished != 0 {
		t.Fatalf("the launch finished %d times before the first frame had shown", finished)
	}
	tt.Advance(launchSettle - time.Millisecond)
	if finished != 0 {
		t.Fatal("the launch finished before launchSettle had passed")
	}
	tt.Advance(time.Millisecond)
	if finished != 1 || a.launch.pending {
		t.Fatalf("after launchSettle the launch finished %d times, pending %v", finished, a.launch.pending)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if finished != 1 {
		t.Fatalf("the launch finished %d times", finished)
	}
}

func TestDeferredStartupDoesNotReadSpaceTreeBeforeWindow(t *testing.T) {
	seed, _ := newTestApp(t)
	sp := seed.state.ActiveSpace()
	sp.Pins = []string{"Welcome.md"}
	seed.saveState()
	cfg := seed.cfg
	cfg.DeferStartup = true
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.tree) != 0 || !a.treeLoading {
		t.Fatalf("startup read the folder before showing a window: %d nodes, loading %v", len(a.tree), a.treeLoading)
	}
	if len(a.state.ActiveSpace().Pins) != 1 {
		t.Fatal("pending tree load pruned pins")
	}
	a.finishLaunch()
	if a.treeLoading || vault.Find(a.tree, "Welcome.md") == nil {
		t.Fatal("deferred folder read did not finish")
	}
}

func TestDeferStartupDefersNonEssentialWorkUntilAfterFirstFrame(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		ConfigDir:    filepath.Join(dir, "config"),
		DocumentsDir: filepath.Join(dir, "docs"),
	}
	seed, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeNative(root(seed), "Welcome.md", "# Welcome\n\nHome doc.\n"); err != nil {
		t.Fatal(err)
	}
	seed.refreshTree()
	seed.open("Welcome.md")

	workID := addSpace(t, seed, "Work", map[string]string{
		"Note1.md": "# Note1\n\nAlpha content.\n",
		"Note2.md": "# Note2\n\nBeta content.\n",
	})
	homeID := seed.state.ActiveSpace().ID
	seed.switchSpace(workID)
	seed.open("Note1.md")
	seed.openSplit(seed.state.ActiveSpace(), "Note2.md", false, false)
	if seed.split == nil || len(seed.split.panes) != 2 {
		t.Fatalf("expected 2-pane split in Work space, got %v", seed.split)
	}
	seed.switchSpace(homeID)
	seed.refreshSearch()
	seed.captureSplits()
	seed.saveState()

	pluginDir := filepath.Join(cfg.ConfigDir, "plugins", "dev.freefolio.demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const manifest = `{"id":"dev.freefolio.demo","name":"Demo","version":"0.1.0","runtime":"wasm","main":"plugin.wasm"}`
	if err := os.WriteFile(filepath.Join(pluginDir, plugin.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	prefsStore := prefs.Store{Path: filepath.Join(cfg.ConfigDir, "prefs.json")}
	p, err := prefsStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	p.PendingUpdate = Version
	if err := prefsStore.Save(p); err != nil {
		t.Fatal(err)
	}

	cfg.DeferStartup = true
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if a.search.cacheLoaded || a.search.index != nil {
		t.Fatalf("expected search cache load to be deferred before first frame: loaded=%v index=%v", a.search.cacheLoaded, a.search.index)
	}
	if !a.launch.loadPlugins || len(a.installed) != 0 {
		t.Fatalf("expected plugin scan to be deferred before first frame: loadPlugins=%v count=%d", a.launch.loadPlugins, len(a.installed))
	}
	if !a.launch.savePrefs {
		t.Fatal("expected PendingUpdate prefs persistence to be deferred")
	}
	if diskPrefs, err := prefsStore.Load(); err != nil || diskPrefs.PendingUpdate != Version {
		t.Fatalf("expected disk PendingUpdate %q before finishLaunch, got %q (err=%v)", Version, diskPrefs.PendingUpdate, err)
	}

	findSpace := func(st *space.State, id string) *space.Space {
		for _, sp := range st.Spaces {
			if sp.ID == id {
				return sp
			}
		}
		return nil
	}
	work := findSpace(a.state, workID)
	if work == nil || work.Split == nil || len(work.Split.Panes) != 2 {
		t.Fatalf("expected saved 2-pane split on inactive Work space, got %+v", work)
	}
	if a.session(workID).splitRestored || a.session(workID).split != nil {
		t.Fatalf("expected inactive Work split restore to be deferred: restored=%v split=%v", a.session(workID).splitRestored, a.session(workID).split)
	}

	a.captureSplits()
	if work.Split == nil || len(work.Split.Panes) != 2 {
		t.Fatalf("captureSplits clobbered unrestored inactive space split: %+v", work.Split)
	}

	a.finishLaunch()

	if a.launch.pending || a.launch.savePrefs || a.launch.deferSearch || a.launch.loadPlugins || a.launch.migrateSpaces {
		t.Fatalf("expected all deferred launch flags cleared, got %+v", a.launch)
	}
	if !a.search.cacheLoaded || a.search.index == nil || a.search.index.Len() == 0 {
		t.Fatalf("expected search index populated after finishLaunch: loaded=%v index=%v", a.search.cacheLoaded, a.search.index)
	}
	if a.launch.loadPlugins || len(a.installed) != 1 || a.installed[0].Manifest.ID != "dev.freefolio.demo" {
		t.Fatalf("expected demo plugin scanned after finishLaunch: loadPlugins=%v installed=%+v", a.launch.loadPlugins, a.installed)
	}
	if diskPrefs, err := prefsStore.Load(); err != nil || diskPrefs.PendingUpdate != "" {
		t.Fatalf("expected PendingUpdate cleared on disk after finishLaunch, got %q (err=%v)", diskPrefs.PendingUpdate, err)
	}

	a.switchSpace(workID)
	if !a.session(workID).splitRestored || a.split == nil || len(a.split.panes) != 2 {
		t.Fatalf("expected Work split lazily restored on switchSpace: restored=%v split=%v", a.session(workID).splitRestored, a.split)
	}
}
