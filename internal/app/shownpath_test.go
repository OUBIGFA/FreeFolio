package app

import (
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/vault"
)

// The title bar asks for the document's path in every frame: it must keep
// the resolved path instead of walking the file system again, and still
// follow the document when it moves.
func TestShownPathFollowsTheDocument(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	d := a.doc
	want := filepath.Join(root(a), "Welcome.md")
	if got := d.shownPath(); got != want {
		t.Fatalf("shown path %q, want %q", got, want)
	}
	if d.shown.path != want || d.shown.rel != "Welcome.md" {
		t.Fatalf("path not kept after a frame: %+v", d.shown)
	}
	// A kept path answers without resolving: a marker survives only then.
	d.shown.path = "kept"
	if got := d.shownPath(); got != "kept" {
		t.Fatalf("the path was resolved again: %q", got)
	}
	if err := vault.Write(root(a), "Journal/Moved.md", "# Moved\n"); err != nil {
		t.Fatal(err)
	}
	d.rel = "Journal/Moved.md"
	if got, want := d.shownPath(), filepath.Join(root(a), "Journal", "Moved.md"); got != want {
		t.Fatalf("after a move the shown path is %q, want %q", got, want)
	}
	d.space.Root = filepath.Join(filepath.Dir(root(a)), "Elsewhere")
	if got, want := d.shownPath(), filepath.Join(d.space.Root, "Journal", "Moved.md"); got != want {
		t.Fatalf("after the space moved the shown path is %q, want %q", got, want)
	}
}
