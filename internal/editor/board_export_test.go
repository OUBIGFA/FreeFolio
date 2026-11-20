package editor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/OUBIGFA/FreeMD/internal/pdf"
	"github.com/OUBIGFA/FreeMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/font/sfnt"
)

func TestBoardSVGEmbedsUsableSubsetFonts(t *testing.T) {
	s := whiteboard.NewScene()
	e := whiteboard.NewElement(whiteboard.KindText, 0, 0, 200, 30)
	e.Text = "Hello 中文"
	e.OriginalText = e.Text
	s.Elements = append(s.Elements, e)
	css, err := BoardFontCSS(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(css, "@font-face") || !strings.Contains(css, "Xiaolai") {
		t.Fatal("portable fonts missing")
	}
	font, err := BoardFontData(100)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := whiteboard.SubsetFont(font, []rune("中文"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) >= len(font)/10 {
		t.Fatal("Chinese font not subset")
	}
	f, err := sfnt.Parse(sub)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	for _, r := range "中文" {
		g, err := f.GlyphIndex(&buf, r)
		if err != nil || g == 0 {
			t.Fatalf("subset lost %c", r)
		}
	}
}

func TestWhiteboardInlineVectorExportAndSourceRoundTrip(t *testing.T) {
	s := whiteboard.NewScene()
	e := whiteboard.NewElement(whiteboard.KindRectangle, 0, 0, 150, 90)
	s.Elements = append(s.Elements, e)
	raw, _ := s.Encode()
	source := "# Keep\n\n```excalidraw\n" + raw + "```\n"
	ed := New(source)
	if got := ExportMarkdown(ed.Doc); got != source {
		t.Fatal("unedited whiteboard source rewritten")
	}
	var output bytes.Buffer
	w := pdf.NewWriter(&output, "Board")
	RegisterBoardPDFFonts(w, s)
	page := w.NewVectorPage(600, 400)
	if err := ExportBoardVector(page, s, whiteboard.Box{X: 20, Y: 20, W: 500, H: 300}, BoardOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := w.FinishVectorPage(page); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("%PDF")) {
		t.Fatal("PDF not written")
	}
	text := document.NewBlock("code", raw)
	text.Language = "excalidraw"
	doc := document.New()
	doc.Blocks = []*document.Block{text}
	if !strings.Contains(ExportHTML(doc), "<svg") {
		t.Fatal("HTML exported JSON instead of picture")
	}
	tt := ui.NewTester(func(c *ui.Context) { View(c, ed, Options{Theme: c.Theme(), Width: 700}) }, 740, 600)
	tt.Frame()
	if len(ed.rows) == 0 {
		t.Fatal("inline scene not laid out")
	}
}
