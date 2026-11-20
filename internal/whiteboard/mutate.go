package whiteboard

import (
	"math"
	"slices"
	"sort"
)

func idSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
func Touch(e *Element) { e.Version++; e.VersionNonce = NewSeed(); e.translations = 0 }
func translateTouch(e *Element) {
	if e.translations == 0 {
		e.shapeNonce = e.VersionNonce
	}
	e.Version++
	e.VersionNonce = NewSeed()
	e.translations++
}

// ExpandSelection keeps labels and frame children with their owning objects.
func (s *Scene) ExpandSelection(ids []string, groups bool) []string {
	set := idSet(ids)
	gs := map[string]bool{}
	if groups {
		for _, e := range s.Elements {
			if set[e.ID] {
				for _, g := range e.GroupIDs {
					gs[g] = true
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, e := range s.Elements {
			if e.IsDeleted || set[e.ID] {
				continue
			}
			yes := (e.ContainerID != "" && set[e.ContainerID]) || (e.FrameID != "" && set[e.FrameID])
			if groups {
				for _, g := range e.GroupIDs {
					yes = yes || gs[g]
				}
			}
			if yes {
				set[e.ID] = true
				changed = true
			}
		}
	}
	var out []string
	for _, e := range s.Elements {
		if set[e.ID] && !e.IsDeleted {
			out = append(out, e.ID)
		}
	}
	return out
}
func (s *Scene) Translate(ids []string, d Point) {
	set := idSet(s.ExpandSelection(ids, false))
	for _, e := range s.Elements {
		if set[e.ID] && !e.Locked {
			e.X += d[0]
			e.Y += d[1]
			translateTouch(e)
		}
	}
	s.UpdateBindings(set)
}
func (s *Scene) Resize(ids []string, target Box) {
	ids = s.ExpandSelection(ids, false)
	old := SelectionBounds(s, ids)
	sx, sy := float64(1), float64(1)
	if old.W != 0 {
		sx = target.W / old.W
	}
	if old.H != 0 {
		sy = target.H / old.H
	}
	set := idSet(ids)
	for _, e := range s.Elements {
		if !set[e.ID] || e.Locked {
			continue
		}
		e.X = target.X + (e.X-old.X)*sx
		e.Y = target.Y + (e.Y-old.Y)*sy
		e.W *= sx
		e.H *= sy
		for i := range e.Points {
			e.Points[i][0] *= sx
			e.Points[i][1] *= sy
		}
		if e.Kind == KindText {
			e.FontSize *= math.Min(math.Abs(sx), math.Abs(sy))
		}
		if e.W < 0 {
			e.X += e.W
			e.W = -e.W
		}
		if e.H < 0 {
			e.Y += e.H
			e.H = -e.H
		}
		Touch(e)
	}
	s.UpdateBindings(set)
}
func (s *Scene) Rotate(ids []string, angle float64) {
	ids = s.ExpandSelection(ids, false)
	c := SelectionBounds(s, ids).Center()
	set := idSet(ids)
	for _, e := range s.Elements {
		if !set[e.ID] || e.Locked {
			continue
		}
		p := RotatePoint(Point{e.X + e.W/2, e.Y + e.H/2}, c, angle)
		e.X = p[0] - e.W/2
		e.Y = p[1] - e.H/2
		e.Angle = math.Mod(e.Angle+angle, 2*math.Pi)
		Touch(e)
	}
	s.UpdateBindings(set)
}
func (s *Scene) Flip(ids []string, horizontal bool) {
	ids = s.ExpandSelection(ids, false)
	b := SelectionBounds(s, ids)
	set := idSet(ids)
	for _, e := range s.Elements {
		if !set[e.ID] || e.Locked {
			continue
		}
		if horizontal {
			e.X = b.X + b.W - (e.X - b.X) - e.W
			e.Angle = -e.Angle
			if e.Kind == KindImage {
				e.Scale[0] *= -1
			}
			for i := range e.Points {
				e.Points[i][0] = e.W - e.Points[i][0]
			}
		} else {
			e.Y = b.Y + b.H - (e.Y - b.Y) - e.H
			e.Angle = -e.Angle
			if e.Kind == KindImage {
				e.Scale[1] *= -1
			}
			for i := range e.Points {
				e.Points[i][1] = e.H - e.Points[i][1]
			}
		}
		Touch(e)
	}
	s.UpdateBindings(set)
}
func (s *Scene) Delete(ids []string) {
	set := idSet(s.ExpandSelection(ids, false))
	for _, e := range s.Elements {
		if set[e.ID] && !e.Locked {
			e.IsDeleted = true
			Touch(e)
		}
	}
	s.RepairBindings()
}
func (s *Scene) Group(ids []string) string {
	ids = s.ExpandSelection(ids, false)
	if len(ids) < 2 {
		return ""
	}
	g := NewID()
	for _, id := range ids {
		if e := s.ElementByID(id); e != nil && !e.Locked {
			e.GroupIDs = append(e.GroupIDs, g)
			Touch(e)
		}
	}
	return g
}
func (s *Scene) Ungroup(ids []string) {
	set := idSet(ids)
	gs := map[string]bool{}
	for _, e := range s.Elements {
		if set[e.ID] && len(e.GroupIDs) > 0 {
			gs[e.GroupIDs[len(e.GroupIDs)-1]] = true
		}
	}
	for _, e := range s.Elements {
		before := len(e.GroupIDs)
		e.GroupIDs = slices.DeleteFunc(e.GroupIDs, func(g string) bool { return gs[g] })
		if before != len(e.GroupIDs) {
			Touch(e)
		}
	}
}
func (s *Scene) Reorder(ids []string, where string) {
	set := idSet(s.ExpandSelection(ids, false))
	switch where {
	case "front", "back":
		sort.SliceStable(s.Elements, func(i, j int) bool {
			a, b := set[s.Elements[i].ID], set[s.Elements[j].ID]
			if where == "front" {
				return !a && b
			}
			return a && !b
		})
	case "forward":
		for i := len(s.Elements) - 2; i >= 0; i-- {
			if set[s.Elements[i].ID] && !set[s.Elements[i+1].ID] {
				s.Elements[i], s.Elements[i+1] = s.Elements[i+1], s.Elements[i]
			}
		}
	case "backward":
		for i := 1; i < len(s.Elements); i++ {
			if set[s.Elements[i].ID] && !set[s.Elements[i-1].ID] {
				s.Elements[i], s.Elements[i-1] = s.Elements[i-1], s.Elements[i]
			}
		}
	}
	for _, e := range s.Elements {
		e.Index = ""
		if set[e.ID] {
			Touch(e)
		}
	}
}
func (s *Scene) Align(ids []string, axis int, position string) {
	b := SelectionBounds(s, ids)
	for _, id := range ids {
		e := s.ElementByID(id)
		if e == nil || e.Locked {
			continue
		}
		r := ElementBounds(e)
		var d float64
		if axis == 0 {
			switch position {
			case "start":
				d = b.X - r.X
			case "end":
				d = b.X + b.W - r.X - r.W
			default:
				d = b.X + b.W/2 - r.X - r.W/2
			}
			s.Translate([]string{id}, Point{d, 0})
		} else {
			switch position {
			case "start":
				d = b.Y - r.Y
			case "end":
				d = b.Y + b.H - r.Y - r.H
			default:
				d = b.Y + b.H/2 - r.Y - r.H/2
			}
			s.Translate([]string{id}, Point{0, d})
		}
	}
}
func (s *Scene) Distribute(ids []string, axis int) {
	var es []*Element
	for _, id := range ids {
		if e := s.ElementByID(id); e != nil && !e.Locked {
			es = append(es, e)
		}
	}
	if len(es) < 3 {
		return
	}
	sort.SliceStable(es, func(i, j int) bool {
		if axis == 0 {
			return es[i].X < es[j].X
		}
		return es[i].Y < es[j].Y
	})
	first, last := ElementBounds(es[0]), ElementBounds(es[len(es)-1])
	start, end, total := first.X, last.X+last.W, float64(0)
	for _, e := range es {
		b := ElementBounds(e)
		if axis == 0 {
			total += b.W
		} else {
			total += b.H
		}
	}
	if axis == 1 {
		start = first.Y
		end = last.Y + last.H
	}
	gap := (end - start - total) / float64(len(es)-1)
	p := start
	for _, e := range es {
		b := ElementBounds(e)
		if axis == 0 {
			s.Translate([]string{e.ID}, Point{p - b.X, 0})
			p += b.W + gap
		} else {
			s.Translate([]string{e.ID}, Point{0, p - b.Y})
			p += b.H + gap
		}
	}
}

func (s *Scene) FrameFromSelection(ids []string) *Element {
	b := SelectionBounds(s, ids).Expand(20)
	f := NewElement(KindFrame, b.X, b.Y, b.W, b.H)
	f.Name = "Frame"
	for _, id := range s.ExpandSelection(ids, false) {
		if e := s.ElementByID(id); e != nil && !e.IsFrame() {
			e.FrameID = f.ID
			Touch(e)
		}
	}
	s.Elements = append([]*Element{f}, s.Elements...)
	return f
}
func (s *Scene) RemoveFromFrame(ids []string) {
	for _, id := range s.ExpandSelection(ids, false) {
		if e := s.ElementByID(id); e != nil {
			e.FrameID = ""
			Touch(e)
		}
	}
}

func (s *Scene) RepairBindings() {
	byID := map[string]*Element{}
	for _, e := range s.Elements {
		if !e.IsDeleted {
			byID[e.ID] = e
		}
	}
	for _, e := range s.Elements {
		if e.IsDeleted {
			continue
		}
		var bound []BoundElement
		for _, b := range e.BoundElements {
			if byID[b.ID] != nil {
				bound = append(bound, b)
			}
		}
		e.BoundElements = bound
		if e.FrameID != "" && byID[e.FrameID] == nil {
			e.FrameID = ""
		}
		if e.ContainerID != "" {
			if host := byID[e.ContainerID]; host != nil {
				addBound(host, e.ID, "text")
			} else {
				e.ContainerID = ""
			}
		}
		for _, b := range []*Binding{e.StartBinding, e.EndBinding} {
			if b != nil {
				if target := byID[b.ElementID]; target != nil {
					addBound(target, e.ID, "arrow")
				}
			}
		}
		if e.StartBinding != nil && byID[e.StartBinding.ElementID] == nil {
			e.StartBinding = nil
		}
		if e.EndBinding != nil && byID[e.EndBinding.ElementID] == nil {
			e.EndBinding = nil
		}
	}
}
func addBound(e *Element, id, kind string) {
	for _, b := range e.BoundElements {
		if b.ID == id {
			return
		}
	}
	e.BoundElements = append(e.BoundElements, BoundElement{ID: id, Type: kind})
}
func BindingPoint(e *Element, b *Binding) Point {
	return RotatePoint(Point{e.X + e.W*b.FixedPoint[0], e.Y + e.H*b.FixedPoint[1]}, Point{e.X + e.W/2, e.Y + e.H/2}, e.Angle)
}
func (s *Scene) Bind(arrowID string, end bool, targetID string, p Point) bool {
	a, e := s.ElementByID(arrowID), s.ElementByID(targetID)
	if a == nil || e == nil || !a.IsLinear() || a.ID == e.ID || !e.IsBindable() {
		return false
	}
	q := RotatePoint(p, Point{e.X + e.W/2, e.Y + e.H/2}, -e.Angle)
	b := &Binding{ElementID: e.ID, Mode: "orbit", FixedPoint: Point{.5, .5}}
	if e.W != 0 {
		b.FixedPoint[0] = (q[0] - e.X) / e.W
	}
	if e.H != 0 {
		b.FixedPoint[1] = (q[1] - e.Y) / e.H
	}
	if end {
		a.EndBinding = b
	} else {
		a.StartBinding = b
	}
	addBound(e, a.ID, "arrow")
	Touch(a)
	s.UpdateBindings(map[string]bool{e.ID: true})
	return true
}
func (s *Scene) UpdateBindings(changed map[string]bool) {
	for _, a := range s.Elements {
		if a.IsDeleted || !a.IsLinear() || len(a.Points) < 2 {
			continue
		}
		start, end := Add(Point{a.X, a.Y}, a.Points[0]), Add(Point{a.X, a.Y}, a.Points[len(a.Points)-1])
		dirty := false
		for i, b := range []*Binding{a.StartBinding, a.EndBinding} {
			if b == nil || !changed[b.ElementID] {
				continue
			}
			target := s.ElementByID(b.ElementID)
			if target == nil || target.IsDeleted {
				continue
			}
			p := BindingPoint(target, b)
			if i == 0 {
				start = p
			} else {
				end = p
			}
			dirty = true
		}
		if !dirty {
			continue
		}
		old := Point{a.X, a.Y}
		a.X, a.Y = start[0], start[1]
		for i := range a.FixedSegments {
			a.FixedSegments[i].Start = Sub(Add(old, a.FixedSegments[i].Start), start)
			a.FixedSegments[i].End = Sub(Add(old, a.FixedSegments[i].End), start)
		}
		for i := range a.Points {
			a.Points[i] = Sub(Add(old, a.Points[i]), start)
		}
		a.Points[0] = Point{}
		a.Points[len(a.Points)-1] = Sub(end, start)
		if a.Elbowed {
			a.Points = s.RouteElbow(a, start, end)
		}
		bb := PointsBounds(a.Points)
		a.W, a.H = bb.W, bb.H
		Touch(a)
	}
}
func ElbowRoute(start, end Point, a, b *Binding) []Point {
	d := Sub(end, start)
	horizontal := math.Abs(d[0]) >= math.Abs(d[1])
	if a != nil {
		horizontal = a.FixedPoint[0] < .1 || a.FixedPoint[0] > .9
	}
	if horizontal {
		return []Point{{0, 0}, {d[0] / 2, 0}, {d[0] / 2, d[1]}, d}
	}
	return []Point{{0, 0}, {0, d[1] / 2}, {d[0], d[1] / 2}, d}
}
