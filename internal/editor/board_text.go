package editor

import (
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// boardTextSession is application-owned input. Composition changes only shown;
// the persisted scene reads committed text until ReplaceText/UnmarkText commits.
type boardTextSession struct {
	board            *Board
	id               string
	shown, committed string
	selection        ui.TextInputRange
	marked           ui.TextInputRange
	hasMarked        bool
	layout           *ui.TextLayout
}

func (b *Board) startText(hit *whiteboard.Element, at whiteboard.Point, o BoardOptions) {
	if b.ReadOnly {
		return
	}
	if b.editing != nil {
		b.finishText(o)
	}
	b.History.Begin(b.Scene)
	var e *whiteboard.Element
	if hit != nil && hit.Kind == whiteboard.KindText {
		e = hit
	} else if hit != nil && hit.BoundTextID() != "" {
		e = b.Scene.ElementByID(hit.BoundTextID())
	}
	if e == nil {
		e = b.newElement(whiteboard.KindText, at, 1, 25)
		b.Scene.Elements = append(b.Scene.Elements, e)
		if hit != nil {
			_ = b.Scene.BindText(e.ID, hit.ID, boardMeasure)
		}
	}
	text := e.OriginalText
	if text == "" {
		text = e.Text
	}
	b.editing = &boardTextSession{board: b, id: e.ID, shown: text, committed: text, selection: ui.TextInputRange{Start: ui.UTF16Len(text), End: ui.UTF16Len(text)}}
	b.textFocus = true
	b.selectIDs([]string{e.ID})
	b.Tool = ToolSelect
}
func (b *Board) finishText(o BoardOptions) {
	if b.editing == nil {
		return
	}
	t := b.editing
	if t.hasMarked {
		t.UnmarkText()
	}
	if e := b.Scene.ElementByID(t.id); e != nil && strings.TrimSpace(t.committed) == "" {
		b.Scene.UnbindText(e.ID)
		boardDeleteElement(b.Scene, e.ID)
		delete(b.Selected, e.ID)
	}
	b.editing = nil
	if b.History.Commit(b.Scene) {
		b.changed(o)
	}
	b.RequestFocus = true
}
func (t *boardTextSession) TextForRange(r ui.TextInputRange) (string, ui.TextInputRange) {
	a, z := ui.UTF16ByteOffset(t.shown, r.Start), ui.UTF16ByteOffset(t.shown, r.End)
	if z < a {
		a, z = z, a
	}
	return t.shown[a:z], ui.TextInputRange{Start: ui.UTF16Len(t.shown[:a]), End: ui.UTF16Len(t.shown[:z])}
}
func (t *boardTextSession) Selection() ui.TextInputSelection {
	return ui.TextInputSelection{Range: t.selection}
}
func (t *boardTextSession) MarkedRange() (ui.TextInputRange, bool) { return t.marked, t.hasMarked }
func (t *boardTextSession) replace(r *ui.TextInputRange, s string) (int, int) {
	target := t.selection
	if t.hasMarked {
		target = t.marked
	}
	if r != nil {
		target = *r
	}
	a, z := ui.UTF16ByteOffset(t.shown, target.Start), ui.UTF16ByteOffset(t.shown, target.End)
	if z < a {
		a, z = z, a
	}
	t.shown = t.shown[:a] + s + t.shown[z:]
	start := ui.UTF16Len(t.shown[:a])
	return start, start + ui.UTF16Len(s)
}
func (t *boardTextSession) ReplaceText(r *ui.TextInputRange, s string) {
	_, end := t.replace(r, s)
	t.hasMarked = false
	t.selection = ui.TextInputRange{Start: end, End: end}
	t.commit()
}
func (t *boardTextSession) SetMarkedText(r *ui.TextInputRange, s string, selected ui.TextInputRange) {
	a, z := t.replace(r, s)
	t.hasMarked = s != ""
	t.marked = ui.TextInputRange{Start: a, End: z}
	t.selection = ui.TextInputRange{Start: a + selected.Start, End: a + selected.End}
	if s == "" {
		t.shown = t.committed
		t.selection = ui.TextInputRange{Start: ui.UTF16Len(t.shown), End: ui.UTF16Len(t.shown)}
	}
}
func (t *boardTextSession) UnmarkText() {
	if t.hasMarked {
		t.hasMarked = false
		t.commit()
	}
}
func (t *boardTextSession) commit() {
	if t.committed == t.shown {
		return
	}
	t.committed = t.shown
	t.board.Scene.SetText(t.id, t.committed, boardMeasure)
	t.board.changed(t.board.options)
}
func (t *boardTextSession) BoundsForRange(r ui.TextInputRange) (ui.Rect, ui.TextInputRange, bool) {
	if t.layout == nil {
		return ui.Rect{}, r, false
	}
	caret := t.layout.Caret(r.End)
	return caret, r, true
}
func (t *boardTextSession) IndexForPoint(p ui.Point) (int, bool) {
	if t.layout == nil {
		return 0, false
	}
	return t.layout.IndexAt(p), true
}
func (t *boardTextSession) SetTextSelection(r ui.TextInputRange) { t.selection = r }
func (b *Board) textCommand(cmd string, o BoardOptions) bool {
	t := b.editing
	if t == nil {
		return false
	}
	switch cmd {
	case "selectAll":
		t.selection = ui.TextInputRange{Start: 0, End: ui.UTF16Len(t.shown)}
		return true
	case "copy", "cut":
		text, _ := t.TextForRange(t.selection)
		b.services.WriteClipboard(text)
		if cmd == "cut" {
			t.ReplaceText(nil, "")
		}
		return true
	case "paste":
		t.ReplaceText(nil, b.services.ReadClipboard())
		return true
	case "undo":
		b.finishText(o)
		b.RunAction("undo", o)
		return true
	}
	return false
}
func (b *Board) textKey(ev ui.InputEvent, o BoardOptions) bool {
	t := b.editing
	if t == nil {
		return false
	}
	if ev.Key == ui.KeyEscape || (ev.Key == ui.KeyEnter && ev.Mods&(ui.Cmd|ui.Ctrl) != 0) {
		b.finishText(o)
		return true
	}
	if ev.Mods&(ui.Cmd|ui.Ctrl) != 0 {
		switch ev.Key {
		case ui.KeyA:
			return b.textCommand("selectAll", o)
		case ui.KeyC:
			return b.textCommand("copy", o)
		case ui.KeyX:
			return b.textCommand("cut", o)
		case ui.KeyV:
			return b.textCommand("paste", o)
		case ui.KeyZ:
			return b.textCommand("undo", o)
		}
	}
	switch ev.Key {
	case ui.KeyEnter:
		t.ReplaceText(nil, "\n")
		return true
	case ui.KeyBackspace, ui.KeyDelete:
		if t.hasMarked {
			return false
		}
		r := t.selection
		if r.Start == r.End && t.layout != nil {
			if ev.Key == ui.KeyBackspace {
				r.Start = t.layout.PreviousBoundary(r.Start)
			} else {
				r.End = t.layout.NextBoundary(r.End)
			}
		}
		t.ReplaceText(&r, "")
		return true
	case ui.KeyLeft, ui.KeyRight:
		if t.layout == nil {
			return false
		}
		at := t.selection.End
		if ev.Key == ui.KeyLeft {
			at = t.layout.PreviousBoundary(at)
		} else {
			at = t.layout.NextBoundary(at)
		}
		if ev.Mods&ui.Shift == 0 {
			t.selection.Start = at
		}
		t.selection.End = at
		return true
	}
	return false
}
