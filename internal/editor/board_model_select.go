package editor

import (
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func (m BoardAIModel) sourceColor() ui.Color {
	if strings.HasPrefix(m.Connection, "comfyui") {
		return ui.Hex("#9159C7")
	}
	if m.Local || m.Connection == "local" || m.Connection == "local-ncnn" {
		return ui.Hex("#27965B")
	}
	return ui.Hex("#397BC8")
}

// aiModelSelect uses the same colored source mark in the trigger and every
// option, while the full text label remains available to keyboard and AT users.
func (b *Board) aiModelSelect(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, key string, disabled bool, height float32) {
	if n.ImageHistory() && n.LinkAt("model") != "" {
		ui.Text(c, i18n.T("模型来自左侧连线")).FontSize(12).TextColor(o.Theme.TextMuted).Grow(1)
		return
	}
	var models []BoardAIModel
	if o.AI != nil && o.AI.Models != nil {
		models = o.AI.Models(n.Role)
	}
	// Old saved combined presets still display correctly; new image selectors
	// list base models while dedicated LoRA nodes list adapters.
	shown := BoardAIModel{Connection: n.Connection, Model: n.Model}
	filtered := make([]BoardAIModel, 0, len(models))
	for _, m := range models {
		if m.Connection == n.Connection && m.Model == n.Model {
			shown = m
		}
		if n.ImageHistory() && m.BaseModel != "" && m.Model != n.Model {
			continue
		}
		if n.Role == whiteboard.AILoRA && n.LinkAt("model") != "" {
			if flow, err := b.Scene.AIModelFlow(n.LinkAt("model")); err == nil && flow != nil && m.BaseModel != flow.Model && m.Model != n.Model {
				continue
			}
		}
		filtered = append(filtered, m)
	}
	models = filtered
	current := n.Connection + "\x00" + n.Model
	before := current
	sel := ui.SelectBase(c.Key(key), &current)
	st := boardChromeFor(o.Theme)
	label := shown.label()
	if n.Model == "" {
		label = i18n.T("选择模型")
	}
	content := func(m BoardAIModel, text string) {
		ui.Box(c).Size(7, 7).Radius(4).Background(m.sourceColor()).Shrink(0)
		ui.Text(c, text).FontSize(11.5).Grow(1).MinWidth(0).TextAlign(ui.Start).SingleLine()
	}
	sel.Trigger.Height(height).Grow(1).MinWidth(0).Padding(4, 8).Gap(6).Justify(ui.Start).AlignItems(ui.Center).Background(st.surfaceMuted).Border(1, st.divider).Radius(5).Disabled(disabled).Label(i18n.T("模型")).Value(label).Description(label).Tooltip(label).Children(func() {
		content(shown, label)
		ui.Text(c, "⌄").FontSize(12).Shrink(0)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Gap(2).Background(st.surface).Border(1, st.divider).Radius(6).MinWidth(300).MaxHeight(360)
		ui.Scroll(c.Key("model-options")).MaxHeight(320).WidthPercent(100).Children(func() {
			for _, m := range models {
				item := sel.Item(m.Connection+"\x00"+m.Model).Padding(7, 8).Gap(7).Justify(ui.Start).AlignItems(ui.Center).Label(m.label()).WidthPercent(100)
				if item.Highlighted() {
					item.Background(st.hover)
				}
				item.Children(func() { content(m, m.label()) })
			}
		})
	})
	if current != before {
		for _, m := range models {
			if current == m.Connection+"\x00"+m.Model {
				b.chooseAIModel(o, id, n, m)
				break
			}
		}
	}
}
