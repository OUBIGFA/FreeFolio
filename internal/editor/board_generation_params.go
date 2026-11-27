package editor

import (
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func aiParamNumber(c *ui.Context, key, label string, value, lo, hi, step float64, disabled bool, change func(float64)) {
	v := value
	_, height := c.Size()
	if height < 600 {
		ui.Row(c.Key("number-" + key)).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T(label)).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0)
			ui.NumberInput(c.Key("parameter-"+key), &v, lo, hi, step).WidthPercent(64).Height(28).Disabled(disabled).Label(i18n.T(label))
		})
	} else {
		ui.Text(c, i18n.T(label)).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.NumberInput(c.Key("parameter-"+key), &v, lo, hi, step).WidthPercent(100).Disabled(disabled).Label(i18n.T(label))
	}
	if v != value && !disabled {
		change(v)
	}
}

func aiParamChoice(c *ui.Context, o BoardOptions, key, label, current string, values, labels []string, disabled bool) string {
	return aiChoiceControl(c, o, key, label, current, values, labels, disabled)
}

func defaultNegativePrompt(n *whiteboard.AINode) string {
	if n.Params.NegativePrompt != nil {
		return *n.Params.NegativePrompt
	}
	if n.Connection == "comfyui-media" && n.Model == "wan22-5b" {
		return "blurry, distorted, static, subtitles, watermark"
	}
	return ""
}

// The selected base decides sampling controls even when a model cable supplies
// it. Distilled Qwen Turbo retains its exact sigma schedule.
func (b *Board) aiComfySamplingParams(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, disabled bool) {
	n = b.Scene.AIInputNode(n)
	if !strings.HasPrefix(n.Connection, "comfyui") || n.Role == whiteboard.AIModel || n.Role == whiteboard.AILoRA {
		return
	}
	set := func(fn func(*whiteboard.AIParams)) {
		if !disabled {
			b.updateAINode(o, id, func(n *whiteboard.AINode) { fn(&n.Params) })
		}
	}
	p := n.Params
	number := func(key, label string, value, lo, hi, step float64, fn func(float64)) {
		aiParamNumber(c, key, label, value, lo, hi, step, disabled, fn)
	}
	choice := func(key, label, current string, values, labels []string) string {
		return aiParamChoice(c, o, key, label, current, values, labels, disabled)
	}
	if n.Connection == "comfyui-sa2va" {
		value := .5
		if p.MaskThreshold != nil {
			value = *p.MaskThreshold
		}
		number("mask-threshold", "抠图阈值", value, 0, 1, .01, func(v float64) { set(func(p *whiteboard.AIParams) { p.MaskThreshold = &v }) })
		return
	}
	if n.Connection == "comfyui" && n.Role == whiteboard.AICutout {
		value := 1.0
		if p.Sensitivity != nil {
			value = *p.Sensitivity
		}
		number("sensitivity", "抠图灵敏度", value, 0, 1, .01, func(v float64) { set(func(p *whiteboard.AIParams) { p.Sensitivity = &v }) })
		number("mask-blur", "边缘柔化", float64(p.MaskBlur), 0, 64, 1, func(v float64) { set(func(p *whiteboard.AIParams) { p.MaskBlur = int(v) }) })
		number("mask-offset", "边缘扩张", float64(p.MaskOffset), -64, 64, 1, func(v float64) { set(func(p *whiteboard.AIParams) { p.MaskOffset = int(v) }) })
		return
	}
	if n.Connection == "comfyui" {
		return
	}
	base := n.Model
	if n.LinkAt("model") != "" {
		if flow, err := b.Scene.AIModelFlow(n.LinkAt("model")); err == nil && flow != nil {
			base = flow.Model
		}
	}
	if o.AI != nil && o.AI.Models != nil {
		for _, m := range o.AI.Models(n.Role) {
			if m.Model == base && m.BaseModel != "" {
				base = m.BaseModel
				break
			}
		}
	}
	steps, guidance := 8, 1.0
	switch n.Connection {
	case "comfyui-seedvr2":
		steps = 0
	case "comfyui-rmbg3":
		steps = 4
	case "comfyui-qwen-image":
		if base == "qwen-image-2.1-int8" {
			steps = 25
		} else {
			steps = 0
		}
	case "comfyui-media":
		if n.Model == "qwen3-tts" {
			steps = 0
		}
		if n.Model == "wan22-5b" {
			steps = 20
			guidance = 5
		}
	}
	aiPropSection(c, o, "生成细节")
	if ui.Button(c.Key("reset-generation"), i18n.T("恢复生成细节默认值")).Disabled(disabled).Clicked() {
		set(func(p *whiteboard.AIParams) {
			p.Seed = nil
			p.Steps = 0
			p.Guidance = nil
			p.NegativePrompt = nil
			p.ColorCorrection = ""
		})
		fresh := *n
		fresh.Params.NegativePrompt = nil
		b.ai.buf.negative = defaultNegativePrompt(&fresh)
	}
	seed := -1.0
	if n.Connection == "comfyui-seedvr2" || n.Connection == "comfyui-rmbg3" {
		seed = 42
	}
	if p.Seed != nil {
		seed = float64(*p.Seed)
	}
	number("seed", "种子（-1 为默认）", seed, -1, 9007199254740991, 1, func(v float64) {
		set(func(p *whiteboard.AIParams) {
			if v < 0 {
				p.Seed = nil
			} else {
				k := int64(v)
				p.Seed = &k
			}
		})
	})
	if n.Connection == "comfyui-seedvr2" {
		method := p.ColorCorrection
		if method == "" {
			method = "none"
		}
		if v := choice("color-correction", "颜色校正", method, []string{"none", "lab", "wavelet", "adain"}, translated("关闭", "贴近原图", "保留细节", "匹配整体色调")); v != method {
			set(func(p *whiteboard.AIParams) { p.ColorCorrection = v })
		}
		return
	}
	if n.Connection == "comfyui-media" && n.Model == "qwen3-tts" {
		return
	}
	if steps > 0 {
		value := steps
		if p.Steps != 0 {
			value = p.Steps
		}
		number("steps", "采样步数", float64(value), 1, 100, 1, func(v float64) { set(func(p *whiteboard.AIParams) { p.Steps = int(v) }) })
	} else {
		ui.Text(c, i18n.T("Turbo 使用固定 8 步采样")).FontSize(12).TextColor(o.Theme.TextMuted)
		if p.Steps != 0 && p.Steps != 8 {
			ui.Text(c, i18n.T("已保存的步数不适用，请恢复生成细节默认值")).FontSize(12).TextColor(o.Theme.Danger)
		}
	}
	if p.Guidance != nil {
		guidance = *p.Guidance
	}
	number("guidance", "提示词引导强度", guidance, 0, 20, .1, func(v float64) { set(func(p *whiteboard.AIParams) { p.Guidance = &v }) })
	negative := n.Connection == "comfyui-zimage" || n.Connection == "comfyui-qwen-image" || n.Connection == "comfyui-media" && (n.Model == "stable-audio3" || n.Model == "wan22-5b")
	if negative {
		ui.Text(c, i18n.T("避免内容（引导强度不为 1 时生效）")).FontSize(12).TextColor(o.Theme.TextMuted)
		if ui.TextArea(c.Key("generation-negative"), &b.ai.buf.negative).WidthPercent(100).Height(aiSettingsTextHeight(c, 84)).Disabled(disabled || guidance == 1).Label(i18n.T("避免内容")).Changed() {
			v := b.ai.buf.negative
			set(func(p *whiteboard.AIParams) { p.NegativePrompt = &v })
		}
	}
}
