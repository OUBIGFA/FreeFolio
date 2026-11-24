package app

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/pdf"
)

func samplePDF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := pdf.NewWriter(&buf, "FreeFolio PDF Reader Test")
	if err := w.AddPage(pdf.Page{Image: image.NewRGBA(image.Rect(0, 0, 40, 56)), Width: pdf.A4Width, Height: pdf.A4Height,
		Text: []pdf.Text{{Value: "Reader", X: 60, Y: 90, Width: 120, Height: 18}}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A PDF opens read-only in a space and in the file browser, renders its
// reader, and is never written back.
func TestPDFOpensReadOnlyInSpaceAndFileBrowser(t *testing.T) {
	data := samplePDF(t)
	check := func(t *testing.T, a *App, full string) {
		t.Helper()
		if !a.doc.isPDF() || a.doc.loadErr != nil || a.doc.pdf.PageCount() != 1 {
			t.Fatalf("not a PDF reader: %+v", a.doc.loadErr)
		}
		if a.doc.dirty() || a.doc.save() != nil {
			t.Fatal("PDF saved")
		}
		if got, _ := os.ReadFile(full); !bytes.Equal(got, data) {
			t.Fatal("PDF changed")
		}
	}
	t.Run("space", func(t *testing.T) {
		a, tt := newTestApp(t)
		full := filepath.Join(root(a), "Paper.pdf")
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
		a.open("Paper.pdf")
		tt.Frame()
		check(t, a, full)
		if _, ok := tt.Find("PDF 阅读器"); !ok {
			t.Fatal("reader not shown")
		}
	})
	t.Run("file browser", func(t *testing.T) {
		a, tt := newTestApp(t)
		full := externalFile(t, "Loose.pdf", string(data))
		if !a.openSystemFile(full) || a.files == nil {
			t.Fatal("open")
		}
		tt.Frame()
		check(t, a, full)
	})
}

// Search and Q&A read a PDF's text, a part per page, not its bytes.
func TestPDFTextForSearchAndQA(t *testing.T) {
	data := string(samplePDF(t))
	ex, err := extractText("Paper.pdf", data)
	if err != nil || !strings.Contains(ex.Text, "Reader") {
		t.Fatalf("search text %q, %v", ex.Text, err)
	}
	page, err := QAPage("Paper.pdf", data)
	if err != nil {
		t.Fatal(err)
	}
	at, ok := page.Locate(1)
	if !ok || at.Block != "page:1" || !strings.Contains(strings.Join(page.Lines, " "), "Reader") {
		t.Fatalf("Q&A page %+v %v", page.Lines, at)
	}
}
