package localresource

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func comfyTestPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDetectComfyLayoutAndExtraModelPaths(t *testing.T) {
	root := t.TempDir()
	comfyDir := filepath.Join(root, "ComfyUI")
	pyDir := filepath.Join(root, "python_embeded")
	if err := os.MkdirAll(comfyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(comfyDir, "main.py"), []byte("# comfy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pyDir, "python.exe"), []byte("py"), 0o600); err != nil {
		t.Fatal(err)
	}
	layout, err := DetectComfyLayout(comfyDir)
	if err != nil {
		t.Fatal(err)
	}
	if layout.MainPy != filepath.Join(comfyDir, "main.py") || layout.Python != filepath.Join(pyDir, "python.exe") {
		t.Fatalf("unexpected layout: %+v", layout)
	}

	resDir := t.TempDir()
	yamlPath, err := WriteComfyExtraModelPaths(resDir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "base_path: "+filepath.ToSlash(filepath.Join(resDir, "comfyui"))) ||
		!strings.Contains(text, "upscale_models: upscale_models") ||
		!strings.Contains(text, "sa2va: sa2va") ||
		!strings.Contains(text, "vitmatte: vitmatte") {
		t.Fatalf("unexpected extra_model_paths.yaml:\n%s", text)
	}
}

func TestComfyManagerProbeUpscaleCutoutAndCancel(t *testing.T) {
	var interrupted atomic.Bool
	var blockHistory atomic.Bool
	out4x := comfyTestPNG(t, 16, 16, color.NRGBA{R: 10, G: 200, B: 80, A: 255})
	outCutout := comfyTestPNG(t, 4, 4, color.NRGBA{R: 200, G: 30, B: 30, A: 120})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/system_stats":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"devices": []map[string]any{{"name": "NVIDIA GeForce RTX 4090"}},
			})
		case r.URL.Path == "/object_info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ImageUpscaleWithModel": map[string]any{},
				"UpscaleModelLoader": map[string]any{
					"input": map[string]any{
						"required": map[string]any{
							"model_name": []any{[]string{"4x-UltraSharp.pth", "4x-Nomos8kDAT.pth"}},
						},
					},
				},
				"RMBG": map[string]any{
					"input": map[string]any{
						"required": map[string]any{
							"model": []any{[]string{"RMBG-3.0", "BEN2"}},
						},
					},
				},
			})
		case r.URL.Path == "/upload/image":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "freefolio_input.png", "subfolder": ""})
		case r.URL.Path == "/prompt":
			_ = json.NewEncoder(w).Encode(map[string]any{"prompt_id": "job-1"})
		case r.URL.Path == "/history/job-1":
			if blockHistory.Load() {
				_ = json.NewEncoder(w).Encode(map[string]any{})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"job-1": map[string]any{
					"status": map[string]any{"status_str": "success", "completed": true},
					"outputs": map[string]any{
						"out": map[string]any{
							"images": []map[string]any{{"filename": "res.png", "subfolder": "", "type": "output"}},
						},
					},
				},
			})
		case r.URL.Path == "/view":
			w.Header().Set("Content-Type", "image/png")
			if r.URL.Query().Get("filename") == "res.png" {
				_, _ = w.Write(out4x)
			}
		case r.URL.Path == "/interrupt":
			interrupted.Store(true)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	mgr := NewComfyManager()
	cfg := ComfyConfig{URL: srv.URL}
	caps, err := mgr.Probe(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if caps.Device != "NVIDIA GeForce RTX 4090" || len(caps.UpscaleModels) != 2 || len(caps.CutoutModels) != 2 || caps.CutoutNode != "RMBG" {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}

	in := comfyTestPNG(t, 4, 4, color.NRGBA{R: 50, G: 50, B: 50, A: 255})
	got2x, err := mgr.Upscale(context.Background(), cfg, "4x-Nomos8kDAT.pth", 2, in)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(got2x))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 8 || decoded.Bounds().Dy() != 8 {
		t.Fatalf("2x upscale size = %v, want 8x8", decoded.Bounds())
	}

	out4x = outCutout
	gotCut, err := mgr.Cutout(context.Background(), cfg, "RMBG-3.0", in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotCut, outCutout) {
		t.Fatal("cutout bytes mismatch")
	}

	blockHistory.Store(true)
	cancelCtx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()
	if _, err := mgr.Cutout(cancelCtx, cfg, "RMBG-3.0", in); err == nil {
		t.Fatal("expected canceled cutout error")
	}
	if !interrupted.Load() {
		t.Fatal("canceled ComfyUI workflow did not call /interrupt")
	}
}

func TestComfyManagerRMBG3AndSa2VACutout(t *testing.T) {
	outPNG := comfyTestPNG(t, 4, 4, color.NRGBA{R: 255, G: 255, B: 255, A: 200})
	var capturedPrompts []map[string]map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/system_stats":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"devices": []map[string]any{{"name": "NVIDIA GeForce RTX 3080 Ti", "vram_total": int64(12288) * 1024 * 1024}},
			})
		case "/object_info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"UNETLoader": map[string]any{"input": map[string]any{"required": map[string]any{"unet_name": []any{[]string{"RMBG-3.0-fp8.safetensors"}}}}},
				"CLIPLoader": map[string]any{"input": map[string]any{"required": map[string]any{"clip_name": []any{[]string{"qwen3vl_4b_int8_convrot.safetensors"}}}}},
				"VAELoader":  map[string]any{"input": map[string]any{"required": map[string]any{"vae_name": []any{[]string{"flux2-vae.safetensors"}}}}},
				"TextGenerateV2": map[string]any{"input": map[string]any{"required": map[string]any{
					"max_length":                []any{"INT", map[string]any{"default": 512}},
					"sampling_mode":             []any{[]string{"on", "off"}, map[string]any{"default": "on"}},
					"sampling_mode.temperature": []any{"FLOAT", map[string]any{"default": 0.7}},
				}}},
				"TextEncodeFibo2":    map[string]any{"input": map[string]any{"required": map[string]any{"prompt": []any{"STRING", map[string]any{"default": ""}}}}},
				"KSampler":           map[string]any{},
				"VAEDecode":          map[string]any{},
				"ImageToMask":        map[string]any{},
				"InvertMask":         map[string]any{},
				"JoinImageWithAlpha": map[string]any{},
				"XJSa2VAImageSegmentationV2": map[string]any{
					"output": []string{"STRING", "MASK"},
					"input": map[string]any{"required": map[string]any{
						"model_name":          []any{[]string{"ByteDance/Sa2VA-InternVL3-2B", "ByteDance/Sa2VA-Qwen3-VL-4B"}},
						"segmentation_prompt": []any{"STRING", map[string]any{"default": ""}},
						"threshold":           []any{"FLOAT", map[string]any{"default": 0.5}},
						"use_8bit":            []any{"BOOLEAN", map[string]any{"default": true}},
						"use_flash_attn":      []any{"BOOLEAN", map[string]any{"default": true}},
						"unload":              []any{"BOOLEAN", map[string]any{"default": true}},
						"process_detail":      []any{"BOOLEAN", map[string]any{"default": true}},
						"detail_erode":        []any{"INT", map[string]any{"default": 6}},
					}},
				},
			})
		case "/upload/image":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "freefolio_input.png", "subfolder": ""})
		case "/prompt":
			var req struct {
				Prompt map[string]struct {
					ClassType string         `json:"class_type"`
					Inputs    map[string]any `json:"inputs"`
				} `json:"prompt"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			entry := map[string]map[string]any{}
			for id, node := range req.Prompt {
				node.Inputs["__class_type"] = node.ClassType
				entry[id] = node.Inputs
			}
			capturedPrompts = append(capturedPrompts, entry)
			_ = json.NewEncoder(w).Encode(map[string]any{"prompt_id": "job-1"})
		case "/history/job-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"job-1": map[string]any{
					"status": map[string]any{"status_str": "success", "completed": true},
					"outputs": map[string]any{
						"out": map[string]any{"images": []map[string]any{{"filename": "out.png", "subfolder": "", "type": "output"}}},
					},
				},
			})
		case "/view":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(outPNG)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	mgr := NewComfyManager()
	cfg := ComfyConfig{URL: srv.URL}
	caps, err := mgr.Probe(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !caps.RMBG3Native || caps.Sa2VANode != "XJSa2VAImageSegmentationV2" || caps.VRAMMiB != 12288 {
		t.Fatalf("unexpected caps: %+v", caps)
	}

	in := comfyTestPNG(t, 4, 4, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	if got, err := mgr.CutoutRMBG3(context.Background(), cfg, "clear glass bottle", in); err != nil || !bytes.Equal(got, outPNG) {
		t.Fatalf("CutoutRMBG3 failed: %v", err)
	}
	if got, err := mgr.CutoutSa2VA(context.Background(), cfg, "ByteDance/Sa2VA-Qwen3-VL-4B", "person on the left", in); err != nil || !bytes.Equal(got, outPNG) {
		t.Fatalf("CutoutSa2VA 4B failed: %v", err)
	}
	if got, err := mgr.CutoutSa2VA(context.Background(), cfg, "ByteDance/Sa2VA-InternVL3-2B", "", in); err != nil || !bytes.Equal(got, outPNG) {
		t.Fatalf("CutoutSa2VA 2B failed: %v", err)
	}

	if len(capturedPrompts) != 3 {
		t.Fatalf("expected 3 prompts, got %d", len(capturedPrompts))
	}
	rmbg := capturedPrompts[0]
	if rmbg["2"]["unet_name"] != "RMBG-3.0-fp8.safetensors" || rmbg["6"]["prompt"] != "clear glass bottle" {
		t.Fatalf("unexpected RMBG3 prompt graph: %+v", rmbg)
	}
	sa4b := capturedPrompts[1]["2"]
	if sa4b["use_8bit"] != true || sa4b["process_detail"] != false || sa4b["segmentation_prompt"] != "person on the left" || sa4b["detail_erode"] != float64(6) {
		t.Fatalf("unexpected Sa2VA 4B inputs on 12GB GPU: %+v", sa4b)
	}
	sa2b := capturedPrompts[2]["2"]
	if sa2b["use_8bit"] != false || sa2b["segmentation_prompt"] == "" {
		t.Fatalf("unexpected Sa2VA 2B inputs: %+v", sa2b)
	}
}
