package app

import (
	"fmt"
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/agent"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// settingsAI owns native provider configuration and persistent model choices.
func (a *App) settingsAI(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, "OneBot", i18n.T("对话、写作与文档编辑。配置供应商并勾选需要使用的模型。"))
	ui.Row(c.Key("provider-actions")).Gap(8).Children(func() {
		ui.MenuButton(c.Key("connections"), i18n.T("选择供应商"), func(m *ui.Menu) {
			for _, conn := range a.prefs.AI.Connections {
				if m.Item(conn.Name).Checked(a.prefs.AI.Active == conn.ID).Chosen() {
					a.selectAIConnection(conn.ID)
				}
			}
		})
		if ui.Button(c.Key("add"), i18n.T("添加供应商")).Clicked() {
			a.addAIConnection("openai")
		}
		ui.MenuButton(c.Key("presets"), i18n.T("常用服务"), func(m *ui.Menu) {
			for _, preset := range agent.Presets {
				if m.Item(preset.Name).Chosen() {
					a.addAIConnection(preset.ID)
				}
			}
		})
	})
	conn := a.prefs.AI.Current()
	if conn == nil {
		ui.Text(c, i18n.T("请先添加供应商")).TextColor(t.TextMuted).Padding(16, 0)
		return
	}
	a.dropStaleModels()
	row := func(key, label string, fn func()) {
		if key == "name" || key == "protocol" || key == "address" || key == "key" {
			ui.Row(c.Key(key)).Gap(12).Padding(5, 0).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T(label)).FontSize(13).Width(88).Shrink(0)
				ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(fn)
			})
			return
		}
		ui.Column(c.Key(key)).Gap(6).Padding(8, 0).Children(func() {
			ui.Text(c, i18n.T(label)).FontSize(13).FontWeight(500)
			ui.Row(c).Gap(8).MinWidth(0).AlignItems(ui.Center).Children(fn)
		})
	}
	row("name", "供应商名称", func() {
		if ui.TextInput(c.Key("field"), &conn.Name).Grow(1).Label(i18n.T("供应商名称")).Changed() {
			a.savePrefs()
		}
		if ui.Button(c.Key("remove"), i18n.T("移除供应商")).Clicked() {
			a.removeAIConnection()
		}
	})
	// Slice mutation may invalidate this connection's address.
	if a.prefs.AI.Current() != conn {
		return
	}
	row("protocol", "接口协议", func() {
		// A select lines up with the text fields beside it; a menu button
		// centred its label and looked like an action rather than a value.
		protocols := []string{agent.Chat, agent.Responses, agent.Anthropic, agent.Gemini, agent.Ollama}
		labels := make([]string, len(protocols))
		for i, p := range protocols {
			labels[i] = oneBotProtocolLabel(p)
		}
		current := oneBotProtocolLabel(conn.Protocol)
		ui.Select(c.Key("field"), &current, labels).Grow(1).Label(i18n.T("接口协议"))
		for i, label := range labels {
			if label == current && protocols[i] != conn.Protocol {
				conn.Protocol = protocols[i]
				a.cancelModelFetch()
				a.savePrefs()
			}
		}
	})
	row("address", "服务地址", func() {
		if ui.TextInput(c.Key("field"), &conn.BaseURL).Grow(1).Placeholder("https://api.example.com/v1").Label(i18n.T("服务地址")).Changed() {
			a.cancelModelFetch()
			a.savePrefs()
		}
	})
	row("key", "API Key", func() {
		a.qaSettings()
		if a.qa.key != "" {
			ui.Text(c, i18n.T("已保存")+" ••••"+lastRunes(a.qa.key, 4)).TextColor(t.TextMuted).Grow(1)
			if ui.Button(c.Key("remove"), i18n.T("移除")).Clicked() {
				a.removeAIKey()
			}
		} else {
			in := ui.TextInput(c.Key("field"), &a.qa.keyInput).Password().Grow(1).Placeholder(i18n.T("本机服务可以不填")).Label(i18n.T("粘贴 API Key"))
			if in.Submitted() {
				a.saveAIKey()
			}
			if ui.Button(c.Key("save"), i18n.T("保存")).Disabled(strings.TrimSpace(a.qa.keyInput) == "").Clicked() {
				a.saveAIKey()
			}
		}
	})
	if a.qa.keyErr != nil {
		ui.Text(c, i18n.T("无法读取 API Key：")+a.qa.keyErr.Error()).TextColor(t.Danger)
	}
	settingsSeparator(c)
	row("models", "模型列表", func() {
		ui.TextInput(c.Key("search"), &a.qa.modelSearch).Grow(1).Placeholder(i18n.T("搜索模型")).Label(i18n.T("搜索模型"))
		label := i18n.T("获取模型")
		if a.qa.modelsBusy {
			label = i18n.T("获取中…")
		}
		if ui.Button(c.Key("fetch"), label).Disabled(a.qa.modelsBusy || a.modelListSettings().CheckService() != nil).Clicked() {
			a.fetchModels()
		}
		if a.qa.modelsBusy && ui.Button(c.Key("cancel"), i18n.T("取消")).Clicked() {
			a.cancelModelFetch()
		}
	})
	ui.Row(c.Key("model-actions")).Gap(8).Children(func() {
		if ui.Button(c.Key("all"), i18n.T("全选")).Clicked() {
			for i := range conn.Models {
				conn.Models[i].Enabled = true
			}
			a.savePrefs()
		}
		if ui.Button(c.Key("none"), i18n.T("清空选择")).Clicked() {
			for i := range conn.Models {
				conn.Models[i].Enabled = false
			}
			conn.Model = ""
			a.savePrefs()
		}
		if ui.Button(c.Key("capabilities"), i18n.T("更新模型能力")).Disabled(a.qa.modelsBusy || conn.Vendor == "" || conn.Vendor == "custom").Clicked() {
			a.fetchModelCapabilities()
		}
	})
	ui.Text(c, a.modelsNote()).FontSize(12).TextColor(t.TextMuted)
	var visible []int
	query := strings.ToLower(a.qa.modelSearch)
	for i, m := range conn.Models {
		if strings.Contains(strings.ToLower(m.ID+" "+m.Name), query) {
			visible = append(visible, i)
		}
	}
	ui.List(c.Key("model-list"), &a.qa.modelsList, len(visible)).Height(min(260, max(48, float32(len(visible))*40))).ItemKey(func(i int) any { return conn.Models[visible[i]].ID }).Rows(func(row ui.ListRow) {
		idx := visible[row.Index]
		model := &conn.Models[idx]
		c := row.Context
		ui.Row(c.Key(model.ID)).Height(38).Gap(8).AlignItems(ui.Center).Children(func() {
			before := model.Enabled
			ui.Checkbox(c.Key("checked"), &model.Enabled, model.ID).Grow(1).MinWidth(0)
			if before != model.Enabled {
				if model.Enabled && conn.Model == "" {
					conn.Model = model.ID
				}
				if !model.Enabled && conn.Model == model.ID {
					conn.Model = ""
				}
				a.savePrefs()
			}
			if quietOneBotAction(c, "details", i18n.T("能力")).Label(i18n.T("模型能力：") + model.ID).Clicked() {
				if a.qa.capabilityModel == model.ID {
					a.qa.capabilityModel = ""
				} else {
					a.qa.capabilityModel = model.ID
				}
			}
		})
	})
	row("manual", "手动添加模型", func() {
		in := ui.TextInput(c.Key("field"), &a.qa.manualName).Grow(1).Label(i18n.T("输入模型名称"))
		if in.Submitted() {
			a.addManualAIModel()
		}
		if ui.Button(c.Key("add"), i18n.T("添加模型")).Disabled(strings.TrimSpace(a.qa.manualName) == "").Clicked() {
			a.addManualAIModel()
		}
	})
	for i := range conn.Models {
		if conn.Models[i].ID == a.qa.capabilityModel {
			a.modelCapabilitiesView(c, &conn.Models[i])
			break
		}
	}
	if a.qa.modelsErr != nil {
		ui.Text(c, i18n.T("获取模型列表失败：")+qaErrorText(a.qa.modelsErr)).FontSize(12).TextColor(t.Danger)
	}
	if a.qa.catalogErr != nil {
		ui.Text(c, i18n.T("更新模型能力失败：")+qaErrorText(a.qa.catalogErr)).FontSize(12).TextColor(t.Danger)
	}
	settingsSeparator(c)
	row("parameters", "生成参数", func() {
		ui.Text(c, i18n.T("输出上限（0 为默认）"))
		value := float64(conn.Parameters.MaxOutput)
		if ui.NumberInput(c.Key("tokens"), &value, 0, 1000000, 256).Width(130).Changed() {
			conn.Parameters.MaxOutput = int(value)
			a.savePrefs()
		}
	})
	row("temperature", "温度", func() {
		custom := conn.Parameters.Temperature != nil
		if ui.Checkbox(c.Key("custom"), &custom, i18n.T("自定义温度")).Changed() {
			if custom {
				v := 0.7
				conn.Parameters.Temperature = &v
			} else {
				conn.Parameters.Temperature = nil
			}
			a.savePrefs()
		}
		if conn.Parameters.Temperature != nil && ui.NumberInput(c.Key("value"), conn.Parameters.Temperature, 0, 2, 0.1).Width(120).Changed() {
			a.savePrefs()
		}
	})
	row("reasoning", "推理强度", func() {
		values := []string{"", "off", "minimal", "low", "medium", "high", "xhigh", "max"}
		if m := conn.CurrentModel(); m != nil && len(m.Capabilities.Efforts) > 0 {
			values = append([]string{"", "off"}, m.Capabilities.Efforts...)
		}
		before := conn.Parameters.Reasoning
		ui.Combobox(c.Key("field"), &conn.Parameters.Reasoning, values).Grow(1).Placeholder(i18n.T("服务默认"))
		if before != conn.Parameters.Reasoning {
			a.savePrefs()
		}
		budget := float64(conn.Parameters.ThinkingBudget)
		if ui.NumberInput(c.Key("budget"), &budget, 0, 1000000, 1024).Width(120).Label(i18n.T("推理预算（0 为自动）")).Changed() {
			conn.Parameters.ThinkingBudget = int(budget)
			a.savePrefs()
		}
	})
	status := i18n.T("可以提问了。")
	if err := a.qaReady(); err != nil {
		status = err.Error()
	}
	ui.Text(c, status).FontSize(13).TextColor(t.TextMuted)
}

func oneBotProtocolLabel(protocol string) string {
	switch protocol {
	case agent.Responses:
		return "OpenAI Responses"
	case agent.Anthropic:
		return "Anthropic Messages"
	case agent.Gemini:
		return "Google Gemini"
	case agent.Ollama:
		return "Ollama"
	default:
		return "OpenAI Chat Completions"
	}
}

func (a *App) modelCapabilitiesView(c *ui.Context, m *agent.ModelInfo) {
	ui.Column(c.Key("model-capabilities")).Gap(8).Padding(12, 0).Children(func() {
		ui.Text(c, m.ID).FontWeight(500)
		ui.Text(c, i18n.T("能力来源：")+m.Source+" "+m.Updated).FontSize(12).TextColor(c.Theme().TextMuted)
		changed := false
		for _, item := range []struct {
			name  string
			value *agent.Support
		}{{"工具调用", &m.Capabilities.Tools}, {"推理", &m.Capabilities.Reasoning}, {"温度参数", &m.Capabilities.Temperature}, {"结构化输出", &m.Capabilities.StructuredOutput}} {
			item := item
			ui.Row(c.Key(item.name)).Gap(8).Children(func() {
				ui.Text(c, i18n.T(item.name)).Grow(1)
				value := string(*item.value)
				if ui.Combobox(c.Key("field"), &value, []string{"", "yes", "no"}).Width(140).Placeholder(i18n.T("未知")).Changed() {
					*item.value = agent.Support(value)
					changed = true
				}
			})
		}
		for _, item := range []struct {
			name  string
			value *[]string
		}{{"输入模态", &m.Capabilities.Input}, {"输出模态", &m.Capabilities.Output}, {"可用推理强度", &m.Capabilities.Efforts}} {
			item := item
			ui.Row(c.Key(item.name)).Gap(8).Children(func() {
				ui.Text(c, i18n.T(item.name)).Grow(1)
				value := strings.Join(*item.value, ", ")
				if ui.TextInput(c.Key("field"), &value).Width(240).Placeholder("text, image, pdf").Changed() {
					var values []string
					for _, v := range strings.Split(value, ",") {
						if v = strings.TrimSpace(v); v != "" {
							values = append(values, v)
						}
					}
					*item.value = values
					changed = true
				}
			})
		}
		for _, item := range []struct {
			name  string
			value *int
		}{{"上下文上限", &m.Capabilities.Context}, {"最大输出", &m.Capabilities.MaxOutput}} {
			item := item
			ui.Row(c.Key(item.name)).Gap(8).Children(func() {
				ui.Text(c, i18n.T(item.name)).Grow(1)
				v := float64(*item.value)
				if ui.NumberInput(c.Key("field"), &v, 0, 10000000, 1024).Width(150).Changed() {
					*item.value = int(v)
					changed = true
				}
			})
		}
		if changed {
			m.UserOverride = true
			m.Source = "user"
			a.savePrefs()
		}
		if m.UserOverride && ui.Button(c.Key("reset"), i18n.T("恢复自动识别")).Clicked() {
			m.UserOverride = false
			m.Capabilities = agent.Capabilities{}
			m.Source = ""
			a.savePrefs()
		}
	})
}
func lastRunes(s string, n int) string { r := []rune(s); return string(r[max(0, len(r)-n):]) }
func (a *App) modelsNote() string {
	if a.qa.modelsBusy {
		return i18n.T("正在获取模型列表…")
	}
	if c := a.prefs.AI.Current(); c != nil {
		enabled := 0
		for _, m := range c.Models {
			if m.Enabled {
				enabled++
			}
		}
		return fmt.Sprintf(i18n.T("%d 个模型，已选 %d 个"), len(c.Models), enabled)
	}
	return i18n.T("请先添加供应商")
}
