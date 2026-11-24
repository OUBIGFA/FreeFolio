package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
)

// Full width spans the text column across the page, keeping its margins,
// and toggles back to the width chosen before it.
func TestFullWidthPageToggles(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.setPageWidth("narrow")
	tt.Frame()
	narrow := a.doc.ed.Padding()
	a.toggleFullWidth()
	tt.Frame()
	if a.prefs.Page != "full" || a.doc.ed.Padding() >= narrow {
		t.Fatalf("full width: page %q padding %v (narrow %v)", a.prefs.Page, a.doc.ed.Padding(), narrow)
	}
	if got := editor.TextPadding(1000, editor.FullColumn); got != 80 {
		t.Fatalf("full width padding %v", got)
	}
	if dir := os.Getenv("FREEFOLIO_SNAPSHOT"); dir != "" {
		savePNG(t, filepath.Join(dir, "page-full-width.png"), tt)
	}
	a.toggleFullWidth()
	tt.Frame()
	if a.prefs.Page != "narrow" || a.doc.ed.Padding() != narrow {
		t.Fatalf("back: page %q padding %v", a.prefs.Page, a.doc.ed.Padding())
	}
	// Markdown source has no column to widen.
	a.doc.toggleMode()
	a.toggleFullWidth()
	if a.prefs.Page != "narrow" {
		t.Fatal("source mode toggled full width")
	}
}
