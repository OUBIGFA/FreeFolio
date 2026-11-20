package whiteboard

import (
	"math"
	"testing"
)

func near(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-8 {
		t.Fatalf("got %.12g want %.12g", got, want)
	}
}

func TestRotatedGeometryAndLasso(t *testing.T) {
	e := NewElement(KindRectangle, 10, 20, 100, 40)
	e.Angle = math.Pi / 2
	b := ElementBounds(e)
	near(t, b.X, 40)
	near(t, b.Y, -10)
	near(t, b.W, 40)
	near(t, b.H, 100)
	if !HitTest(e, Point{60, 0}, 0) || HitTest(e, Point{15, 25}, 0) {
		t.Fatal("rotation ignored by hit testing")
	}
	s := NewScene()
	s.Elements = []*Element{e, NewElement(KindEllipse, 300, 300, 60, 40)}
	ids := s.Lasso([]Point{{35, -15}, {85, -15}, {85, 95}, {35, 95}}, false)
	if len(ids) != 1 || ids[0] != e.ID {
		t.Fatalf("lasso: %v", ids)
	}
}

func TestTransformsUpdateBindingsAndUndoOneGesture(t *testing.T) {
	s := NewScene()
	a := NewElement(KindRectangle, 0, 0, 100, 80)
	b := NewElement(KindRectangle, 300, 0, 100, 80)
	line := NewElement(KindArrow, 100, 40, 200, 0)
	line.StartBinding = &Binding{ElementID: a.ID, FixedPoint: Point{1, .5}, Mode: "orbit"}
	line.EndBinding = &Binding{ElementID: b.ID, FixedPoint: Point{0, .5}, Mode: "orbit"}
	s.Elements = []*Element{a, b, line}
	s.RepairBindings()
	h := NewHistory()
	h.Begin(s)
	s.Translate([]string{a.ID}, Point{20, 40})
	s.Translate([]string{a.ID}, Point{10, 10})
	if !h.Commit(s) {
		t.Fatal("gesture missing")
	}
	near(t, line.X, 130)
	near(t, line.Y, 90)
	near(t, line.X+line.Points[len(line.Points)-1][0], 300)
	near(t, line.Y+line.Points[len(line.Points)-1][1], 40)
	old, ok := h.Undo(s)
	if !ok {
		t.Fatal("undo missing")
	}
	near(t, old.ElementByID(a.ID).X, 0)
	near(t, old.ElementByID(line.ID).X, 100)
	if h.CanUndo() {
		t.Fatal("one gesture generated multiple history entries")
	}
	next, ok := h.Redo(old)
	if !ok {
		t.Fatal("redo missing")
	}
	near(t, next.ElementByID(a.ID).Y, 50)
}

func TestGroupFrameResizeAndIndependentPaste(t *testing.T) {
	s := NewScene()
	f := NewElement(KindFrame, 0, 0, 400, 300)
	e := NewElement(KindRectangle, 20, 30, 100, 60)
	e.FrameID = f.ID
	txt := NewElement(KindText, 30, 40, 60, 25)
	txt.Text = "label"
	txt.OriginalText = "label"
	txt.ContainerID = e.ID
	txt.FrameID = f.ID
	e.BoundElements = []BoundElement{{ID: txt.ID, Type: "text"}}
	s.Elements = []*Element{f, e, txt}
	s.Translate([]string{f.ID}, Point{50, 70})
	near(t, e.X, 70)
	near(t, txt.Y, 110)
	s.Resize([]string{e.ID}, Box{X: 70, Y: 100, W: 200, H: 120})
	near(t, e.W, 200)
	near(t, txt.X, 90)
	g := s.Group([]string{e.ID, txt.ID})
	if g == "" || e.GroupIDs[0] != g || txt.GroupIDs[0] != g {
		t.Fatal("group missing")
	}
	s.Delete([]string{e.ID})
	if !e.IsDeleted || !txt.IsDeleted {
		t.Fatal("bound label survived deletion")
	}
}

func TestHistoryDoesNotRecordViewOnlyOrAliasImages(t *testing.T) {
	s := NewScene()
	e := NewElement(KindImage, 0, 0, 80, 80)
	e.FileID = "f"
	s.Elements = []*Element{e}
	s.Files["f"] = &FileData{ID: "f", DataURL: "original", MIMEType: "image/png"}
	h := NewHistory()
	h.Begin(s)
	if h.Commit(s) || h.CanUndo() {
		t.Fatal("unchanged scene recorded")
	}
	h.Begin(s)
	s.Files["f"].DataURL = "changed"
	if !h.Commit(s) {
		t.Fatal("resource update omitted")
	}
	u, ok := h.Undo(s)
	if !ok || u.Files["f"].DataURL != "original" {
		t.Fatal("image history missing")
	}
	u.Files["f"].DataURL = "local"
	if s.Files["f"].DataURL != "changed" {
		t.Fatal("history shares mutable file")
	}
}

func TestElbowRoutesOutsideConnectedShapes(t *testing.T) {
	s := NewScene()
	left := NewElement(KindRectangle, 0, 0, 100, 100)
	right := NewElement(KindRectangle, 220, 0, 100, 100)
	a := NewElement(KindArrow, 0, 50, 320, 0)
	a.Elbowed = true
	a.StartBinding = &Binding{ElementID: left.ID, FixedPoint: Point{0, .5}, Mode: "orbit"}
	a.EndBinding = &Binding{ElementID: right.ID, FixedPoint: Point{1, .5}, Mode: "orbit"}
	s.Elements = []*Element{left, right, a}
	s.UpdateBindings(map[string]bool{left.ID: true, right.ID: true})
	if len(a.Points) < 4 {
		t.Fatal("route crosses bound shapes")
	}
	ps := WorldPoints(a)
	for i := 1; i < len(ps); i++ {
		if ps[i][0] != ps[i-1][0] && ps[i][1] != ps[i-1][1] {
			t.Fatal("elbow route is not orthogonal")
		}
		mid := Lerp(ps[i-1], ps[i], .5)
		if (Box{1, 1, 98, 98}).Contains(mid) || (Box{221, 1, 98, 98}).Contains(mid) {
			t.Fatalf("route crosses node: %v", ps)
		}
	}
}
