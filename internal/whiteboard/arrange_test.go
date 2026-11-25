package whiteboard

import "testing"

func TestAutoLayoutPreservesGroupsAndLockedNodes(t *testing.T) {
	for _, direction := range []string{"LR", "RL", "TB", "BT"} {
		s := NewScene()
		a := NewElement(KindRectangle, 20, 30, 160, 80)
		b := NewElement(KindRectangle, 50, 60, 100, 60)
		c := NewElement(KindRectangle, 500, 20, 80, 90)
		locked := NewElement(KindRectangle, 800, 900, 60, 70)
		locked.Locked = true
		s.Elements = []*Element{a, b, c, locked}
		s.Group([]string{a.ID, b.ID})
		arrow := NewElement(KindArrow, 0, 0, 50, 50)
		s.Elements = append(s.Elements, arrow)
		s.Bind(arrow.ID, false, b.ID, Point{b.X + b.W, b.Y + b.H/2})
		s.Bind(arrow.ID, true, c.ID, Point{c.X, c.Y + c.H/2})
		if err := s.AutoLayout(nil, direction); err != nil {
			t.Fatal(err)
		}
		if b.X-a.X != 30 || b.Y-a.Y != 30 {
			t.Fatal("group layout changed")
		}
		if locked.X != 800 || locked.Y != 900 {
			t.Fatal("locked node moved")
		}
		if ElementBounds(a).Intersects(ElementBounds(c)) || ElementBounds(b).Intersects(ElementBounds(c)) {
			t.Fatal("layout overlaps")
		}
		if direction == "LR" && c.X < a.X || direction == "RL" && c.X > a.X || direction == "TB" && c.Y < a.Y || direction == "BT" && c.Y > a.Y {
			t.Fatal("wrong rank direction")
		}
		if arrow.EndBinding.ElementID != c.ID {
			t.Fatal("binding lost")
		}
	}
}

func TestOutlineSceneKeepsEditableTextAndConnections(t *testing.T) {
	s, err := OutlineScene("项目\n  - 研究\n    - 资料\n  - 实施", nil)
	if err != nil {
		t.Fatal(err)
	}
	texts, arrows := 0, 0
	for _, e := range s.Elements {
		if e.Kind == KindText {
			texts++
		}
		if e.Kind == KindArrow {
			arrows++
			if e.StartBinding == nil || e.EndBinding == nil || !e.DimArrow {
				t.Fatal("unbound diagram")
			}
		}
	}
	if texts != 4 || arrows != 3 {
		t.Fatalf("%d labels, %d arrows", texts, arrows)
	}
	if _, err = s.Encode(); err != nil {
		t.Fatal(err)
	}
}

func TestOutlineHeadingsAndCyclicLayout(t *testing.T) {
	s, err := OutlineScene("# Root\n- Child\n  - Grandchild\n## Other", nil)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	var nodes []*Element
	for _, e := range s.Elements {
		if e.Kind == KindArrow {
			count++
		}
		if e.Shape != "" {
			nodes = append(nodes, e)
		}
	}
	if count != 3 {
		t.Fatalf("heading outline produced %d links", count)
	}
	cycle := NewElement(KindArrow, 0, 0, 100, 0)
	s.Elements = append(s.Elements, cycle)
	s.Bind(cycle.ID, false, nodes[2].ID, Point{nodes[2].X + nodes[2].W, nodes[2].Y})
	s.Bind(cycle.ID, true, nodes[0].ID, Point{nodes[0].X, nodes[0].Y})
	if err = s.AutoLayout(nil, "LR"); err != nil {
		t.Fatal(err)
	}
	for i, a := range nodes {
		for _, b := range nodes[i+1:] {
			if ElementBounds(a).Intersects(ElementBounds(b)) {
				t.Fatal("cyclic layout overlaps")
			}
		}
	}
}
