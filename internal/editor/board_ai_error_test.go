package editor

import (
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"strings"
	"testing"
)

func TestNodeErrorOpensFullSelectableCopyableText(t *testing.T) {
	b := NewBoard(nil)
	o := (&fakeAI{}).options(t)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1100, 800)
	tt.Frame()
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{500, 350}, o)
	message := "ComfyUI HTTP 400: " + strings.Repeat("required_input_missing sampling_mode.temperature\n", 100) + "END"
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Error = message })
	tt.Frame()
	rev := b.Revision()
	if err := tt.Click("查看完整错误"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	field, ok := tt.AccessByRole(ui.RoleTextField, "完整错误内容")
	if !ok || !field.ReadOnly || !strings.HasSuffix(field.Value, "END") || !strings.Contains(field.Value, message) {
		t.Fatalf("error truncated or not selectable: %+v", field)
	}
	if err := tt.Click("复制完整错误"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(tt.Clipboard(), message) {
		t.Fatal("copy truncated error")
	}
	if b.Revision() != rev {
		t.Fatal("opening error modified board")
	}
}

func TestAllSettingsPopupAnchorsToNode(t *testing.T) {
	b := NewBoard(nil)
	o := (&fakeAI{}).options(t)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 900)
	tt.Frame()
	b.AddAINode(whiteboard.AIAudio, whiteboard.Point{620, 360}, o)
	tt.Frame()
	anchor, ok := tt.AccessByRole(ui.RoleButton, "全部设置")
	if !ok {
		t.Fatal("settings button missing")
	}
	if err := tt.Click("全部设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	panel, ok := tt.AccessByRole(ui.RoleGroup, "节点属性")
	if !ok {
		t.Fatal("settings popup missing")
	}
	if panel.Bounds.X < anchor.Bounds.X-50 || panel.Bounds.X > anchor.Bounds.X+60 {
		t.Fatalf("settings detached from trigger: anchor=%v panel=%v", anchor.Bounds, panel.Bounds)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if b.ai.panel {
		t.Fatal("Escape did not close popup")
	}
}
