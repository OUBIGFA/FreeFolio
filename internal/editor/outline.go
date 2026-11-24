package editor

import (
	"sort"
	"strconv"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// The outline docks to the right of the writing surface. Narrow panes open
// it over the page without changing the user's saved docking preference.

const (
	// outlineWidth is the room the open outline takes.
	outlineWidth = 248
	// Leave space for the document's right-margin controls beside a docked outline.
	outlineMinWidth = 136
	outlineGutter   = 40
	// outlineRow is the height of an entry.
	outlineRow = 28
	// outlineAll is how many headings the outline builds every row for;
	// a longer one builds those in view and outlineSpare on either side.
	outlineAll   = 36
	outlineSpare = 12
	// outlineIndent is how far each heading level steps in.
	outlineIndent = 12
	// outlineSlide is how long the page takes to slide over.
	outlineSlide = 220 * time.Millisecond
	// jumpDuration is how long a click on an entry scrolls for.
	jumpDuration = 320 * time.Millisecond
)

type outlineMode int

const (
	outlineOff      outlineMode = iota // no outline: no headings, or no host
	outlineBeside                      // the outline takes room beside the page
	outlineFloating                    // too narrow: it floats over the page
)

type outlineState struct {
	open     bool // the host's choice, for outlineBeside
	set      func(bool)
	mode     outlineMode
	floating bool    // the floating outline is showing
	width    float32 // panel width available inside this editor pane
	// buttonTop is where both the « and the ≡ buttons sit, so collapsing
	// and opening keep the pointer on the button.
	buttonTop float32
	scrollMax float32
	jump      outlineJump
	// list is where the entries are scrolled; dragging is the heading
	// being dragged, whose row is built while it is out of view so that
	// the drag goes on (outlineList).
	list     ui.ScrollState
	dragging string
	// panelHovered is whether the pointer was over the floating panel in
	// the last frame, which keeps the panel open as it leaves the rail.
	panelHovered bool

	headingsDoc   *document.Document
	headingsRev   uint64
	headingsKnown bool
	headingsHas   bool
}

// outlineJump scrolls the page to a heading picked in the outline.
type outlineJump struct {
	id             string
	from, to, last float32
	start          time.Time
}

// hasHeadings reports whether the outline has more than the page title to
// list.
func (e *Editor) hasHeadings() bool {
	s := &e.outlineState
	rev := e.Revision()
	if s.headingsKnown && s.headingsDoc == e.Doc {
		if s.headingsRev == rev {
			return s.headingsHas
		}
		if change, ok := e.TextChangeSince(s.headingsRev); ok {
			if leaf, exists := e.Engine.LeafByID(change.ID); exists && leaf.Block.Kind != "heading" {
				s.headingsRev = rev
				return s.headingsHas
			}
		}
	}
	has := false
	for i, b := range e.Doc.Blocks {
		if i > 0 && b.Kind == "heading" && b.Text() != "" {
			has = true
			break
		}
	}
	s.headingsDoc, s.headingsRev, s.headingsKnown, s.headingsHas = e.Doc, rev, true, has
	return has
}

// hasBookmarks reports whether the outline has bookmarks to list.
func (e *Editor) hasBookmarks() bool {
	if len(e.bookmarks.marks) == 0 || e.bookmarks.toggle == nil {
		return false
	}
	for _, b := range e.Doc.Blocks {
		if _, ok := e.bookmarks.marks[b.ID]; ok {
			return true
		}
	}
	return false
}

// fitOutline uses the clear right margin of the visible content. Wide blocks
// can extend past the writing column; then only a temporary overlay fits.
func (e *Editor) fitOutline(o Options) {
	s := &e.outlineState
	s.open, s.set = o.Outline, o.SetOutline
	s.mode = outlineOff
	if s.set == nil || !(e.hasHeadings() || e.hasBookmarks()) {
		s.floating, s.panelHovered = false, false
		return
	}
	margin := (o.Width - pageColumn(o.Width, o.Column)) / 2
	// Query the existing viewport indexes after layout and scroll updates, so
	// offscreen tables neither close the outline nor require a document scan.
	for i := range e.visibleRows(0) {
		r := e.indexedRow(i).rect
		margin = min(margin, o.Width-r.X-r.W)
	}
	for i := range e.visibleDecorations(0) {
		r := e.indexedDecoration(i).rect
		margin = min(margin, o.Width-r.X-r.W)
	}
	available := margin - outlineGutter
	s.mode = outlineFloating
	s.width = min(outlineWidth, o.Width)
	if available >= outlineMinWidth {
		s.mode = outlineBeside
		s.width = min(outlineWidth, available)
		s.floating, s.panelHovered = false, false
	}
	s.buttonTop = pageTop + 12
	if margin < 96 {
		s.buttonTop = 12
	}
}

// OutlineOpen reports whether the outline shows.
func (e *Editor) OutlineOpen() bool {
	s := &e.outlineState
	return s.mode == outlineBeside && s.open || s.mode == outlineFloating && s.floating
}

// ToggleOutline opens or collapses the outline: beside the page it is the
// reader's lasting choice, over a narrow page it shows until dismissed.
func (e *Editor) ToggleOutline() {
	s := &e.outlineState
	switch s.mode {
	case outlineBeside:
		if s.set != nil {
			s.set(!s.open)
			s.open = !s.open
		}
	case outlineFloating:
		s.floating = !s.floating
	}
}

// jumpTo scrolls the page to y, easing out.
func (e *Editor) jumpTo(c *ui.Context, id string, y float32) {
	e.scrollGlide.active = false
	to := min(max(0, y-24), e.outlineState.scrollMax)
	if c.Preferences().ReduceMotion {
		e.Scroll.Y = to
		return
	}
	e.outlineState.jump = outlineJump{id: id, from: e.Scroll.Y, to: to, last: e.Scroll.Y, start: c.Now()}
	c.After(0)
}

// stepJump moves a jump on; scrolling by hand stops it.
func (e *Editor) stepJump(c *ui.Context) {
	j := &e.outlineState.jump
	if j.id == "" {
		return
	}
	if d := e.Scroll.Y - j.last; d > 1 || d < -1 {
		j.id = ""
		return
	}
	t := float32(c.Now().Sub(j.start)) / float32(jumpDuration)
	if t >= 1 {
		e.Scroll.Y = j.to
		j.id = ""
		return
	}
	t = 1 - (1-t)*(1-t)*(1-t)
	e.Scroll.Y = j.from + (j.to-j.from)*t
	j.last = e.Scroll.Y
	c.After(0)
}

// The outline is anchored to the pane, independently of document scrolling.
func (e *Editor) outlineView(c *ui.Context, pane ui.Element) {
	s := &e.outlineState
	if s.mode == outlineOff {
		return
	}
	if s.mode == outlineFloating {
		e.floatingOutline(c, pane)
		return
	}
	t := c.Theme()
	// Keep the full scrollbar hit area clear, including while the outline fades.
	// An overlay in this lane steals hover and presses from the page scroll.
	barWidth := (ui.ScrollbarStyle{}).Resolve(t).TrackWidth
	target := float32(0)
	if e.OutlineOpen() {
		target = 1
	}
	shown := pane.Animate("outline-open", target, outlineSlide)
	if shown < 1 {
		open := ui.ButtonBase(c.Key("outline-open")).Absolute().Right(barWidth+16).Top(s.buttonTop).Size(28, 28).Radius(6).Center().
			Label(i18n.T("展开目录")).Tooltip(i18n.T("展开目录")).KeepFocus().Cursor(ui.CursorPointer).Opacity(1 - shown)
		color := t.Text.Alpha(.55)
		if open.Hovered() {
			open.Background(t.Text.Alpha(.06))
			color = t.Text.Alpha(.85)
		}
		open.Children(func() { ui.Icon(c, iconOutline).FontSize(18).TextColor(color) })
		if open.Clicked() {
			e.ToggleOutline()
		}
	}
	if shown <= 0 {
		return
	}
	// The page position buttons stand at the margin's foot, under the
	// outline's list.
	bottom := float32(16)
	if e.scrollEndsShown() {
		bottom += scrollEndsH + 16
	}
	panel := ui.Column(c.Key("outline")).Absolute().Right(barWidth).Top(0).Width(s.width).Height(e.viewport).
		Padding(s.buttonTop, 16, bottom, 12).Gap(8).Opacity(shown)
	panel.Children(func() {
		close := ui.ButtonBase(c.Key("outline-close")).Size(28, 28).Radius(6).Center().AlignSelf(ui.End).
			Label(i18n.T("收起目录")).Tooltip(i18n.T("收起目录")).KeepFocus().Cursor(ui.CursorPointer)
		color := t.Text.Alpha(.55)
		if close.Hovered() {
			close.Background(t.Text.Alpha(.06))
			color = t.Accent
		}
		close.Children(func() { ui.Icon(c, iconChevronsRight).FontSize(18).TextColor(color) })
		if close.Clicked() {
			e.ToggleOutline()
		}
		e.outlineList(c)
	})
}

// Over a narrow page the outline is a rail at the page's right edge: a
// short line per heading, stepped in by level, the section being read in
// the accent color. Hovering the rail opens the outline as a small panel
// as tall as its entries, on a hairline border without a heavy shadow; a
// click on the rail keeps it open until a press outside or Escape.
const (
	railStepMax   = 10 // the most room a heading's line takes on the rail
	railStepMin   = 4
	railLineMax   = 16 // the line of a top-level heading
	railLineMin   = 6
	railWidth     = 28
	floatPanelMax = 0.6 // of the view's height
	floatFade     = 150 * time.Millisecond
)

func (e *Editor) floatingOutline(c *ui.Context, pane ui.Element) {
	s := &e.outlineState
	t := c.Theme()
	right := max(float32(6), (ui.ScrollbarStyle{}).Resolve(t).TrackWidth)
	n := len(e.outline)
	active := e.activeOutline()
	top := e.outlineTopLevel()
	// The rail takes at most half the view; a longer outline shows the
	// lines around the section being read.
	railMax := max(80, e.viewport*0.5)
	step := min(float32(railStepMax), max(railStepMin, railMax/float32(max(n, 1))))
	count := min(n, max(1, int(railMax/step)))
	from := min(max(0, active-count/2), n-count)
	rail := ui.ButtonBase(c.Key("outline-open")).Absolute().Right(right).Top(s.buttonTop).
		Width(railWidth).Height(float32(count)*step + 12).
		Label(i18n.T("展开目录")).KeepFocus().Cursor(ui.CursorPointer).Expanded(s.floating)
	railHovered := rail.Hovered()
	rail.Draw(func(p *ui.Painter, r ui.Rect) {
		for i := range count {
			item := e.indexedOutline(from + i)
			w := max(float32(railLineMin), railLineMax-float32(min(item.level-top, 4))*3)
			color := t.Text.Alpha(.22)
			if railHovered {
				color = t.Text.Alpha(.38)
			}
			if from+i == active {
				color = t.Accent
			}
			y := r.Y + 6 + float32(i)*step + step/2 - 1
			p.Fill(ui.Rect{X: r.X + r.W - 6 - w, Y: y, W: w, H: 2}, color, 1)
		}
	})
	if rail.Clicked() {
		s.floating = !s.floating
	}
	open := s.floating || railHovered || s.panelHovered
	// Animate goes at once when the desktop asks for less motion.
	shown := pane.Animate("outline-float", boolFloat(open), floatFade)
	if shown <= 0 {
		s.panelHovered = false
		return
	}
	// As tall as its entries, within floatPanelMax of the view: the list
	// scrolls past that.
	panel := ui.Column(c.Key("outline")).Absolute().Right(right).Top(s.buttonTop-6).
		Width(min(float32(outlineWidth), s.width-right-6)).MaxHeight(max(120, e.viewport*floatPanelMax)).Padding(6).Radius(10).
		Background(e.page.Popover).Border(1, t.Text.Alpha(.12)).KeepFocus().
		Role(ui.RoleDialog).Label(i18n.T("目录")).Opacity(shown)
	panel.Children(func() { e.outlineList(c) })
	s.panelHovered = panel.Hovered()
	if s.floating && !railHovered && panel.PressedOutside() || s.floating && c.Shortcut(0, ui.KeyEscape) {
		s.floating = false
	}
}

func boolFloat(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// activeOutline is the entry of the section being read, or of the
// heading a jump goes to.
func (e *Editor) activeOutline() int {
	active := max(0, sort.Search(len(e.outline), func(i int) bool { return e.indexedOutline(i).y > e.Scroll.Y+96 })-1)
	if e.outlineState.jump.id != "" {
		// A jump marks where it goes at once, not each heading it passes.
		for i, item := range e.outline {
			if item.id == e.outlineState.jump.id {
				active = i
			}
		}
	}
	return active
}

// outlineTopLevel is the level of the outline's highest heading, from
// which the entries step in.
func (e *Editor) outlineTopLevel() int {
	top := 6
	for _, item := range e.outline {
		top = min(top, item.level)
	}
	return top
}

type outlinePillMatch struct{ ed *Editor }

// outlineList lists the entries: the section being read in the accent
// color, a click scrolling to it, a heading dragged onto another moving
// its section before it.
func (e *Editor) outlineList(c *ui.Context) {
	t := c.Theme()
	title := ""
	if len(e.Doc.Blocks) > 0 {
		title = e.Doc.Blocks[0].ID
	}
	active := e.activeOutline()
	top := e.outlineTopLevel()
	// Only the rows near what the list shows are built: a long page has
	// thousands of headings, and building each one every frame was most of
	// a frame. Spacers keep the rows out of view taking their room.
	s := &e.outlineState
	from, to := 0, len(e.outline)
	if len(e.outline) > outlineAll {
		// Y may be set past the end (the container clamps it as it lays
		// out); MaxY is how far the last frame let it go.
		y := max(0, min(s.list.Y, s.list.MaxY))
		from = min(len(e.outline), max(0, int(y/outlineRow)-outlineSpare))
		to = min(len(e.outline), from+int(e.viewport/outlineRow)+2*outlineSpare)
	}
	dragAt := -1
	if s.dragging != "" {
		for i, item := range e.outline {
			if item.id == s.dragging {
				dragAt = i
				break
			}
		}
	}
	dragging := ""
	ui.Scroll(c.Key("outline-scroll")).Grow(1).MinHeight(0).TrackScroll(&s.list).Children(func() {
		// Keep the offscreen drag source at its original keyed position, with
		// spacers for everything between it and the visible rows.
		cursor := 0
		rows := func(a, z int) {
			if a >= z {
				return
			}
			if a > cursor {
				ui.Box(c.Key("outline-gap:" + strconv.Itoa(cursor))).Height(float32(a-cursor) * outlineRow)
			}
			for i := a; i < z; i++ {
				item := *e.indexedOutline(i)
				on := i == active
				row := ui.ButtonBase(c.Key("outline:"+item.id)).Height(outlineRow).Radius(6).
					Padding(0, 8, 0, 8+float32(min(item.level-top, 4))*outlineIndent).
					Label(i18n.T("跳到 ") + item.text).Tooltip(item.text).KeepFocus().Cursor(ui.CursorPointer)
				if item.id != title {
					row.Drag(outlineDrag{item.id})
				}
				if row.Hovered() {
					row.Background(t.Text.Alpha(.05))
				}
				// A section dragged onto a heading goes before it.
				dragged, over := ui.DragOver[outlineDrag](row)
				over = over && dragged.id != item.id && item.id != title
				if d, ok := ui.Drop[outlineDrag](row); ok && d.id != item.id && item.id != title {
					e.Engine.MoveSection(d.id, item.id)
				}
				if row.Dragging() {
					row.Opacity(.4)
					dragging = item.id
				}
				if over {
					row.Draw(func(p *ui.Painter, r ui.Rect) {
						p.Fill(ui.Rect{X: r.X, Y: r.Y - 1, W: r.W, H: 2}, t.Accent, 1)
					})
				}
				color, weight := t.Text.Alpha(.72), 400
				if on {
					color, weight = t.Accent, 500
				}
				row.Children(func() {
					if on {
						slidingPill(c, outlinePillMatch{e}, t.Accent.Alpha(0.08), 6)
					}
					ui.Row(c).Grow(1).MinWidth(0).Justify(ui.Start).AlignItems(ui.Center).Children(func() {
						ui.Text(c, item.text).FontSize(14).FontWeight(weight).TextColor(color).SingleLine().MinWidth(0).Shrink(1)
					})
				})
				if row.Clicked() {
					e.jumpTo(c, item.id, item.y)
				}
			}
			cursor = z
		}
		if dragAt >= 0 && dragAt < from {
			rows(dragAt, dragAt+1)
		}
		rows(from, to)
		if dragAt >= to {
			rows(dragAt, dragAt+1)
		}
		if cursor < len(e.outline) {
			ui.Box(c.Key("outline-gap:" + strconv.Itoa(cursor))).Height(float32(len(e.outline)-cursor) * outlineRow)
		}
		// Below the last heading: the end of the document.
		end := ui.Box(c.Key("outline-end")).Height(16)
		_, over := ui.DragOver[outlineDrag](end)
		if d, ok := ui.Drop[outlineDrag](end); ok {
			e.Engine.MoveSection(d.id, "")
		}
		if over {
			end.Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(ui.Rect{X: r.X, Y: r.Y + 1, W: r.W, H: 2}, t.Accent, 1)
			})
		}
		e.bookmarkSection(c)
	})
	s.dragging = dragging
}
