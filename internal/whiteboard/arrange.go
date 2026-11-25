package whiteboard

import (
	"fmt"
	"strings"
)

// AutoLayout follows dim0's four directions and 75/150 node/rank spacing.
// It uses a stable native layered layout rather than adding a JavaScript
// runtime for Dagre. Groups and frames move as units; locked units stay put.
func (s *Scene) AutoLayout(ids []string, direction string) error {
	if direction != "LR" && direction != "RL" && direction != "TB" && direction != "BT" {
		return fmt.Errorf("whiteboard: invalid layout direction %q", direction)
	}
	if len(ids) == 0 {
		for _, e := range s.Elements {
			if !e.IsDeleted {
				ids = append(ids, e.ID)
			}
		}
	}
	chosen := idSet(s.ExpandSelection(ids, true))
	parent := map[string]string{}
	for _, e := range s.Elements {
		if chosen[e.ID] && !e.IsDeleted && (e.Kind != KindText || e.ContainerID == "" || chosen[e.ContainerID]) {
			parent[e.ID] = e.ID
		}
	}
	var root func(string) string
	root = func(id string) string {
		p, ok := parent[id]
		if !ok {
			return ""
		}
		if p != id {
			parent[id] = root(p)
		}
		return parent[id]
	}
	join := func(a, b string) {
		x, y := root(a), root(b)
		if x != "" && y != "" && x != y {
			parent[x] = y
		}
	}
	groups := map[string]string{}
	for _, e := range s.Elements {
		if root(e.ID) == "" {
			continue
		}
		join(e.ID, e.ContainerID)
		join(e.ID, e.FrameID)
		for _, g := range e.GroupIDs {
			if other := groups[g]; other != "" {
				join(e.ID, other)
			} else {
				groups[g] = e.ID
			}
		}
	}
	// Standalone lines keep their coordinates. Lines inside a selected group
	// or frame move with that unit, including unbound decorative connectors.
	for _, e := range s.Elements {
		if e.IsLinear() && root(e.ID) == e.ID && len(e.GroupIDs) == 0 && !chosen[e.FrameID] {
			delete(parent, e.ID)
		}
	}
	type unit struct {
		key          string
		box          Box
		ids          []string
		locked       bool
		rank, degree int
		next         []int
	}
	var units []*unit
	indices := map[string]int{}
	for _, e := range s.Elements {
		key := root(e.ID)
		if key == "" {
			continue
		}
		i, ok := indices[key]
		if !ok {
			i = len(units)
			indices[key] = i
			units = append(units, &unit{key: key, box: ElementBounds(e)})
		} else {
			units[i].box = Union(units[i].box, ElementBounds(e))
		}
		units[i].ids = append(units[i].ids, e.ID)
		units[i].locked = units[i].locked || e.Locked
	}
	seen := map[[2]int]bool{}
	edge := func(from, to string) {
		a, z := root(from), root(to)
		if a == "" || z == "" || a == z {
			return
		}
		i, j := indices[a], indices[z]
		key := [2]int{i, j}
		if seen[key] || units[i].locked || units[j].locked {
			return
		}
		seen[key] = true
		units[i].next = append(units[i].next, j)
		units[j].degree++
	}
	for _, e := range s.Elements {
		if e.IsDeleted {
			continue
		}
		if e.IsLinear() && e.StartBinding != nil && e.EndBinding != nil {
			edge(e.StartBinding.ElementID, e.EndBinding.ElementID)
		}
		if n := AINodeOf(e); n != nil {
			for _, l := range n.Links {
				edge(l.From, e.ID)
			}
		}
	}
	var queue []int
	for i, u := range units {
		if !u.locked && u.degree == 0 {
			queue = append(queue, i)
		}
	}
	maxRank := 0
	for at := 0; at < len(queue); at++ {
		i := queue[at]
		u := units[i]
		maxRank = max(maxRank, u.rank)
		for _, j := range u.next {
			v := units[j]
			v.rank = max(v.rank, u.rank+1)
			v.degree--
			if v.degree == 0 {
				queue = append(queue, j)
			}
		}
	}
	// Cycles remain editable: put unresolved nodes in a separate rank, in
	// scene order. No recursion or unbounded rank relaxation on cyclic graphs.
	for _, u := range units {
		if !u.locked && u.degree > 0 {
			u.rank = maxRank + 1
		}
	}
	vertical := direction == "TB" || direction == "BT"
	lanes := map[int][]*unit{}
	sizes := map[int]float64{}
	var bounds Box
	first := true
	for _, u := range units {
		if u.locked {
			continue
		}
		if first {
			bounds = u.box
			first = false
		} else {
			bounds = Union(bounds, u.box)
		}
		lanes[u.rank] = append(lanes[u.rank], u)
		size := u.box.W
		if vertical {
			size = u.box.H
		}
		sizes[u.rank] = max(sizes[u.rank], size)
		maxRank = max(maxRank, u.rank)
	}
	total := float64(0)
	for rank := 0; rank <= maxRank; rank++ {
		total += sizes[rank]
		if rank < maxRank {
			total += 150
		}
	}
	deltas := map[string]Point{}
	main := float64(0)
	for rank := 0; rank <= maxRank; rank++ {
		cross := float64(0)
		for _, u := range lanes[rank] {
			m := main
			size := u.box.W
			if vertical {
				size = u.box.H
			}
			if direction == "RL" || direction == "BT" {
				m = total - main - size
			}
			x, y := bounds.X+m, bounds.Y+cross
			if vertical {
				x, y = bounds.X+cross, bounds.Y+m
			}
			d := Point{x - u.box.X, y - u.box.Y}
			for _, id := range u.ids {
				deltas[id] = d
			}
			if vertical {
				cross += u.box.W + 75
			} else {
				cross += u.box.H + 75
			}
		}
		main += sizes[rank] + 150
	}
	changed := map[string]bool{}
	for _, e := range s.Elements {
		if d, ok := deltas[e.ID]; ok && d != (Point{}) {
			e.X += d[0]
			e.Y += d[1]
			translateTouch(e)
			changed[e.ID] = true
		}
	}
	s.UpdateBindings(changed)
	return nil
}

// OutlineScene turns an indented outline into editable cards and bound arrows.
// Like dim0's staged mindmaps, the whole result is inserted as one history step.
func OutlineScene(text string, measure MeasureFunc) (*Scene, error) {
	s := NewScene()
	type entry struct {
		depth int
		id    string
	}
	var stack []entry
	headingDepth := -1
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		depth := 0
		for _, r := range line {
			if r == ' ' {
				depth++
			} else if r == '\t' {
				depth += 2
			} else {
				break
			}
		}
		marks := 0
		for marks < len(trim) && trim[marks] == '#' {
			marks++
		}
		if marks > 0 && marks <= 6 && marks < len(trim) && trim[marks] == ' ' {
			trim = strings.TrimSpace(trim[marks:])
			depth = (marks - 1) * 2
			headingDepth = depth
		}
		if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ ") {
			trim = strings.TrimSpace(trim[2:])
			if headingDepth >= 0 {
				depth += headingDepth + 2
			}
		}
		if trim == "" {
			continue
		}
		e := NewDimShape("layered-rect", 0, float64(len(s.Elements))*120, 200, 80)
		t := NewElement(KindText, 0, 0, 0, 0)
		t.Text, t.OriginalText = trim, trim
		t.FontSize = 18
		t.TextAlign = "center"
		t.VerticalAlign = "middle"
		s.Elements = append(s.Elements, e, t)
		if err := s.BindText(t.ID, e.ID, measure); err != nil {
			return nil, err
		}
		for len(stack) > 0 && stack[len(stack)-1].depth >= depth {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			from := s.ElementByID(stack[len(stack)-1].id)
			a := NewElement(KindArrow, from.X+from.W, from.Y+from.H/2, 100, 0)
			a.DimArrow = true
			a.EndArrowhead = ArrowTriangle
			a.Roundness = &Roundness{Type: 2}
			s.Elements = append(s.Elements, a)
			s.Bind(a.ID, false, from.ID, Point{from.X + from.W, from.Y + from.H/2})
			s.Bind(a.ID, true, e.ID, Point{e.X, e.Y + e.H/2})
		}
		stack = append(stack, entry{depth, e.ID})
	}
	if len(s.Elements) == 0 {
		return nil, fmt.Errorf("whiteboard: outline is empty")
	}
	if err := s.AutoLayout(nil, "LR"); err != nil {
		return nil, err
	}
	return s, nil
}
