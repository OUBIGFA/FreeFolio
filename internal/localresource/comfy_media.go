package localresource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
)

type ComfyMediaRequest struct {
	Model, Prompt, Lyrics, Speaker, Language  string
	Seconds                                   int
	BPM, FPS                                  int
	KeyScale, TimeSignature, VoiceStyle, Size string
	Options                                   ComfyOptions
	Image                                     []byte
}

func mediaNode(kind string, inputs map[string]any) any {
	return map[string]any{"class_type": kind, "inputs": inputs}
}
func mediaRef(id string, index int) []any { return []any{id, index} }

// GenerateLocalMedia builds bounded, single-result native workflows. TTS is the
// one opt-in custom-node integration; no cloud or billed partner nodes are used.
func (m *ComfyManager) GenerateLocalMedia(ctx context.Context, cfg ComfyConfig, req ComfyMediaRequest) (ComfyOutput, error) {
	pack, ok := ComfyPack(req.Model)
	if !ok || (!strings.HasPrefix(pack.Kind, "audio-") && !strings.HasPrefix(pack.Kind, "video-")) {
		return ComfyOutput{}, errors.New("unknown media model")
	}
	base, err := m.EnsureRunning(ctx, cfg)
	if err != nil {
		return ComfyOutput{}, err
	}
	if cfg.ResourceDir != "" && !PackPresent(cfg.ResourceDir, pack) {
		return ComfyOutput{}, errors.New("download the media model in Optional resources first")
	}
	caps, _, err := m.ensureCaps(ctx, cfg)
	if err != nil {
		return ComfyOutput{}, err
	}
	graph, err := m.mediaGraph(ctx, base, pack, req)
	if err != nil {
		return ComfyOutput{}, err
	}

	for _, node := range graph {
		kind := node.(map[string]any)["class_type"].(string)
		if !caps.NodeTypes[kind] {
			return ComfyOutput{}, fmt.Errorf("ComfyUI node %s is unavailable; update or install the required voice plugin", kind)
		}
	}
	files, err := m.runFilesWorkflow(ctx, base, graph)
	if err != nil {
		return ComfyOutput{}, err
	}
	if len(files) != 1 {
		return ComfyOutput{}, fmt.Errorf("expected one generated media file, received %d", len(files))
	}
	out := files[0]
	if strings.HasPrefix(pack.Kind, "audio-") {
		if strings.ToLower(path.Ext(out.Filename)) != ".flac" || !bytes.HasPrefix(out.Data, []byte("fLaC")) {
			return ComfyOutput{}, errors.New("ComfyUI returned an invalid FLAC file")
		}
	} else if strings.ToLower(path.Ext(out.Filename)) != ".mp4" || len(out.Data) < 12 || string(out.Data[4:8]) != "ftyp" {
		return ComfyOutput{}, errors.New("ComfyUI returned an invalid MP4 file")
	}
	return out, nil
}

func (m *ComfyManager) mediaGraph(ctx context.Context, base string, pack ResourcePack, req ComfyMediaRequest) (map[string]any, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	if len(req.Image) > 0 && pack.Kind != "video-wan" {
		return nil, errors.New("this media model does not accept an image")
	}
	g := map[string]any{}
	file := func(i int) string { return path.Base(pack.Files[i].RelPath) }
	seed, _, _, err := req.Options.sampling(randomComfySeed(), 8, 1)
	if err != nil {
		return nil, err
	}
	sample := func(model, positive, negative, latent []any, steps int, cfg float64, sampler string) any {
		if req.Options.Steps != 0 {
			steps = req.Options.Steps
		}
		if req.Options.Guidance != nil {
			cfg = *req.Options.Guidance
		}
		return mediaNode("KSampler", map[string]any{"model": model, "positive": positive, "negative": negative, "latent_image": latent, "seed": seed, "steps": steps, "cfg": cfg, "sampler_name": sampler, "scheduler": "simple", "denoise": 1.0})
	}
	saveAudio := func(ref []any) {
		g["save"] = mediaNode("SaveAudioAdvanced", map[string]any{"audio": ref, "filename_prefix": "freefolio/audio", "format": "flac"})
	}
	switch pack.Kind {
	case "audio-music":
		seconds := req.Seconds
		if seconds == 0 {
			seconds = 30
		}
		if seconds < 5 || seconds > 120 {
			return nil, errors.New("music duration must be 5 to 120 seconds")
		}
		g["model"] = mediaNode("UNETLoader", map[string]any{"unet_name": file(0), "weight_dtype": "default"})
		g["sampling"] = mediaNode("ModelSamplingAuraFlow", map[string]any{"model": mediaRef("model", 0), "shift": 3.0})
		g["clip"] = mediaNode("DualCLIPLoader", map[string]any{"clip_name1": file(1), "clip_name2": file(2), "type": "ace", "device": "default"})
		g["vae"] = mediaNode("VAELoader", map[string]any{"vae_name": file(3)})
		language := req.Language
		if language == "" {
			language = "zh"
		}
		lyrics := req.Lyrics
		if strings.TrimSpace(lyrics) == "" {
			lyrics = "[Instrumental]"
			language = "unknown"
		}
		bpm, key, meter, err := musicMetadata(req.BPM, req.KeyScale, req.TimeSignature)
		if err != nil {
			return nil, err
		}
		g["text"] = mediaNode("TextEncodeAceStepAudio1.5", map[string]any{"clip": mediaRef("clip", 0), "tags": req.Prompt, "lyrics": lyrics, "seed": seed, "bpm": bpm, "duration": seconds, "timesignature": meter, "language": language, "keyscale": key, "generate_audio_codes": true, "cfg_scale": 2.0, "temperature": .85, "top_p": .9, "top_k": 0, "min_p": 0.0})
		g["negative"] = mediaNode("ConditioningZeroOut", map[string]any{"conditioning": mediaRef("text", 0)})
		g["latent"] = mediaNode("EmptyAceStep1.5LatentAudio", map[string]any{"seconds": seconds, "batch_size": 1})
		g["sample"] = sample(mediaRef("sampling", 0), mediaRef("text", 0), mediaRef("negative", 0), mediaRef("latent", 0), 8, 1, "euler")
		g["decode"] = mediaNode("VAEDecodeAudio", map[string]any{"samples": mediaRef("sample", 0), "vae": mediaRef("vae", 0)})
		saveAudio(mediaRef("decode", 0))
	case "audio-sfx":
		seconds := req.Seconds
		if seconds == 0 {
			seconds = 10
		}
		if seconds < 1 || seconds > 60 {
			return nil, errors.New("sound duration must be 1 to 60 seconds")
		}
		g["model"] = mediaNode("CheckpointLoaderSimple", map[string]any{"ckpt_name": file(0)})
		g["clip"] = mediaNode("CLIPLoader", map[string]any{"clip_name": file(1), "type": "stable_audio", "device": "default"})
		g["text"] = mediaNode("CLIPTextEncode", map[string]any{"clip": mediaRef("clip", 0), "text": req.Prompt})
		g["negative"] = mediaNode("CLIPTextEncode", map[string]any{"clip": mediaRef("clip", 0), "text": req.Options.negative("")})
		g["latent"] = mediaNode("EmptyLatentAudio", map[string]any{"seconds": seconds, "batch_size": 1})
		g["sample"] = sample(mediaRef("model", 0), mediaRef("text", 0), mediaRef("negative", 0), mediaRef("latent", 0), 8, 1, "lcm")
		g["decode"] = mediaNode("VAEDecodeAudio", map[string]any{"samples": mediaRef("sample", 0), "vae": mediaRef("model", 2)})
		saveAudio(mediaRef("decode", 0))
	case "audio-tts":
		if len([]rune(req.Prompt)) > 1000 {
			return nil, errors.New("speech text is limited to 1000 characters per run")
		}
		speaker := req.Speaker
		if speaker == "" {
			speaker = "Vivian"
		}
		language := req.Language
		if language == "" || language == "zh" {
			language = "Chinese"
		}
		if language == "en" {
			language = "English"
		}
		kind := "AILab_Qwen3TTSCustomVoice"
		if len(req.Options.Native) > 0 {
			kind = "AILab_Qwen3TTSCustomVoice_Advanced"
		}
		g["speech"] = mediaNode(kind, map[string]any{"text": req.Prompt, "speaker": speaker, "model_size": "1.7B", "language": language, "instruct": req.VoiceStyle, "unload_models": true, "seed": seed})
		saveAudio(mediaRef("speech", 0))
	case "video-wan":
		seconds := req.Seconds
		if seconds == 0 {
			seconds = 4
		}
		if seconds < 1 || seconds > 5 {
			return nil, errors.New("local video duration must be 1 to 5 seconds")
		}
		g["model"] = mediaNode("UNETLoader", map[string]any{"unet_name": file(0), "weight_dtype": "default"})
		g["sampling"] = mediaNode("ModelSamplingSD3", map[string]any{"model": mediaRef("model", 0), "shift": 8.0})
		g["clip"] = mediaNode("CLIPLoader", map[string]any{"clip_name": file(1), "type": "wan", "device": "default"})
		g["vae"] = mediaNode("VAELoader", map[string]any{"vae_name": file(2)})
		g["text"] = mediaNode("CLIPTextEncode", map[string]any{"clip": mediaRef("clip", 0), "text": req.Prompt})
		g["negative"] = mediaNode("CLIPTextEncode", map[string]any{"clip": mediaRef("clip", 0), "text": req.Options.negative("blurry, distorted, static, subtitles, watermark")})
		width, height, fps, err := localVideoGeometry(req.Size, req.FPS)
		if err != nil {
			return nil, err
		}
		frames := ((seconds*fps+3)/4)*4 + 1
		latent := map[string]any{"vae": mediaRef("vae", 0), "width": width, "height": height, "length": frames, "batch_size": 1}
		if len(req.Image) > 0 {
			name, _, _, err := m.encodeAndUploadPNG(ctx, base, req.Image)
			if err != nil {
				return nil, err
			}
			g["image"] = mediaNode("LoadImage", map[string]any{"image": name})
			latent["start_image"] = mediaRef("image", 0)
		}
		g["latent"] = mediaNode("Wan22ImageToVideoLatent", latent)
		g["sample"] = sample(mediaRef("sampling", 0), mediaRef("text", 0), mediaRef("negative", 0), mediaRef("latent", 0), 20, 5, "uni_pc")
		g["decode"] = mediaNode("VAEDecodeTiled", map[string]any{"samples": mediaRef("sample", 0), "vae": mediaRef("vae", 0), "tile_size": 256, "overlap": 64, "temporal_size": 32, "temporal_overlap": 8})
		g["video"] = mediaNode("CreateVideo", map[string]any{"images": mediaRef("decode", 0), "fps": float64(fps)})
		g["save"] = mediaNode("SaveVideo", map[string]any{"video": mediaRef("video", 0), "filename_prefix": "freefolio/video", "format": "mp4", "format.codec": "h264"})
	default:
		return nil, errors.New("media workflow is not configured")
	}
	if req.Model == "qwen3-tts" && len(req.Options.Native) > 0 {
		inputs := g["speech"].(map[string]any)["inputs"].(map[string]any)
		inputs["device"], inputs["precision"], inputs["do_sample"] = "auto", "bf16", false
	}
	if err := applyNativeControls(g, "comfyui-media", req.Model, req.Options.Native, m.nativeInputSpecs()); err != nil {
		return nil, err
	}
	return g, nil
}
