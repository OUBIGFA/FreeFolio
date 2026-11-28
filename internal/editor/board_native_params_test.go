package editor

import (
	"fmt"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestNativeParameterEditUndoAndLock(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection = "comfyui-zimage"; n.Model = "z-image-int8" })
	tt := ui.NewTester(func(c *ui.Context) {
		o.Theme = c.Theme()
		b.aiNativeParameterFields(c, o, id, whiteboard.AINodeOf(b.Scene.ElementByID(id)), b.ReadOnly)
	}, 900, 700)
	tt.Frame()
	found := false
	for _, a := range tt.AccessAll() {
		if a.Max == 1 && a.Min == 0 {
			tt.ClickAt(a.Bounds.X+20, a.Bounds.Y+a.Bounds.H/2)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("denoise control missing")
	}
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("0.65")
	tt.Frame()
	if whiteboard.AINodeOf(b.Scene.ElementByID(id)).Params.Native["8.denoise"] != .65 {
		t.Fatal("parameter not saved")
	}
	if !b.Undo() {
		t.Fatal("edit cannot undo")
	}
	tt.Frame()
	if len(whiteboard.AINodeOf(b.Scene.ElementByID(id)).Params.Native) != 0 {
		t.Fatal("undo lost default")
	}
	b.ReadOnly = true
	tt.Frame()
	for _, a := range tt.AccessAll() {
		if a.Max == 1 && a.Min == 0 && !a.Disabled {
			t.Fatal("readonly parameter editable")
		}
	}
}

func TestAdvancedParameterPagesFitSmallWindows(t *testing.T) {
	for _, size := range [][2]int{{1200, 800}, {960, 640}, {640, 420}} {
		for _, model := range []struct {
			role              whiteboard.AIRole
			connection, model string
		}{
			{whiteboard.AIImage, "comfyui-zimage", "z-image-gguf-q8"}, {whiteboard.AIAudio, "comfyui-media", "ace15-turbo"}, {whiteboard.AIAudio, "comfyui-media", "qwen3-tts"}, {whiteboard.AICutout, "comfyui-rmbg3", "rmbg3-fp8"}, {whiteboard.AIUpscale, "comfyui-seedvr2", "seedvr2-3b-int8"}, {whiteboard.AIModel, "comfyui-zimage", "z-image-bf16"},
		} {
			t.Run(fmt.Sprintf("%s/%d", model.model, size[0]), func(t *testing.T) {
				b := NewBoard(nil)
				o := (&fakeAI{}).options(t)
				tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, size[0], size[1])
				tt.Frame()
				b.Zoom = .4
				id := b.AddAINode(model.role, b.viewCenter(), o)
				b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection = model.connection; n.Model = model.model })
				tt.Frame()
				tt.Frame()
				if err := tt.Click("全部设置"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if err := tt.Click("更多模型参数"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				tt.Frame()
				for page := 0; page < 30; page++ {
					group, ok := tt.AccessByRole(ui.RoleGroup, "节点属性")
					if !ok {
						t.Fatal("parameter panel disappeared")
					}
					r := group.Bounds
					if r.X < 0 || r.Y < 0 || r.X+r.W > float32(size[0])+1 || r.Y+r.H > float32(size[1])+1 {
						t.Fatalf("page %d exceeds window: %v", page, r)
					}
					next, ok := tt.AccessByRole(ui.RoleButton, "下一页")
					if !ok || next.Disabled {
						break
					}
					if err := tt.Click("下一页"); err != nil {
						t.Fatal(err)
					}
					tt.Frame()
					tt.Frame()
				}
			})
		}
	}
}

func TestNativeTextParameterUndoRefreshesVisibleValue(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) {
		n.Connection = "comfyui-qwen-image"
		n.Model = "qwen-image-2.1-turbo-int8"
		n.Params.Native = map[string]any{"8.sigmas": "1, 0.5, 0"}
	})
	tt := ui.NewTester(func(c *ui.Context) {
		o.Theme = c.Theme()
		b.aiNativeParameterFields(c, o, id, whiteboard.AINodeOf(b.Scene.ElementByID(id)), false)
	}, 1200, 900)
	tt.Frame()
	for _, field := range tt.AccessAll() {
		if field.Name == "采样噪声序列" && field.Focusable {
			tt.ClickAt(field.Bounds.X+20, field.Bounds.Y+field.Bounds.H/2)
			break
		}
	}
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("1, 0.8, 0")
	tt.Frame()
	if whiteboard.AINodeOf(b.Scene.ElementByID(id)).Params.Native["8.sigmas"] != "1, 0.8, 0" {
		t.Fatal("sigma edit did not reach saved state")
	}
	if !b.Undo() {
		t.Fatal("cannot undo sigma edit")
	}
	tt.Frame()
	for _, field := range tt.AccessAll() {
		if field.Name == "采样噪声序列" && field.Focusable {
			if field.Value != "1, 0.5, 0" {
				t.Fatalf("undo left stale visible sigma: %s", field.Value)
			}
			return
		}
	}
	t.Fatal("sigma input missing")
}

func TestSpeechSamplingControlsRequireSampling(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	id := b.AddAINode(whiteboard.AIAudio, whiteboard.Point{}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection = "comfyui-media"; n.Model = "qwen3-tts" })
	tt := ui.NewTester(func(c *ui.Context) {
		o.Theme = c.Theme()
		b.aiNativeParameterFields(c, o, id, whiteboard.AINodeOf(b.Scene.ElementByID(id)), false)
	}, 1200, 1200)
	tt.Frame()
	check := func(want bool) {
		t.Helper()
		for _, f := range tt.AccessAll() {
			if f.Min == .1 && f.Max == 2 {
				if f.Disabled != want {
					t.Fatalf("speech temperature disabled=%v, want %v", f.Disabled, want)
				}
				return
			}
		}
		t.Fatal("speech temperature control missing")
	}
	check(true)
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Params.Native = map[string]any{"speech.do_sample": true} })
	tt.Frame()
	check(false)
}
