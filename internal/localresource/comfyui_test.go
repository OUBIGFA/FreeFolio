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

	"go.yaml.in/yaml/v3"
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

	resDir := filepath.Join(t.TempDir(), "my resources #1")
	yamlPath, err := WriteComfyExtraModelPaths(resDir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]map[string]string
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v\n%s", err, string(raw))
	}
	ff := parsed["freefolio"]
	if ff["base_path"] != filepath.ToSlash(filepath.Join(resDir, "comfyui")) ||
		ff["upscale_models"] != "upscale_models" ||
		ff["sa2va"] != "sa2va" ||
		ff["vitmatte"] != "vitmatte" {
		t.Fatalf("unexpected extra_model_paths.yaml:\n%s", string(raw))
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDetectComfyLayoutsAllVariants(t *testing.T) {
	t.Run("PortableAndSubdir", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, "ComfyUI", "main.py"), "# portable")
		writeTestFile(t, filepath.Join(root, "python_embeded", "python.exe"), "py")

		for _, pick := range []string{root, filepath.Join(root, "ComfyUI")} {
			layout, err := detectComfyLayout(pick, comfyDetectEnv{goos: "windows"})
			if err != nil {
				t.Fatalf("pick=%s err=%v", pick, err)
			}
			if layout.MainPy != filepath.Join(root, "ComfyUI", "main.py") ||
				layout.Python != filepath.Join(root, "python_embeded", "python.exe") ||
				!layout.IsolatedPython || layout.Source != "portable" {
				t.Fatalf("pick=%s unexpected layout: %+v", pick, layout)
			}
		}
	})

	t.Run("ThirdPartyPackPythonEmbedded", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, "ComfyUI_windows_portable", "ComfyUI", "main.py"), "# pack")
		writeTestFile(t, filepath.Join(root, "ComfyUI_windows_portable", "python_embedded", "python.exe"), "py")

		layout, err := detectComfyLayout(root, comfyDetectEnv{goos: "windows"})
		if err != nil {
			t.Fatal(err)
		}
		if layout.MainPy != filepath.Join(root, "ComfyUI_windows_portable", "ComfyUI", "main.py") ||
			layout.Python != filepath.Join(root, "ComfyUI_windows_portable", "python_embedded", "python.exe") ||
			!layout.IsolatedPython {
			t.Fatalf("unexpected layout: %+v", layout)
		}
	})

	t.Run("SourceVenvWindowsAndUnix", func(t *testing.T) {
		winRoot := t.TempDir()
		writeTestFile(t, filepath.Join(winRoot, "main.py"), "# src")
		writeTestFile(t, filepath.Join(winRoot, ".venv", "Scripts", "python.exe"), "py")
		layout, err := detectComfyLayout(winRoot, comfyDetectEnv{goos: "windows"})
		if err != nil {
			t.Fatal(err)
		}
		if layout.Python != filepath.Join(winRoot, ".venv", "Scripts", "python.exe") || !layout.IsolatedPython {
			t.Fatalf("unexpected windows venv layout: %+v", layout)
		}

		unixRoot := t.TempDir()
		writeTestFile(t, filepath.Join(unixRoot, "main.py"), "# src")
		writeTestFile(t, filepath.Join(unixRoot, "venv", "bin", "python3"), "py")
		layout, err = detectComfyLayout(unixRoot, comfyDetectEnv{goos: "linux"})
		if err != nil {
			t.Fatal(err)
		}
		if layout.Python != filepath.Join(unixRoot, "venv", "bin", "python3") || !layout.IsolatedPython {
			t.Fatalf("unexpected linux venv layout: %+v", layout)
		}
	})

	t.Run("ComfyDesktopV2UpgradedOverLegacy", func(t *testing.T) {
		root := t.TempDir()
		programsDir := filepath.Join(root, "Programs", "ComfyUI")
		writeTestFile(t, filepath.Join(programsDir, "ComfyUI.exe"), "exe")
		writeTestFile(t, filepath.Join(programsDir, "Comfy Desktop", "Comfy Desktop.exe"), "exe")
		// Stale legacy embedded ComfyUI should NOT be preferred over active Comfy Desktop v2 install.
		writeTestFile(t, filepath.Join(programsDir, "resources", "ComfyUI", "main.py"), "# stale 0.19.3")

		v2Install := filepath.Join(root, "Comfy-Desktop", "ComfyUI-Installs", "ComfyUI")
		v2Main := filepath.Join(v2Install, "ComfyUI", "main.py")
		writeTestFile(t, v2Main, "# v0.46.2")

		dataDir := filepath.Join(root, "Data")
		v2Py := filepath.Join(dataDir, ".venv", "Scripts", "python.exe")
		writeTestFile(t, v2Py, "py")
		if err := os.MkdirAll(filepath.Join(dataDir, "input"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dataDir, "output"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dataDir, "user"), 0o700); err != nil {
			t.Fatal(err)
		}

		appData := filepath.Join(root, "AppData")
		instYAML := filepath.Join(appData, "Comfy Desktop", "instance-model-paths", "inst-local.yaml")
		writeTestFile(t, instYAML, "comfy:\n  base_path: .\n")
		installationsJSON, _ := json.Marshal([]map[string]any{
			{
				"id":             "inst-cloud",
				"sourceId":       "cloud",
				"remoteUrl":      "https://cloud.comfy.org/",
				"status":         "installed",
				"lastLaunchedAt": 9999999999,
			},
			{
				"id":                "inst-local",
				"sourceId":          "standalone",
				"status":            "installed",
				"installPath":       v2Install,
				"adopted":           true,
				"adoptedBaseDir":    dataDir,
				"adoptedPythonPath": v2Py,
				"inputDir":          filepath.Join(dataDir, "input"),
				"outputDir":         filepath.Join(dataDir, "output"),
				"lastLaunchedAt":    1000,
			},
		})
		writeTestFile(t, filepath.Join(appData, "Comfy Desktop", "installations.json"), string(installationsJSON))

		env := comfyDetectEnv{userConfigDir: appData, localAppData: filepath.Join(root, "LocalAppData"), goos: "windows"}
		for _, pick := range []string{
			programsDir,
			filepath.Join(programsDir, "Comfy Desktop"),
			v2Install,
			filepath.Join(v2Install, "ComfyUI"),
			filepath.Dir(v2Install),
			dataDir,
		} {
			layout, err := detectComfyLayout(pick, env)
			if err != nil {
				t.Fatalf("pick=%s err=%v", pick, err)
			}
			if layout.MainPy != v2Main || layout.Python != v2Py || layout.BaseDir != dataDir ||
				layout.UserDir != filepath.Join(dataDir, "user") ||
				layout.InputDir != filepath.Join(dataDir, "input") ||
				layout.OutputDir != filepath.Join(dataDir, "output") ||
				len(layout.ExtraModelPaths) != 1 || layout.ExtraModelPaths[0] != instYAML ||
				!layout.IsolatedPython || layout.Source != "comfy-desktop" {
				t.Fatalf("pick=%s unexpected layout: %+v", pick, layout)
			}
		}
	})

	t.Run("ComfyDesktopV2MultipleInstallsRanking", func(t *testing.T) {
		root := t.TempDir()
		appData := filepath.Join(root, "AppData")
		programsDir := filepath.Join(root, "Programs", "ComfyUI")
		writeTestFile(t, filepath.Join(programsDir, "Comfy Desktop", "Comfy Desktop.exe"), "exe")

		instA := filepath.Join(root, "Installs", "A")
		instB := filepath.Join(root, "Installs", "B")
		writeTestFile(t, filepath.Join(instA, "ComfyUI", "main.py"), "# A")
		writeTestFile(t, filepath.Join(instA, ".venv", "Scripts", "python.exe"), "pyA")
		writeTestFile(t, filepath.Join(instB, "ComfyUI", "main.py"), "# B")
		writeTestFile(t, filepath.Join(instB, ".venv", "Scripts", "python.exe"), "pyB")

		installs, _ := json.Marshal([]map[string]any{
			{"id": "a", "sourceId": "standalone", "status": "installed", "installPath": instA, "lastLaunchedAt": 100},
			{"id": "b", "sourceId": "standalone", "status": "installed", "installPath": instB, "lastLaunchedAt": 200},
		})
		writeTestFile(t, filepath.Join(appData, "Comfy Desktop", "installations.json"), string(installs))
		env := comfyDetectEnv{userConfigDir: appData, goos: "windows"}

		layout, err := detectComfyLayout(programsDir, env)
		if err != nil {
			t.Fatal(err)
		}
		if layout.MainPy != filepath.Join(instB, "ComfyUI", "main.py") {
			t.Fatalf("expected latest launched install B, got %+v", layout)
		}

		writeTestFile(t, filepath.Join(appData, "Comfy Desktop", "settings.json"), `{"autoLaunchOnStartup":"a"}`)
		layout, err = detectComfyLayout(programsDir, env)
		if err != nil {
			t.Fatal(err)
		}
		if layout.MainPy != filepath.Join(instA, "ComfyUI", "main.py") {
			t.Fatalf("expected autoLaunchOnStartup install A, got %+v", layout)
		}
	})

	t.Run("LegacyComfyUIDesktopV0", func(t *testing.T) {
		root := t.TempDir()
		localAppData := filepath.Join(root, "LocalAppData")
		appDir := filepath.Join(localAppData, "Programs", "ComfyUI")
		mainPy := filepath.Join(appDir, "resources", "ComfyUI", "main.py")
		writeTestFile(t, mainPy, "# legacy")

		baseDir := filepath.Join(root, "ComfyData")
		py := filepath.Join(baseDir, ".venv", "Scripts", "python.exe")
		writeTestFile(t, py, "py")
		if err := os.MkdirAll(filepath.Join(baseDir, "user"), 0o700); err != nil {
			t.Fatal(err)
		}

		appData := filepath.Join(root, "AppData")
		legacyYAML := filepath.Join(appData, "ComfyUI", "extra_models_config.yaml")
		writeTestFile(t, legacyYAML, "comfyui:\n  base_path: .\n")
		cfgJSON, _ := json.Marshal(map[string]string{"basePath": baseDir})
		writeTestFile(t, filepath.Join(appData, "ComfyUI", "config.json"), string(cfgJSON))

		env := comfyDetectEnv{userConfigDir: appData, localAppData: localAppData, goos: "windows"}
		for _, pick := range []string{appDir, baseDir} {
			layout, err := detectComfyLayout(pick, env)
			if err != nil {
				t.Fatalf("pick=%s err=%v", pick, err)
			}
			if layout.MainPy != mainPy || layout.Python != py || layout.BaseDir != baseDir ||
				len(layout.ExtraModelPaths) != 1 || layout.ExtraModelPaths[0] != legacyYAML ||
				layout.Source != "comfyui-desktop" {
				t.Fatalf("pick=%s unexpected layout: %+v", pick, layout)
			}
		}
	})

	t.Run("WindowsAppsStubFilteredOut", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, "main.py"), "# src")
		stub := filepath.Join(root, "LocalAppData", "Microsoft", "WindowsApps", "python.exe")
		writeTestFile(t, stub, "")

		_, err := detectComfyLayout(root, comfyDetectEnv{
			goos: "windows",
			lookPath: func(bin string) (string, error) {
				if bin == "python" {
					return stub, nil
				}
				return "", os.ErrNotExist
			},
		})
		if err == nil {
			t.Fatal("expected error when only WindowsApps python stub exists")
		}
	})
}

func TestBuildComfyArgsAndEnv(t *testing.T) {
	root := t.TempDir()
	mainPy := filepath.Join(root, "ComfyUI", "main.py")
	py := filepath.Join(root, "Data", ".venv", "Scripts", "python.exe")
	writeTestFile(t, mainPy, "# main")
	writeTestFile(t, py, "py")
	writeTestFile(t, filepath.Join(root, "Data", ".venv", "pyvenv.cfg"), "home = C:\\Python312\n")
	instYAML := filepath.Join(root, "inst.yaml")
	ffYAML := filepath.Join(root, "ff.yaml")
	writeTestFile(t, instYAML, "a: 1\n")
	writeTestFile(t, ffYAML, "b: 2\n")

	layout := ComfyLayout{
		Root:            filepath.Dir(mainPy),
		MainPy:          mainPy,
		Python:          py,
		BaseDir:         filepath.Join(root, "Data"),
		UserDir:         filepath.Join(root, "Data", "user"),
		InputDir:        filepath.Join(root, "Data", "input"),
		OutputDir:       filepath.Join(root, "Data", "output"),
		ExtraModelPaths: []string{instYAML, instYAML, filepath.Join(root, "missing.yaml")},
		IsolatedPython:  true,
	}
	args := buildComfyArgs(layout, 8199, ffYAML, "windows")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-s " + mainPy,
		"--listen 127.0.0.1 --port 8199 --disable-auto-launch",
		"--base-directory " + layout.BaseDir,
		"--user-directory " + layout.UserDir,
		"--input-directory " + layout.InputDir,
		"--output-directory " + layout.OutputDir,
		"--extra-model-paths-config " + instYAML,
		"--extra-model-paths-config " + ffYAML,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %v", want, args)
		}
	}
	if strings.Count(joined, instYAML) != 1 {
		t.Fatalf("duplicate extra-model-paths-config in %v", args)
	}

	env := buildComfyEnv([]string{"PATH=C:\\Windows\\System32"}, layout, "windows")
	envMap := map[string]string{}
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if ok {
			envMap[k] = v
		}
	}
	if envMap["PYTHONUNBUFFERED"] != "1" || envMap["PYTHONUTF8"] != "1" || envMap["PYTHONIOENCODING"] != "utf-8" {
		t.Fatalf("unexpected python env vars: %+v", envMap)
	}
	if envMap["VIRTUAL_ENV"] != filepath.Join(root, "Data", ".venv") {
		t.Fatalf("unexpected VIRTUAL_ENV: %q", envMap["VIRTUAL_ENV"])
	}
	if !strings.HasPrefix(envMap["PATH"], filepath.Join(root, "Data", ".venv", "Scripts")+string(os.PathListSeparator)) {
		t.Fatalf("venv Scripts not prepended to PATH: %q", envMap["PATH"])
	}
}

func TestComfyWorkflowExecutionErrorDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/system_stats":
			_ = json.NewEncoder(w).Encode(map[string]any{"system": map[string]any{"comfyui_version": "0.46.2"}})
		case "/upload/image":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "in.png"})
		case "/prompt":
			_ = json.NewEncoder(w).Encode(map[string]any{"prompt_id": "err-1"})
		case "/history/err-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"err-1": map[string]any{
					"status": map[string]any{
						"status_str": "error",
						"completed":  false,
						"messages": [][]any{
							{"execution_start", map[string]any{"prompt_id": "err-1"}},
							{"execution_error", map[string]any{"node_type": "RMBG", "exception_message": "CUDA out of memory"}},
						},
					},
				},
			})
		}
	}))
	defer srv.Close()

	mgr := NewComfyManager()
	in := comfyTestPNG(t, 4, 4, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	_, err := mgr.Cutout(context.Background(), ComfyConfig{URL: srv.URL}, "RMBG-3.0", in)
	if err == nil || !strings.Contains(err.Error(), "RMBG") || !strings.Contains(err.Error(), "CUDA out of memory") {
		t.Fatalf("expected detailed ComfyUI execution error, got %v", err)
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
