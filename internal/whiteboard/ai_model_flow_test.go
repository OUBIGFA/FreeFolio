package whiteboard

import "testing"

func TestAIModelFlowResolvesBaseAndOrderedLoRAChain(t *testing.T) {
	s := NewScene()
	base := NewAINode(AIRole("model"), 0, 0)
	lora := NewAINode(AIRole("lora"), 400, 0)
	next := NewAINode(AIRole("lora"), 800, 0)
	gen := NewAINode(AIImage, 1200, 0)
	s.Elements = []*Element{base, lora, next, gen}
	set := func(e *Element, role AIRole, model, from string) {
		v := .5
		n := &AINode{V: 1, Role: role, Connection: "comfyui-zimage", Model: model, Params: AIParams{LoRAStrength: &v}}
		if from != "" {
			n.Links = []AILink{{Slot: "model", From: from}}
		}
		if err := SetAINode(e, n); err != nil {
			t.Fatal(err)
		}
	}
	set(base, "model", "z-image-turbo-int8", "")
	set(lora, "lora", "z-pencil", base.ID)
	set(next, "lora", "z-detail", lora.ID)
	set(gen, AIImage, "z-image-turbo-int8", next.ID)
	if AIOutputType("model") != "MODEL" || AIOutputType("lora") != "MODEL" {
		t.Fatal("model nodes do not expose typed model output")
	}
	flow, err := s.AIModelFlow(gen.ID)
	if err != nil {
		t.Fatal(err)
	}
	if flow.Model != "z-image-turbo-int8" || len(flow.LoRAs) != 2 || flow.LoRAs[0].ID != "z-pencil" || flow.LoRAs[1].ID != "z-detail" {
		t.Fatalf("wrong model flow: %+v", flow)
	}
	set(base, "model", "qwen-image-2.1-turbo-int8", "")
	bn := AINodeOf(base)
	bn.Connection = "comfyui-qwen-image"
	_ = SetAINode(base, bn)
	if _, err := s.AIModelFlow(gen.ID); err == nil {
		t.Fatal("incompatible LoRA family accepted")
	}
	gn := AINodeOf(gen)
	gn.Links = []AILink{{Slot: "model", From: base.ID}}
	_ = SetAINode(gen, gn)
	if _, ok := AIFindInputByType(s.AIInputNode(gn), AITypeImage); !ok {
		t.Fatal("Qwen connection kept the stored Z-Image image restrictions")
	}
	set(gen, AIImage, "z-image-turbo-int8", next.ID)
	set(lora, "lora", "z-pencil", "")
	if _, err := s.AIModelFlow(gen.ID); err == nil {
		t.Fatal("missing base silently ignored")
	}
}
