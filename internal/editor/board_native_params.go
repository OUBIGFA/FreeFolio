package editor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func (b *Board) nativeControls(o BoardOptions, n *whiteboard.AINode) []localresource.NativeControl {
	if n.Role == whiteboard.AIText && o.AI != nil && o.AI.Models != nil {
		var out []localresource.NativeControl
		for _, m := range o.AI.Models(n.Role) {
			if m.Model == n.Model && m.Connection == n.Connection {
				for _, p := range m.TextControls {
					kind := "INT"
					step := 1.0
					if p.Key == "text.temperature" {
						kind = "FLOAT"
						step = .05
					}
					if len(p.Choices) > 0 {
						kind = "COMBO"
					}
					out = append(out, localresource.NativeControl{Key: p.Key, Label: p.Label, Kind: kind, Default: p.Default, Min: 0, Max: p.Max, Step: step, Choices: p.Choices})
				}
				break
			}
		}
		return out
	}
	effective := b.Scene.AIInputNode(n)
	base := effective.Model
	if flow, err := b.Scene.AIModelFlow(n.LinkAt("model")); err == nil && flow != nil {
		base = flow.Model
	}
	var specs localresource.ComfyInputSpecs
	if o.AI != nil && o.AI.NativeSpecs != nil {
		specs = o.AI.NativeSpecs()
	}
	connection := effective.Connection
	if connection == "comfyui" && n.Role == whiteboard.AIUpscale {
		connection = "comfyui-upscale"
	}
	controls := localresource.NativeControls(connection, base, specs)
	if n.Role == whiteboard.AILoRA {
		return nil
	}
	if n.Role == whiteboard.AIModel {
		return slices.DeleteFunc(controls, func(c localresource.NativeControl) bool { return !c.Loader })
	}
	return controls
}

// Advanced controls use bounded pages of flat columns. No setting is clipped
// or hidden in a scroll area on a small window; keyboard focus stays available.
func (b *Board) aiNativeParameterFields(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, disabled bool) {
	controls := b.nativeControls(o, n)
	inherited := map[string]any{}
	if n.LinkAt("model") != "" {
		if flow, err := b.Scene.AIModelFlow(n.LinkAt("model")); err == nil && flow != nil {
			inherited = flow.Native
		}
	}
	effectiveValues := map[string]any{}
	for _, field := range controls {
		effectiveValues[field.Key] = field.Default
	}
	for key, value := range inherited {
		effectiveValues[key] = value
	}
	for key, value := range n.Params.Native {
		effectiveValues[key] = value
	}
	winW, winH := c.Size()
	columns := max(1, min(3, int((winW-48)/260)))
	rows := max(1, int((winH-210)/70))
	perPage := columns * rows
	pages := max(1, (len(controls)+perPage-1)/perPage)
	s := b.aiState()
	s.parameterPage = max(0, min(s.parameterPage, pages-1))
	set := func(key string, value any) {
		if disabled {
			return
		}
		b.updateAINode(o, id, func(node *whiteboard.AINode) {
			if node.Params.Native == nil {
				node.Params.Native = map[string]any{}
			}
			node.Params.Native[key] = value
		})
	}
	ui.Row(c.Key("native-navigation")).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.Button(c.Key("native-reset"), i18n.T("恢复模型参数默认值")).Disabled(disabled).Clicked() {
			b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Params.Native = nil })
			s.nativeText = nil
		}
		if pages > 1 {
			if ui.Button(c.Key("native-previous"), i18n.T("上一页")).Disabled(s.parameterPage == 0).Clicked() {
				s.parameterPage--
			}
			ui.Text(c, fmt.Sprintf("%d / %d", s.parameterPage+1, pages))
			if ui.Button(c.Key("native-next"), i18n.T("下一页")).Disabled(s.parameterPage == pages-1).Clicked() {
				s.parameterPage++
			}
		}
	})
	begin := s.parameterPage * perPage
	end := min(begin+perPage, len(controls))
	ui.Row(c.Key("native-columns")).Gap(16).AlignItems(ui.Start).Children(func() {
		for col := 0; col < columns; col++ {
			columnRows := (end - begin + columns - 1) / columns
			first := begin + col*columnRows
			last := min(first+columnRows, end)
			if first >= last {
				break
			}
			ui.Column(c.Key(fmt.Sprint("native-column-", col))).Grow(1).MinWidth(0).Gap(8).Children(func() {
				for _, field := range controls[first:last] {
					fieldDisabled := disabled || !field.Enabled(effectiveValues)
					value, exists := n.Params.Native[field.Key]
					if !exists {
						value, exists = inherited[field.Key]
					}
					if !exists {
						value = field.Default
					}
					key := id + ":" + field.Key
					switch field.Kind {
					case "FLOAT", "INT":
						v, _ := value.(float64)
						aiParamNumber(c, key, field.Label, v, field.Min, field.Max, field.Step, fieldDisabled, func(v float64) { set(field.Key, v) })
					case "COMBO":
						v, _ := value.(string)
						if next := aiParamChoice(c, o, key, field.Label, v, field.Choices, field.Choices, fieldDisabled); next != v {
							set(field.Key, next)
						}
					case "BOOLEAN":
						v, _ := value.(bool)
						current := "false"
						if v {
							current = "true"
						}
						if next := aiParamChoice(c, o, key, field.Label, current, []string{"false", "true"}, translated("关闭", "开启"), fieldDisabled); next != current {
							set(field.Key, next == "true")
						}
					case "STRING":
						v, _ := value.(string)
						if s.nativeText == nil {
							s.nativeText = map[string]*string{}
						}
						buf := s.nativeText[key]
						if buf == nil {
							buf = new(string)
							*buf = v
							s.nativeText[key] = buf
						} else if *buf != v {
							// Undo and upstream edits replace saved state while the
							// settings stay open; reflect that state in the text widget.
							*buf = v
						}
						ui.Text(c, i18n.T(field.Label)).FontSize(12).TextColor(o.Theme.TextMuted)
						if ui.TextInput(c.Key("native-text-"+key), buf).WidthPercent(100).Height(28).Disabled(fieldDisabled).Label(i18n.T(field.Label)).Changed() {
							set(field.Key, *buf)
						}
					}
					if err := field.Validate(value); err != nil {
						ui.Text(c, i18n.T("参数值超出模型支持范围")).FontSize(12).TextColor(o.Theme.Danger)
					}
				}
			})
		}
	})
}

func (b *Board) hasNativeControls(o BoardOptions, n *whiteboard.AINode) bool {
	return (n.Role == whiteboard.AIText || strings.HasPrefix(b.Scene.AIInputNode(n).Connection, "comfyui") || n.Connection == "local-ncnn") && len(b.nativeControls(o, n)) > 0
}
