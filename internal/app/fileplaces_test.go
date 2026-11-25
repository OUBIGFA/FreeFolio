package app

import (
	"bytes"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/pdf"
	"github.com/egoist/mygo/ui"
)

func longNote() string {
	var body []string
	for i := range 200 {
		body = append(body, fmt.Sprintf("Paragraph %d of a long note.", i))
	}
	return "# Long\n\n" + strings.Join(body, "\n\n")
}

// The file browser opens a file again where it was read to, in this run
// and the next, unless the preference opens files at the top.
func TestFileBrowserReopensAtPlace(t *testing.T) {
	a, tt := newTestApp(t)
	full := externalFile(t, "Long.md", longNote())
	other := externalFile(t, "Other.md", "# Other\n")
	if !a.openSystemFile(full) {
		t.Fatal("open")
	}
	settle(tt)
	a.doc.ed.ScrollToReadingBlock(120)
	settle(tt)
	at := a.doc.ed.ReadingBlock()
	if at < 100 {
		t.Fatalf("not scrolled down: block %d", at)
	}
	a.closeFileTab(a.fileTabShown())
	if !a.openSystemFile(other) || !a.openSystemFile(full) {
		t.Fatal("reopen")
	}
	settle(tt)
	if got := a.doc.ed.ReadingBlock(); got < at-2 || got > at+2 {
		t.Fatalf("reopened at block %d, left at %d", got, at)
	}

	// The next run remembers it too.
	b, err := New(a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	b.tour = tour{}
	tt2 := ui.NewTester(b.View, 1200, 760)
	settle(tt2)
	if !b.openSystemFile(full) {
		t.Fatal("open in the next run")
	}
	settle(tt2)
	if got := b.doc.ed.ReadingBlock(); got < at-2 || got > at+2 {
		t.Fatalf("next run opened at block %d, left at %d", got, at)
	}

	// Turned off, a file opens at its top.
	b.prefs.Files.NoRememberPlace = true
	b.closeFileTab(b.fileTabShown())
	if !b.openSystemFile(full) {
		t.Fatal("open with the preference off")
	}
	settle(tt2)
	if got := b.doc.ed.ReadingBlock(); got != 0 {
		t.Fatalf("opened at block %d with the preference off", got)
	}
}

// A source file of one long code block keeps its place within the block.
func TestFileBrowserReopensSourceAtPlace(t *testing.T) {
	a, tt := newTestApp(t)
	var lines []string
	for i := range 600 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	full := externalFile(t, "long.txt", strings.Join(lines, "\n")+"\n")
	if !a.openSystemFile(full) {
		t.Fatal("open")
	}
	settle(tt)
	a.doc.ed.ToggleCodeFold(a.doc.ed.Doc.Blocks[0].ID) // a long block opens folded
	settle(tt)
	a.doc.ed.Scroll.Y = 4000
	settle(tt)
	_, frac := a.doc.ed.ReadingPlace()
	if frac <= 0 {
		t.Fatalf("no place within the block: %v", frac)
	}
	y := a.doc.ed.Scroll.Y
	a.closeFileTab(a.fileTabShown())
	if !a.openSystemFile(full) {
		t.Fatal("reopen")
	}
	settle(tt)
	if got := a.doc.ed.Scroll.Y; got < y-60 || got > y+60 {
		t.Fatalf("reopened at %v, left at %v", got, y)
	}
}

// A PDF opens again at the page it was left on.
func TestFileBrowserReopensPDFAtPage(t *testing.T) {
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, "FreeFolio Place Test")
	for i := range 5 {
		if err := w.AddPage(pdf.Page{Image: image.NewRGBA(image.Rect(0, 0, 40, 56)), Width: pdf.A4Width, Height: pdf.A4Height,
			Text: []pdf.Text{{Value: fmt.Sprintf("Page %d", i+1), X: 60, Y: 90, Width: 120, Height: 18}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	a, tt := newTestApp(t)
	full := externalFile(t, "Pages.pdf", buf.String())
	if !a.openSystemFile(full) || !a.doc.isPDF() {
		t.Fatal("open")
	}
	settle(tt)
	a.doc.pdf.GoToPage(3, true)
	settle(tt)
	if a.doc.pdf.CurrentPage != 3 {
		t.Fatalf("not on page 4: %d", a.doc.pdf.CurrentPage)
	}
	a.closeFileTab(a.fileTabShown())
	if !a.openSystemFile(full) {
		t.Fatal("reopen")
	}
	settle(tt)
	if got := a.doc.pdf.CurrentPage; got != 3 {
		t.Fatalf("reopened at page index %d, left at 3", got)
	}
}
