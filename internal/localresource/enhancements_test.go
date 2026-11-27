package localresource

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnhancementInputKeepsAspectAndAlpha(t *testing.T) {
	data := comfyTestPNG(t, 8, 4, color.NRGBA{R: 90, G: 130, B: 180, A: 128})
	output, err := enhancementInput(data)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 2048 || img.Bounds().Dy() != 1024 {
		t.Fatalf("wrong enhancement size: %v", img.Bounds())
	}
	_, _, _, alpha := img.At(100, 100).RGBA()
	if alpha != 0x8080 {
		t.Fatalf("lost alpha: %x", alpha)
	}
}

func TestLoRARejectsMissingTamperedAndInvalidStrength(t *testing.T) {
	pack, _ := ComfyPack("z-detail")
	caps := &ComfyCapabilities{LoRANative: true, LoRAModels: []string{"freefolio-z-detail.safetensors"}}
	root := t.TempDir()
	req := ComfyImageRequest{LoRA: "z-detail", Strength: .35}
	if _, err := prepareLoRA(ComfyConfig{ResourceDir: root}, caps, &req, "zimage"); err == nil {
		t.Fatal("missing local file accepted")
	}
	f := filepath.Join(root, pack.Files[0].RelPath)
	if err := os.MkdirAll(filepath.Dir(f), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareLoRA(ComfyConfig{ResourceDir: root}, caps, &req, "zimage"); err == nil {
		t.Fatal("tampered local file accepted")
	}
	for _, strength := range []float64{-1, 3, math.NaN(), math.Inf(1)} {
		req.Strength = strength
		if _, err := prepareLoRA(ComfyConfig{}, caps, &req, "zimage"); err == nil {
			t.Fatal("invalid strength accepted")
		}
	}
	req.Strength = .35
	caps.LoRANative = false
	if _, err := prepareLoRA(ComfyConfig{}, caps, &req, "zimage"); err == nil {
		t.Fatal("missing native loader accepted")
	}
	req.Strength = 0
	if name, err := prepareLoRA(ComfyConfig{}, caps, &req, "zimage"); err != nil || name != "" {
		t.Fatal("disabled adapter still requires its file or loader")
	}
}

func TestEnhancementDownloadsIncludeMatchingBase(t *testing.T) {
	for _, id := range []string{"z-detail", "qwen-restore", "qwen-consistency"} {
		pack, _ := ComfyPack(id)
		plan, err := PackDownload(pack)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Files) != 4 || !strings.Contains(plan.Files[0].RelPath, "diffusion_models/") || !strings.Contains(plan.Files[3].RelPath, "loras/") {
			t.Fatalf("download lacks matching base and LoRA: %+v", plan)
		}
		if id == "z-detail" && !strings.Contains(plan.Files[0].RelPath, "z_image_turbo") {
			t.Fatal("wrong base")
		}
		if id != "z-detail" && !strings.Contains(plan.Files[0].RelPath, "qwen_image_2.1_turbo") {
			t.Fatal("wrong base")
		}
	}
}

func TestRemovePackRetainsAnotherInstalledPacksSharedVAE(t *testing.T) {
	root := t.TempDir()
	file := func(name, content string) PackFile {
		p := PackFile{RelPath: "comfyui/" + name, Size: int64(len(content)), SHA: fmt.Sprintf("%x", sha256.Sum256([]byte(content)))}
		target := filepath.Join(root, p.RelPath)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	shared := file("vae/shared.safetensors", "shared")
	a := ResourcePack{ID: "3b", Files: []PackFile{file("diffusion_models/3b.safetensors", "3b"), shared}}
	b := ResourcePack{ID: "7b", Files: []PackFile{file("diffusion_models/7b.safetensors", "7b"), shared}}
	old := ComfyPacks
	ComfyPacks = []ResourcePack{a, b}
	t.Cleanup(func() { ComfyPacks = old })
	if err := RemovePack(root, a, os.Remove); err != nil {
		t.Fatal(err)
	}
	if !PackPresent(root, b) {
		t.Fatal("removing 3B broke installed 7B")
	}
	if err := RemovePack(root, b, os.Remove); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, shared.RelPath)); !os.IsNotExist(err) {
		t.Fatal("unshared VAE not removed")
	}
}
