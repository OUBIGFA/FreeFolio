package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestQuickNotePreservesMainDocumentAndSavesOnClose(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	before := a.doc
	var made *fakeWindow
	a.newWindow = func(title string, view func(*ui.Context), close func() bool) windowHandle {
		made = &fakeWindow{title: title, view: view, close: close}
		return made
	}
	a.openQuickNote()
	if made == nil || a.quick.window == nil || a.doc != before {
		t.Fatal("quick capture changed main document")
	}
	d := a.quick.window.doc
	d.ed.ReplaceText(nil, "captured text")
	if !made.close() {
		t.Fatal("quick note failed to save")
	}
	data, e := os.ReadFile(d.fullPath())
	if e != nil || string(data) == "" {
		t.Fatalf("quick note missing: %q %v", data, e)
	}
	if a.doc != before {
		t.Fatal("closing quick capture changed main workspace")
	}
}
