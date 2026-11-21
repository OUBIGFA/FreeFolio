package editor

import (
	"image"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/egoist/mygo/ui"
)

// An image within text shows in the line at its size, its syntax kept in
// the text: the caret in it shows the source, and the text writes it back
// as written.
func TestInlineImageShowsInTheLine(t *testing.T) {
	const url = "https://pbs.twimg.com/media/HKTj795WUAENStm?format=jpg&name=900x900"
	src := "行内：「![](" + url + ")」后\n"
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	bmp := ui.NewBitmap(img)
	e := New(src)
	load := func(s string) ImageResource {
		if s != url {
			t.Errorf("loaded %q", s)
		}
		return ImageResource{Bitmap: bmp, Width: 40, Height: 30}
	}
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), LoadImage: load})
	}, 900, 650)
	tt.Frame()
	objects := e.rows[0].layout.objects
	if len(objects) != 1 || !objects[0].image || objects[0].picture == nil || objects[0].rect.W != 40 || objects[0].rect.H != 30 {
		t.Fatalf("objects %+v", objects)
	}
	// The caret in the syntax shows the source.
	focus(e, tt)
	at := document.Len("行内：「![")
	e.SetSelection(at, at)
	tt.Frame()
	if len(e.rows[0].layout.objects) != 0 {
		t.Fatal("the caret in the image does not show its source")
	}
	// Edited elsewhere, the paragraph writes the image back unescaped.
	e.SetSelection(e.Len(), e.Len())
	e.ReplaceText(nil, "。")
	tt.Frame()
	if got := ExportMarkdown(e.Doc); !strings.Contains(got, "「![]("+url+")」后。") {
		t.Fatalf("export %q", got)
	}
}

// While an image loads, or when it cannot show, a chip names it.
func TestInlineImageChipWithoutPicture(t *testing.T) {
	e, tt := newTester("see ![logo](assets/logo.png) here")
	tt.Frame()
	objects := e.rows[0].layout.objects
	if len(objects) != 1 || objects[0].picture != nil || objects[0].label == nil {
		t.Fatalf("objects %+v", objects)
	}
	// Literal brackets in plain text still escape.
	doc := ImportMarkdown(`a \[b\] c`)
	if got := ExportMarkdown(doc); got != "a \\[b\\] c\n" && got != "a \\[b] c\n" {
		t.Fatalf("plain brackets %q", got)
	}
}

// README badges, written as linked <img> tags one per line inside a
// paragraph, show as images side by side, as GitHub and mark2 show them;
// the unedited paragraph writes its HTML back as written.
func TestHTMLBadgeParagraphShowsAsARowOfImages(t *testing.T) {
	src := "<a href=\"mygo.json\"><img src=\"https://img.shields.io/badge/base-ArcMD-F21158\" alt=\"基于 ArcMD\"></a>\n" +
		"<a href=\"LICENSE\"><img src=\"assets/my logo.svg\" alt=\"MIT [许可]\" width=\"80\"></a>\n"
	img := image.NewRGBA(image.Rect(0, 0, 100, 20))
	bmp := ui.NewBitmap(img)
	e := New(src)
	var loaded []string
	load := func(s string) ImageResource {
		loaded = append(loaded, s)
		return ImageResource{Bitmap: bmp, Width: 100, Height: 20}
	}
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), LoadImage: load})
	}, 900, 650)
	tt.Frame()
	b := e.Doc.Blocks[0]
	if b.Kind != "paragraph" || !strings.Contains(b.Text(), "![基于 ArcMD](https://img.shields.io/badge/base-ArcMD-F21158)") ||
		!strings.Contains(b.Text(), "![MIT 许可|80](assets/my%20logo.svg)") {
		t.Fatalf("imported %s %q", b.Kind, b.Text())
	}
	if runs := document.SliceRuns(b.Runs, 0, 1); len(runs) == 0 || runs[0].Style.Link != "mygo.json" {
		t.Fatalf("the badge keeps its link: %+v", runs)
	}
	objects := e.rows[0].layout.objects
	if len(objects) != 2 || objects[0].picture == nil || objects[1].picture == nil {
		t.Fatalf("objects %+v, loaded %q", objects, loaded)
	}
	var rects []ui.Rect
	for _, o := range e.rows[0].layout.Objects() {
		rects = append(rects, o.Rect)
	}
	if len(rects) != 2 || rects[0].Y+rects[0].H != rects[1].Y+rects[1].H || rects[1].X <= rects[0].X+rects[0].W {
		t.Fatalf("badges not side by side: %+v", rects)
	}
	if objects[1].rect.W != 80 {
		t.Fatalf("width attribute: %+v", objects[1].rect)
	}
	if got := ExportMarkdown(e.Doc); got != src {
		t.Fatalf("export %q", got)
	}
}

// Text between images keeps its line breaks: only a paragraph of images
// alone flows as a row.
func TestImageRowNeedsImagesAlone(t *testing.T) {
	text := "![a](a.png)\n![b](b.png)"
	objs := []inlineObject{{start: 0, end: 11, image: true}, {start: 12, end: 23, image: true}}
	if !imageRow(text, objs) {
		t.Fatal("two images on their own lines form a row")
	}
	if imageRow("x\n"+text, []inlineObject{{start: 2, end: 13, image: true}, {start: 14, end: 25, image: true}}) {
		t.Fatal("text before the images keeps its line")
	}
	if imageRow("![a](a.png)", objs[:1]) {
		t.Fatal("a single image is no row")
	}
}
