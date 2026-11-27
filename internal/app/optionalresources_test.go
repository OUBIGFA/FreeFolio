package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

func TestOptionalResourcesChooseDirectoryAndDoNotAutoDownload(t *testing.T) {
	a, tester := newTestApp(t)
	a.openSettings(tabResources)
	tester.Frame()
	if !tester.HasText("BEN2") || !tester.HasText("BiRefNet HR Matting") {
		t.Fatal("missing cutout choices")
	}
	root := t.TempDir()
	a.setResourceDirectory(root)
	tester.Frame()
	saved, err := a.prefStore.Load()
	if err != nil || saved.ResourceDir != root || a.resourceDirectory() != root {
		t.Fatalf("resource directory not saved: %v %v", saved.ResourceDir, err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("choosing a folder downloaded files: %v", files)
	}
	for _, item := range localresource.Models {
		if _, ok := tester.Access("下载 " + item.Name); !ok {
			t.Fatalf("missing download action for %s", item.Name)
		}
	}
	if upscale, ok := localresource.UpscaleRuntime(); ok {
		if !tester.HasText(upscale.Name) {
			t.Fatalf("missing upscale choice %s", upscale.Name)
		}
		if _, ok := tester.Access("下载 " + upscale.Name); !ok {
			t.Fatalf("missing download action for %s", upscale.Name)
		}
	}
	if !tester.HasText("ComfyUI 引擎（GPU 加速）") {
		t.Fatal("missing ComfyUI engine section")
	}
	for _, pack := range localresource.ComfyPacks {
		if !tester.HasText(pack.Name) {
			t.Fatalf("missing ComfyUI pack %s", pack.Name)
		}
		if _, ok := tester.Access("下载 " + pack.Name); !ok {
			t.Fatalf("missing download action for %s", pack.Name)
		}
	}
	a.setComfyDirectory(root)
	tester.Frame()
	saved, err = a.prefStore.Load()
	if err != nil || saved.ComfyUI.Dir != root {
		t.Fatalf("ComfyUI directory not saved: %+v %v", saved.ComfyUI, err)
	}
}

func TestBoardCutoutIncludesRMBG3FP8AndSa2VA(t *testing.T) {
	a, _ := newTestApp(t)
	a.resources.comfyCaps = &localresource.ComfyCapabilities{
		Device:      "NVIDIA GeForce RTX 3080 Ti",
		VRAMMiB:     12288,
		RMBG3Native: true,
		RMBG3Models: []string{"RMBG-3.0-fp8.safetensors"},
		Sa2VANode:   "XJSa2VAImageSegmentationV2",
	}
	models := a.boardAIModels(whiteboard.AICutout)
	want := map[string]bool{
		"comfyui-rmbg3\x00rmbg3-fp8":                    false,
		"comfyui-sa2va\x00ByteDance/Sa2VA-InternVL3-2B": false,
		"comfyui-sa2va\x00ByteDance/Sa2VA-Qwen3-VL-4B":  false,
	}
	for _, m := range models {
		k := m.Connection + "\x00" + m.Model
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Fatalf("missing AICutout model %q in %+v", k, models)
		}
	}
}

func TestBoardUpscaleAndImageIncludeSeedVR2ZImageAndQwenImage(t *testing.T) {
	a, _ := newTestApp(t)
	a.resources.comfyCaps = &localresource.ComfyCapabilities{
		Device:          "NVIDIA GeForce RTX 3080 Ti",
		VRAMMiB:         12288,
		SeedVR2Native:   true,
		SeedVR2Models:   []string{"seedvr2_7b_sharp_int8_convrot.safetensors"},
		ZImageNative:    true,
		ZImageModels:    []string{"z_image_turbo_int8_convrot.safetensors"},
		QwenImageNative: true,
		QwenImageModels: []string{"qwen_image_2.1_turbo_int8_convrot.safetensors"},
	}
	var hasSeedVR2 bool
	for _, m := range a.boardAIModels(whiteboard.AIUpscale) {
		if m.Connection == "comfyui-seedvr2" && m.Model == "seedvr2-7b-sharp-int8" {
			hasSeedVR2 = true
		}
	}
	if !hasSeedVR2 {
		t.Fatal("missing SeedVR2 7B Sharp in AIUpscale models")
	}
	var hasZImage, hasQwen bool
	for _, m := range a.boardAIModels(whiteboard.AIImage) {
		if m.Connection == "comfyui-zimage" && m.Model == "z-image-turbo-int8" {
			hasZImage = true
		}
		if m.Connection == "comfyui-qwen-image" && m.Model == "qwen-image-2.1-turbo-int8" {
			hasQwen = true
		}
	}
	if !hasZImage || !hasQwen {
		t.Fatalf("missing ComfyUI image models: zimage=%v qwen=%v", hasZImage, hasQwen)
	}
}

func TestOptionalResourcesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to render optional resources settings")
	a, tt := newTestApp(t)
	tt.SetSize(1240, 820)
	a.resources.gpuChecked = true
	a.resources.gpuDone = true
	a.resources.gpuFound = true
	a.resources.gpu = localresource.GPUInfo{Name: "NVIDIA GeForce RTX 3080 Ti", VRAMMiB: 12288}
	a.resources.comfyCaps = &localresource.ComfyCapabilities{
		Device:        "NVIDIA GeForce RTX 3080 Ti",
		VRAMMiB:       12288,
		RMBG3Native:   true,
		RMBG3Models:   []string{"RMBG-3.0-fp8.safetensors"},
		Sa2VANode:     "XJSa2VAImageSegmentationV2",
		UpscaleModels: []string{"4x-UltraSharp.pth"},
	}
	a.openSettings(tabResources)
	for range 4 {
		tt.Frame()
	}
	outPath := filepath.Join(dir, fmt.Sprintf("settings-%s.png", tabResources))
	file, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsTabsHaveIcons(t *testing.T) {
	for _, tab := range settingsTabs() {
		if settingsIcons[tab.icon] == nil {
			t.Fatalf("settings tab %q (%s) is missing icon %q", tab.id, tab.label, tab.icon)
		}
	}
}
