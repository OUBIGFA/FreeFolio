package app

import (
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

// boardEnhancementPresets is catalog-backed, so every optional adapter has a
// matching node even before download. Resource inspection runs asynchronously.
func (wa *whiteboardAIFeature) boardEnhancementPresets() []editor.BoardAIPreset {
	wa.host.optionalResources().ensureResourcesInspected()
	var out []editor.BoardAIPreset
	for _, pack := range localresource.ComfyPacks {
		m := editor.BoardAIModel{Model: pack.ModelID, Name: pack.Name, ConnectionName: "ComfyUI", Local: true, Category: pack.Category, Instruction: pack.Instruction, BaseModel: pack.BaseModel, NeedsDownload: !wa.host.optionalResources().installed[pack.ID]}
		role := whiteboard.AIImage
		switch pack.Kind {
		case "audio-music", "audio-sfx", "audio-tts":
			m.Connection = "comfyui-media"
			role = whiteboard.AIAudio
			switch pack.Kind {
			case "audio-music":
				m.Category = "音乐"
			case "audio-sfx":
				m.Category = "音效"
			case "audio-tts":
				m.Category = "语音"
			}
		case "video-wan":
			m.Connection = "comfyui-media"
			role = whiteboard.AIVideo
			m.Category = "本地视频"
		case "lora-zimage":
			m.Connection = "comfyui-zimage"
		case "lora-qwen":
			m.Connection = "comfyui-qwen-image"
			if pack.ID == "qwen-restore" {
				role = whiteboard.AIEnhance
			}
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
			m.NeedsDownload = m.NeedsDownload || !wa.host.optionalResources().installed[pack.BaseModel]
		}
		out = append(out, editor.BoardAIPreset{Role: role, Model: wa.boardModelAvailability(m)})
	}
	return out
}
