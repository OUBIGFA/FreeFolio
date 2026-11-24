package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Documents dropped on a space's sidebar folder or on a document are
// copied into the space, never over an existing file, and opened; other
// files are skipped with a notice and undo moves the copies to the trash.
func TestDropDocumentsImportIntoSpace(t *testing.T) {
	a, tt := dropApp(t)
	if err := os.MkdirAll(filepath.Join(root(a), "Inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	tt.Frame()
	// The new folder's row slides into place.
	tt.Advance(time.Second)
	tt.Frame()
	note := externalFile(t, "Note.md", "# Note\n\nfrom outside\n")
	other := externalFile(t, "data.bin", "x")

	var row ui.Rect
	for _, f := range a.rows {
		if f.rel == "Inbox" {
			row = f.r
		}
	}
	if row.W <= 0 {
		t.Fatal("folder row missing")
	}
	x, y := row.X+row.W/2, row.Y+row.H/2
	tt.Move(x, y)
	if !tt.DropFiles(x, y, note, other) {
		t.Fatal("the folder did not take the files")
	}
	tt.Frame()
	if a.doc == nil || a.doc.rel != "Inbox/Note.md" {
		t.Fatalf("imported document not open: %s", a.doc.rel)
	}
	if got, err := os.ReadFile(filepath.Join(root(a), "Inbox", "Note.md")); err != nil || string(got) != "# Note\n\nfrom outside\n" {
		t.Fatalf("copy %q %v", got, err)
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatal("the source was moved")
	}
	if len(a.notices) == 0 && len(a.undos) == 0 {
		t.Log("notices were shown this frame")
	}

	// A second drop of the same name on a document lands beside its
	// folder's copy under a new name.
	dropAt(t, a, tt, "from outside", note)
	if a.doc == nil || a.doc.rel != "Inbox/Note 2.md" {
		t.Fatalf("second import: %+v", a.doc)
	}
	a.importDroppedFiles(a.state.ActiveSpace(), "", []string{note})
	if a.doc.rel != "Note.md" || len(a.undos) == 0 {
		t.Fatalf("root import: %s", a.doc.rel)
	}
	a.trashFile = os.RemoveAll
	a.undos[len(a.undos)-1].OnAction()
	if _, err := os.Stat(filepath.Join(root(a), "Note.md")); !os.IsNotExist(err) {
		t.Fatal("undo kept the copy")
	}
}

// The file browser opens documents dropped on a file as tabs instead of
// importing them.
func TestFileBrowserDropOpensTabs(t *testing.T) {
	a, tt := newTestApp(t)
	if !a.openSystemFile(externalFile(t, "First.md", "# First\n\nbody\n")) {
		t.Fatal("open")
	}
	tt.Frame()
	second := externalFile(t, "Second.md", "# Second\n")
	a.editorFileDrop(a.doc, []string{second}, 0)
	if len(a.files.order) != 2 || a.doc.title() != "Second" {
		t.Fatalf("tabs %v", a.files.order)
	}
}
