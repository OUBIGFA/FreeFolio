package whiteboard

import (
	"slices"
	"strings"
)

// GroupRegion is derived from live members, so moving or resizing members never
// leaves a detached background element behind. The header has its own space.
type GroupRegion struct {
	ID, Name string
	Bounds   Box
	Members  []string
	Locked   bool
}

func (s *Scene) GroupRegions() []GroupRegion {
	var out []GroupRegion
	indices := map[string]int{}
	depths := map[string]int{}
	for _, e := range s.Elements {
		if e.IsDeleted || len(e.GroupIDs) == 0 {
			continue
		}
		bounds := ElementBounds(e)
		for depth, id := range e.GroupIDs {
			depths[id] = max(depths[id], depth)
			index, ok := indices[id]
			if !ok {
				index = len(out)
				indices[id] = index
				out = append(out, GroupRegion{ID: id, Name: s.GroupNames[id], Bounds: bounds})
			} else {
				out[index].Bounds = Union(out[index].Bounds, bounds)
			}
			out[index].Members = append(out[index].Members, e.ID)
			out[index].Locked = out[index].Locked || e.Locked
		}
	}
	for i := range out {
		levels := float64(depths[out[i].ID] + 1)
		out[i].Bounds = out[i].Bounds.Expand(24 * levels)
		out[i].Bounds.Y -= 28 * levels
		out[i].Bounds.H += 28 * levels
	}
	// Outer groups are painted first; inner groups remain visible above them.
	slices.SortStableFunc(out, func(a, b GroupRegion) int {
		x, y := a.Bounds.W*a.Bounds.H, b.Bounds.W*b.Bounds.H
		if x > y {
			return -1
		}
		if x < y {
			return 1
		}
		return 0
	})
	return out
}

// RenameGroup changes metadata only. Locked members protect the whole group.
func (s *Scene) RenameGroup(id, name string) bool {
	var members []*Element
	for _, e := range s.Elements {
		if !e.IsDeleted && slices.Contains(e.GroupIDs, id) {
			if e.Locked {
				return false
			}
			members = append(members, e)
		}
	}
	name = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(name))
	if len(members) == 0 || s.GroupNames[id] == name {
		return false
	}
	if s.GroupNames == nil {
		s.GroupNames = map[string]string{}
	}
	if name == "" {
		delete(s.GroupNames, id)
	} else {
		s.GroupNames[id] = name
	}
	// Member revisions also invalidate retained group artwork in the view.
	for _, e := range members {
		Touch(e)
	}
	return true
}
