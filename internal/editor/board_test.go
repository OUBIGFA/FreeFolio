package editor

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestBoardNativeDrawUndoAndTextComposition(t *testing.T) {
	b := NewBoard(whiteboard.NewScene())
	changes := 0
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{OnChange: func() { changes++ }}) }, 960, 640)
	b.RequestFocus = true
	tt.Frame()
	tt.Key(0, ui.KeyR)
	tt.Frame()
	tt.Press(260, 180)
	tt.Move(460, 300)
	tt.Release(460, 300)
	tt.Frame()
	if len(b.Scene.VisibleElements()) != 1 || b.Scene.Elements[0].Kind != whiteboard.KindRectangle || changes == 0 {
		t.Fatalf("rectangle not drawn: %+v", b.Scene.Elements)
	}
	if b.Tool != ToolSelect {
		t.Fatal("tool did not return to selection")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if len(b.Scene.Elements) != 0 {
		t.Fatal("undo did not remove rectangle")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
	tt.Frame()
	if len(b.Scene.Elements) != 1 {
		t.Fatal("redo did not restore rectangle")
	}
	tt.Key(0, ui.KeyT)
	tt.Frame()
	tt.Press(500, 350)
	tt.Release(500, 350)
	tt.Frame()
	tt.Compose("候选", 2)
	tt.Frame()
	if b.Scene.PlainText() != "" {
		t.Fatal("uncommitted IME text leaked to scene")
	}
	tt.Type("中文")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if b.Scene.PlainText() != "中文" {
		t.Fatalf("committed text lost: %q", b.Scene.PlainText())
	}
}

func TestBoardImageClipboardCropAndActions(t *testing.T) {
	b := NewBoard(whiteboard.NewScene())
	var pngbuf bytes.Buffer
	if err := png.Encode(&pngbuf, image.NewRGBA(image.Rect(0, 0, 80, 40))); err != nil {
		t.Fatal(err)
	}
	if err := b.InsertImage(pngbuf.Bytes(), whiteboard.Point{100, 100}); err != nil {
		t.Fatal(err)
	}
	if len(b.Scene.Files) != 1 || len(b.SelectedIDs()) != 1 {
		t.Fatal("image not selected")
	}
	b.RunAction("flip-horizontal", BoardOptions{})
	img := b.Scene.Elements[0]
	if img.Scale[0] != -1 {
		t.Fatal("flip did not update image")
	}
	if err := b.SetImageCrop(img.ID, whiteboard.ImageCrop{X: 10, Y: 5, Width: 40, Height: 20, NaturalWidth: 80, NaturalHeight: 40}); err != nil {
		t.Fatal(err)
	}
	if !b.Undo() || b.Scene.ElementByID(img.ID).Crop != nil {
		t.Fatal("crop undo failed")
	}
	b.RunAction("duplicate", BoardOptions{})
	if len(b.Scene.VisibleElements()) != 2 || len(b.Scene.Files) != 1 {
		t.Fatal("duplicate lost/shared incorrect image")
	}
	b.RunAction("select-all", BoardOptions{})
	b.RunAction("group", BoardOptions{})
	if b.Scene.Elements[0].GroupIDs[0] != b.Scene.Elements[1].GroupIDs[0] {
		t.Fatal("group action failed")
	}
}

func TestBoardPolylineAndFlowchartKeyboard(t *testing.T) {
	b := NewBoard(whiteboard.NewScene())
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{}) }, 960, 640)
	b.RequestFocus = true
	tt.Frame()
	tt.Key(0, ui.KeyL)
	tt.Frame()
	for _, p := range [][2]float32{{300, 220}, {450, 220}, {450, 360}} {
		tt.Press(p[0], p[1])
		tt.Release(p[0], p[1])
		tt.Frame()
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(b.Scene.Elements) != 1 || len(b.Scene.Elements[0].Points) != 3 {
		t.Fatalf("multipoint line: %+v", b.Scene.Elements)
	}
	r := whiteboard.NewElement(whiteboard.KindRectangle, 300, 300, 130, 70)
	b.Scene.Elements = append(b.Scene.Elements, r)
	b.selectIDs([]string{r.ID})
	tt.Key(ui.Cmd, ui.KeyRight)
	tt.Frame()
	if len(b.Scene.Elements) != 4 {
		t.Fatalf("connected node not added: %d", len(b.Scene.Elements))
	}
	last := b.Scene.Elements[len(b.Scene.Elements)-1]
	if last.Kind != whiteboard.KindArrow || last.StartBinding == nil || last.EndBinding == nil {
		t.Fatal("new node has no bound connector")
	}
}

func TestBoardDenseCacheTracksTextAndBackground(t *testing.T) {
	s := whiteboard.NewScene()
	for i := 0; i < 600; i++ {
		e := whiteboard.NewElement(whiteboard.KindRectangle, float64(i%30)*25, float64(i/30)*25, 20, 20)
		e.Roughness = 0
		s.Elements = append(s.Elements, e)
	}
	b := NewBoard(s)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{}) }, 960, 640)
	tt.Frame()
	tt.Frame()
	if b.viewCache.layer == nil {
		t.Fatal("dense cache not created")
	}
	layer := b.viewCache.layer
	b.PanX += 3
	tt.Frame()
	if layer != b.viewCache.layer {
		t.Fatal("pan rebuilt unchanged paths")
	}
	s.AppState.ViewBackgroundColor = "#fff9db"
	tt.Frame()
	if layer == b.viewCache.layer {
		t.Fatal("background-dependent content cache stale")
	}
}

// Ported from ArcMD 2fb223e: the pen keeps drawing stroke after stroke
// without a selection box, until Escape returns to selection.
func TestBoardFreedrawContinuousWithoutSelectionBox(t *testing.T) {
	scene := whiteboard.NewScene()
	rect := whiteboard.NewElement(whiteboard.KindRectangle, 80, 80, 120, 70)
	rect.ID = "existing-rect"
	scene.Elements = append(scene.Elements, rect)
	b := NewBoard(scene)
	b.Selected = map[string]bool{rect.ID: true}
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{}) }, 960, 640)
	b.RequestFocus = true
	tt.Frame()
	tt.Frame()
	tt.Key(0, ui.KeyP)
	tt.Frame()
	if b.Tool != ToolFreedraw || b.ToolLocked {
		t.Fatalf("expected unlocked pen after P, tool=%v locked=%v", b.Tool, b.ToolLocked)
	}
	if len(b.Selected) != 0 {
		t.Fatalf("switching to the pen kept the selection: %+v", b.Selected)
	}
	// Screen points: the board fits its content, so scene points would land
	// wherever the fit puts them. These stay inside the 960x640 view, below
	// the toolbar and right of the fitted rectangle.
	stroke := func(sx0, sy0, sx1, sy1 float32) {
		if whiteboard.HitTest(rect, b.scenePoint(sx0-b.lastRect.X, sy0-b.lastRect.Y), 4/b.Zoom) {
			t.Fatalf("stroke start %v,%v lands on the rectangle", sx0, sy0)
		}
		tt.Press(sx0, sy0)
		tt.Frame()
		tt.Move((sx0+sx1)/2, (sy0+sy1)/2)
		tt.Frame()
		if len(b.Selected) != 0 {
			t.Fatalf("stroke selected mid-drag: %+v", b.Selected)
		}
		tt.Move(sx1, sy1)
		tt.Release(sx1, sy1)
		tt.Frame()
	}
	x0 := b.lastRect.X + b.lastRect.W*0.7
	y0 := b.lastRect.Y + b.lastRect.H*0.45
	stroke(x0, y0, x0+100, y0)
	if b.Tool != ToolFreedraw || len(b.Selected) != 0 {
		t.Fatalf("after first stroke tool=%v selected=%+v", b.Tool, b.Selected)
	}
	if n := len(b.Scene.Elements); n != 2 || b.Scene.Elements[1].Kind != whiteboard.KindFreedraw {
		t.Fatalf("first stroke not added: %+v", b.Scene.Elements)
	}
	first := b.Scene.Elements[1]
	stroke(x0, y0+90, x0+120, y0+130)
	if b.Tool != ToolFreedraw || len(b.Selected) != 0 {
		t.Fatalf("after second stroke tool=%v selected=%+v", b.Tool, b.Selected)
	}
	if n := len(b.Scene.Elements); n != 3 || b.Scene.Elements[2].Kind != whiteboard.KindFreedraw {
		t.Fatalf("second stroke not added: %+v", b.Scene.Elements)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if b.Tool != ToolSelect {
		t.Fatalf("Escape did not return to selection: %v", b.Tool)
	}
	tt.ClickAt(x0+50, y0)
	tt.Frame()
	if len(b.Selected) != 1 || !b.Selected[first.ID] {
		t.Fatalf("clicking the first stroke did not select it: %+v", b.Selected)
	}
}
