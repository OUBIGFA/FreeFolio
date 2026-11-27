package localresource

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PackFile describes one file within a multi-file ComfyUI model pack.
type PackFile struct {
	RelPath string
	URL     string
	Size    int64
	SHA     string
}

// ResourcePack describes a multi-file optional model pack downloaded into the
// ComfyUI extra model directory under resourceDir.
type ResourcePack struct {
	ID             string
	Name           string
	License        string
	Kind           string // "rmbg3" or "sa2va"
	ModelID        string
	MinVRAMGiB     int
	Use8BitDefault bool
	Files          []PackFile
	// BaseModel is an optional catalog dependency downloaded with a LoRA.
	BaseModel       string
	Category        string
	Trigger         string
	Instruction     string
	DefaultStrength float64
}

// TotalSize returns the total byte size of all files in the pack.
func (p ResourcePack) TotalSize() int64 {
	var total int64
	for _, f := range p.Files {
		total += f.Size
	}
	return total
}

func hfPackFile(localPrefix, repoBase, name string, size int64, sha string) PackFile {
	return PackFile{
		RelPath: localPrefix + "/" + name,
		URL:     repoBase + name,
		Size:    size,
		SHA:     sha,
	}
}

// ComfyPacks defines the GPU-accelerated quantized model packs that can be
// downloaded on demand into resourceDir/comfyui/.
var ComfyPacks = append([]ResourcePack{
	{
		ID:         "rmbg3-fp8",
		Name:       "BRIA RMBG-3.0 (FP8)",
		License:    "CC-BY-NC-4.0",
		Kind:       "rmbg3",
		ModelID:    "rmbg3-fp8",
		MinVRAMGiB: 8,
		Files: []PackFile{
			{
				RelPath: "comfyui/diffusion_models/RMBG-3.0-fp8.safetensors",
				URL:     "https://huggingface.co/1038lab/RMBG-3.0/resolve/main/RMBG-3.0-fp8.safetensors",
				Size:    8131690763,
				SHA:     "f0e2fbe70aa0438683d2d8ac02a49d482de53a90bea1df6cbc2102db08eb55fd",
			},
			{
				RelPath: "comfyui/text_encoders/qwen3vl_4b_int8_convrot.safetensors",
				URL:     "https://huggingface.co/briaai/RMBG-3.0-Comfy/resolve/main/text_encoders/qwen3vl_4b_int8_convrot.safetensors",
				Size:    4858559240,
				SHA:     "32de113cbf8282b02cbff0794b637b9d6676da09314a1db5a30bcea9bf4ee01c",
			},
			{
				RelPath: "comfyui/vae/flux2-vae.safetensors",
				URL:     "https://huggingface.co/briaai/RMBG-3.0-Comfy/resolve/main/vae/flux2-vae.safetensors",
				Size:    336211292,
				SHA:     "868fe7b343cc8f3a19dbcfcafbc3d5f888802be3f89bd81b65b3621a066ce8f3",
			},
		},
	},
	{
		ID:         "sa2va-internvl3-2b",
		Name:       "Sa2VA-InternVL3-2B",
		License:    "Apache-2.0",
		Kind:       "sa2va",
		ModelID:    "ByteDance/Sa2VA-InternVL3-2B",
		MinVRAMGiB: 6,
		Files: []PackFile{
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "added_tokens.json", 881, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "chat_template.jinja", 875, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "config.json", 3749, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "configuration_intern_vit.py", 5548, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "configuration_internlm2.py", 7005, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "configuration_phi3.py", 10396, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "configuration_sa2va_chat.py", 5632, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "flash_attention.py", 3459, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "merges.txt", 1671853, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "model-00001-of-00002.safetensors", 4986531024, "5f9376af5c21d92c0b03c3c6d4f4ff1cd55c6023074a26aea4a5f70058f3ca2d"),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "model-00002-of-00002.safetensors", 3670290768, "03dedd090c2292445928ecc649ebc6ee1713eac468d2ab97f88d1d048378aec4"),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "model.safetensors.index.json", 170030, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "modeling_intern_vit.py", 14906, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "modeling_internlm2.py", 61744, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "modeling_phi3.py", 74445, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "modeling_sa2va_chat.py", 38707, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "sam2.py", 176490, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "special_tokens_map.json", 613, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "templates.py", 6956, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "tokenization_internlm2.py", 8791, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "tokenization_internlm2_fast.py", 7788, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "tokenizer.json", 11424454, "6d47206c0ce94d1927b85b19c789f12d3cb3cceacb4b84f4adcdbd54bb0c94cd"),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "tokenizer_config.json", 7156, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-InternVL3-2B", "https://huggingface.co/ByteDance/Sa2VA-InternVL3-2B/resolve/main/", "vocab.json", 2776833, ""),
		},
	},
	{
		ID:             "sa2va-qwen3-vl-4b",
		Name:           "Sa2VA-Qwen3-VL-4B",
		License:        "Apache-2.0",
		Kind:           "sa2va",
		ModelID:        "ByteDance/Sa2VA-Qwen3-VL-4B",
		MinVRAMGiB:     10,
		Use8BitDefault: true,
		Files: []PackFile{
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "added_tokens.json", 798, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "chat_template.jinja", 5292, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "config.json", 1721, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "configuration_sa2va_chat.py", 908, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "merges.txt", 1671853, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "model-00001-of-00005.safetensors", 4934328696, "ed2f72ade8566319cf154c12f4765166fa4bc2524614aa446aef2a123dd28f09"),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "model-00002-of-00005.safetensors", 4944311840, "11aa35d66d696c6e4ec0b4246a9edb0f81049bd59e94cf4a376dd8713eddec71"),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "model-00003-of-00005.safetensors", 4944311896, "991fc96652cddd8992e32aa1cfe61b4c153cca335b72c2c26341ff185735649c"),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "model-00004-of-00005.safetensors", 4998026056, "600c3ea1ea7fe5421a30ef8b67bba119eeb10168818bfe57a9852a8b9572543b"),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "model-00005-of-00005.safetensors", 407536000, "35642f38b2551f8a32c3ee8bf9b579ebc71c9e7366601d4250e2407a20157ff4"),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "model.safetensors.index.json", 176696, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "modeling_sa2va_qwen.py", 9407, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "preprocessor_config.json", 782, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "sam2.py", 176490, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "special_tokens_map.json", 613, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "templates.py", 6956, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "tokenizer.json", 11423560, "ade4f08c34dfcbd3b4d11082162940b785e27af18f916549aa4bc223155c91b8"),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "tokenizer_config.json", 6328, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "video_preprocessor_config.json", 817, ""),
			hfPackFile("comfyui/sa2va/ByteDance/Sa2VA-Qwen3-VL-4B", "https://huggingface.co/ByteDance/Sa2VA-Qwen3-VL-4B/resolve/main/", "vocab.json", 2776833, ""),
		},
	},
	{
		ID:         "seedvr2-7b-sharp-int8",
		Name:       "SeedVR2 7B Sharp (INT8)",
		License:    "Apache-2.0",
		Kind:       "seedvr2",
		ModelID:    "seedvr2-7b-sharp-int8",
		MinVRAMGiB: 10,
		Files: []PackFile{
			{
				RelPath: "comfyui/diffusion_models/seedvr2_7b_sharp_int8_convrot.safetensors",
				URL:     "https://huggingface.co/Comfy-Org/SeedVR2/resolve/main/diffusion_models/seedvr2_7b_sharp_int8_convrot.safetensors",
				Size:    8334897976,
				SHA:     "db48be2f1cc7e36b01a2aa529810f5d9c6a971edd29be225cf1b0eb18d51c366",
			},
			{
				RelPath: "comfyui/vae/seedvr2_ema_vae_fp16.safetensors",
				URL:     "https://huggingface.co/Comfy-Org/SeedVR2/resolve/main/vae/seedvr2_ema_vae_fp16.safetensors",
				Size:    501324814,
				SHA:     "20678548f420d98d26f11442d3528f8b8c94e57ee046ef93dbb7633da8612ca1",
			},
		},
	},
	{
		ID:         "z-image-turbo-int8",
		Name:       "Z-Image-Turbo (INT8)",
		License:    "Apache-2.0",
		Kind:       "zimage",
		ModelID:    "z-image-turbo-int8",
		MinVRAMGiB: 8,
		Files: []PackFile{
			{
				RelPath: "comfyui/diffusion_models/z_image_turbo_int8_convrot.safetensors",
				URL:     "https://huggingface.co/Comfy-Org/z_image_turbo/resolve/main/split_files/diffusion_models/z_image_turbo_int8_convrot.safetensors",
				Size:    6201001296,
				SHA:     "be517ebd47c912a5626a588e1aeea43e6be4a43c0cdcd2b48a2a780d9f358635",
			},
			{
				RelPath: "comfyui/text_encoders/qwen_3_4b_fp8_mixed.safetensors",
				URL:     "https://huggingface.co/Comfy-Org/z_image_turbo/resolve/main/split_files/text_encoders/qwen_3_4b_fp8_mixed.safetensors",
				Size:    5631994051,
				SHA:     "72450b19758172c5a7273cf7de729d1c17e7f434a104a00167624cba94f68f15",
			},
			{
				RelPath: "comfyui/vae/ae.safetensors",
				URL:     "https://huggingface.co/Comfy-Org/z_image_turbo/resolve/main/split_files/vae/ae.safetensors",
				Size:    335304388,
				SHA:     "afc8e28272cd15db3919bacdb6918ce9c1ed22e96cb12c4d5ed0fba823529e38",
			},
		},
	},
	{
		ID:         "qwen-image-2.1-turbo-int8",
		Name:       "Qwen-Image 2.1 Turbo (INT8)",
		License:    "Qwen",
		Kind:       "qwen-image",
		ModelID:    "qwen-image-2.1-turbo-int8",
		MinVRAMGiB: 10,
		Files: []PackFile{
			{
				RelPath: "comfyui/diffusion_models/qwen_image_2.1_turbo_int8_convrot.safetensors",
				URL:     "https://huggingface.co/Comfy-Org/Qwen-Image-2.1/resolve/main/diffusion_models/qwen_image_2.1_turbo_int8_convrot.safetensors",
				Size:    7256787829,
				SHA:     "8144a0182964aa8403ad6dbc011db837304167d235161b9aabaa8532e967314c",
			},
			{
				RelPath: "comfyui/text_encoders/qwen3vl_8b_int8_convrot.safetensors",
				URL:     "https://huggingface.co/Comfy-Org/Qwen-Image-2.1/resolve/main/text_encoders/qwen3vl_8b_int8_convrot.safetensors",
				Size:    9350798360,
				SHA:     "8bfd0f6e12abf2d2d697ecc888e5e90b0d6741d6708f05799f53afa560452e8f",
			},
			{
				RelPath: "comfyui/vae/qwen_image_2.1_vae_bf16.safetensors",
				URL:     "https://huggingface.co/Comfy-Org/Qwen-Image-2.1/resolve/main/vae/qwen_image_2.1_vae_bf16.safetensors",
				Size:    675509688,
				SHA:     "bb21f7473051e1ac368515dd3f2e15cd44d7a11748ee8823e1ddca3e4876b7c9",
			},
		},
	},
}, enhancementPacks...)

// ComfyPack returns the resource pack matching id.
func ComfyPack(id string) (ResourcePack, bool) {
	for _, p := range ComfyPacks {
		if p.ID == id {
			return p, true
		}
	}
	return ResourcePack{}, false
}

// ComfyPackByModel returns the resource pack matching ModelID.
func ComfyPackByModel(modelID string) (ResourcePack, bool) {
	for _, p := range ComfyPacks {
		if p.ModelID == modelID {
			return p, true
		}
	}
	return ResourcePack{}, false
}

// PackDownload includes a LoRA's base without making removal delete that base.
// Each catalog LoRA has one complete base pack; installed files are reused.
func PackDownload(pack ResourcePack) (ResourcePack, error) {
	if pack.BaseModel == "" {
		return pack, nil
	}
	base, ok := ComfyPack(pack.BaseModel)
	if !ok {
		return ResourcePack{}, fmt.Errorf("unknown base model: %s", pack.BaseModel)
	}
	pack.Files = append(append([]PackFile(nil), base.Files...), pack.Files...)
	return pack, nil
}

func resolvePackFilePath(root, rel string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("invalid resource directory")
	}
	nativeRel := filepath.FromSlash(strings.TrimSpace(rel))
	if nativeRel == "" || !filepath.IsLocal(nativeRel) {
		return "", fmt.Errorf("unsafe pack file path: %s", rel)
	}
	return filepath.Join(root, nativeRel), nil
}

func verifyPackFile(root string, pf PackFile) (string, error) {
	target, err := resolvePackFilePath(root, pf.RelPath)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != pf.Size {
		return "", fmt.Errorf("unexpected pack file: %s", pf.RelPath)
	}
	if pf.SHA == "" {
		return target, nil
	}
	f, err := os.Open(target)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, pf.Size+1))
	if err != nil {
		return "", err
	}
	if n != pf.Size || fmt.Sprintf("%x", h.Sum(nil)) != pf.SHA {
		return "", fmt.Errorf("SHA-256 mismatch: %s", pf.RelPath)
	}
	return target, nil
}

// PackPresent reports whether every file in pack exists on disk with its exact size.
func PackPresent(root string, pack ResourcePack) bool {
	if len(pack.Files) == 0 {
		return false
	}
	for _, pf := range pack.Files {
		target, err := resolvePackFilePath(root, pf.RelPath)
		if err != nil {
			return false
		}
		info, err := os.Lstat(target)
		if err != nil || !info.Mode().IsRegular() || info.Size() != pf.Size {
			return false
		}
	}
	return true
}

// InstallPack downloads any missing verified files in pack into root/comfyui/...
// Verified files are skipped and interrupted weight files resume by byte range.
func InstallPack(ctx context.Context, root string, pack ResourcePack, progress func(done, total int64)) error {
	client := &http.Client{Timeout: 2 * time.Hour, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.User != nil || len(via) >= 10 {
			return errors.New("unsafe resource redirect")
		}
		return nil
	}}
	return installPack(ctx, root, pack, client, progress)
}

func installPack(ctx context.Context, root string, pack ResourcePack, client *http.Client, progress func(done, total int64)) error {
	total := pack.TotalSize()
	var completed int64
	if progress != nil {
		progress(0, total)
	}
	for _, pf := range pack.Files {
		target, err := verifyPackFile(root, pf)
		if err == nil {
			completed += pf.Size
			if progress != nil {
				progress(completed, total)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		target, err = resolvePackFilePath(root, pf.RelPath)
		if err != nil {
			return err
		}
		err = downloadFile(ctx, target, pf.URL, pf.Size, pf.SHA, client, func(done int64) {
			if progress != nil {
				progress(completed+done, total)
			}
		})
		if err != nil {
			return err
		}
		completed += pf.Size
	}
	return nil
}

// RemovePack verifies all present pack files against expected size and SHA-256
// before passing each file to trash. Missing files are skipped cleanly.
func RemovePack(root string, pack ResourcePack, trash func(string) error) error {
	var targets []string
	for _, pf := range pack.Files {
		// SeedVR2 variants share a VAE; removing one must leave the other
		// installed variant usable. A standalone LoRA never owns its base.
		shared := false
		for _, other := range ComfyPacks {
			if other.ID == pack.ID || !PackPresent(root, other) {
				continue
			}
			for _, f := range other.Files {
				if f.RelPath == pf.RelPath && f.SHA == pf.SHA {
					shared = true
				}
			}
		}
		if shared {
			continue
		}
		target, err := verifyPackFile(root, pf)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		targets = append(targets, target)
	}
	for _, target := range targets {
		if err := trash(target); err != nil {
			return err
		}
	}
	return nil
}
