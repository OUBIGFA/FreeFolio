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
	if !strings.Contains(text, "base_path: "+filepath.ToSlash(filepath.Join(resDir, "comfyui"))) || !strings.Contains(text, "upscale_models: upscale_models") {
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
