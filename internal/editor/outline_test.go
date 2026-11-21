package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Wide blocks must not hide behind the outline, including on locked pages.
func TestOutlineCollapsesAroundVisibleWideTable(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		for _, column := range []float32{560, 820, 1100} {
			t.Run(fmt.Sprintf("locked=%t/column=%g", readOnly, column), func(t *testing.T) {
				table := "| A | B | C | D |\n| --- | --- | --- | --- |\n|" + strings.Repeat(strings.Repeat("wide text ", 8)+"|", 4) + "\n\n"
				e := New("# Title\n\n## Before\n\n" + strings.Repeat("Body text.\n\n", 30) + "## Table\n\n" + table + "## After\n\n" + strings.Repeat("More text.\n\n", 30))
				e.ReadOnly = readOnly
				open := true
				tt := ui.NewTester(func(c *ui.Context) {
					w, _ := c.Size()
					View(c, e, Options{Width: w, Column: column, Theme: c.Theme(), Outline: open, SetOutline: func(v bool) { open = v }})
				}, 1600, 650)
				tt.Frame()
				tt.Frame()
				if !e.OutlineOpen() {
					t.Fatal("an offscreen table collapsed the outline")
				}
				var tableRect ui.Rect
				for _, d := range e.decorations {
					if d.kind == "table" {
						tableRect = d.rect
						break
					}
				}
				if tableRect.W <= 1300 || tableRect.Y <= 650 {
					t.Fatalf("expected a wide offscreen table, got %+v", tableRect)
				}
				padding := e.padding
				e.Scroll.Y = tableRect.Y - 100
				tt.Frame()
				tt.Advance(time.Second)
				tt.Frame()
				if e.OutlineOpen() || tt.HasText("收起目录") {
					t.Fatal("the outline still covers the visible table")
				}
				if !open || e.padding != padding {
					t.Fatal("automatic collapse changed the saved preference or writing column")
				}
				if err := tt.Click("展开目录"); err != nil {
					t.Fatal(err)
				}
				tt.Advance(time.Second)
				tt.Frame()
				if !e.OutlineOpen() || !tt.HasText("跳到 Table") {
					t.Fatal("the collapsed outline cannot be opened temporarily")
				}
				tt.Move(100, 400)
				tt.Key(0, ui.KeyEscape)
				e.Scroll.Y = 0
				tt.Frame()
				tt.Advance(time.Second)
				tt.Frame()
				if !e.OutlineOpen() {
					t.Fatal("the outline did not return after the wide table left the viewport")
				}
				e.Scroll.Y = tableRect.Y - 100
				tt.Frame()
				if e.OutlineOpen() {
					t.Fatal("returning to the wide table kept the outline open")
				}
				tt.SetSize(2800, 650)
				tt.Advance(time.Second)
				tt.Frame()
				if !e.OutlineOpen() {
					t.Fatal("the outline did not use the clear margin after widening the window")
				}
			})
		}
	}
}
