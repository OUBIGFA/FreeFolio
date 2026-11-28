package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/pdfedit"
	"github.com/OUBIGFA/FreeFolio/internal/space"
)

// A source file opens as one code block of its language and saves back
// exactly as typed, keeping its byte order mark and CR LF line ends.
func TestSourceFileSavesAsTyped(t *testing.T) {
	a, tt := newTestApp(t)
	full := filepath.Join(root(a), "config.yaml")
	original := "\uFEFFname: demo\r\nlist:\r\n  - a\r\n"
	if err := os.WriteFile(full, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	a.open("config.yaml")
	tt.Frame()
	d := a.doc
	if d.srcFile == nil || d.loadErr != nil || d.ed.Locked {
		t.Fatalf("not an editable source document: %v", d.loadErr)
	}
	blocks := d.ed.Committed().Blocks
	if len(blocks) != 1 || blocks[0].Kind != "code" || blocks[0].Language != "yaml" || blocks[0].Text() != "name: demo\nlist:\n  - a\n" {
		t.Fatalf("blocks %+v", blocks)
	}
	if d.dirty() {
		t.Fatal("dirty on opening")
	}
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(full); string(got) != original {
		t.Fatalf("an unchanged save rewrote the file: %q", got)
	}
	d.ed.Engine.ReplaceDocument(sourceModel("config.yaml", "name: edited\nlist:\n  - a\n"))
	if !d.dirty() {
		t.Fatal("edit not seen")
	}
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(full); string(got) != "\uFEFFname: edited\r\nlist:\r\n  - a\r\n" {
		t.Fatalf("saved %q", got)
	}
	if d.dirty() {
		t.Fatal("dirty after saving")
	}
}

// A source file not in UTF-8 shows decoded but locked: writing it back
// would change its encoding.
func TestForeignEncodingSourceIsLocked(t *testing.T) {
	gbk := []byte{0xC4, 0xE3, 0xBA, 0xC3, '\n'} // 你好 in GBK
	d := openSourceDocument(&space.Space{ID: "s", Root: t.TempDir()}, "note.txt", string(gbk))
	if !d.ed.Locked || !d.ed.ReadOnly || d.ed.Committed().Blocks[0].Text() != "你好\n" {
		t.Fatalf("locked %v, text %q", d.ed.Locked, d.ed.Committed().Blocks[0].Text())
	}
	d.ed.SetReadOnly(false)
	if !d.ed.ReadOnly {
		t.Fatal("the lock was lifted")
	}
}

// A converted file opens locked as its Markdown, is never written, and
// saves an editable .md copy beside itself.
func TestConvertedFileIsReadOnlyWithCopy(t *testing.T) {
	a, tt := newTestApp(t)
	full := filepath.Join(root(a), "Letter.rtf")
	original := []byte(`{\rtf1\ansi Hello Converted\par Second line\par}`)
	if err := os.WriteFile(full, original, 0o600); err != nil {
		t.Fatal(err)
	}
	a.open("Letter.rtf")
	tt.Frame()
	d := a.doc
	if !d.converted || d.loadErr != nil || !d.ed.Locked || !d.ed.ReadOnly {
		t.Fatalf("not a locked converted document: %v", d.loadErr)
	}
	if !strings.Contains(d.ed.Text(), "Hello Converted") {
		t.Fatalf("text %q", d.ed.Text())
	}
	d.ed.SetReadOnly(false)
	if !d.ed.ReadOnly || d.dirty() || d.save() != nil {
		t.Fatal("a converted document can be edited or saved")
	}
	if err := writeFile(d.space, d.rel, "x"); err == nil {
		t.Fatal("wrote a converted file")
	}
	if _, ok := tt.Find("存为 Markdown 副本"); !ok {
		t.Fatal("no copy button", tt.Texts())
	}
	a.saveEditableCopy(d)
	tt.Frame()
	cp, err := os.ReadFile(filepath.Join(root(a), "Letter.md"))
	if err != nil || !strings.Contains(string(cp), "Hello Converted") {
		t.Fatalf("copy %q, %v", cp, err)
	}
	if a.doc.rel != "Letter.md" || a.doc.ed.Locked {
		t.Fatalf("the copy did not open editable: %s", a.doc.rel)
	}
	if got, _ := os.ReadFile(full); !bytes.Equal(got, original) {
		t.Fatal("the original changed")
	}
}

// Search and Q&A read converted and source files as their text.
func TestFormatTextForSearchAndQA(t *testing.T) {
	rtf := `{\rtf1\ansi Quarterly Budget\par}`
	if ex, err := extractText("Plan.rtf", rtf); err != nil || !strings.Contains(ex.Text, "Quarterly Budget") {
		t.Fatalf("search %q %v", ex.Text, err)
	}
	if ex, _ := extractText("main.go", "package main\r\n"); !strings.Contains(ex.Text, "package main") {
		t.Fatalf("source search %q", ex.Text)
	}
	page, err := QAPage("Plan.rtf", rtf)
	if err != nil || !strings.Contains(strings.Join(page.Lines, " "), "Quarterly Budget") {
		t.Fatalf("Q&A %v %v", page, err)
	}
}

// Rotating a PDF's pages changes the file in place and undo puts it back;
// extracting writes a new file and leaves it.
func TestPDFPagesEdit(t *testing.T) {
	a, tt := newTestApp(t)
	data := samplePDF(t)
	full := filepath.Join(root(a), "Paper.pdf")
	if err := os.WriteFile(full, data, 0o600); err != nil {
		t.Fatal(err)
	}
	a.open("Paper.pdf")
	tt.Frame()
	a.pdfPages.openPDFPages(a.doc)
	a.pdfPages.op = pdfOpExtract
	a.pdfPages.runPDFPages()
	if a.pdfPages.err != "" {
		t.Fatal(a.pdfPages.err)
	}
	if got, _ := os.ReadFile(full); !bytes.Equal(got, data) {
		t.Fatal("extracting changed the original")
	}
	matches, _ := filepath.Glob(filepath.Join(root(a), "*.pdf"))
	if len(matches) != 2 {
		t.Fatalf("files %v", matches)
	}
	a.open("Paper.pdf")
	tt.Frame()
	a.pdfPages.openPDFPages(a.doc)
	a.pdfPages.op, a.pdfPages.degrees = pdfOpRotate, 90
	a.pdfPages.runPDFPages()
	if a.pdfPages.err != "" || a.pdfPages.open {
		t.Fatalf("rotate: %q", a.pdfPages.err)
	}
	rotated, _ := os.ReadFile(full)
	if bytes.Equal(rotated, data) {
		t.Fatal("not rotated")
	}
	if n, err := pdfedit.PageCount(t.Context(), rotated, ""); err != nil || n != 1 {
		t.Fatalf("rotated PDF broken: %d %v", n, err)
	}
	if !a.doc.isPDF() || a.doc.pdf.PageCount() != 1 {
		t.Fatal("the reader did not reload")
	}
	if len(a.undos) == 0 || a.undos[len(a.undos)-1].OnAction == nil {
		t.Fatal("no undo")
	}
	a.undos[len(a.undos)-1].OnAction()
	if got, _ := os.ReadFile(full); !bytes.Equal(got, data) {
		t.Fatal("undo did not restore the PDF")
	}
}
