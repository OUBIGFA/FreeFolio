package editor

import (
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

func TestClickingAfterLinkPlacesCaretWithoutOpeningPopover(t *testing.T) {
	url := "https://github.com/OUBIGFA/FreeFolio/issues/20"
	e := New("- [" + url + "](" + url + ")")
	var opened string
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{Width: 820, Theme: c.Theme(), OpenLink: func(u string) { opened = u }})
	}, 820, 600)
	tt.Frame()

	b, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing editor surface")
	}
	r := e.rows[0]
	rects := r.layout.SelectionRects(ui.TextInputRange{Start: 0, End: r.leaf.End - r.leaf.Start})
	if len(rects) == 0 {
		t.Fatal("missing link selection rects")
	}
	linkRect := rects[0]
	cy := b.Y + r.y + linkRect.Y + linkRect.H/2
	afterX := b.X + r.x + linkRect.X + linkRect.W + 48
	midX := b.X + r.x + linkRect.X + linkRect.W/2

	// Clicking in blank space to the right of the link places caret at end without opening popover.
	tt.ClickAt(afterX, cy)
	tt.Frame()
	if e.linkOpen || tt.HasText("编辑链接") {
		t.Fatal("clicking after link unexpectedly opened link popover")
	}
	if e.Caret != r.leaf.End || e.Anchor != r.leaf.End {
		t.Fatalf("expected caret at line end %d, got (%d, %d)", r.leaf.End, e.Anchor, e.Caret)
	}
	tt.Type(" 已修复")
	tt.Frame()
	if e.Text() != url+" 已修复" || e.Doc.Blocks[0].Runs[len(e.Doc.Blocks[0].Runs)-1].Style.Link != "" {
		t.Fatalf("typing after link failed or extended link: %+v", e.Doc.Blocks[0].Runs)
	}

	// Cmd-clicking in blank space to the right of the link does not trigger OpenLink.
	tt.ClickAtWith(ui.Cmd, afterX+60, cy)
	tt.Frame()
	if opened != "" {
		t.Fatalf("Cmd-clicking blank space after link opened %q", opened)
	}

	// Dragging across the link selects text without opening the popover.
	tt.Press(midX-30, cy)
	tt.Move(midX+30, cy)
	tt.Frame()
	tt.Release(midX+30, cy)
	tt.Frame()
	if e.linkOpen || e.Anchor == e.Caret {
		t.Fatalf("dragging across link should select text without popover: linkOpen=%v sel=(%d,%d)", e.linkOpen, e.Anchor, e.Caret)
	}

	// Single-clicking directly inside the link glyphs opens the link popover; Cmd-clicking opens URL.
	tt.ClickAtWith(ui.Cmd, midX, cy)
	tt.Frame()
	if opened != url {
		t.Fatalf("Cmd-clicking link glyphs got %q, want %q", opened, url)
	}
	tt.Advance(time.Second)
	tt.ClickAt(midX, cy)
	tt.Frame()
	if !e.linkOpen || !tt.HasText("编辑链接") {
		t.Fatal("clicking directly inside link glyphs did not open link popover")
	}
}
