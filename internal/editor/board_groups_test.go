package editor

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func groupedBoard() (*Board, string) {
	b := NewBoard(nil)
	a := whiteboard.NewAINode(whiteboard.AIText, 100, 140)
	z := whiteboard.NewAINode(whiteboard.AIImage, 530, 140)
	b.Scene.Elements = []*whiteboard.Element{a, z}
	b.selectIDs([]string{a.ID, z.ID})
	b.RunAction("group", BoardOptions{})
	return b, a.GroupIDs[0]
}

func TestBoardGroupRenameIsUndoableAndReadOnlySafe(t *testing.T) {
	b, id := groupedBoard()
	defer b.Close()
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{}) }, 1200, 800)
	b.RunAction("rename-group", BoardOptions{})
	tt.Frame()
	if !tt.HasText("重命名分组") {
		t.Fatal("group rename entry did not open")
	}
	b.propertyInput = "Product images"
	if err := tt.Click("保存分组名称"); err != nil {
		t.Fatal(err)
	}
	if b.Scene.GroupNames[id] != "Product images" {
		t.Fatal("rename did not save")
	}
	if !b.Undo() || b.Scene.GroupNames[id] != "" {
		t.Fatal("rename was not one undo step")
	}
	if !b.Redo() || b.Scene.GroupNames[id] != "Product images" {
		t.Fatal("redo lost group name")
	}
	b.ReadOnly = true
	b.RunAction("rename-group", BoardOptions{ReadOnly: true})
	if b.panel == "group-name" {
		t.Fatal("read-only board opened rename editor")
	}
	if output := os.Getenv("FREEFOLIO_GROUP_SNAPSHOT"); output != "" {
		if err := os.MkdirAll(output, 0o755); err != nil {
			t.Fatal(err)
		}
		b.Selected = map[string]bool{}
		tt.Frame()
		file, err := os.Create(filepath.Join(output, "groups.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGroupTitleOpensRenameFromKeyboard(t *testing.T) {
	b, _ := groupedBoard()
	defer b.Close()
	b.Selected = map[string]bool{}
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{}) }, 1200, 800)
	tt.Frame()
	if err := tt.Click("分组"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	if b.panel != "group-name" {
		t.Fatal("focused group title did not open the rename editor")
	}
	tt.Key(0, ui.KeyEscape)
	if b.panel == "group-name" {
		t.Fatal("Escape did not cancel group rename")
	}
	b.Tool = ToolHand
	tt.Frame()
	if _, ok := tt.Access("分组"); ok {
		t.Fatal("group title intercepted the hand tool")
	}
}

func TestGroupBackgroundTracksMoveAndUngroup(t *testing.T) {
	b, _ := groupedBoard()
	defer b.Close()
	first := b.groupRegions()[0].Bounds
	b.Scene.Translate(b.SelectedIDs(), whiteboard.Point{100, 50})
	got := b.groupRegions()[0].Bounds
	if got.X != first.X+100 || got.Y != first.Y+50 {
		t.Fatal("group background did not follow the nodes")
	}
	b.RunAction("ungroup", BoardOptions{})
	if len(b.groupRegions()) != 0 {
		t.Fatal("ungroup left a background")
	}
	if !b.Undo() || len(b.groupRegions()) != 1 {
		t.Fatal("undo did not restore the background")
	}
}

func TestGroupBackgroundDragMovesAllMembersAndUndoesOnce(t *testing.T) {
	b, _ := groupedBoard()
	defer b.Close()
	b.Selected = map[string]bool{}
	b.hover = whiteboard.Point{90, 200}
	if got := b.canvasCursor(0, false); got != ui.CursorGrab {
		t.Fatalf("group background cursor = %v", got)
	}
	b.pointerDown(ui.InputEvent{X: 90, Y: 200}, BoardOptions{})
	b.pointerMove(ui.InputEvent{X: 130, Y: 230}, BoardOptions{})
	b.pointerUp(ui.InputEvent{X: 130, Y: 230}, BoardOptions{})
	if b.Scene.Elements[0].X != 140 || b.Scene.Elements[1].X != 570 {
		t.Fatal("background drag did not move the whole group")
	}
	if !b.Undo() || b.Scene.Elements[0].X != 100 || b.Scene.Elements[1].X != 530 {
		t.Fatal("background drag was not one undo step")
	}
}

func TestGroupEditingMovesOneMemberAndExitsWithoutUngrouping(t *testing.T) {
	b, id := groupedBoard()
	defer b.Close()
	b.RunAction("edit-group", BoardOptions{})
	if b.isolatedGroup != id {
		t.Fatal("group edit mode did not open")
	}
	b.pointerDown(ui.InputEvent{X: 150, Y: 160}, BoardOptions{})
	b.pointerMove(ui.InputEvent{X: 180, Y: 180}, BoardOptions{})
	b.pointerUp(ui.InputEvent{X: 180, Y: 180}, BoardOptions{})
	if b.Scene.Elements[0].X != 130 || b.Scene.Elements[1].X != 530 {
		t.Fatal("editing one group member moved its sibling")
	}
	b.RunAction("exit-group", BoardOptions{})
	if b.isolatedGroup != "" || len(b.Selected) != 2 || b.Scene.Elements[0].GroupIDs[0] != id {
		t.Fatal("leaving group edit lost the group")
	}
}

func TestLockedGroupBackgroundDoesNotPartiallyMoveMembers(t *testing.T) {
	b, _ := groupedBoard()
	defer b.Close()
	b.Scene.Elements[1].Locked = true
	whiteboard.Touch(b.Scene.Elements[1])
	b.Selected = map[string]bool{}
	b.pointerDown(ui.InputEvent{X: 90, Y: 200}, BoardOptions{})
	b.pointerMove(ui.InputEvent{X: 130, Y: 230}, BoardOptions{})
	b.pointerUp(ui.InputEvent{X: 130, Y: 230}, BoardOptions{})
	if b.Scene.Elements[0].X != 100 || b.Scene.Elements[1].X != 530 {
		t.Fatal("locked group moved partially")
	}
}

func TestGroupBackgroundCancelAndReadOnlyKeepPositions(t *testing.T) {
	b, _ := groupedBoard()
	defer b.Close()
	b.Selected = map[string]bool{}
	b.pointerDown(ui.InputEvent{X: 90, Y: 200}, BoardOptions{})
	b.pointerMove(ui.InputEvent{X: 130, Y: 230}, BoardOptions{})
	b.pointerUp(ui.InputEvent{Canceled: true}, BoardOptions{})
	if b.Scene.Elements[0].X != 100 || b.Scene.Elements[1].X != 530 {
		t.Fatal("cancel retained group movement")
	}
	b.ReadOnly = true
	b.pointerDown(ui.InputEvent{X: 90, Y: 200}, BoardOptions{ReadOnly: true})
	b.pointerMove(ui.InputEvent{X: 130, Y: 230}, BoardOptions{ReadOnly: true})
	b.pointerUp(ui.InputEvent{X: 130, Y: 230}, BoardOptions{ReadOnly: true})
	if b.Scene.Elements[0].X != 100 || b.Scene.Elements[1].X != 530 {
		t.Fatal("read-only group moved")
	}
}

// The group title keeps one on-screen size at every zoom: its header band and
// its clickable title stay 28 px per nesting level, and the region still
// contains its members.
func TestGroupTitleKeepsScreenSizeAcrossZoom(t *testing.T) {
	b, id := groupedBoard()
	defer b.Close()
	b.Selected = map[string]bool{}
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{}) }, 1200, 800)
	tt.Frame() // The first frame fits the board; the loop then sets each zoom.
	members := whiteboard.SelectionBounds(b.Scene, b.groupRegions()[0].Members)
	for _, zoom := range []float64{0.25, 0.5, 1, 2, 4} {
		b.Zoom, b.PanX, b.PanY = zoom, 40, 300
		tt.Frame()
		var region whiteboard.GroupRegion
		for _, g := range b.groupRegions() {
			if g.ID == id {
				region = g
			}
		}
		header := (members.Y-region.Bounds.Y)*zoom - 24*zoom
		if diff := header - boardGroupHeader; diff > 0.01 || diff < -0.01 {
			t.Fatalf("zoom %v: header band is %.2f px, want %v", zoom, header, boardGroupHeader)
		}
		if !region.Bounds.Contains(whiteboard.Point{members.X, members.Y}) || !region.Bounds.Contains(whiteboard.Point{members.X + members.W, members.Y + members.H}) {
			t.Fatalf("zoom %v: region %+v no longer contains members %+v", zoom, region.Bounds, members)
		}
		title, ok := tt.Find("分组")
		if !ok || title.H < 27.5 || title.H > 28.5 {
			t.Fatalf("zoom %v: title control is %+v, want 28 px tall", zoom, title)
		}
		if top := float32(region.Bounds.Y*zoom + b.PanY); title.Y < top || title.Y+title.H > top+boardGroupHeader+1 {
			t.Fatalf("zoom %v: title %+v outside the header band at %v", zoom, title, top)
		}
	}
}
