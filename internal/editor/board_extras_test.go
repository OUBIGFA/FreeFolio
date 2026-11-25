package editor

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestBoardSearchAllResultsAndNavigation(t *testing.T) {
	b := NewBoard(nil)
	for i := range 220 {
		e := whiteboard.NewElement(whiteboard.KindText, float64(i*240), 0, 120, 30)
		e.Text = fmt.Sprintf("主题 %d", i)
		b.Scene.Elements = append(b.Scene.Elements, e)
	}
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{}) }, 960, 640)
	tt.Frame()
	b.RunAction("search", BoardOptions{})
	b.search.query = "主题"
	tt.Frame()
	if len(b.search.rows) != 220 {
		t.Fatalf("search truncated at %d", len(b.search.rows))
	}
	for range 15 {
		tt.Key(0, ui.KeyDown)
	}
	if b.search.index != 15 {
		t.Fatalf("selection %d", b.search.index)
	}
	tt.Key(0, ui.KeyEnter)
	if !b.Selected[b.search.rows[15].ElementID] {
		t.Fatal("search did not reveal selected result")
	}
	tt.Key(0, ui.KeyEscape)
	if b.panel != "" {
		t.Fatal("search did not close")
	}
}

func TestBoardDim0LibraryAndLayoutUndo(t *testing.T) {
	b := NewBoard(nil)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{}) }, 1000, 700)
	tt.Frame()
	b.panel = "library"
	b.libraryQuery = "dim0"
	tt.Frame()
	if err := tt.Click("手绘思考云"); err != nil {
		t.Fatal(err)
	}
	if len(b.Scene.Elements) != 1 || b.Scene.Elements[0].Shape != "thought-cloud" {
		t.Fatal("shape was not inserted")
	}
	if !b.Undo() || len(b.Scene.Elements) != 0 {
		t.Fatal("shape insertion not undoable")
	}
	s, err := whiteboard.OutlineScene("Root\n  Child\n  Other", boardMeasure)
	if err != nil {
		t.Fatal(err)
	}
	b.Scene = s
	b.selectIDs(nil)
	before := s.Clone()
	b.autoLayout("TB", BoardOptions{})
	if !b.Undo() {
		t.Fatal("layout not undoable")
	}
	for i, e := range b.Scene.Elements {
		if e.X != before.Elements[i].X || e.Y != before.Elements[i].Y {
			t.Fatal("layout undo moved content")
		}
	}
	if b.History.CanUndo() {
		t.Fatal("layout recorded more than one step")
	}
}

func TestDim0BoardSnapshot(t *testing.T) {
	dir := os.Getenv("FREEFOLIO_SNAPSHOT")
	if dir == "" {
		t.Skip("set FREEFOLIO_SNAPSHOT for visual inspection")
	}
	s := whiteboard.NewScene()
	for i, kind := range whiteboard.DimShapeNames {
		e := whiteboard.NewDimShape(kind, float64(i%4)*260, float64(i/4)*240, 180, 110)
		s.Elements = append(s.Elements, e)
		label := whiteboard.NewElement(whiteboard.KindText, e.X, e.Y+145, 220, 28)
		label.Text = kind
		label.OriginalText = kind
		label.FontSize = 17
		s.Elements = append(s.Elements, label)
	}
	for i, head := range []whiteboard.Arrowhead{whiteboard.ArrowOpen, whiteboard.ArrowTriangle, "barb"} {
		e := whiteboard.NewElement(whiteboard.KindArrow, float64(i)*300, 550, 200, 50)
		e.Points = []whiteboard.Point{{0, 0}, {100, -50}, {200, 50}}
		e.DimArrow = true
		e.EndArrowhead = head
		e.Roundness = &whiteboard.Roundness{Type: 2}
		s.Elements = append(s.Elements, e)
	}
	data, err := BoardPNG(s, BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "dim0-shapes.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDim0UnlinkSnapshot(t *testing.T) {
	dir := os.Getenv("FREEFOLIO_SNAPSHOT")
	if dir == "" {
		t.Skip("set FREEFOLIO_SNAPSHOT for visual inspection")
	}
	for _, dark := range []bool{false, true} {
		b := NewBoard(nil)
		o := BoardOptions{AI: &BoardAI{}}
		tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1080, 720)
		tt.SetDark(dark)
		tt.Frame()
		from := b.AddAINode(whiteboard.AIPrompt, whiteboard.Point{260, 360}, o)
		to := b.AddAINode(whiteboard.AIText, whiteboard.Point{820, 400}, o)
		b.setAILink(to, "prompt", from)
		b.Selected = map[string]bool{}
		b.ai.link = &aiLinkRef{to, "prompt"}
		b.Zoom, b.PanX, b.PanY = 1, 0, 0
		tt.Frame()
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, fmt.Sprintf("dim0-unlink-%t.png", dark)))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tt.Image())
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
