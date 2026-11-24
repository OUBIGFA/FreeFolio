package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The toolbar's close button shows the space's home page, and the space
// stays there when the user leaves it and comes back, or relaunches.
func TestCloseDocumentShowsSpaceHome(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	settle(tt)
	if a.doc == nil {
		t.Fatal("the welcome document is not open")
	}
	if err := tt.Click("关闭文档"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.doc != nil || sp.LastOpen != "" {
		t.Fatalf("closed: doc %v, last open %q", a.doc, sp.LastOpen)
	}
	if !tt.HasText("新建文档") {
		t.Fatal("the space's home page is not shown")
	}
	if _, ok := tt.Find("关闭文档"); ok {
		t.Fatal("the close button stays without a document")
	}
	other := filepath.Join(t.TempDir(), "Work")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	work := a.state.Add("Work", other)
	a.switchSpace(work.ID)
	settle(tt)
	a.switchSpace(sp.ID)
	settle(tt)
	if a.doc != nil {
		t.Fatalf("coming back reopened %q", a.doc.rel)
	}
}

// ⌘W closes a pinned document too: it leaves the open pins and stays pinned.
func TestCloseShortcutClosesPinnedDocument(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	a.togglePin(sp, "Welcome.md")
	settle(tt)
	if !a.isOpen(sp, "Welcome.md") {
		t.Fatal("the pinned welcome document is not open")
	}
	tt.Key(ui.Cmd, ui.KeyW)
	settle(tt)
	if a.doc != nil || a.isOpen(sp, "Welcome.md") || !sp.IsPinned("Welcome.md") {
		t.Fatalf("⌘W: doc %v, open %v, pinned %v", a.doc, a.isOpen(sp, "Welcome.md"), sp.IsPinned("Welcome.md"))
	}
}

// A folder dropped from the file manager on the space bar's + becomes a
// space named as the folder, in place, with a glyph and a color; dropping
// it again goes to that space instead of making another.
func TestFolderDropOnPlusMakesSpace(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	dir := filepath.Join(t.TempDir(), "Recipes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Soup.md"), []byte("# Soup"), 0o644); err != nil {
		t.Fatal(err)
	}
	drop := func(paths ...string) {
		t.Helper()
		r, ok := tt.Find("新建空间")
		if !ok {
			t.Fatal("no + on the space bar")
		}
		if !tt.DropFiles(r.X+r.W/2, r.Y+r.H/2, paths...) {
			t.Fatal("the + did not take the drop")
		}
		settle(tt)
	}
	before := len(a.state.Spaces)
	drop(dir)
	sp := a.state.ActiveSpace()
	if len(a.state.Spaces) != before+1 || sp.Name != "Recipes" || sp.Root != dir {
		t.Fatalf("dropped folder: %d spaces, active %q at %q", len(a.state.Spaces), sp.Name, sp.Root)
	}
	if sp.Symbol == "" || sp.Theme == nil || len(sp.Theme.Colors) == 0 || a.spaceForm.open {
		t.Fatalf("new space: symbol %q, theme %v, form open %v", sp.Symbol, sp.Theme, a.spaceForm.open)
	}
	if _, err := os.Stat(filepath.Join(dir, "Soup.md")); err != nil {
		t.Fatal("the folder's documents must stay where they are")
	}
	a.switchSpace(a.state.Spaces[0].ID)
	settle(tt)
	drop(dir, filepath.Join(dir, "Soup.md"))
	if len(a.state.Spaces) != before+1 || a.state.ActiveSpace() != sp {
		t.Fatal("dropping a space's folder again must switch to it, and a file makes no space")
	}
}
