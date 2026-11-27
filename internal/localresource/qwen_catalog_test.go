package localresource

import "testing"

func TestQwenCatalogSeparatesBaseVariantsAndRemovesDetailer(t *testing.T) {
	if _, ok := ComfyPack("qwen-detail"); ok {
		t.Fatal("removed Turbo Detail Enhancer is still selectable")
	}
	base, ok := ComfyPack("qwen-image-2.1-int8")
	if !ok || base.Files[0].RelPath != "comfyui/diffusion_models/qwen_image_2.1_int8_convrot.safetensors" {
		t.Fatal("ordinary Qwen base missing")
	}
	for _, id := range []string{"qwen-consistency", "qwen-consistency-2000", "qwen-fusal-turbo", "qwen-character-swap", "qwen-mosaic"} {
		pack, ok := ComfyPack(id)
		if !ok || pack.BaseModel != "qwen-image-2.1-turbo-int8" {
			t.Fatalf("missing exact Turbo adapter %s", id)
		}
		if len(pack.Files) != 1 || pack.Files[0].SHA == "" {
			t.Fatalf("adapter lacks verified weight: %s", id)
		}
	}
	pack, _ := ComfyPack("qwen-fusal-turbo")
	download, err := PackDownload(pack)
	if err != nil {
		t.Fatal(err)
	}
	if len(download.Files) != 5 || pack.TotalSize() != 671174216 {
		t.Fatalf("encoder dependency missing or counted as LoRA bytes: %+v", download)
	}
}
