package editor

import (
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// A page longer than scrollEndsPages views shows, at the pane's bottom
// right, a pair of buttons that go back to its top and on to its end. A
// shorter page is in reach of the wheel and has none.
const scrollEndsPages = 3

// scrollEnds shows the buttons over the pane, clear of the outline beside
// the page when it is open. The one leading where the view already is
// stays, dimmed and disabled, so the other never moves under the pointer.
func (e *Editor) scrollEnds(c *ui.Context) {
	end := e.outlineState.scrollMax
	if e.printing || e.viewport <= 0 || end+e.viewport < scrollEndsPages*e.viewport {
		return
	}
	t := c.Theme()
	pg := e.page
	right := float32(16)
	if s := &e.outlineState; s.mode == outlineBeside && e.OutlineOpen() {
		right += s.width
	}
	group := ui.Column(c.Key("scroll-ends")).Absolute().Right(right).Bottom(16).Padding(2).Gap(2).
		Radius(8).Background(pg.Popover).Border(1, pg.CodeBorder).Role(ui.RoleGroup).Label(i18n.T("页面位置"))
	group.Children(func() {
		button := func(key string, icon *ui.SVG, label string, disabled bool, to float32) {
			b := ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(6).Center().Label(label).Tooltip(label).
				KeepFocus().Cursor(ui.CursorPointer).Disabled(disabled)
			ink := t.Text.Alpha(.55)
			if disabled {
				ink = t.Text.Alpha(.22)
			} else if b.Hovered() {
				b.Background(t.Text.Alpha(.06))
				ink = t.Text.Alpha(.85)
			}
			b.Children(func() { ui.Icon(c, icon).FontSize(16).TextColor(ink) })
			if b.Clicked() {
				// jumpTo leaves 24 above its target, which the top
				// clamps away and the end needs added back; the jump's
				// id names no heading, so the outline keeps its own.
				e.jumpTo(c, key, to+24)
			}
		}
		button("scroll-top", iconArrowUp, i18n.T("回到顶部"), e.Scroll.Y <= 1, 0)
		button("scroll-end", iconArrowDown, i18n.T("回到底部"), e.Scroll.Y >= end-1, end)
	})
}
