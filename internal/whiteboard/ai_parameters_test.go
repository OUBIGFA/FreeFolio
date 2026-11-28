package whiteboard

import (
	"reflect"
	"testing"
)

func TestGenerationSettingsSurviveBoardSaveAndInvalidateCache(t *testing.T) {
	seed := int64(0)
	cfg := 2.5
	negative := ""
	threshold := .7
	n := &AINode{V: 1, Role: AIAudio, Connection: "comfyui-media", Model: "ace15-turbo", Params: AIParams{Native: map[string]any{"text.temperature": .67, "text.generate_audio_codes": false}, BPM: 86, KeyScale: "A minor", TimeSignature: "3", VoiceStyle: "calm", FPS: 16, Seconds: 15, Seed: &seed, Steps: 12, Guidance: &cfg, NegativePrompt: &negative, ColorCorrection: "lab", MaskThreshold: &threshold, MaskBlur: 4, MaskOffset: -2}}
	s := NewScene()
	e := NewAINode(AIAudio, 0, 0)
	s.Elements = append(s.Elements, e)
	if err := SetAINode(e, n); err != nil {
		t.Fatal(err)
	}
	data, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	got := AINodeOf(back.Elements[0])
	if !reflect.DeepEqual(n.Params, got.Params) {
		t.Fatalf("settings lost: %+v", got.Params)
	}
	hash := AIHash(got, nil)
	got.Params.Native["text.temperature"] = .71
	if hash == AIHash(got, nil) {
		t.Fatal("tempo change reused previous generation")
	}
}
