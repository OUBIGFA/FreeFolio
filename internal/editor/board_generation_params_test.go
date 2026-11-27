package editor

import (
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestMusicParameterControlsEditAndUndo(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	id := b.AddAINode(whiteboard.AIAudio, whiteboard.Point{}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection = "comfyui-media"; n.Model = "ace15-turbo" })
	tt := ui.NewTester(func(c *ui.Context) {
		o.Theme = c.Theme()
		n := whiteboard.AINodeOf(b.Scene.ElementByID(id))
		ui.Column(c).Gap(6).Children(func() { b.aiLocalMediaParams(c, o, id, n, false, false) })
	}, 400, 1800)
	tt.Frame()
	// Edit the real numeric input rather than mutating the stored model.
	found := false
	for _, a := range tt.AccessAll() {
		if a.Max == 300 {
			tt.ClickAt(a.Bounds.X+15, a.Bounds.Y+a.Bounds.H/2)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("BPM input missing")
	}
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("86")
	tt.Frame()
	if n := whiteboard.AINodeOf(b.Scene.ElementByID(id)); n.Params.BPM != 86 {
		t.Fatalf("tempo was not saved: %d", n.Params.BPM)
	}
	if !b.Undo() {
		t.Fatal("tempo edit cannot be undone")
	}
	tt.Frame()
	if n := whiteboard.AINodeOf(b.Scene.ElementByID(id)); n.Params.BPM != 0 {
		t.Fatal("undo did not restore legacy default")
	}
	for _, label := range []string{"调性", "拍号"} {
		if _, ok := tt.AccessByRole(ui.RolePopUpButton, label); !ok {
			t.Fatalf("missing %s selector", label)
		}
	}
	key, _ := tt.AccessByRole(ui.RolePopUpButton, "调性")
	tt.ClickAt(key.Bounds.X+20, key.Bounds.Y+key.Bounds.H/2)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if whiteboard.AINodeOf(b.Scene.ElementByID(id)).Params.KeyScale == "" {
		t.Fatal("key selector did not save its selection")
	}
}

func TestNativeParametersFollowModelCapabilities(t *testing.T) {
	for _, tc := range []struct {
		connection, model string
		role              whiteboard.AIRole
		shown, hidden     string
	}{
		{"comfyui-media", "qwen3-tts", whiteboard.AIAudio, "语气与情绪", "调性"},
		{"comfyui-media", "wan22-5b", whiteboard.AIVideo, "视频尺寸", "调性"},
		{"comfyui-seedvr2", "seedvr2-3b-int8", whiteboard.AIUpscale, "颜色校正", "采样步数"},
		{"comfyui-sa2va", "sa2va", whiteboard.AICutout, "抠图阈值", "采样步数"},
		{"comfyui-qwen-image", "qwen-image-2.1-turbo-int8", whiteboard.AIImage, "Turbo 使用固定 8 步采样", "采样步数"},
		{"comfyui-qwen-image", "qwen-image-2.1-int8", whiteboard.AIImage, "采样步数", "Turbo 使用固定 8 步采样"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			b := NewBoard(nil)
			o := BoardOptions{AI: &BoardAI{}}
			id := b.AddAINode(tc.role, whiteboard.Point{}, o)
			b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection = tc.connection; n.Model = tc.model })
			tt := ui.NewTester(func(c *ui.Context) {
				o.Theme = c.Theme()
				b.aiParamsFields(c, o, id, whiteboard.AINodeOf(b.Scene.ElementByID(id)))
			}, 400, 1800)
			tt.Frame()
			if !tt.HasText(tc.shown) || tt.HasText(tc.hidden) || tt.HasText("附加参数（JSON，可选）") {
				t.Fatalf("incorrect controls: %v", tt.Texts())
			}
		})
	}
}

func TestLockedMusicNumericInputDoesNotChangeSettings(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	id := b.AddAINode(whiteboard.AIAudio, whiteboard.Point{}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection = "comfyui-media"; n.Model = "ace15-turbo" })
	b.Scene.ElementByID(id).Locked = true
	tt := ui.NewTester(func(c *ui.Context) {
		o.Theme = c.Theme()
		b.aiParamsFields(c, o, id, whiteboard.AINodeOf(b.Scene.ElementByID(id)))
	}, 400, 1800)
	tt.Frame()
	for _, a := range tt.AccessAll() {
		if a.Max == 300 {
			if !a.Disabled {
				t.Fatal("locked BPM input active")
			}
			tt.ClickAt(a.Bounds.X+10, a.Bounds.Y+10)
		}
	}
	tt.Type("90")
	tt.Frame()
	if whiteboard.AINodeOf(b.Scene.ElementByID(id)).Params.BPM != 0 {
		t.Fatal("locked tempo changed")
	}
}

func TestNativeSamplingResetRestoresModelDefaults(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	id := b.AddAINode(whiteboard.AIVideo, whiteboard.Point{}, o)
	seed := int64(7)
	guidance := 2.0
	negative := ""
	b.updateAINode(o, id, func(n *whiteboard.AINode) {
		n.Connection = "comfyui-media"
		n.Model = "wan22-5b"
		n.Params.Seed = &seed
		n.Params.Steps = 35
		n.Params.Guidance = &guidance
		n.Params.NegativePrompt = &negative
	})
	tt := ui.NewTester(func(c *ui.Context) {
		o.Theme = c.Theme()
		b.aiComfySamplingParams(c, o, id, whiteboard.AINodeOf(b.Scene.ElementByID(id)), false)
	}, 400, 900)
	tt.Frame()
	if err := tt.Click("恢复生成细节默认值"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	p := whiteboard.AINodeOf(b.Scene.ElementByID(id)).Params
	if p.Seed != nil || p.Steps != 0 || p.Guidance != nil || p.NegativePrompt != nil || b.ai.buf.negative == "" {
		t.Fatal("reset did not restore model defaults")
	}
}

func TestConnectedBaseChoosesEffectiveParameterControls(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	base := b.AddAINode(whiteboard.AIModel, whiteboard.Point{}, o)
	b.updateAINode(o, base, func(n *whiteboard.AINode) { n.Connection = "comfyui-qwen-image"; n.Model = "qwen-image-2.1-int8" })
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{400, 0}, o)
	b.setAILink(id, "model", base)
	tt := ui.NewTester(func(c *ui.Context) {
		o.Theme = c.Theme()
		b.aiParamsFields(c, o, id, whiteboard.AINodeOf(b.Scene.ElementByID(id)))
	}, 400, 1800)
	tt.Frame()
	if !tt.HasText("采样步数") || tt.HasText("质量") || tt.HasText("附加参数（JSON，可选）") {
		t.Fatal("parameters ignored connected base")
	}
	b.updateAINode(o, base, func(n *whiteboard.AINode) { n.Model = "qwen-image-2.1-turbo-int8" })
	tt.Frame()
	if tt.HasText("采样步数") || !tt.HasText("Turbo 使用固定 8 步采样") {
		t.Fatal("base switch kept ordinary sampler controls")
	}
}
