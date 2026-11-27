package editor

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestBoardAIModelLabelsDistinguishSources(t *testing.T) {
	for _, connection := range []string{"local", "local-ncnn", "comfyui", "comfyui-rmbg3", "comfyui-sa2va", "comfyui-seedvr2", "comfyui-zimage", "comfyui-qwen-image", "provider-id"} {
		m := BoardAIModel{Connection: connection, ConnectionName: "Engine", Model: "model-id", Name: "Model"}
		want := i18n.T("本地模型")
		if connection == "provider-id" {
			want = i18n.T("供应商模型")
		}
		if label := m.label(); !strings.HasPrefix(label, want+" · ") || !strings.Contains(label, "Engine · Model") {
			t.Errorf("%s label = %q", connection, label)
		}
	}
}

func TestBoardAICutoutDisablesUnsupportedPromptAndRestoresIt(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(nil)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1280, 860)
	tt.Frame()
	promptID := b.AddAINode(whiteboard.AIPrompt, whiteboard.Point{240, 320}, o)
	id := b.AddAINode(whiteboard.AICutout, whiteboard.Point{760, 320}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) {
		n.Connection, n.Model, n.Instruction = "local", "ben2", "keep this note"
	})
	b.Selected = map[string]bool{}
	tt.Frame()
	field, ok := tt.Access(i18n.T("抠图说明"))
	if !ok || !field.Disabled || !strings.Contains(field.Description, i18n.T("此模型不支持提示词")) {
		t.Fatalf("unsupported prompt field is not explained and disabled: %+v", field)
	}
	tt.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
	tt.Type("must not be saved")
	tt.Frame()
	if b.aiNode(b.Scene.ElementByID(id)).Instruction != "keep this note" {
		t.Fatal("disabled prompt accepted typing")
	}
	sx, sy := slotAt(b, promptID, "")
	ex, ey := slotAt(b, id, "prompt")
	e := b.Scene.ElementByID(id)
	l := b.aiLayout(e, b.aiNode(e))
	if b.aiBeginDrag(aiInputPoint(e, l, 1)) {
		t.Fatal("disabled prompt started a reverse connection")
	}
	drag(tt, sx, sy, ex, ey)
	if b.aiNode(b.Scene.ElementByID(id)).LinkAt("prompt") != "" {
		t.Fatal("unsupported prompt accepted a dragged link")
	}
	if err := b.connectAI(b.Scene.ElementByID(promptID), e, false); err == nil {
		t.Fatal("dropping text on the node body accepted an unsupported prompt")
	}
	b.selectIDs([]string{id})
	b.ai.panel = true
	tt.Frame()
	found := false
	for _, prop := range tt.AccessAll() {
		if prop.Name == i18n.T("指令") && prop.Role == ui.RoleTextField {
			found = true
			if !prop.Disabled || !strings.Contains(prop.Description, i18n.T("此模型不支持提示词")) {
				t.Fatalf("property panel prompt is editable or unexplained: %+v", prop)
			}
		}
	}
	if !found {
		t.Fatal("property panel prompt missing")
	}
	b.ai.panel = false
	b.chooseAIModel(o, id, b.aiNode(e), BoardAIModel{Connection: "comfyui-sa2va", Model: "Sa2VA"})
	tt.Frame()
	if field, ok := tt.Access(i18n.T("抠图说明")); !ok || field.Disabled || field.Value != "keep this note" {
		t.Fatalf("supported model did not restore the saved prompt: %+v", field)
	}
	if err := b.connectAI(b.Scene.ElementByID(promptID), e, false); err != nil {
		t.Fatalf("supported prompt rejected text: %v", err)
	}
	b.chooseAIModel(o, id, b.aiNode(e), BoardAIModel{Connection: "local", Model: "ben2"})
	if len(b.aiLinks()) != 0 {
		t.Fatal("disabled prompt link remains active on the canvas")
	}
	if !b.Undo() || len(b.aiLinks()) != 1 || b.aiNode(e).LinkAt("prompt") != promptID {
		t.Fatal("undo did not restore the supported model and its prompt link")
	}
}
