package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/vault"
)

// asyncTreeApp reads the active space's folder off the main thread, as the
// running app does, the results waiting on the returned channel.
func asyncTreeApp(t *testing.T) (*App, chan func()) {
	t.Helper()
	a, _ := newTestApp(t)
	updates := make(chan func(), 16)
	a.readTreesAsync = true
	a.runAsync = func(fn func()) { updates <- fn }
	t.Cleanup(func() {
		a.readTreesAsync = false
		a.runAsync = nil
	})
	return a, updates
}

func awaitTreeUpdate(t *testing.T, updates <-chan func()) func() {
	t.Helper()
	select {
	case fn := <-updates:
		return fn
	case <-time.After(5 * time.Second):
		t.Fatal("folder scan did not return")
		return nil
	}
}

func TestInactiveSpaceTreeLoadsOffMainThread(t *testing.T) {
	a, updates := asyncTreeApp(t)
	id := addSpace(t, a, "Other", map[string]string{"Note.md": "# Note"})
	sp := a.state.Get(id)
	nodes, err := a.treeOf(sp)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("inactive space was scanned before a background result: %d nodes, %v", len(nodes), err)
	}
	// Repeated animation frames share one pending read.
	a.treeOf(sp)
	awaitTreeUpdate(t, updates)()
	nodes, err = a.treeOf(sp)
	if err != nil || vault.Find(nodes, "Note.md") == nil {
		t.Fatalf("background tree did not arrive: %v", err)
	}
}

func TestInactiveSpaceTreeDropsInvalidatedRead(t *testing.T) {
	a, updates := asyncTreeApp(t)
	id := addSpace(t, a, "Other", map[string]string{"Note.md": "# Note"})
	sp := a.state.Get(id)
	a.treeOf(sp)
	old := awaitTreeUpdate(t, updates)
	delete(a.pageTrees, id)
	if err := os.WriteFile(filepath.Join(sp.Root, "New.md"), []byte("# New"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.treeOf(sp)
	current := awaitTreeUpdate(t, updates)
	current()
	old()
	nodes, err := a.treeOf(sp)
	if err != nil || vault.Find(nodes, "New.md") == nil {
		t.Fatal("stale scan replaced a newer folder tree")
	}
}

func TestSidebarDatabasePairingFollowsRefreshedTree(t *testing.T) {
	a, _ := newTestApp(t)
	sp := a.state.ActiveSpace()
	folder := &vault.Node{Name: "Tasks", Path: "Tasks", Dir: true}
	db := &vault.Node{Name: "Tasks.db.md", Path: "Tasks.db.md"}
	a.setTree(sp, []*vault.Node{folder, db}, nil)
	if !a.databases.isFoldedDBFolder(sp, folder) || a.databases.managedFolderFor(sp, db) != folder {
		t.Fatal("database did not own its sibling folder")
	}
	a.setTree(sp, []*vault.Node{folder}, nil)
	if a.databases.isFoldedDBFolder(sp, folder) {
		t.Fatal("removed database still hides its folder")
	}
	a.setTree(sp, []*vault.Node{db}, nil)
	if a.databases.managedFolderFor(sp, db) != nil {
		t.Fatal("removed folder still belongs to the database")
	}
}

func folderWith(t *testing.T, name string, files ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		full := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("# "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A folder dropped on + becomes a space at once; its folders are read off
// the main thread and shown when read, with no starter page written.
func TestDroppedFolderSpaceReadsTreeOffMainThread(t *testing.T) {
	a, updates := asyncTreeApp(t)
	dir := folderWith(t, "Big", "Soup.md", "deep/a/b/Note.md")
	a.spacesFromFolders([]string{dir})
	sp := a.state.ActiveSpace()
	if sp.Root != dir {
		t.Fatalf("active space at %q, want %q", sp.Root, dir)
	}
	if !a.treeLoading {
		t.Fatal("the new space's folder must be read off the main thread")
	}
	if _, err := os.Stat(filepath.Join(dir, starterFile())); err == nil {
		t.Fatal("a folder holding documents must not get a starter page")
	}
	(<-updates)()
	if a.treeLoading || vault.Find(a.tree, "Soup.md") == nil || vault.Find(a.tree, "deep/a/b/Note.md") == nil {
		t.Fatalf("read tree: loading %v, %d docs", a.treeLoading, len(a.docs))
	}
	if len(a.docs) != 2 {
		t.Fatalf("docs = %d, want 2", len(a.docs))
	}
}

// A read overtaken by a switch to another space is dropped.
func TestStaleTreeReadIsDropped(t *testing.T) {
	a, updates := asyncTreeApp(t)
	home := a.state.ActiveSpace()
	dir := folderWith(t, "Other", "Only.md")
	a.spacesFromFolders([]string{dir})
	a.switchSpace(home.ID)
	// Both reads come back; only the one for the space now active counts.
	for i := 0; i < 2; i++ {
		(<-updates)()
	}
	if a.state.ActiveSpace() != home || vault.Find(a.tree, "Only.md") != nil {
		t.Fatal("the other space's folder must not show in the active space")
	}
	if a.treeLoading {
		t.Fatal("the active space's read must finish loading")
	}
}

// The space left keeps what was read of it for the strip, so that sliding
// away from a large space does not list its folders again.
func TestLeftSpaceKeepsItsTree(t *testing.T) {
	a, updates := asyncTreeApp(t)
	home := a.state.ActiveSpace()
	dir := folderWith(t, "Big", "Soup.md")
	a.spacesFromFolders([]string{dir})
	big := a.state.ActiveSpace()
	(<-updates)()
	a.switchSpace(home.ID)
	pt, ok := a.pageTrees[big.ID]
	if !ok || vault.Find(pt.nodes, "Soup.md") == nil {
		t.Fatal("the space left must keep its read tree for the strip")
	}
}
