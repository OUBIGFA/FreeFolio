package app

import (
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

// boardEnhancementPresets is catalog-backed, so every optional adapter has a
// matching node even before download. Resource inspection runs asynchronously.
func (a *App) boardEnhancementPresets() []editor.BoardAIPreset {
	a.ensureResourcesInspected()
	var out []editor.BoardAIPreset
	for _, pack := range localresource.ComfyPacks {
		m := editor.BoardAIModel{Model: pack.ModelID, Name: pack.Name, ConnectionName: "ComfyUI", Local: true, Category: pack.Category, Instruction: pack.Instruction, BaseModel: pack.BaseModel, NeedsDownload: !a.resources.installed[pack.ID]}
		role := whiteboard.AIImage
		switch pack.Kind {
		case "lora-zimage":
			m.Connection = "comfyui-zimage"
		case "lora-qwen":
			m.Connection = "comfyui-qwen-image"
			role = whiteboard.AIEnhance
		case "seedvr2":
			m.Connection = "comfyui-seedvr2"
			role = whiteboard.AIUpscale
		case "upscale":
			m.Connection = "comfyui"
			role = whiteboard.AIUpscale
		default:
			continue
		}
		if pack.BaseModel != "" {
			strength := pack.DefaultStrength
			m.LoRAStrength = &strength
			m.NeedsDownload = m.NeedsDownload || !a.resources.installed[pack.BaseModel]
		}
		out = append(out, editor.BoardAIPreset{Role: role, Model: m})
	}
	return out
}
