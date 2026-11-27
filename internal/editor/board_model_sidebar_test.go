package editor

import (
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestAISidebarWiderAndGroupsShareLeftEdge(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{Presets: func() []BoardAIPreset {
		return []BoardAIPreset{{Role: whiteboard.AIImage, Model: BoardAIModel{Model: "z-detail", Name: "Z-Image Detailer", BaseModel: "z-image-turbo-int8"}}}
	}}}
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1000, 1800)
	tt.Frame()
	b.ai.bar = true
	tt.Frame()
	found := false
	for _, panel := range tt.AccessAll() {
		if panel.Name == "AI 节点" && panel.Role == ui.RoleGroup {
			found = true
			if panel.Bounds.W < 330 {
				t.Fatal("sidebar too narrow")
			}
		}
	}
	if !found {
		t.Fatal("sidebar missing")
	}
	var x float32 = -1
	for _, name := range []string{"生成图像", "Z-Image Detailer", "头脑风暴"} {
		item, ok := tt.Access(name)
		if !ok {
			t.Fatalf("missing sidebar item %s", name)
		}
		if x < 0 {
			x = item.Bounds.X
		}
		if item.Bounds.X != x || item.Bounds.W < 300 {
			t.Fatalf("item not left-aligned: %s %+v", name, item.Bounds)
		}
	}
}
