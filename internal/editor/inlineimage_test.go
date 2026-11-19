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
