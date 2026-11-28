package editor

import (
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestEnhancementNodeRunsWithSelectedHistoryAndRoundTripsStrength(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(nil)
	source := b.AddAINode(whiteboard.AIImage, whiteboard.Point{200, 200}, o)
	target := b.AddAINode(whiteboard.AIEnhance, whiteboard.Point{700, 200}, o)
	f.files["source.png"] = testPNG(t)
	b.updateAINode(o, source, func(n *whiteboard.AINode) {
		n.AddOutputs([]whiteboard.AIOutput{{Kind: "image", Path: "source.png", MIME: "image/png"}})
	})
	strength := 0.0
	b.updateAINode(o, target, func(n *whiteboard.AINode) {
		n.Model = "qwen-restore"
		n.Params.LoRAStrength = &strength
		n.Instruction = "keep the person"
	})
	if err := b.connectAI(b.Scene.ElementByID(source), b.Scene.ElementByID(target), false); err != nil {
		t.Fatal(err)
	}
	b.RunAI([]string{target}, false, true, o)
	if len(f.jobs) != 1 || len(f.jobs[0].Images) != 1 || f.jobs[0].Prompt != "keep the person" {
		t.Fatalf("wrong enhancement job: %+v", f.jobs)
	}
	if b.DisplayedAIImage(target) == "" {
		t.Fatal("enhancement output cannot be opened or connected to comparison")
	}
	clone := b.Scene.Clone()
	n := whiteboard.AINodeOf(clone.ElementByID(target))
	if n.Role != whiteboard.AIEnhance || n.Params.LoRAStrength == nil || *n.Params.LoRAStrength != 0 || n.Output() == nil {
		t.Fatalf("enhancement did not round trip: %+v", n)
	}
	b.RunAI([]string{target}, false, true, o)
	if len(b.aiNode(b.Scene.ElementByID(target)).Outputs) != 2 {
		t.Fatal("enhancement overwrote its history")
	}
}

func TestLoRAPresetCreatesOneUndoableNodeAndHonorsLocks(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	strength := .35
	m := BoardAIModel{Connection: "comfyui-zimage", Model: "z-detail", Name: "Z-Image Detailer", LoRAStrength: &strength}
	o.AI.Presets = func() []BoardAIPreset { return []BoardAIPreset{{Role: whiteboard.AIImage, Model: m}} }
	o.AI.Models = func(whiteboard.AIRole) []BoardAIModel { return []BoardAIModel{m} }
	b := NewBoard(nil)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 850)
	tt.Frame()
	b.ai.bar = true
	b.ai.query = "Z-Image Detailer"
	tt.Frame()
	if err := tt.Click("Z-Image Detailer"); err != nil {
		t.Fatal(err)
	}
	ids := b.SelectedIDs()
	if len(ids) != 1 {
		t.Fatal("preset not selected")
	}
	e := b.Scene.ElementByID(ids[0])
	n := b.aiNode(e)
	if n.Model != "z-detail" || n.Params.LoRAStrength == nil || *n.Params.LoRAStrength != .35 {
		t.Fatalf("wrong preset: %+v", n)
	}
	if !b.Undo() || len(b.Scene.Elements) != 0 {
		t.Fatal("creating a preset took more than one undo")
	}
	if !b.Redo() {
		t.Fatal("cannot redo preset")
	}
	tt.Frame()
	e = b.Scene.ElementByID(ids[0])
	e.Locked = true
	tt.Frame()
	found := false
	for _, field := range tt.AccessAll() {
		if field.Min == -100 && field.Max == 100 {
			found = true
			if !field.Disabled {
				t.Fatal("locked LoRA strength editable")
			}
		}
	}
	if !found {
		t.Fatal("LoRA strength control missing")
	}
}

func TestAnnotatedReferenceSkipsTextOnlyDefault(t *testing.T) {
	for _, ready := range []bool{false, true} {
		b := NewBoard(nil)
		source := whiteboard.NewAINode(whiteboard.AIImageInput, 0, 0)
		b.Scene.Elements = append(b.Scene.Elements, source)
		z := BoardAIModel{Connection: "comfyui-zimage", Model: "z-pencil"}
		o := BoardOptions{AI: &BoardAI{Default: func(whiteboard.AIRole) BoardAIModel { return z }, Models: func(whiteboard.AIRole) []BoardAIModel {
			if ready {
				return []BoardAIModel{z, {Connection: "comfyui-qwen-image", Model: "qwen-image-2.1-turbo-int8"}}
			}
			return []BoardAIModel{z}
		}}}
		input, generated, err := b.AddAnnotatedReference(source.ID, "annotation.png", 64, 64, true, o)
		if err != nil {
			t.Fatal(err)
		}
		n := b.aiNode(b.Scene.ElementByID(generated))
		if n.LinkAt("image.0") != input {
			t.Fatal("annotation image was not linked")
		}
		if ready && n.Connection != "comfyui-qwen-image" || !ready && n.Model != "" {
			t.Fatalf("wrong edit default: %+v", n)
		}
	}
}
