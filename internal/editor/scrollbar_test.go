package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestDocumentScrollbarStaysVisibleAndDraggable(t *testing.T) {
	var text strings.Builder
	for i := range 40 {
		fmt.Fprintf(&text, "## Heading %d\n\n%s", i, strings.Repeat("Document body text.\n\n", 3))
	}
	for _, test := range []struct {
		mode     string
		readOnly bool
		dark     bool
	}{
		{mode: "none"},
		{mode: "docked"},
		{mode: "docked", readOnly: true, dark: true},
		{mode: "rail"},
		{mode: "floating"},
		{mode: "floating", readOnly: true, dark: true},
		{mode: "narrow_page_controls"},
	} {
		t.Run(fmt.Sprintf("%s/locked=%v/dark=%v", test.mode, test.readOnly, test.dark), func(t *testing.T) {
			mode := test.mode
			e := New(text.String())
			e.ReadOnly = test.readOnly
			width := 900
			if mode == "docked" {
				width = 1600
			} else if mode == "narrow_page_controls" {
				width = 320
			}
			open := true
			tt := ui.NewTester(func(c *ui.Context) {
				w, _ := c.Size()
				o := Options{Width: w, Column: 560, Theme: c.Theme(), Outline: open}
				if mode == "docked" || mode == "rail" || mode == "floating" {
					o.SetOutline = func(v bool) { open = v }
				}
				View(c, e, o)
			}, width, 650)
			tt.SetDark(test.dark)
			e.Scroll.Y = e.Scroll.MaxY / 4
			if mode == "narrow_page_controls" {
				e.Scroll.Y = e.Scroll.MaxY * .95
			}
			tt.Frame()
			if mode == "floating" {
				e.ToggleOutline()
			}
			tt.Advance(time.Second)
			tt.Frame()
			bar := ui.Scrollbar{Box: e.scrollBounds, View: e.viewport, Content: e.viewport + e.Scroll.MaxY, Offset: e.Scroll.Y, Style: ui.ScrollbarStyle{}.Resolve(ui.LightTheme())}
			start, length := bar.Thumb()
			x, y := e.scrollBounds.X+e.scrollBounds.W-7, start+length/2
			tt.Move(-10, -10)
			hidden := tt.Image().RGBAAt(int(x), int(y))
			tt.Move(float32(width)/2, 100)
			if tt.Image().RGBAAt(int(x), int(y)) == hidden {
				t.Fatal("scrollbar did not appear over the document")
			}
			tt.Move(x, y)
			tt.Advance(time.Second)
			if tt.Image().RGBAAt(int(x), int(y)) == hidden {
				t.Error("scrollbar disappeared when the pointer reached its thumb")
			}
			before, selection := e.Scroll.Y, e.Selection().Range
			textBefore := e.Text()
			tt.Press(x, y)
			tt.Move(x, y+100)
			tt.Release(x, y+100)
			if e.Scroll.Y <= before+100 {
				t.Errorf("dragging the scrollbar did not scroll the document: before=%v after=%v", before, e.Scroll.Y)
			}
			if e.Selection().Range != selection {
				t.Error("dragging the scrollbar changed the text selection")
			}
			if e.Text() != textBefore {
				t.Error("dragging the scrollbar changed the document")
			}
		})
	}
}
