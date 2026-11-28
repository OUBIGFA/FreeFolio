package app

import (
	"path"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

func (wa *whiteboardAIFeature) boardModelAvailability(m editor.BoardAIModel) editor.BoardAIModel {
	installed := wa.host.optionalResources().installed
	if m.Connection == "local" {
		runtime, ok := localresource.Runtime()
		m.NeedsDownload = !ok || !installed[runtime.ID] || !installed[m.Model]
	} else if m.Connection == "local-ncnn" {
		runtime, ok := localresource.UpscaleRuntime()
		m.NeedsDownload = !ok || !installed[runtime.ID]
	}
	pack, isPack := localresource.ComfyPackByModel(m.Model)
	if isPack && strings.HasPrefix(m.Connection, "comfyui") {
		m.Description = i18n.T(pack.Description)
		m.NeedsDownload = !installed[pack.ID] || pack.BaseModel != "" && !installed[pack.BaseModel]
	}
	if m.NeedsDownload {
		m.Unavailable = i18n.T("请先下载模型及依赖")
		return m
	}
	if !strings.HasPrefix(m.Connection, "comfyui") {
		return m
	}
	cfg := wa.host.optionalResources().comfyConfig()
	if cfg.Dir == "" && cfg.URL == "" {
		m.Unavailable = i18n.T("请先配置 ComfyUI")
		return m
	}
	caps := wa.host.optionalResources().comfyCaps
	if caps == nil {
		// Listing models must not start or reconnect an engine the user stopped.
		// The run path starts it on demand and validates its nodes and weights.
		m.Unavailable = ""
		return m
	}
	supported := true
	switch m.Connection {
	case "comfyui-zimage":
		supported = caps.ZImageNative
	case "comfyui-qwen-image":
		base := m.Model
		if pack.BaseModel != "" {
			base = pack.BaseModel
		}
		if base == "qwen-image-2.1-int8" {
			supported = caps.QwenImageBaseNative
		} else {
			supported = caps.QwenImageNative
		}
	case "comfyui-seedvr2":
		supported = caps.SeedVR2Native
	case "comfyui-rmbg3":
		supported = caps.RMBG3Native
	case "comfyui-sa2va":
		supported = caps.Sa2VANode != ""
	case "comfyui":
		if caps.NodeTypes != nil {
			if pack.Kind == "upscale" {
				supported = caps.NodeTypes["UpscaleModelLoader"] && caps.NodeTypes["ImageUpscaleWithModel"] && caps.NodeTypes["SaveImage"]
			} else if !isPack {
				supported = caps.NodeTypes["RMBG"] || caps.NodeTypes["ImageUpscaleWithModel"]
			}
		}
	case "comfyui-media":
		nodes := map[string][]string{
			"audio-music": {"UNETLoader", "DualCLIPLoader", "VAELoader", "TextEncodeAceStepAudio1.5", "EmptyAceStep1.5LatentAudio", "ModelSamplingAuraFlow", "ConditioningZeroOut", "KSampler", "VAEDecodeAudio", "SaveAudioAdvanced"},
			"audio-sfx":   {"CheckpointLoaderSimple", "CLIPLoader", "CLIPTextEncode", "EmptyLatentAudio", "KSampler", "VAEDecodeAudio", "SaveAudioAdvanced"},
			"audio-tts":   {"AILab_Qwen3TTSCustomVoice", "SaveAudioAdvanced"},
			"video-wan":   {"UNETLoader", "CLIPLoader", "VAELoader", "CLIPTextEncode", "ModelSamplingSD3", "Wan22ImageToVideoLatent", "KSampler", "VAEDecodeTiled", "CreateVideo", "SaveVideo"},
		}
		for _, node := range nodes[pack.Kind] {
			supported = supported && caps.NodeTypes[node]
		}
	}
	if pack.BaseModel != "" {
		supported = supported && caps.LoRANative
	}
	if !supported {
		m.Unavailable = i18n.T("引擎缺少所需节点")
		return m
	}
	if isPack && caps.ModelFiles != nil {
		plan, err := localresource.PackDownload(pack)
		if err != nil {
			m.Unavailable = err.Error()
			return m
		}
		for _, f := range plan.Files {
			parts := strings.Split(f.RelPath, "/")
			if len(parts) < 3 {
				continue
			}
			folder := parts[1]
			switch folder {
			case "diffusion_models", "text_encoders", "vae", "loras", "checkpoints", "upscale_models":
			default:
				continue
			}
			found := false
			for _, name := range caps.ModelFiles[folder] {
				if path.Base(strings.ReplaceAll(name, "\\", "/")) == path.Base(f.RelPath) {
					found = true
					break
				}
			}
			if !found {
				m.Unavailable = i18n.T("引擎尚未加载模型，重新连接后再用")
				return m
			}
		}
	}
	m.Unavailable = ""
	return m
}

func (wa *whiteboardAIFeature) boardRoleUnavailable(role whiteboard.AIRole) string {
	switch role {
	case whiteboard.AIText, whiteboard.AIImage, whiteboard.AIEnhance, whiteboard.AIModel, whiteboard.AILoRA, whiteboard.AIVideo, whiteboard.AIAudio, whiteboard.AICutout, whiteboard.AIUpscale:
	default:
		return ""
	}
	reason := i18n.T("没有可用模型")
	for _, m := range wa.boardAIModels(role) {
		if !m.NeedsDownload && m.Unavailable == "" {
			return ""
		}
		if m.Unavailable != "" {
			reason = m.Unavailable
		}
	}
	return reason
}
