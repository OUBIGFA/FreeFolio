package editor

import (
	"fmt"
	"math"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestAddingAINodesPreservesViewportAtCurrentCenter(t *testing.T) {
	for _, zoom := range []float64{0.6, 1.7} {
		for _, flow := range []bool{false, true} {
			t.Run(fmt.Sprintf("zoom=%g/flow=%v", zoom, flow), func(t *testing.T) {
				f := &fakeAI{}
				o := f.options(t)
				strength := .35
				o.AI.Presets = func() []BoardAIPreset {
					return []BoardAIPreset{{Role: whiteboard.AIImage, Model: BoardAIModel{
						Connection: "comfyui-zimage", Model: "z-detail", BaseModel: "z-image-turbo-int8",
						Name: "Z-Image Detailer", LoRAStrength: &strength,
					}}}
				}
				b := NewBoard(nil)
				// An unrelated distant element must not pull the viewport away
				// when a new workflow is added to a zoomed and panned canvas.
				b.Scene.Elements = append(b.Scene.Elements, whiteboard.NewElement(whiteboard.KindRectangle, 12000, -9000, 200, 100))
				tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 850)
				tt.Frame()
				b.Zoom, b.PanX, b.PanY = zoom, -357, 219
				b.ai.bar = true
				label := "图片放大"
				if flow {
					label = "Z-Image Detailer"
				}
				b.ai.query = label
				tt.Frame()
				center := b.viewCenter()
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if b.Zoom != zoom || b.PanX != -357 || b.PanY != 219 {
					t.Fatalf("adding a node moved the viewport: zoom=%g pan=(%g,%g)", b.Zoom, b.PanX, b.PanY)
				}
				var middle *whiteboard.Element
				for _, e := range b.Scene.Elements {
					if n := whiteboard.AINodeOf(e); n != nil && ((!flow && n.Role == whiteboard.AIUpscale) || (flow && n.Role == whiteboard.AILoRA)) {
						middle = e
					}
				}
				if middle == nil || math.Abs(middle.X+middle.W/2-center[0]) > 1e-6 || math.Abs(middle.Y+middle.H/2-center[1]) > 1e-6 {
					t.Fatalf("node or workflow was not placed at the original viewport center: %+v, want %v", middle, center)
				}
			})
		}
	}
}

func TestLoRAFlowRunsFromFinalNodeAndInvalidatesCache(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(nil)
	strength := .35
	m := BoardAIModel{Connection: "comfyui-zimage", Model: "z-detail", BaseModel: "z-image-turbo-int8", Name: "Detailer", LoRAStrength: &strength}
	b.insertAIModelPreset(BoardAIPreset{Role: whiteboard.AIImage, Model: m}, "Detailer flow", o)
	id := b.SelectedIDs()[0]
	n := b.aiNode(b.Scene.ElementByID(id))
	lora := b.Scene.ElementByID(n.LinkAt("model"))
	if len(b.Scene.Elements) != 3 || lora == nil {
		t.Fatal("preset did not create a real model flow")
	}
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Instruction = "a tree" })
	b.RunAI([]string{id}, false, false, o)
	if len(f.jobs) != 1 || f.jobs[0].ModelFlow == nil || f.jobs[0].Node.Model != "z-image-turbo-int8" || f.jobs[0].ModelFlow.LoRAs[0].ID != "z-detail" {
		t.Fatalf("model chain missing from actual job: %+v", f.jobs)
	}
	b.RunAI([]string{id}, false, false, o)
	if len(f.jobs) != 1 {
		t.Fatal("unchanged flow was not cached")
	}
	b.updateAINode(o, lora.ID, func(n *whiteboard.AINode) { v := .8; n.Params.LoRAStrength = &v })
	b.RunAI([]string{id}, false, false, o)
	if len(f.jobs) != 2 || *f.jobs[1].ModelFlow.LoRAs[0].Strength != .8 {
		t.Fatal("upstream LoRA change reused stale output")
	}
	b.RunAI([]string{id}, true, true, o)
	if len(f.jobs) != 3 {
		t.Fatal("model/LoRA nodes made extra generation calls")
	}
	if len(b.aiNode(lora).Outputs) != 0 {
		t.Fatal("LoRA node stored an image or text result")
	}
	base := b.aiNode(lora).LinkAt("model")
	b.updateAINode(o, base, func(n *whiteboard.AINode) { n.Params.Native = map[string]any{"3.device": "cpu"} })
	b.RunAI([]string{id}, false, false, o)
	if len(f.jobs) != 4 || f.jobs[3].ModelFlow.Native["3.device"] != "cpu" {
		t.Fatal("base parameter change reused stale output")
	}
	b.updateAINode(o, lora.ID, func(n *whiteboard.AINode) { v := false; n.Params.LoRAAutoTrigger = &v })
	b.RunAI([]string{id}, false, false, o)
	if len(f.jobs) != 5 || f.jobs[4].ModelFlow.LoRAs[0].AutoTrigger == nil || *f.jobs[4].ModelFlow.LoRAs[0].AutoTrigger {
		t.Fatal("LoRA trigger change reused stale output")
	}

}

func TestModelSelectorUsesSourceMarksAndKeyboard(t *testing.T) {
	models := []BoardAIModel{{Connection: "local", Model: "ben", Name: "BEN"}, {Connection: "comfyui-zimage", Model: "z", Name: "Z"}, {Connection: "cloud", Model: "remote", Name: "Remote"}}
	if models[0].sourceColor() != ui.Hex("#27965B") || models[1].sourceColor() != ui.Hex("#9159C7") || models[2].sourceColor() != ui.Hex("#397BC8") {
		t.Fatal("source colors do not match requested categories")
	}
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{Models: func(whiteboard.AIRole) []BoardAIModel { return models }}}
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection, n.Model = models[0].Connection, models[0].Model })
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Width(400).Children(func() { b.aiModelSelect(c, o, id, b.aiNode(b.Scene.ElementByID(id)), "selector", false, 30) })
	}, 600, 400)
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if b.aiNode(b.Scene.ElementByID(id)).Model != "z" {
		t.Fatal("keyboard selection did not choose ComfyUI model")
	}
}
