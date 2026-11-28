package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// The sidebar used to build every row of every open folder in every frame:
// a space with a couple of thousand rows open took some 10 ms a frame, which
// typing, hovering and scrolling all paid. A rowWindow builds only the rows
// near what the list shows, and one empty box of their height in place of
// each run of the others, so the list keeps its length, its scroll range and
// every row its place. Drags build every row, as before: their slots and
// drops measure rows anywhere in the list.

// rowEmptyHeight is the height of an open empty folder's "空文件夹" line.
const rowEmptyHeight = 28

// rowWindow decides, row by row, what one section of a space's list builds.
type rowWindow struct {
	a       *App
	c       *ui.Context
	space   string
	section string
	now     time.Time
	// on is false while every row is built.
	on bool
	// top is where the section's first row was in the frame laid out
	// before, in window DIPs, and lo and hi the band rows are built in.
	top, lo, hi float32
	// off is the next row's offset in the section, index its place in the
	// section's order, and last the place of the row skip last let through.
	off         float32
	index, last int
	// run and runRows are the rows left out since the last row built.
	run     float32
	runRows int
	spacers int
}

// rowFocus is the place of the sidebar row that has the keyboard focus.
type rowFocus struct {
	space, section string
	index          int
	set            bool
}

// spacedRow is when a row was last left out, or, once it is built again,
// when that happened (scrolledIn).
type spacedRow struct {
	spaced bool
	at     time.Time
}

// rowMargin is the least distance above and below the visible list within
// which rows are built, so that a frame's scroll lands on rows already
// there.
const rowMargin = 400

// beginRows starts the rows of a section, the element col, in the list tree
// that shows the space's scroll state ss.
func (a *App) beginRows(c *ui.Context, spaceID, section string, col, tree ui.Element, ss *ui.ScrollState) *rowWindow {
	w := &rowWindow{a: a, c: c, space: spaceID, section: section, now: c.Now(), last: -1}
	w.on = !a.rowsAll && !a.listDragging && !a.dragGrab.active && !a.dragSettle.active &&
		a.carried == nil && a.carrying == nil && !a.slot.active && a.listDrop == nil
	view := tree.Bounds()
	if view.H <= 0 {
		// The list was not laid out yet, as at launch: the window's height
		// stands for it, from where it is scrolled to.
		_, h := c.Size()
		view = ui.Rect{H: h}
	}
	margin := max(view.H, rowMargin)
	w.lo, w.hi = view.Y-margin, view.Y+view.H+margin
	if b := col.Bounds(); b.W > 0 {
		w.top = b.Y
	} else {
		// Taken as the list's top: what sits above the section is short,
		// the margin covers it, and the spacers' InView builds the frame
		// again with the section's place.
		w.top = view.Y - ss.Y
	}
	if a.spaced == nil {
		a.spaced = map[place]spacedRow{}
	} else if len(a.spaced) > 1<<14 {
		clear(a.spaced) // rows long gone; at worst one fades in once more
	}
	return w
}

// skip reports whether the row of the section called key, h DIPs high,
// is left out this frame. Either way it takes its place in the section.
func (w *rowWindow) skip(key string, h float32) bool {
	if w == nil {
		return false
	}
	index := w.index
	w.index++
	y := w.top + w.off
	w.off += h + rowGap
	if !w.on || y+h >= w.lo && y <= w.hi || w.keeps(key, index) {
		w.flush()
		w.last = index
		return false
	}
	if w.runRows > 0 {
		w.run += rowGap
	}
	w.run += h
	w.runRows++
	w.a.spaced[place{w.space, key}] = spacedRow{spaced: true, at: w.now}
	return true
}

// keeps reports whether a row out of view is built all the same: the one
// being renamed, and the focused one with its neighbors, which the keys
// move to next.
func (w *rowWindow) keeps(key string, index int) bool {
	a := w.a
	if a.renaming == key && a.renameSpace == w.space {
		return true
	}
	f := a.rowFocus
	return f.set && f.space == w.space && f.section == w.section && index >= f.index-2 && index <= f.index+2
}

// flush builds one box as high as the rows left out since the last row
// built. Should a scroll bring it into view before the rows are there, as a
// drag of the scroll bar does, InView builds the next frame at once, which
// builds them.
func (w *rowWindow) flush() {
	if w == nil || w.runRows == 0 {
		return
	}
	ui.Box(w.c.Key(fmt.Sprintf("rows-%s-%d", w.section, w.spacers))).Height(w.run).Shrink(0).InView()
	w.spacers++
	w.run, w.runRows = 0, 0
}

// focused notes that the row skip let through last has the keyboard focus.
func (w *rowWindow) focused() {
	if w != nil && w.last >= 0 {
		w.a.rowFocusNow = rowFocus{space: w.space, section: w.section, index: w.last, set: true}
	}
}

// scrolledIn reports whether the row called key of the space is built
// because the list scrolled to it, rather than because it came into the
// list: it then shows at once, without the fade of a row coming in.
func (a *App) scrolledIn(spaceID, key string, now time.Time) bool {
	k := place{spaceID, key}
	e, ok := a.spaced[k]
	switch {
	case !ok:
		return false
	case e.spaced && !e.at.Equal(now):
		a.spaced[k] = spacedRow{at: now}
		return true
	case !e.spaced && e.at.Equal(now):
		return true // another pass of the same frame
	case !e.spaced:
		delete(a.spaced, k)
	}
	return false
}
