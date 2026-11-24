package editor

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

// htmlRowOf is the laid out row of the block with the ID.
func htmlRowOf(t *testing.T, e *Editor, id string) row {
	t.Helper()
	idx, ok := e.rowIDs[id]
	if !ok {
		t.Fatal("no row for", id)
	}
	return *e.indexedRow(idx)
}

// HTML that a native block cannot hold, as a cell's color and a font's
// size, stays as written and shows what it says; the caret in it shows the
// source, and the Markdown keeps it to the byte.
func TestHTMLBlockShowsAsWritten(t *testing.T) {
	const banner = `<center><table><tr><td bgcolor=#f1f6fb><font size=4>免费使用高端模型</font></td></tr></table></center>`
	src := "# 渠道\n\n" + banner + "\n\nafter\n"
	e, tt := newTester(src)
	b := e.Doc.Blocks[1]
	if b.Kind != "raw" || b.Text() != banner {
		t.Fatalf("banner imported as %s %q", b.Kind, b.Text())
	}
	r := htmlRowOf(t, e, b.ID)
	if r.html == nil {
		t.Fatal("the banner shows its source at rest")
	}
	var fill, text bool
	cell := ui.Hex("#f1f6fb")
	for _, op := range r.html.ops {
		switch op.kind {
		case htmlFill:
			fill = fill || op.color == cell
		case htmlText:
			// Centered: the cell is narrower than the block and in its middle.
			if op.r.W < r.rect.W-200 && op.r.X > 100 {
				text = true
			}
		}
	}
	if !fill || !text {
		t.Fatalf("banner ops %+v", r.html.ops)
	}
	// The caret in the block shows its source.
	focus(e, tt)
	leaf := e.Engine.LeafAt(0)
	for _, l := range e.Doc.Leaves() {
		if l.Block.ID == b.ID {
			leaf = l
		}
	}
	e.SetSelection(leaf.Start+3, leaf.Start+3)
	tt.Frame()
	if htmlRowOf(t, e, b.ID).html != nil {
		t.Fatal("the caret in the block does not show its source")
	}
	if got := ExportMarkdown(e.Doc); got != src {
		t.Fatalf("export changed the HTML:\n%q", got)
	}
}

// A block of line breaks stands a line high for each, as in the browser.
func TestHTMLLineBreaksStandALineEach(t *testing.T) {
	e, _ := newTester("a\n\n<br>\n<br>\n\nb")
	one, _ := newTester("a\n\n<br>\n\nb")
	two := htmlRowOf(t, e, e.Doc.Blocks[1].ID).html
	single := htmlRowOf(t, one, one.Doc.Blocks[1].ID).html
	if two == nil || single == nil || two.h < single.h*1.8 || single.h < 16 {
		t.Fatalf("heights %v and %v", two, single)
	}
}

// HTML FreeFolio writes itself, as a centered paragraph, stays a native
// block, editable as one.
func TestOwnHTMLStaysNative(t *testing.T) {
	doc := ImportMarkdown(`<p align="center">居中</p>`)
	if b := doc.Blocks[0]; b.Kind != "paragraph" || b.Align != "center" {
		t.Fatalf("%s %q", b.Kind, b.Align)
	}
	if doc := ImportMarkdown(`<img src="a.png" width="200">`); doc.Blocks[0].Kind != "image" {
		t.Fatal("an image tag is an image")
	}
}

func TestCSSValues(t *testing.T) {
	for in, want := range map[string]string{"#f1f6fb": "#f1f6fb", "f1f6fb": "#f1f6fb", "#abc": "#aabbcc", "red": "#ff0000", "rgb(255, 0, 0)": "#ff0000"} {
		c, ok := cssColor(in)
		if w := ui.Hex(want); !ok || c.R != w.R || c.G != w.G || c.B != w.B {
			t.Errorf("%s: %v", in, c)
		}
	}
	if _, ok := cssColor("solid"); ok {
		t.Error("a word is no color")
	}
	if s := fontTagSize("4", 16); s != 18 {
		t.Error("font size 4", s)
	}
	if s := cssFontSize("150%", 16, 16); s != 24 {
		t.Error("150%", s)
	}
	css := parseCSS("color: Red; padding: 4px 8px !important")
	if css["color"] != "red" || cssBox(css, "padding", 16) != [4]float32{4, 8, 4, 8} {
		t.Errorf("%v", css)
	}
}

// Inline HTML keeps its exact color and size, writes them back as CSS, and
// reads them again as it wrote them.
func TestInlineHTMLExactColorAndSize(t *testing.T) {
	doc := ImportMarkdown(`前 <font color=#123abc size=5>字</font> <span style="color:#2f8f4e;font-size:20px">绿</span> 后`)
	runs := doc.Blocks[0].Runs
	var font, span bool
	for _, r := range runs {
		font = font || r.Text == "字" && r.Style.Color == "#123abc" && r.Style.Size == 1.5
		span = span || r.Text == "绿" && r.Style.Color == "#2f8f4e" && r.Style.Size == 1.25
	}
	if !font || !span {
		t.Fatalf("runs %+v", runs)
	}
	doc.ImportSource = ""
	out := ExportMarkdown(doc)
	back := ImportMarkdown(out)
	if !sameRunsSemantics(back.Blocks[0].Runs, runs) {
		t.Fatalf("round trip through %q: %+v", out, back.Blocks[0].Runs)
	}
	// Palette colors stay palette names.
	if r := ImportMarkdown(`<font color=red>x</font>`).Blocks[0].Runs[0]; r.Style.Color != "red" {
		t.Fatalf("palette color %q", r.Style.Color)
	}
}

// Images in an HTML block are inline, as in the browser: side by side in
// a centered paragraph, one under the other across a <br>.
func TestHTMLBlockImagesFlowInline(t *testing.T) {
	const row = `<p align="center"><a href="a"><img src="a.svg" alt="A"></a> <a href="b"><img src="b.svg" alt="B"></a></p>`
	const stacked = `<p align="center"><img src="a.svg" alt="A"><br><img src="b.svg" alt="B"></p>`
	bmp := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 100, 20)))
	for _, tc := range []struct {
		src  string
		side bool
	}{{row, true}, {stacked, false}} {
		e := New(tc.src + "\n\nafter\n")
		tt := ui.NewTester(func(c *ui.Context) {
			w, _ := c.Size()
			View(c, e, Options{Width: w, Theme: c.Theme(), LoadImage: func(string) ImageResource {
				return ImageResource{Bitmap: bmp, Width: 100, Height: 20}
			}})
		}, 900, 650)
		tt.Frame()
		r := htmlRowOf(t, e, e.Doc.Blocks[0].ID)
		var imgs []ui.Rect
		for _, op := range r.html.ops {
			if op.kind == htmlImage {
				imgs = append(imgs, op.r)
			}
		}
		if len(imgs) != 2 || imgs[0].W != 100 || imgs[0].H != 20 {
			t.Fatalf("%s: images %+v", tc.src, imgs)
		}
		if side := imgs[0].Y == imgs[1].Y && imgs[1].X > imgs[0].X+imgs[0].W; side != tc.side {
			t.Fatalf("%s: side by side %v, images %+v", tc.src, side, imgs)
		}
		// Centered: the left image starts well right of the block's start.
		if imgs[0].X < 200 {
			t.Fatalf("%s: not centered %+v", tc.src, imgs)
		}
	}
}
