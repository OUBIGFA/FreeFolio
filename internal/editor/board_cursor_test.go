package editor

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func cursorBoard(o *BoardOptions) (*Board, *ui.Tester, *whiteboard.Element) {
	b := NewBoard(nil)
	b.fitted, b.RequestFocus = true, true
	e := whiteboard.NewElement(whiteboard.KindRectangle, 300, 220, 200, 160)
	b.Scene.Elements = append(b.Scene.Elements, e)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, *o) }, 1200, 800)
	tt.Frame()
	return b, tt, e
}

func wantBoardCursor(t *testing.T, tt *ui.Tester, want ui.Cursor) {
	t.Helper()
	if got := tt.Cursor(); got != want {
		t.Fatalf("cursor = %v, want %v", got, want)
	}
}

func TestBoardCursorMarqueeThenMove(t *testing.T) {
	b, tt, e := cursorBoard(&BoardOptions{})
	z := whiteboard.NewElement(whiteboard.KindEllipse, 580, 240, 100, 100)
	b.Scene.Elements = append(b.Scene.Elements, z)
	tt.Frame()
	tt.Press(270, 190)
	tt.Move(720, 420)
	wantBoardCursor(t, tt, ui.CursorCrosshair)
	tt.Release(720, 420)
	if len(b.SelectedIDs()) != 2 {
		t.Fatal("marquee did not select both shapes")
	}
	tt.Move(350, 280)
	wantBoardCursor(t, tt, ui.CursorGrab)
	tt.Press(350, 280)
	wantBoardCursor(t, tt, ui.CursorGrabbing)
	tt.Move(390, 310)
	if e.X != 340 || e.Y != 250 || z.X != 620 || z.Y != 270 {
		t.Fatal("the selected shapes did not move together")
	}
	// A tool modifier during a drag must not replace the active cursor.
	tt.KeyDown(ui.KeySpace)
	wantBoardCursor(t, tt, ui.CursorGrabbing)
	tt.KeyUp(ui.KeySpace)
	tt.Release(390, 310)
	wantBoardCursor(t, tt, ui.CursorGrab)
	b.Undo()
	if b.Scene.ElementByID(e.ID).X != 300 || b.Scene.ElementByID(z.ID).X != 580 {
		t.Fatal("moving the selection was not one undo step")
	}
}

func TestBoardCursorHandlesFollowHitTargets(t *testing.T) {
	for _, zoom := range []float64{0.5, 1, 2} {
		for _, test := range []struct {
			name   string
			x, y   float64
			cursor ui.Cursor
			mode   string
		}{
			{"nw", 0, 0, ui.CursorResizeNWSE, "resize"},
			{"n", 100, 0, ui.CursorResizeNS, "resize"},
			{"ne", 200, 0, ui.CursorResizeNESW, "resize"},
			{"e", 200, 80, ui.CursorResizeEW, "resize"},
			{"se", 200, 160, ui.CursorResizeNWSE, "resize"},
			{"s", 100, 160, ui.CursorResizeNS, "resize"},
			{"sw", 0, 160, ui.CursorResizeNESW, "resize"},
			{"w", 0, 80, ui.CursorResizeEW, "resize"},
			{"rotation", 100, -25, ui.CursorGrab, "rotate"},
		} {
			t.Run(fmt.Sprintf("%s/%g", test.name, zoom), func(t *testing.T) {
				b, tt, e := cursorBoard(&BoardOptions{})
				b.selectIDs([]string{e.ID})
				b.Zoom, b.PanX, b.PanY = zoom, 300*(1-zoom), 220*(1-zoom)
				tt.Frame()
				x, y := float32(300+test.x*zoom), float32(220+test.y*zoom)
				tt.Move(x, y)
				wantBoardCursor(t, tt, test.cursor)
				tt.Press(x, y)
				if b.mode != test.mode {
					t.Fatalf("mode = %q, want %q", b.mode, test.mode)
				}
				tt.Move(x+20, y+15)
				want := test.cursor
				if test.mode == "rotate" {
					want = ui.CursorGrabbing
				}
				wantBoardCursor(t, tt, want)
				if e.X == 300 && e.Y == 220 && e.W == 200 && e.H == 160 && e.Angle == 0 {
					t.Fatal("handle did not transform the shape")
				}
				tt.Release(x+20, y+15)
				tt.Move(950, 650)
				wantBoardCursor(t, tt, ui.CursorDefault)
			})
		}
	}
}

func TestBoardCursorTextAndTools(t *testing.T) {
	b, tt, _ := cursorBoard(&BoardOptions{})
	for _, tool := range boardTools {
		b.Tool = tool.tool
		tt.Frame()
		tt.Move(950, 650)
		want := ui.CursorCrosshair
		switch tool.tool {
		case ToolHand:
			want = ui.CursorGrab
		case ToolSelect:
			want = ui.CursorDefault
		case ToolText:
			want = ui.CursorText
		}
		if tt.Cursor() != want {
			t.Fatalf("tool %v: cursor %v, want %v", tool.tool, tt.Cursor(), want)
		}
	}
	b.Tool = ToolText
	tt.Frame()
	tt.ClickAt(700, 400)
	tt.Type("cursor")
	tt.Move(710, 410)
	wantBoardCursor(t, tt, ui.CursorText)
	tt.Key(0, ui.KeyEscape)
	tt.Move(950, 650)
	wantBoardCursor(t, tt, ui.CursorDefault)
}

func TestBoardCursorLockedAndReadOnly(t *testing.T) {
	o := BoardOptions{}
	b, tt, e := cursorBoard(&o)
	e.Locked = true
	b.selectIDs([]string{e.ID})
	tt.Frame()
	tt.Move(350, 280)
	wantBoardCursor(t, tt, ui.CursorNotAllowed)
	drag(tt, 350, 280, 400, 320)
	if e.X != 300 || e.Y != 220 {
		t.Fatal("locked shape moved")
	}
	tt.Move(300, 220)
	if tt.Cursor() == ui.CursorResizeNWSE {
		t.Fatal("locked shape offered a resize handle")
	}
	o.ReadOnly = true
	b.Tool = ToolText
	tt.Frame()
	tt.Move(950, 650)
	wantBoardCursor(t, tt, ui.CursorDefault)
	tt.KeyDown(ui.KeySpace)
	wantBoardCursor(t, tt, ui.CursorGrab)
	tt.Press(950, 650)
	tt.Move(970, 670)
	wantBoardCursor(t, tt, ui.CursorGrabbing)
	tt.Release(970, 670)
	tt.KeyUp(ui.KeySpace)
	wantBoardCursor(t, tt, ui.CursorDefault)
}

func TestBoardCursorLinksAndCopyModifiers(t *testing.T) {
	opened := ""
	b, tt, e := cursorBoard(&BoardOptions{OpenLink: func(url string) { opened = url }})
	e.Link = "https://example.com"
	tt.Move(350, 280)
	wantBoardCursor(t, tt, ui.CursorGrab)
	tt.HoldModifiers(ui.Ctrl)
	wantBoardCursor(t, tt, ui.CursorPointer)
	tt.ClickAtWith(ui.Ctrl, 350, 280)
	if opened != e.Link {
		t.Fatal("link was not opened")
	}
	tt.HoldModifiers(ui.Alt)
	wantBoardCursor(t, tt, ui.CursorCopy)
	// Tester.Press has no modifier argument. Use the canvas's real input
	// handler for this modified press, then inspect its normal view output.
	b.input(ui.InputEvent{Kind: ui.InputPointerDown, X: 350, Y: 280, Mods: ui.Alt}, b.options)
	tt.Frame()
	b.input(ui.InputEvent{Kind: ui.InputPointerMove, X: 390, Y: 320, Mods: ui.Alt}, b.options)
	tt.Frame()
	wantBoardCursor(t, tt, ui.CursorGrabbing)
	b.input(ui.InputEvent{Kind: ui.InputPointerUp, X: 390, Y: 320, Mods: ui.Alt}, b.options)
	tt.Move(390, 320)
	if len(b.Scene.VisibleElements()) != 2 {
		t.Fatal("Alt drag did not copy")
	}
	tt.HoldModifiers(0)
	wantBoardCursor(t, tt, ui.CursorGrab)
}

func TestBoardCursorLineEditing(t *testing.T) {
	for _, at := range []whiteboard.Point{{400, 380}, {350, 340}} {
		b, tt, e := cursorBoard(&BoardOptions{})
		e.Kind, e.Points, e.W, e.H = whiteboard.KindLine, []whiteboard.Point{{0, 80}, {100, 160}, {200, 80}}, 200, 160
		b.linearEdit = e.ID
		b.selectIDs([]string{e.ID})
		tt.Frame()
		x, y := float32(at[0]), float32(at[1])
		tt.Move(x, y)
		wantBoardCursor(t, tt, ui.CursorGrab)
		tt.Press(x, y)
		tt.Move(x+20, y+10)
		wantBoardCursor(t, tt, ui.CursorGrabbing)
		if b.mode != "line-point" {
			t.Fatalf("mode = %q", b.mode)
		}
		tt.Release(x+20, y+10)
		if whiteboard.Distance(whiteboard.Add(e.Points[b.linearPoint], whiteboard.Point{e.X, e.Y}), whiteboard.Point{at[0] + 20, at[1] + 10}) > 0.01 {
			t.Fatal("point did not follow the pointer")
		}
	}
}

func TestBoardCursorRestoresAtStationaryPointer(t *testing.T) {
	b, tt, _ := cursorBoard(&BoardOptions{})
	tt.Move(350, 280)
	wantBoardCursor(t, tt, ui.CursorGrab)
	tt.Scroll(350, 280, 300, 0)
	tt.Advance(time.Second)
	wantBoardCursor(t, tt, ui.CursorDefault)
	tt.Scroll(350, 280, -300, 0)
	tt.Advance(time.Second)
	wantBoardCursor(t, tt, ui.CursorGrab)
	tt.Press(350, 280)
	tt.Move(390, 320)
	tt.Key(0, ui.KeyEscape)
	if b.mode != "" || math.Abs(b.Scene.Elements[0].X-300) > 0.01 {
		t.Fatal("Escape did not cancel the drag")
	}
	wantBoardCursor(t, tt, ui.CursorGrab)
	tt.Release(390, 320)
}

func TestBoardCursorAILinks(t *testing.T) {
	o := BoardOptions{AI: &BoardAI{}}
	b := NewBoard(nil)
	b.fitted = true
	img := b.AddAINode(whiteboard.AIImage, whiteboard.Point{300, 300}, o)
	cat := b.AddAINode(whiteboard.AIConcat, whiteboard.Point{800, 300}, o)
	b.Selected = map[string]bool{}
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1400, 900)
	tt.Frame()
	x, y := slotAt(b, img, "")
	tt.Move(x, y)
	wantBoardCursor(t, tt, ui.CursorCrosshair)
	tt.Press(x, y)
	c := b.Scene.ElementByID(cat)
	tt.Move(float32(c.X+c.W/2), float32(c.Y+20))
	wantBoardCursor(t, tt, ui.CursorNotAllowed)
	tt.Release(float32(c.X+c.W/2), float32(c.Y+20))
	if len(b.aiNode(c).Links) != 0 {
		t.Fatal("incompatible link was created")
	}
}

func TestBoardCursorResizeCrossesOppositeEdge(t *testing.T) {
	b, tt, e := cursorBoard(&BoardOptions{})
	b.selectIDs([]string{e.ID})
	tt.Frame()
	tt.Press(500, 380)
	tt.Move(280, 400)
	wantBoardCursor(t, tt, ui.CursorResizeNESW)
	b.input(ui.InputEvent{Kind: ui.InputPointerMove, X: 280, Y: 400, Mods: ui.Shift}, b.options)
	tt.HoldModifiers(ui.Shift)
	// Constrained aspect ratio flips both axes, even when the pointer only
	// crosses one opposite edge; feedback must follow the actual transform.
	wantBoardCursor(t, tt, ui.CursorResizeNWSE)
	tt.Move(280, 200)
	wantBoardCursor(t, tt, ui.CursorResizeNWSE)
	tt.CancelPress()
	if b.mode != "" || b.Scene.ElementByID(e.ID).W != 200 {
		t.Fatal("canceled resize was not restored")
	}
	tt.Move(350, 280)
	wantBoardCursor(t, tt, ui.CursorGrab)
}

func TestBoardCursorRotatedCurveControlPoint(t *testing.T) {
	b, tt, e := cursorBoard(&BoardOptions{})
	e.Kind, e.DimArrow, e.Angle = whiteboard.KindArrow, true, math.Pi/2
	e.W, e.H = 200, 100
	e.Points = []whiteboard.Point{{0, 0}, {100, 100}, {200, 0}}
	b.selectIDs([]string{e.ID})
	b.linearEdit = e.ID
	tt.Frame()
	// Local (100, 100), rotated 90 degrees about (100, 50), is (50, 50).
	tt.Move(350, 270)
	wantBoardCursor(t, tt, ui.CursorGrab)
	tt.Press(350, 270)
	tt.Move(370, 280)
	wantBoardCursor(t, tt, ui.CursorGrabbing)
	tt.Release(370, 280)
	if len(e.Points) != 3 || whiteboard.Distance(e.Points[1], whiteboard.Point{110, 80}) > 0.01 {
		t.Fatalf("wrong rotated control point: %v", e.Points)
	}
}

func TestBoardCursorAIHandlesAndLinkSelection(t *testing.T) {
	o := BoardOptions{AI: &BoardAI{}}
	b := NewBoard(nil)
	b.fitted = true
	from := b.AddAINode(whiteboard.AIPrompt, whiteboard.Point{300, 300}, o)
	to := b.AddAINode(whiteboard.AIText, whiteboard.Point{800, 300}, o)
	b.selectIDs([]string{from})
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1400, 900)
	tt.Frame()
	e := b.Scene.ElementByID(from)
	// AI cards omit the rotation handle; it must not remain an invisible target.
	tt.Move(float32(e.X+e.W/2), float32(e.Y-25))
	wantBoardCursor(t, tt, ui.CursorDefault)
	tt.Press(float32(e.X+e.W/2), float32(e.Y-25))
	if b.mode != "marquee" {
		t.Fatalf("hidden handle started %q", b.mode)
	}
	tt.CancelPress()
	b.setAILink(to, "prompt", from)
	tt.Frame()
	links := b.aiLinks()
	if len(links) != 1 {
		t.Fatal("fixture link missing")
	}
	mid := aiSplineAt(links[0].curve, .5)
	tt.Move(float32(mid[0]), float32(mid[1]))
	wantBoardCursor(t, tt, ui.CursorPointer)
	tt.ClickAt(float32(mid[0]), float32(mid[1]))
	if b.ai.link == nil {
		t.Fatal("link was not selected")
	}
	field, ok := tt.Access("文字内容")
	if !ok {
		t.Fatal("inline text field missing")
	}
	tt.Move(field.Bounds.X+20, field.Bounds.Y+15)
	wantBoardCursor(t, tt, ui.CursorText)
}

func TestBoardCursorAnnotationTextOnLockedBase(t *testing.T) {
	a, err := NewImageAnnotation(testPNG(t), nil, whiteboard.Point{1, 1})
	if err != nil {
		t.Fatal(err)
	}
	b := a.Board
	tt := ui.NewTester(func(c *ui.Context) { ImageAnnotationView(c, a, false, nil) }, 900, 650)
	tt.Frame()
	b.Tool = ToolText
	tt.Frame()
	tt.Move(450, 325)
	wantBoardCursor(t, tt, ui.CursorText)
	tt.ClickAt(450, 325)
	tt.Type("annotation")
	tt.Key(0, ui.KeyEscape)
	if b.Scene.PlainText() != "annotation" {
		t.Fatal("protected base blocked a text annotation")
	}
}
