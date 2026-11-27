# Optional enhancement resources

These resources are downloaded on request and are not bundled with FreeFolio.
The exact source revisions, filenames, byte counts, digests, and model-card
links are recorded in [enhancements.json](../manifests/enhancements.json).

- Z-Image Detailer: styly-agents, Apache-2.0 as stated in the model-card text.
- V1 Realism: youknownothing, Apache-2.0.
- Qwen Image 2.1 Restore: ausboss, Qwen Research license.
- Qwen 2.1 Detail Enhancer: reverentelusarca; the public model card does not
  declare a standalone license, so use and integration require author permission.
- Elusarca Anime: reverentelusarca, Apache-2.0.
- Pencil Sketch: Ttio2, Apache-2.0.
- Linex Storybook: ABDALLALSWAITI, CreativeML OpenRAIL-M.
- 3D Render Style, Mockups, ColoringBook, LineartAnime, Doodle, Icons, PixelArt
  and Isometric: artificialguybr / Redmond, Apache-2.0.
- SeedVR2: ByteDance-Seed, repackaged by Comfy-Org, Apache-2.0.
- Real-ESRGAN Anime 6B: Xintao Wang and contributors, BSD-3-Clause.

The native workflow integration was checked against ComfyUI v0.46.0,
revision `a277b5bf213e8a44fa34817b7770b54f86ef4cea`. FreeFolio builds API graphs
in Go and does not copy or redistribute ComfyUI Python code. The graphs use
core nodes, including `LoraLoaderModelOnly`, `TextEncodeQwenImage21`,
`SeedVR2Conditioning`, and `ImageUpscaleWithModel`.

Author examples inform starting settings only. They do not establish measured
quality, memory use, or speed for the user's hardware. Qwen base-model permission
does not substitute for a separate adapter's terms. See each pinned model card
for the applicable terms and attribution.
