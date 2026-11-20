package whiteboard

import (
	"reflect"
	"slices"
)

// History stores per-element and per-resource deltas. A gesture has one baseline;
// transient viewport/selection never enters this content history.
type History struct {
	undo, redo []sceneDelta
	pending    *Scene
}
type elementDelta struct{ before, after *Element }
type fileDelta struct{ before, after *FileData }
type sceneDelta struct {
	elements                map[string]elementDelta
	files                   map[string]fileDelta
	beforeOrder, afterOrder []string
	beforeApp, afterApp     AppState
	beforeExtra, afterExtra ExtraFields
}

func NewHistory() History { return History{} }
func (h *History) Begin(s *Scene) {
	if h.pending == nil {
		h.pending = s.Clone()
	}
}
func (h *History) Cancel()       { h.pending = nil }
func (h *History) CanUndo() bool { return len(h.undo) > 0 }
func (h *History) CanRedo() bool { return len(h.redo) > 0 }
func (h *History) Commit(s *Scene) bool {
	if h.pending == nil {
		return false
	}
	before := h.pending
	h.pending = nil
	d, changed := diffScene(before, s)
	if !changed {
		return false
	}
	h.undo = append(h.undo, d)
	if len(h.undo) > 100 {
		h.undo = slices.Clone(h.undo[len(h.undo)-100:])
	}
	h.redo = nil
	return true
}
func (h *History) Record(before, after *Scene) bool {
	h.pending = before.Clone()
	return h.Commit(after)
}
func diffScene(a, b *Scene) (sceneDelta, bool) {
	d := sceneDelta{elements: map[string]elementDelta{}, files: map[string]fileDelta{}, beforeApp: a.AppState, afterApp: b.AppState, beforeExtra: cloneExtra(a.Extra), afterExtra: cloneExtra(b.Extra)}
	d.beforeApp.Extra = cloneExtra(a.AppState.Extra)
	d.afterApp.Extra = cloneExtra(b.AppState.Extra)
	old, new := map[string]*Element{}, map[string]*Element{}
	for _, e := range a.Elements {
		old[e.ID] = e
		d.beforeOrder = append(d.beforeOrder, e.ID)
	}
	for _, e := range b.Elements {
		new[e.ID] = e
		d.afterOrder = append(d.afterOrder, e.ID)
	}
	for id, e := range old {
		if !reflect.DeepEqual(e, new[id]) {
			d.elements[id] = elementDelta{e.Clone(), new[id].Clone()}
		}
	}
	for id, e := range new {
		if old[id] == nil {
			d.elements[id] = elementDelta{nil, e.Clone()}
		}
	}
	for id, f := range a.Files {
		if !reflect.DeepEqual(f, b.Files[id]) {
			d.files[id] = fileDelta{f.Clone(), b.Files[id].Clone()}
		}
	}
	for id, f := range b.Files {
		if a.Files[id] == nil {
			d.files[id] = fileDelta{nil, f.Clone()}
		}
	}
	return d, len(d.elements) > 0 || len(d.files) > 0 || !slices.Equal(d.beforeOrder, d.afterOrder) || !reflect.DeepEqual(d.beforeApp, d.afterApp) || !reflect.DeepEqual(d.beforeExtra, d.afterExtra)
}
func applyDelta(s *Scene, d sceneDelta, forward bool) *Scene {
	out := s.Clone()
	elems := map[string]*Element{}
	for _, e := range out.Elements {
		elems[e.ID] = e
	}
	for id, c := range d.elements {
		e := c.before
		if forward {
			e = c.after
		}
		if e == nil {
			delete(elems, id)
		} else {
			elems[id] = e.Clone()
		}
	}
	order := d.beforeOrder
	out.AppState = d.beforeApp
	out.Extra = cloneExtra(d.beforeExtra)
	if forward {
		order = d.afterOrder
		out.AppState = d.afterApp
		out.Extra = cloneExtra(d.afterExtra)
	}
	out.AppState.Extra = cloneExtra(out.AppState.Extra)
	out.Elements = nil
	out.byID = nil
	for _, id := range order {
		if e := elems[id]; e != nil {
			out.Elements = append(out.Elements, e)
		}
	}
	for id, c := range d.files {
		f := c.before
		if forward {
			f = c.after
		}
		if f == nil {
			delete(out.Files, id)
		} else {
			out.Files[id] = f.Clone()
		}
	}
	return out
}
func (h *History) Undo(s *Scene) (*Scene, bool) {
	h.pending = nil
	if !h.CanUndo() {
		return s, false
	}
	d := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	h.redo = append(h.redo, d)
	return applyDelta(s, d, false), true
}
func (h *History) Redo(s *Scene) (*Scene, bool) {
	h.pending = nil
	if !h.CanRedo() {
		return s, false
	}
	d := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	h.undo = append(h.undo, d)
	return applyDelta(s, d, true), true
}
