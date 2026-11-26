package editor

import (
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestBoardStyleControlsLeaveNarrowCanvasFree(t *testing.T) {
	board := NewBoard(whiteboard.NewScene())
	options := BoardOptions{}
	tester := ui.NewTester(func(ctx *ui.Context) { BoardView(ctx, board, options) }, 460, 440)
	tester.Frame()
	shape := whiteboard.NewElement(whiteboard.KindRectangle, 50, 100, 85, 60)
	board.Scene.Elements = append(board.Scene.Elements, shape)
	board.Selected = map[string]bool{shape.ID: true}
	tester.Frame()
	if tester.HasText("不透明度") {
		t.Fatal("narrow canvas opened the entire style panel over the selection")
	}
	if err := tester.Click("图形样式"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("不透明度") {
		t.Fatal("style controls unavailable on demand")
	}
	tester.SetSize(1050, 700)
	tester.Frame()
	tester.Frame()
	label, found := tester.Find("描边")
	if !found || label.X < 500 {
		t.Fatalf("controls cover the selected shape on the left: %v", label)
	}
	options.ReadOnly = true
	tester.Frame()
	if tester.HasText("不透明度") || tester.HasText("图形样式") {
		t.Fatal("read-only board exposes editing controls")
	}
}

func TestBoardNodeActionsStayInsideCanvas(t *testing.T) {
	options := (&fakeAI{}).options(t)
	board := NewBoard(whiteboard.NewScene())
	tester := ui.NewTester(func(ctx *ui.Context) { BoardView(ctx, board, options) }, 650, 500)
	tester.Frame()
	id := board.AddAINode(whiteboard.AIText, whiteboard.Point{}, options)
	node := board.Scene.ElementByID(id)
	node.X, node.Y = 570, 155
	board.Selected = map[string]bool{id: true}
	tester.Frame()
	tester.Frame()
	actions, found := tester.Find("节点操作")
	if !found || actions.W < 90 || actions.X < 0 || actions.X+actions.W > 650 || actions.Y < 48 || actions.Y+actions.H > 500 {
		t.Fatalf("edge node actions are clipped: %v", actions)
	}
}

func TestBoardNarrowToolbarKeepsActiveTool(t *testing.T) {
	options := (&fakeAI{}).options(t)
	board := NewBoard(whiteboard.NewScene())
	tester := ui.NewTester(func(ctx *ui.Context) { BoardView(ctx, board, options) }, 360, 450)
	board.Tool = ToolFrame
	tester.Frame()
	tester.Frame()
	for _, label := range []string{"画框 F", "选择 V", "更多工具"} {
		bounds, visible := tester.Find(label)
		if !visible || bounds.W < 28 || bounds.X < 0 || bounds.X+bounds.W > 360 {
			t.Fatalf("tool %q not fully visible: %v", label, bounds)
		}
	}
}
