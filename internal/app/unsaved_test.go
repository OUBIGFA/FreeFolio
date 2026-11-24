package app

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// With autosave off, edits stay in memory: leaving a document asks to save,
// don't save or cancel, and the save shortcut writes at once.
func TestAutosaveToggleAndUnsavedPrompt(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	orig, err := vault.Read(root(a), "Welcome.md")
	if err != nil {
		t.Fatal(err)
	}

	a.openSettings(tabWriting)
	tt.Frame()
	if !tt.HasText("自动保存") {
		t.Fatal("no autosave switch in the writing settings")
	}
	a.toggleAutosave()
	tt.Frame()
	if !a.prefs.NoAutosave {
		t.Fatal("the switch did not turn autosave off")
	}
	if saved, err := (prefs.Store{Path: a.prefStore.Path}).Load(); err != nil || !saved.NoAutosave {
		t.Fatalf("NoAutosave not kept: %+v (%v)", saved, err)
	}
	a.settingsOpen = false
	tt.Frame()

	caretAtEnd(a, tt)
	tt.Type(" Draft without autosave.")
	tt.Frame()
	tt.Advance(4 * autosaveDelay)
	tt.Frame()
	if !a.doc.dirty() {
		t.Fatal("the edit was saved in the background")
	}
	if disk, _ := vault.Read(root(a), "Welcome.md"); disk != orig {
		t.Fatalf("autosave off still wrote the file: %q", disk)
	}

	a.open("Journal/Ideas.md")
	tt.Frame()
	if !a.unsaved.open || a.doc.rel != "Welcome.md" || !tt.HasText("保存对「Welcome」的修改吗？") {
		t.Fatalf("no prompt before leaving: open=%v doc=%q", a.unsaved.open, a.doc.rel)
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.unsaved.open || a.doc.rel != "Welcome.md" || !strings.Contains(a.doc.ed.Text(), "Draft without autosave.") {
		t.Fatalf("cancel left the document: %q", a.doc.rel)
	}

	a.open("Journal/Ideas.md")
	tt.Frame()
	if err := tt.Click("不保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.rel != "Journal/Ideas.md" {
		t.Fatalf("don't save did not go on: %q", a.doc.rel)
	}
	if disk, _ := vault.Read(root(a), "Welcome.md"); disk != orig {
		t.Fatalf("don't save wrote the file: %q", disk)
	}

	caretAtEnd(a, tt)
	tt.Type(" Explicit manual save.")
	tt.Frame()
	tt.Advance(3 * autosaveDelay)
	tt.Frame()
	if !a.doc.dirty() {
		t.Fatal("saved before the shortcut")
	}
	tt.Key(ui.Cmd, ui.KeyS)
	tt.Frame()
	if disk, _ := vault.Read(root(a), "Journal/Ideas.md"); a.doc.dirty() || !strings.Contains(disk, "Explicit manual save.") {
		t.Fatalf("the save shortcut did not write: %q", disk)
	}

	// Quitting asks too; Save writes before going on.
	caretAtEnd(a, tt)
	tt.Type(" Save on quit.")
	tt.Frame()
	quit := false
	if a.leaveDocs(a.residentDocuments(), func() { quit = true }) {
		t.Fatal("left with unsaved edits")
	}
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if disk, _ := vault.Read(root(a), "Journal/Ideas.md"); !quit || !strings.Contains(disk, "Save on quit.") {
		t.Fatalf("save on quit: proceeded %v, file %q", quit, disk)
	}
}
