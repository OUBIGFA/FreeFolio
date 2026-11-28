package localresource

import (
	"context"
	"math"
	"testing"
)

func TestMediaCreativeParametersReachNativeInputs(t *testing.T) {
	seed := int64(0)
	guidance := 3.5
	negative := "noise and distortion"
	for _, id := range []string{"ace15-turbo", "stable-audio3", "qwen3-tts", "wan22-5b"} {
		t.Run(id, func(t *testing.T) {
			pack, _ := ComfyPack(id)
			req := ComfyMediaRequest{Model: id, Prompt: "test", BPM: 86, KeyScale: "A minor", TimeSignature: "3", VoiceStyle: "温柔、慢速", Size: "480x832", FPS: 16, Seconds: 3, Options: ComfyOptions{Seed: &seed, Steps: 12, Guidance: &guidance, NegativePrompt: &negative}}
			if id == "ace15-turbo" {
				req.Seconds = 15
			}
			g, err := (&ComfyManager{}).mediaGraph(context.Background(), "", pack, req)
			if err != nil {
				t.Fatal(err)
			}
			in := func(key string) map[string]any { return g[key].(map[string]any)["inputs"].(map[string]any) }
			switch id {
			case "ace15-turbo":
				if in("text")["bpm"] != 86 || in("text")["keyscale"] != "A minor" || in("text")["timesignature"] != "3" || in("text")["lyrics"] != "[Instrumental]" {
					t.Fatal("music metadata ignored")
				}
			case "qwen3-tts":
				if in("speech")["instruct"] != req.VoiceStyle || in("speech")["seed"] != uint64(0) {
					t.Fatal("speech parameters ignored")
				}
				return
			case "wan22-5b":
				if in("latent")["width"] != 480 || in("latent")["height"] != 832 || in("latent")["length"] != 49 || in("video")["fps"] != float64(16) {
					t.Fatal("video geometry ignored")
				}
			}
			if in("sample")["seed"] != uint64(0) || in("sample")["steps"] != 12 || in("sample")["cfg"] != guidance {
				t.Fatal("sampling settings ignored")
			}
			if id != "ace15-turbo" && in("negative")["text"] != negative {
				t.Fatal("negative prompt ignored")
			}
		})
	}
}

func TestComfyOptionsRejectInvalidNumericSettings(t *testing.T) {
	seed := int64(-1)
	tooLarge := int64(9007199254740992)
	nan := math.NaN()
	infinite := math.Inf(1)
	negative := -.1
	for _, options := range []ComfyOptions{{Seed: &seed}, {Seed: &tooLarge}, {Guidance: &nan}, {Guidance: &infinite}, {Guidance: &negative}, {Steps: 10001}} {
		if _, _, _, err := options.sampling(42, 8, 1); err == nil {
			t.Fatalf("invalid settings accepted: %+v", options)
		}
	}
	empty := ""
	if (ComfyOptions{NegativePrompt: &empty}).negative("default") != "" {
		t.Fatal("explicit empty negative prompt restored default")
	}
}

func TestMediaCreativeParametersRejectInvalidValues(t *testing.T) {
	for _, req := range []ComfyMediaRequest{
		{Model: "ace15-turbo", BPM: -1}, {Model: "ace15-turbo", BPM: 301}, {Model: "ace15-turbo", KeyScale: "H major"}, {Model: "ace15-turbo", TimeSignature: "5"},
		{Model: "wan22-5b", Size: "99999x99999"}, {Model: "wan22-5b", FPS: 0 - 1}, {Model: "wan22-5b", Options: ComfyOptions{Steps: -2}},
	} {
		req.Prompt = "test"
		pack, _ := ComfyPack(req.Model)
		if _, err := (&ComfyManager{}).mediaGraph(context.Background(), "", pack, req); err == nil {
			t.Fatalf("invalid parameters accepted: %+v", req)
		}
	}
}
