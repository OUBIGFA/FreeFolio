package localresource

import (
	"context"
	"testing"
)

func TestNativeControlValuesReachWorkflow(t *testing.T) {
	g := map[string]any{"1": mediaNode("UNETLoader", map[string]any{"unet_name": "selected.safetensors", "weight_dtype": "default"}), "2": mediaNode("ModelSamplingAuraFlow", map[string]any{"shift": 3.0}), "8": mediaNode("KSampler", map[string]any{"sampler_name": "res_multistep", "scheduler": "simple", "denoise": 1.0})}
	opts := ComfyOptions{Native: map[string]any{"8.sampler_name": "euler", "8.scheduler": "karras", "8.denoise": .65, "2.shift": 5.0}}
	if err := applyNativeControls(g, "comfyui-zimage", "z-image-int8", opts.Native, nil); err != nil {
		t.Fatal(err)
	}
	if g["8"].(map[string]any)["inputs"].(map[string]any)["denoise"] != .65 || g["2"].(map[string]any)["inputs"].(map[string]any)["shift"] != 5.0 {
		t.Fatal("settings ignored")
	}
	for _, bad := range []map[string]any{{"1.unet_name": "another.safetensors"}, {"8.denoise": 2.0}, {"8.sampler_name": "made_up"}, {"8.denoise": []any{"1", 0}}} {
		if applyNativeControls(g, "comfyui-zimage", "z-image-int8", bad, nil) == nil {
			t.Fatalf("invalid setting accepted: %v", bad)
		}
	}
}

func TestNativeControlsMatchModelFamilies(t *testing.T) {
	for _, tc := range []struct{ connection, model, key string }{
		{"comfyui-zimage", "z-image-gguf-q8", "1.dequant_dtype"},
		{"comfyui-media", "ace15-turbo", "text.temperature"},
		{"comfyui-media", "qwen3-tts", "speech.repetition_penalty"},
		{"comfyui-media", "wan22-5b", "decode.tile_size"},
		{"comfyui-qwen-image", "qwen-image-2.1-turbo-int8", "8.sigmas"},
		{"comfyui-seedvr2", "seedvr2-3b-int8", "7.tile_size"},
	} {
		found := false
		for _, c := range NativeControls(tc.connection, tc.model, nil) {
			found = found || c.Key == tc.key
		}
		if !found {
			t.Errorf("missing control %s %s", tc.model, tc.key)
		}
	}
}

func TestMediaAdvancedSettingsReachActualNodes(t *testing.T) {
	for _, tc := range []struct {
		model, node, key string
		value            any
	}{
		{"ace15-turbo", "text", "temperature", .6},
		{"qwen3-tts", "speech", "repetition_penalty", 1.4},
		{"wan22-5b", "decode", "tile_size", float64(512)},
		{"stable-audio3", "sample", "scheduler", "karras"},
	} {
		pack, _ := ComfyPack(tc.model)
		g, err := (&ComfyManager{}).mediaGraph(context.Background(), "", pack, ComfyMediaRequest{Model: tc.model, Prompt: "test", Options: ComfyOptions{Native: map[string]any{tc.node + "." + tc.key: tc.value}}})
		if err != nil {
			t.Fatal(err)
		}
		node := g[tc.node].(map[string]any)
		if node["inputs"].(map[string]any)[tc.key] != tc.value {
			t.Fatal("advanced setting ignored")
		}
		if tc.model == "qwen3-tts" && node["class_type"] != "AILab_Qwen3TTSCustomVoice_Advanced" {
			t.Fatal("basic voice node cannot consume advanced fields")
		}
	}
}
