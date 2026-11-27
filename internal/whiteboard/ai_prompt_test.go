package whiteboard

import "testing"

func TestZImageLoRANodeAcceptsTextButRejectsImageLinks(t *testing.T) {
	n := &AINode{Role: AIImage, Connection: "comfyui-zimage", Model: "z-pencil"}
	if _, ok := AIFindInputByType(n, AITypeImage); ok {
		t.Fatal("text-only LoRA node accepts images")
	}
	if _, ok := AIFindInputByType(n, AITypeString); !ok {
		t.Fatal("text-only LoRA node rejects prompts")
	}
	n.Connect("image.0", "photo")
	if n.LinkAt("image.0") != "" {
		t.Fatal("disabled reference image connected")
	}
}

func TestCutoutPromptFollowsModelCapability(t *testing.T) {
	for _, tc := range []struct {
		connection string
		accepts    bool
	}{
		{"local", false}, {"comfyui", false}, {"", false},
		{"comfyui-rmbg3", true}, {"comfyui-sa2va", true},
	} {
		t.Run(tc.connection, func(t *testing.T) {
			n := &AINode{Role: AICutout, Connection: tc.connection}
			if _, ok := AIFindInputByType(n, AITypeString); ok != tc.accepts {
				t.Fatalf("prompt connection available = %v, want %v", ok, tc.accepts)
			}
			n.Connect("prompt", "text")
			if got := n.LinkAt("prompt") != ""; got != tc.accepts {
				t.Fatalf("prompt connected = %v, want %v", got, tc.accepts)
			}
			n.Connect("image", "picture")
			if n.LinkAt("image") != "picture" {
				t.Fatal("image input was disabled")
			}
		})
	}
}

func TestUnsupportedPromptLinkIsPreservedButNotScheduled(t *testing.T) {
	s := NewScene()
	prompt := NewAINode(AIPrompt, 0, 0)
	target := NewAINode(AICutout, 400, 0)
	s.Elements = append(s.Elements, prompt, target)
	n := AINodeOf(target)
	n.Connection, n.Instruction = "local", "keep my subject note"
	// Old boards can contain a prompt link that the selected model ignores.
	n.Links = []AILink{{Slot: "prompt", From: prompt.ID}}
	if err := SetAINode(target, n); err != nil {
		t.Fatal(err)
	}
	if len(s.AIInputs(target.ID)) != 0 || len(s.AIDependencies()[target.ID]) != 0 {
		t.Fatal("unsupported prompt still feeds or schedules an upstream node")
	}
	n = AINodeOf(target)
	if n.Instruction != "keep my subject note" || n.LinkAt("prompt") != prompt.ID {
		t.Fatal("switching model erased user input")
	}
	n.Connection = "comfyui-sa2va"
	if err := SetAINode(target, n); err != nil {
		t.Fatal(err)
	}
	if len(s.AIInputs(target.ID)) != 1 || len(s.AIDependencies()[target.ID]) != 1 {
		t.Fatal("switching back did not restore the prompt input")
	}
}
