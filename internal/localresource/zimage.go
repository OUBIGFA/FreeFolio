package localresource

// These are three encodings of the same undistilled model. Keep exact filenames
// so an unavailable ordinary weight never falls back to a Turbo checkpoint.
var zImagePacks = []ResourcePack{
	zImagePack("z-image-int8", "Z-Image (INT8 ConvRot)", "z_image_int8_convrot.safetensors", 6201001296, "eb2b470694fbd9778f15efd309000ea0091312c018c10a352c120b1da9bf4279", 8),
	zImagePack("z-image-bf16", "Z-Image (BF16)", "z_image_bf16.safetensors", 12309866400, "996a67d3ff666946b1c25cbc16d1b1918b6cc0ac166309e23fe3b3d830263dee", 12),
	zImagePack("z-image-gguf-q8", "Z-Image (GGUF Q8_0)", "z-image-Q8_0.gguf", 7224707136, "a51d4e36f84caa972e0eff65e8bd2961add14374d8a7038a51eefea2fe45f2c9", 10),
}

func zImagePack(id, name, file string, size int64, sha string, vram int) ResourcePack {
	const source = "https://huggingface.co/Comfy-Org/Z-Image/resolve/94d47a468ec820d29a5dc09d4642539d76d49542/split_files/"
	model := hfPackFile("comfyui/diffusion_models", source+"diffusion_models/", file, size, sha)
	p := ResourcePack{ID: id, ModelID: id, Name: name, Kind: "zimage", License: "Apache-2.0", MinVRAMGiB: vram,
		Files: []PackFile{model}, Dependencies: []PackFile{
			hfPackFile("comfyui/text_encoders", source+"text_encoders/", "qwen_3_4b_fp8_mixed.safetensors", 5631994051, "72450b19758172c5a7273cf7de729d1c17e7f434a104a00167624cba94f68f15"),
			hfPackFile("comfyui/vae", source+"vae/", "ae.safetensors", 335304388, "afc8e28272cd15db3919bacdb6918ce9c1ed22e96cb12c4d5ed0fba823529e38"),
		}}
	if id == "z-image-bf16" {
		p.Dependencies[0] = hfPackFile("comfyui/text_encoders", source+"text_encoders/", "qwen_3_4b.safetensors", 8044982048, "6c671498573ac2f7a5501502ccce8d2b08ea6ca2f661c458e708f36b36edfc5a")
	}
	if id == "z-image-gguf-q8" {
		p.Dependencies = append(p.Dependencies, ggufPluginArchive)
		p.Files[0].URL = "https://huggingface.co/unsloth/Z-Image-GGUF/resolve/a84a5f4e7dcff6bc446c0a54a477a4a21aa800b0/" + file
	}
	return p
}
