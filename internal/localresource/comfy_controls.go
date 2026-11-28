package localresource

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ComfyInputSpecs is the last explicitly probed engine schema. Reading it never
// starts the engine. Offline controls use the audited bundled workflow defaults.
type ComfyInputSpecs map[string]map[string][]json.RawMessage

// NativeControl describes a scalar widget, never a graph edge or file path.
type NativeControl struct {
	Key, Label, Kind, NodeType string
	Default                    any
	Min, Max, Step             float64
	Choices                    []string
	Loader                     bool
}

// Enabled mirrors conditional inputs in the native implementation. Inactive
// values stay saved, but their controls explain that they do not affect this run.
func (c NativeControl) Enabled(values map[string]any) bool {
	if c.Key == "cfg_norm.pre_cfg" {
		strength, _ := values["cfg_norm.strength"].(float64)
		return strength > 0
	}
	if c.Key == "text.thinking_budget" {
		return values["text.reasoning"] != "" && values["text.reasoning"] != "off"
	}
	if strings.HasPrefix(c.Key, "speech.") && slices.Contains([]string{"speech.temperature", "speech.top_p", "speech.top_k"}, c.Key) {
		return values["speech.do_sample"] == true
	}
	if c.NodeType == "TextEncodeAceStepAudio1.5" && slices.Contains([]string{"text.cfg_scale", "text.temperature", "text.top_p", "text.top_k", "text.min_p"}, c.Key) {
		return values["text.generate_audio_codes"] != false
	}
	if strings.HasPrefix(c.Key, "6.sampling_mode.") {
		return values["6.sampling_mode"] != "off"
	}
	return true
}

type nativeNode struct{ id, kind string }

// NativeControls covers primitives on the nodes used by a selected workflow.
// The engine's schema extends the known controls without exposing arbitrary
// node classes, file names, output destinations or graph wiring.
func NativeControls(connection, model string, specs ComfyInputSpecs) []NativeControl {
	if p, ok := ComfyPackByModel(model); ok && p.BaseModel != "" {
		model = p.BaseModel
	}
	var out []NativeControl
	var nodes []nativeNode
	add := func(id, kind string) { nodes = append(nodes, nativeNode{id, kind}) }
	num := func(id, kind, key, label string, value, lo, hi, step float64) {
		out = append(out, NativeControl{Key: id + "." + key, Label: label, Kind: "FLOAT", NodeType: kind, Default: value, Min: lo, Max: hi, Step: step})
		if step >= 1 {
			out[len(out)-1].Kind = "INT"
		}
	}
	choice := func(id, kind, key, label, value string, values ...string) {
		out = append(out, NativeControl{Key: id + "." + key, Label: label, Kind: "COMBO", NodeType: kind, Default: value, Choices: values})
	}
	boolean := func(id, kind, key, label string, value bool) {
		out = append(out, NativeControl{Key: id + "." + key, Label: label, Kind: "BOOLEAN", NodeType: kind, Default: value})
	}
	loader := func(id string) {
		add(id, "UNETLoader")
		choice(id, "UNETLoader", "weight_dtype", "权重加载精度", "default", "default", "fp8_e4m3fn", "fp8_e4m3fn_fast", "fp8_e5m2")
	}
	clip := func(id, kind string) {
		add(id, kind)
		choice(id, kind, "device", "文本编码器运行位置", "default", "default", "cpu")
	}
	sampler := func(id, defaultSampler string) {
		add(id, "KSampler")
		choice(id, "KSampler", "sampler_name", "采样器", defaultSampler, "euler", "euler_ancestral", "heun", "dpm_2", "dpm_2_ancestral", "lms", "dpm_fast", "dpm_adaptive", "dpmpp_2s_ancestral", "dpmpp_sde", "dpmpp_2m", "dpmpp_2m_sde", "dpmpp_3m_sde", "ddpm", "lcm", "ddim", "uni_pc", "uni_pc_bh2", "res_multistep")
		choice(id, "KSampler", "scheduler", "调度器", "simple", "simple", "normal", "karras", "exponential", "sgm_uniform", "ddim_uniform", "beta", "linear_quadratic", "kl_optimal")
		num(id, "KSampler", "denoise", "降噪强度", 1, 0, 1, .01)
	}
	shift := func(id, kind string, value float64) {
		add(id, kind)
		num(id, kind, "shift", "采样偏移", value, 0, 100, .01)
	}
	tiled := func(id, kind, prefix string, tile, overlap, frames float64) {
		labels := []string{"解码分块尺寸", "解码分块重叠", "解码时序分块", "解码时序重叠"}
		if prefix == "编码" {
			labels = []string{"编码分块尺寸", "编码分块重叠", "编码时序分块", "编码时序重叠"}
		}
		add(id, kind)
		num(id, kind, "tile_size", labels[0], tile, 64, 4096, 64)
		num(id, kind, "overlap", labels[1], overlap, 0, 4096, 32)
		num(id, kind, "temporal_size", labels[2], frames, 8, 4096, 4)
		num(id, kind, "temporal_overlap", labels[3], 8, 4, 4096, 4)
	}
	switch connection {
	case "comfyui-zimage":
		if model == "z-image-gguf-q8" {
			add("1", "UnetLoaderGGUFAdvanced")
			for _, v := range []struct{ key, label string }{{"dequant_dtype", "GGUF 解量化精度"}, {"patch_dtype", "GGUF 补丁精度"}} {
				choice("1", "UnetLoaderGGUFAdvanced", v.key, v.label, "default", "default", "target", "float32", "float16", "bfloat16")
			}
			boolean("1", "UnetLoaderGGUFAdvanced", "patch_on_device", "在显卡应用补丁", false)
		} else {
			loader("1")
		}
		clip("3", "CLIPLoader")
		shift("2", "ModelSamplingAuraFlow", 3)
		sampler("8", "res_multistep")
		if model != "z-image-turbo-int8" {
			add("cfg_norm", "CFGNorm")
			num("cfg_norm", "CFGNorm", "strength", "CFG 归一化强度（0 为关闭）", 0, 0, 100, .01)
			boolean("cfg_norm", "CFGNorm", "pre_cfg", "在引导组合前归一化", false)
		}
	case "comfyui-qwen-image":
		loader("1")
		clip("3", "CLIPLoader")
		add("2", "QwenImage21Cache")
		add("5", "TextEncodeQwenImage21")
		choice("2", "QwenImage21Cache", "device", "参考图缓存位置", "auto", "auto", "gpu", "cpu", "off")
		choice("2", "QwenImage21Cache", "dtype", "参考图缓存精度", "default", "default", "int8", "int4")
		num("5", "TextEncodeQwenImage21", "resolution", "参考图编码尺寸", 1024, 0, 4096, 32)
		if model == "qwen-image-2.1-int8" {
			sampler("11", "euler")
		} else {
			add("8", "ManualSigmas")
			add("9", "KSamplerSelect")
			out = append(out, NativeControl{Key: "8.sigmas", Label: "采样噪声序列", Kind: "STRING", NodeType: "ManualSigmas", Default: "1.0, 0.978453, 0.95418, 0.926626, 0.89508, 0.845148, 0.704534, 0.414568, 0"})
			choice("9", "KSamplerSelect", "sampler_name", "采样器", "euler", "euler", "heun", "dpmpp_2m", "res_multistep")
		}
	case "comfyui-seedvr2":
		loader("6")
		tiled("7", "VAEEncodeTiled", "编码", 512, 128, 4096)
		tiled("10", "VAEDecodeTiled", "解码", 512, 128, 4096)
		sampler("9", "euler")
		num("9", "KSampler", "steps", "采样步数", 1, 1, 10000, 1)
		num("9", "KSampler", "cfg", "提示词引导强度", 1, 0, 100, .1)
		add("3", "ResizeImageMaskNode")
		choice("3", "ResizeImageMaskNode", "scale_method", "缩放插值", "lanczos", "nearest-exact", "bilinear", "area", "bicubic", "lanczos")
	case "comfyui-rmbg3":
		loader("2")
		clip("3", "CLIPLoader")
		clip("5", "CLIPLoader")
		sampler("8", "euler")
		add("6", "TextGenerateV2")
		add("7", "TextEncodeFibo2")
		num("6", "TextGenerateV2", "max_length", "描述最大长度", 512, 1, 32768, 1)
		choice("6", "TextGenerateV2", "sampling_mode", "描述采样模式", "on", "on", "off")
		for _, v := range []struct {
			key, label  string
			def, lo, hi float64
		}{{"temperature", "描述温度", .7, .01, 2}, {"top_p", "描述 Top-p", .95, 0, 1}, {"min_p", "描述 Min-p", .05, 0, 1}, {"repetition_penalty", "描述重复惩罚", 1.05, 0, 5}, {"presence_penalty", "描述存在惩罚", 0, 0, 5}} {
			num("6", "TextGenerateV2", "sampling_mode."+v.key, v.label, v.def, v.lo, v.hi, .01)
		}
		num("6", "TextGenerateV2", "sampling_mode.top_k", "描述 Top-k", 64, 0, 1000, 1)
		num("6", "TextGenerateV2", "sampling_mode.seed", "描述种子", 0, 0, 9007199254740991, 1)
		boolean("6", "TextGenerateV2", "thinking", "描述推理模式", false)
		boolean("6", "TextGenerateV2", "use_default_template", "使用描述模板", true)
		choice("6", "TextGenerateV2", "mtp", "描述预测策略", "auto", "auto", "off", "2", "3", "4", "5", "tree2s1", "tree2s2", "tree3s1", "tree4s1m8")
	case "comfyui-media":
		switch model {
		case "ace15-turbo":
			loader("model")
			clip("clip", "DualCLIPLoader")
			shift("sampling", "ModelSamplingAuraFlow", 3)
			sampler("sample", "euler")
			add("text", "TextEncodeAceStepAudio1.5")
			boolean("text", "TextEncodeAceStepAudio1.5", "generate_audio_codes", "生成音频编码", true)
			for _, v := range []struct {
				key, label    string
				def, hi, step float64
			}{{"cfg_scale", "音频编码引导", 2, 100, .1}, {"temperature", "音频编码温度", .85, 2, .01}, {"top_p", "音频编码 Top-p", .9, 1, .01}, {"top_k", "音频编码 Top-k", 0, 100, 1}, {"min_p", "音频编码 Min-p", 0, 1, .001}} {
				num("text", "TextEncodeAceStepAudio1.5", v.key, v.label, v.def, 0, v.hi, v.step)
			}
		case "stable-audio3":
			clip("clip", "CLIPLoader")
			sampler("sample", "lcm")
		case "wan22-5b":
			loader("model")
			clip("clip", "CLIPLoader")
			shift("sampling", "ModelSamplingSD3", 8)
			sampler("sample", "uni_pc")
			tiled("decode", "VAEDecodeTiled", "解码", 256, 64, 32)
		case "qwen3-tts":
			const kind = "AILab_Qwen3TTSCustomVoice_Advanced"
			add("speech", kind)
			choice("speech", kind, "device", "语音运行位置", "auto", "auto", "cpu", "cuda", "cuda:0")
			choice("speech", kind, "precision", "语音精度", "bf16", "bf16", "fp16", "fp32")
			choice("speech", kind, "attention", "语音注意力实现", "auto", "auto", "sage_attn", "flash_attn", "sdpa", "eager")
			boolean("speech", kind, "unload_models", "生成后卸载语音模型", true)
			boolean("speech", kind, "do_sample", "语音随机采样", false)
			for _, v := range []struct {
				key, label        string
				def, lo, hi, step float64
			}{{"max_new_tokens", "语音最大长度", 2048, 256, 4096, 256}, {"top_p", "语音 Top-p", .9, 0, 1, .05}, {"top_k", "语音 Top-k", 50, 0, 200, 1}, {"temperature", "语音温度", .9, .1, 2, .05}, {"repetition_penalty", "语音重复惩罚", 1, .5, 2, .05}} {
				num("speech", kind, v.key, v.label, v.def, v.lo, v.hi, v.step)
			}
		}
	case "comfyui-sa2va":
		kind := "XJSa2VAImageSegmentationV2"
		for _, k := range []string{"XJSa2VAImageSegmentationV2", "XJSa2VAImageSegmentation", "Sa2VANodeTpl"} {
			if specs[k] != nil {
				kind = k
				break
			}
		}
		add("2", kind)
		boolean("2", kind, "use_flash_attn", "使用 Flash Attention", false)
		if kind == "Sa2VANodeTpl" {
			boolean("2", kind, "use_8bit_quantization", "使用 8 位量化", strings.Contains(model, "4b"))
		} else {
			boolean("2", kind, "use_8bit", "使用 8 位量化", strings.Contains(model, "4b"))
			boolean("2", kind, "unload", "生成后卸载抠图模型", true)
		}
	case "comfyui-upscale":
		add("3", "ImageUpscaleWithModel")
	case "comfyui":
		add("2", "RMBG")
		num("2", "RMBG", "process_res", "抠图处理尺寸", 1024, 256, 2048, 32)
		boolean("2", "RMBG", "invert_output", "反转抠图结果", false)
		choice("2", "RMBG", "background", "抠图背景", "Alpha", "Alpha", "Black", "White", "Green", "Blue", "Red")
	case "local-ncnn":
		num("upscale", "RealESRGAN", "tile_size", "放大分块尺寸", 0, 0, 4096, 32)
		num("upscale", "RealESRGAN", "gpu", "放大显卡编号", -1, -1, 64, 1)
		boolean("upscale", "RealESRGAN", "tta", "放大自集成增强", false)
		out = append(out, NativeControl{Key: "upscale.threads", Label: "加载、处理、保存线程", Kind: "STRING", NodeType: "RealESRGAN", Default: "1:2:2"})

	}
	// Enrich primitive definitions with the running version's exact ranges and
	// enumeration values. Newly exposed scalar inputs receive their native name.
	for _, node := range nodes {
		fields := specs[node.kind]
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if protectedNativeInput(key, node.kind) {
				continue
			}
			spec := fields[key]
			if len(spec) == 0 {
				continue
			}
			c := NativeControl{Key: node.id + "." + key, Label: key, NodeType: node.kind, Step: 1, Min: -1e9, Max: 1e9}
			_ = json.Unmarshal(spec[0], &c.Kind)
			if json.Unmarshal(spec[0], &c.Choices) == nil {
				c.Kind = "COMBO"
			}
			if !slices.Contains([]string{"INT", "FLOAT", "BOOLEAN", "STRING", "COMBO"}, c.Kind) {
				continue
			}
			var meta struct {
				Default any      `json:"default"`
				Min     *float64 `json:"min"`
				Max     *float64 `json:"max"`
				Step    *float64 `json:"step"`
			}
			if len(spec) > 1 {
				_ = json.Unmarshal(spec[1], &meta)
			}
			c.Default = meta.Default
			if meta.Min != nil {
				c.Min = *meta.Min
			}
			if meta.Max != nil {
				c.Max = min(*meta.Max, 9007199254740991)
			}
			if meta.Step != nil {
				c.Step = *meta.Step
			}
			index := slices.IndexFunc(out, func(old NativeControl) bool { return old.Key == c.Key })
			if index >= 0 {
				c.Label = out[index].Label
				c.Default = out[index].Default
				out[index] = c
			} else {
				if c.Default == nil {
					switch c.Kind {
					case "COMBO":
						if len(c.Choices) > 0 {
							c.Default = c.Choices[0]
						}
					case "INT", "FLOAT":
						c.Default = 0.0
					case "BOOLEAN":
						c.Default = false
					case "STRING":
						c.Default = ""
					}
				}
				out = append(out, c)
			}
		}
	}
	for i := range out {
		if connection == "comfyui-rmbg3" && out[i].Key == "3.device" {
			out[i].Label = "抠图编码器运行位置"
		}
		if connection == "comfyui-rmbg3" && out[i].Key == "5.device" {
			out[i].Label = "描述编码器运行位置"
		}
		out[i].Loader = slices.Contains([]string{"UNETLoader", "UnetLoaderGGUFAdvanced", "CLIPLoader", "DualCLIPLoader"}, out[i].NodeType)
	}
	return out
}

func protectedNativeInput(key, kind string) bool {
	// Primary user fields already have controls and must have one source of truth.
	if slices.Contains([]string{"unet_name", "clip_name", "clip_name1", "clip_name2", "vae_name", "ckpt_name", "model_name", "model", "model_size", "type", "filename_prefix", "format", "format.codec", "image", "prompt", "text", "tags", "lyrics", "instruct", "speaker", "language", "bpm", "duration", "timesignature", "keyscale", "seconds", "width", "height", "length", "batch_size", "fps", "seed", "noise_seed", "negative_prompt", "threshold", "mask_threshold", "sensitivity", "mask_blur", "mask_offset", "color_correction_method", "resize_type", "resize_type.multiplier"}, key) {
		return true
	}
	if kind == "KSampler" && (key == "steps" || key == "cfg") {
		return true
	}
	return false
}

// Validate before mutation so malformed saved settings cannot partially change
// a workflow or silently fall back. No arbitrary inputs or links are accepted.
func applyNativeControls(graph map[string]any, connection, model string, values map[string]any, specs ComfyInputSpecs) error {
	controls := NativeControls(connection, model, specs)
	for key, value := range values {
		i := slices.IndexFunc(controls, func(c NativeControl) bool { return c.Key == key })
		if i < 0 {
			return fmt.Errorf("unsupported model parameter: %s", key)
		}
		c := controls[i]
		if err := c.Validate(value); err != nil {
			return err
		}
		id, field, _ := strings.Cut(key, ".")
		node, ok := graph[id].(map[string]any)
		if !ok || node["class_type"] != c.NodeType {
			return fmt.Errorf("parameter unavailable in this workflow: %s", key)
		}
		inputs := node["inputs"].(map[string]any)
		if _, linked := inputs[field].([]any); linked {
			return fmt.Errorf("parameter is supplied by a connection: %s", key)
		}
	}
	for key, value := range values {
		id, field, _ := strings.Cut(key, ".")
		graph[id].(map[string]any)["inputs"].(map[string]any)[field] = value
	}
	return nil
}

// Validate rejects non-finite numbers, invalid enums and malformed sigma schedules.
func (c NativeControl) Validate(value any) error {
	valid := false
	switch c.Kind {
	case "BOOLEAN":
		_, valid = value.(bool)
	case "COMBO":
		s, ok := value.(string)
		valid = ok && slices.Contains(c.Choices, s)
	case "STRING":
		s, ok := value.(string)
		valid = ok && len(s) <= 32768
		if valid && strings.HasSuffix(c.Key, ".sigmas") {
			parts := strings.Split(s, ",")
			valid = len(parts) >= 2 && len(parts) <= 1001
			previous := math.Inf(1)
			for _, p := range parts {
				v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
				if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > previous {
					valid = false
					break
				}
				previous = v
			}
			valid = valid && previous == 0
		}
	case "INT", "FLOAT":
		v, ok := value.(float64)
		valid = ok && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= c.Min && v <= c.Max && (c.Kind != "INT" || v == math.Trunc(v))
	}
	if !valid {
		return fmt.Errorf("invalid model parameter %s: %v", c.Key, value)
	}
	return nil
}

func (m *ComfyManager) nativeInputSpecs() ComfyInputSpecs {
	caps := m.Caps()
	if caps != nil {
		return caps.InputSpecs
	}
	return nil
}
