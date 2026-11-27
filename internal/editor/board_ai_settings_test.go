package editor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestAllSettingsShowsMusicControlsWithoutScrolling(t *testing.T) {
	for _, size := range [][2]int{{1200, 800}, {960, 640}, {640, 420}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			b := NewBoard(nil)
			o := (&fakeAI{}).options(t)
			tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, size[0], size[1])
			tt.Frame()
			if size[1] < 600 {
				b.Zoom = .45
			}
			id := b.AddAINode(whiteboard.AIAudio, b.viewCenter(), o)
			b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection = "comfyui-media"; n.Model = "ace15-turbo" })
			tt.Frame()
			tt.Frame()
			if err := tt.Click("全部设置"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			panel, ok := tt.AccessByRole(ui.RoleGroup, "节点属性")
			if !ok {
				t.Fatal("settings missing")
			}
			for _, label := range []string{"速度（BPM）", "调性", "拍号", "歌词", "采样步数", "提示词引导强度", "恢复生成细节默认值"} {
				found := false
				for _, field := range tt.AccessAll() {
					if field.Name == label {
						found = true
						if field.Bounds.Y < 0 || field.Bounds.Y+field.Bounds.H > float32(size[1])+1 || field.Bounds.X < 0 || field.Bounds.X+field.Bounds.W > float32(size[0])+1 || field.Offscreen {
							t.Fatalf("%s is clipped at %v, panel %v", label, field.Bounds, panel.Bounds)
						}
					}
				}
				if !found {
					t.Fatalf("missing %s", label)
				}
			}
			before := panel.Bounds
			tt.Scroll(before.X+20, before.Y+100, 0, 250)
			tt.Frame()
			after, _ := tt.AccessByRole(ui.RoleGroup, "节点属性")
			if before != after.Bounds {
				t.Fatal("settings scrolled")
			}
		})
	}
}

func TestRunningNodeKeepsSettingsClosed(t *testing.T) {
	for _, label := range []string{"运行节点", "连同上游运行", "强制重新运行"} {
		t.Run(label, func(t *testing.T) {
			b := NewBoard(nil)
			o := (&fakeAI{}).options(t)
			tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
			tt.Frame()
			id := b.AddAINode(whiteboard.AIText, b.viewCenter(), o)
			b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Instruction = "test" })
			tt.Frame()
			if err := tt.Click("全部设置"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				tt.Frame()
			}
			if b.ai.panel {
				t.Fatal("run kept settings open")
			}
			if _, ok := tt.AccessByRole(ui.RoleGroup, "节点属性"); ok {
				t.Fatal("settings reopened after generation")
			}
			if whiteboard.AINodeOf(b.Scene.ElementByID(id)).Output() == nil {
				t.Fatal("node did not run")
			}
		})
	}
}

func TestSettingsPanelFitsForEveryNodeFamily(t *testing.T) {
	for _, size := range [][2]int{{1200, 800}, {960, 640}, {640, 420}} {
		for _, tc := range []struct {
			role              whiteboard.AIRole
			connection, model string
		}{
			{whiteboard.AIAudio, "comfyui-media", "ace15-turbo"}, {whiteboard.AIAudio, "comfyui-media", "qwen3-tts"}, {whiteboard.AIVideo, "comfyui-media", "wan22-5b"},
			{whiteboard.AIImage, "comfyui-qwen-image", "qwen-image-2.1-int8"}, {whiteboard.AIImage, "provider", "gpt-image-2"}, {whiteboard.AIText, "provider", "text"},
			{whiteboard.AICutout, "comfyui-rmbg3", "rmbg3-fp8"}, {whiteboard.AIUpscale, "comfyui-seedvr2", "seedvr2-3b-int8"}, {whiteboard.AILoRA, "comfyui-zimage", "z-detail"},
			{whiteboard.AIPrompt, "", ""}, {whiteboard.AIConcat, "", ""}, {whiteboard.AICompare, "", ""},
		} {
			t.Run(fmt.Sprintf("%s/%s/%d", tc.role, tc.model, size[0]), func(t *testing.T) {
				b := NewBoard(nil)
				o := (&fakeAI{}).options(t)
				tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, size[0], size[1])
				tt.Frame()
				b.Zoom = .4
				id := b.AddAINode(tc.role, b.viewCenter(), o)
				b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection = tc.connection; n.Model = tc.model })
				tt.Frame()
				tt.Frame()
				if err := tt.Click("全部设置"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				tt.Frame()
				panel, ok := tt.AccessByRole(ui.RoleGroup, "节点属性")
				if !ok {
					t.Fatal("panel missing")
				}
				r := panel.Bounds
				if r.Y < 0 || r.X < 0 || r.Y+r.H > float32(size[1])+1 || r.X+r.W > float32(size[0])+1 {
					t.Fatalf("panel exceeds viewport: %v", r)
				}
			})
		}
	}
}

func TestImageSettingsWithResultAndReferencesRemainVisible(t *testing.T) {
	b := NewBoard(nil)
	o := (&fakeAI{}).options(t)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 960, 640)
	tt.Frame()
	b.Zoom = .4
	source := b.AddAINode(whiteboard.AIPrompt, whiteboard.Point{}, o)
	id := b.AddAINode(whiteboard.AIImage, b.viewCenter(), o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) {
		n.Connection = "comfyui-qwen-image"
		n.Model = "qwen-image-2.1-int8"
		n.Connect("prompt", source)
		n.Outputs = []whiteboard.AIOutput{{Kind: "image", Path: "test.png", MIME: "image/png", Prompt: strings.Repeat("generated ", 200)}}
	})
	tt.Frame()
	tt.Frame()
	if err := tt.Click("全部设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	panel, ok := tt.AccessByRole(ui.RoleGroup, "节点属性")
	if !ok || panel.Bounds.Y+panel.Bounds.H > 641 {
		t.Fatalf("result actions overflow: %v", panel.Bounds)
	}
}
