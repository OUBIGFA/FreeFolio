package editor

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestBoardMenuSubmenuOpensOnHover(t *testing.T) {
	b, _ := groupedBoard()
	defer b.Close()
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetMenuPresentation(ui.PopoverMenus)
		BoardView(c, b, BoardOptions{AI: &BoardAI{}})
	}, 1200, 800)
	tt.Frame()
	tt.RightClickAt(1100, 600)
	tt.Advance(300 * time.Millisecond)
	r, ok := tt.Find("组合与锁定")
	if !ok {
		t.Fatal("group submenu was not exposed")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(400 * time.Millisecond)
	if !tt.HasText("编辑组内节点") {
		t.Fatal("hovering did not expand group actions")
	}
	if dir := os.Getenv("FREEFOLIO_GROUP_SNAPSHOT"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "group-menu.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBoardContextMenuGroupsActionsAndKeepsGroupEditing(t *testing.T) {
	b, id := groupedBoard()
	defer b.Close()
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{AI: &BoardAI{}}) }, 1200, 800)
	tt.Frame()
	tt.RightClickAt(1100, 600)
	if got := tt.Menu(); len(got) > 12 {
		t.Fatalf("context menu still exposes %d top-level actions", len(got))
	}
	if err := tt.ChooseMenuItem("组合与锁定", "编辑组内节点"); err != nil {
		t.Fatal(err)
	}
	if b.isolatedGroup != id {
		t.Fatal("group submenu did not enter member editing")
	}
	tt.RightClickAt(1100, 600)
	if err := tt.ChooseMenuItem("组合与锁定", "完成组内编辑"); err != nil {
		t.Fatal(err)
	}
	tt.RightClickAt(1100, 600)
	if err := tt.ChooseMenuItem("编辑", "复制图形"); err != nil {
		t.Fatal(err)
	}
	if len(b.Scene.Elements) != 4 {
		t.Fatal("nested duplicate action was lost")
	}
}
