package editor

import (
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// A page longer than scrollEndsPages views shows a pair of buttons that go
// back to its top and on to its end. A shorter page is in reach of the
// wheel and has none.
//
// The buttons stand in the page's right margin, outside the text column,
// so they cover no text, however narrow the window: the margin is never
// narrower than the minimum padding, which holds them.
//
// A trip does not animate through the page. A long page lays out what is
// out of view by estimate and shapes it as it comes into view, which moves
// the page's end and shifts the view to keep what is read in place: an
// animation toward a target fixed when it started stops short, or reads
// those shifts as the reader's own scrolling and gives up. A trip instead
// sets the view at its end in every frame until the end stops moving.
const (
	scrollEndsPages = 3
	scrollEndsSize  = 28 // a button
	scrollEndsPad   = 2  // around the buttons, within the group's border
	// scrollTripFrames bounds a trip to the end that the page keeps
	// growing under, as a page loading many pictures may.
	scrollTripFrames = 90
)

// scrollTrip is a trip under way to the page's top (dir -1) or end (dir
// 1): the end as the last frame left it, and the frames it stood still.
type scrollTrip struct {
	dir     int
	lastEnd float32
	still   int
	frames  int
}

// startScrollTrip goes to the page's top (dir -1) or end (dir 1), in place
// of any glide or outline jump under way.
func (e *Editor) startScrollTrip(c *ui.Context, dir int) {
	e.scrollGlide.active = false
	e.outlineState.jump = outlineJump{}
	e.scrollTrip = scrollTrip{dir: dir, lastEnd: -1}
	e.stepScrollTrip(c)
}

// stepScrollTrip sets the view at the trip's end for this frame and ends
// the trip once the page's end has held still for two frames.
func (e *Editor) stepScrollTrip(c *ui.Context) {
	tr := &e.scrollTrip
	if tr.dir == 0 {
		return
	}
	if tr.dir < 0 {
		// The top is where it is however the rest lays out.
		e.Scroll.Y = 0
		*tr = scrollTrip{}
		return
	}
	end := e.outlineState.scrollMax
	e.Scroll.Y = end
	if end == tr.lastEnd {
		tr.still++
	} else {
		tr.still = 0
	}
	tr.lastEnd = end
	tr.frames++
	if tr.still >= 2 || tr.frames >= scrollTripFrames {
		*tr = scrollTrip{}
		return
	}
	c.After(0)
}

// scrollEndsShown reports whether the page is long enough for the buttons.
func (e *Editor) scrollEndsShown() bool {
	return !e.printing && e.viewport > 0 && e.outlineState.scrollMax+e.viewport >= scrollEndsPages*e.viewport
}

// scrollEndsH is the group's height, which the outline beside the page
// leaves free at its foot.
const scrollEndsH = 2*scrollEndsSize + 2 + 2*scrollEndsPad + 2

// scrollEnds shows the buttons at the foot of the page's right margin,
// under the outline when it stands there. The one leading
// where the view already is stays, dimmed and disabled, so the other never
// moves under the pointer.
func (e *Editor) scrollEnds(c *ui.Context) {
	if !e.scrollEndsShown() {
		return
	}
	end := e.outlineState.scrollMax
	t := c.Theme()
	pg := e.page
	w := float32(scrollEndsSize + 2*scrollEndsPad + 2)
	right := min(16, max(4, (e.padding-w)/2))
	group := ui.Column(c.Key("scroll-ends")).Absolute().Right(right).Bottom(16).Width(w).Padding(scrollEndsPad).Gap(2).
		Radius(8).Background(pg.Popover).Border(1, pg.CodeBorder).Role(ui.RoleGroup).Label(i18n.T("页面位置"))
	group.Children(func() {
		button := func(key string, icon *ui.SVG, label string, dir int, disabled bool) {
			b := ui.ButtonBase(c.Key(key)).Size(scrollEndsSize, scrollEndsSize).Radius(6).Center().Label(label).Tooltip(label).
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
				e.startScrollTrip(c, dir)
			}
		}
		button("scroll-top", iconArrowUp, i18n.T("回到顶部"), -1, e.Scroll.Y <= 1 && e.scrollTrip.dir == 0)
		button("scroll-end", iconArrowDown, i18n.T("回到底部"), 1, e.Scroll.Y >= end-1 && e.scrollTrip.dir == 0)
	})
}
