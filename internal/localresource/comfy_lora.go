package localresource

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

// prepareLoRA checks the selected family and exact catalog filename before
// submitting a graph; an unavailable adapter must never silently disappear.
func prepareLoRA(cfg ComfyConfig, caps *ComfyCapabilities, req *ComfyImageRequest, family string) (string, error) {
	if req.LoRA == "" {
		return "", nil
	}
	pack, ok := ComfyPack(req.LoRA)
	if !ok || pack.Kind != "lora-"+family {
		return "", fmt.Errorf("LoRA is not compatible with this model: %s", req.LoRA)
	}
	if math.IsNaN(req.Strength) || math.IsInf(req.Strength, 0) || req.Strength < 0 || req.Strength > 2 {
		return "", fmt.Errorf("LoRA strength must be between 0 and 2")
	}
	if req.Strength == 0 {
		return "", nil
	}
	if !caps.LoRANative {
		return "", fmt.Errorf("ComfyUI does not expose LoraLoaderModelOnly; update ComfyUI first")
	}
	name := filepath.Base(pack.Files[0].RelPath)
	found := false
	for _, candidate := range caps.LoRAModels {
		if candidate == name {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("LoRA %s is not available; download it in Settings → Optional resources and reconnect ComfyUI", pack.Name)
	}
	if cfg.ResourceDir != "" {
		if _, err := verifyPackFile(cfg.ResourceDir, pack.Files[0]); err != nil {
			return "", fmt.Errorf("verify %s: %w", pack.Name, err)
		}
	}
	if req.Strength != 0 && pack.Trigger != "" && !strings.Contains(strings.ToLower(req.Prompt), strings.ToLower(pack.Trigger)) {
		req.Prompt = pack.Trigger + ", " + req.Prompt
	}
	return name, nil
}

// enhancementInput follows the restore adapter's approximately 2 MP input
// convention, keeping aspect and alpha without depending on custom nodes.
func enhancementInput(data []byte) ([]byte, error) {
	if len(data) > maxInput {
		return nil, fmt.Errorf("input image exceeds 64 MiB")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 32*1024*1024 {
		return nil, fmt.Errorf("input image exceeds 32 megapixels")
	}
	factor := math.Sqrt(2 * 1024 * 1024 / (float64(cfg.Width) * float64(cfg.Height)))
	w := max(32, int(math.Round(float64(cfg.Width)*factor/32))*32)
	h := max(32, int(math.Round(float64(cfg.Height)*factor/32))*32)
	if w > 8192 || h > 8192 {
		return nil, fmt.Errorf("image aspect ratio is too extreme for enhancement")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	err = png.Encode(&out, dst)
	return out.Bytes(), err
}

// attachLoRA keeps all model consumers on the same patched model.
func attachLoRA(graph map[string]any, name string, strength float64) []any {
	if name == "" || strength == 0 {
		return []any{"1", 0}
	}
	graph["lora"] = map[string]any{"class_type": "LoraLoaderModelOnly", "inputs": map[string]any{"model": []any{"1", 0}, "lora_name": name, "strength_model": strength}}
	return []any{"lora", 0}
}
