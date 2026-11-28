package localresource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestZImageOrdinaryPacks(t *testing.T) {
	for _, id := range []string{"z-image-int8", "z-image-bf16", "z-image-gguf-q8"} {
		p, ok := ComfyPack(id)
		if !ok || p.Kind != "zimage" || p.ModelID != id {
			t.Fatalf("missing ordinary model %s", id)
		}
		if strings.Contains(p.Files[0].RelPath, "turbo") {
			t.Fatal("ordinary model uses Turbo")
		}
		if _, err := PackDownload(p); err != nil {
			t.Fatal(err)
		}
	}
}

// Stop at the submission boundary: this tests the real graph and model probe
// without downloading weights or running inference on the user's GPU.
func TestZImageOrdinarySelectionAndDefaults(t *testing.T) {
	for _, tc := range []struct{ id, file, loader string }{
		{"z-image-int8", "z_image_int8_convrot.safetensors", "UNETLoader"},
		{"z-image-bf16", "z_image_bf16.safetensors", "UNETLoader"},
		{"z-image-gguf-q8", "z-image-Q8_0.gguf", "UnetLoaderGGUFAdvanced"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			var graph map[string]struct {
				Class  string         `json:"class_type"`
				Inputs map[string]any `json:"inputs"`
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/system_stats":
					w.Write([]byte(`{}`))
				case "/object_info":
					info := map[string]any{}
					for _, name := range []string{"ModelSamplingAuraFlow", "CLIPTextEncode", "ConditioningZeroOut", "EmptySD3LatentImage", "KSampler", "VAEDecode", "CFGNorm"} {
						info[name] = map[string]any{}
					}
					for _, entry := range []struct {
						node, key string
						values    []string
					}{
						{"UNETLoader", "unet_name", []string{"z_image_turbo_int8_convrot.safetensors", "z_image_int8_convrot.safetensors", "z_image_bf16.safetensors"}},
						{"UnetLoaderGGUFAdvanced", "unet_name", []string{"z-image-Q8_0.gguf"}},
						{"CLIPLoader", "clip_name", []string{"qwen_3_4b_fp8_mixed.safetensors", "qwen_3_4b.safetensors"}},
						{"VAELoader", "vae_name", []string{"ae.safetensors"}},
					} {
						info[entry.node] = map[string]any{"input": map[string]any{"required": map[string]any{entry.key: []any{entry.values}}}}
					}
					json.NewEncoder(w).Encode(info)
				case "/prompt":
					var body struct {
						Prompt json.RawMessage `json:"prompt"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					json.Unmarshal(body.Prompt, &graph)
					http.Error(w, "captured", http.StatusBadRequest)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			m := NewComfyManager()
			_, _ = m.GenerateZImageTurbo(context.Background(), ComfyConfig{URL: srv.URL}, ComfyImageRequest{BaseModel: tc.id, Prompt: "test", Options: ComfyOptions{Native: map[string]any{"cfg_norm.strength": .75}}})
			if graph["1"].Class != tc.loader || graph["1"].Inputs["unet_name"] != tc.file {
				t.Fatalf("wrong model: %+v", graph["1"])
			}
			if graph["cfg_norm"].Inputs["strength"] != .75 {
				t.Fatal("CFG normalization ignored")
			}
			if graph["8"].Inputs["steps"] != float64(40) || graph["8"].Inputs["cfg"] != float64(4) {
				t.Fatalf("wrong ordinary defaults: %+v", graph["8"])
			}
		})
	}
}
