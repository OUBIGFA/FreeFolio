package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// settingsAI owns native provider configuration and persistent model choices.
func (a *App) settingsAI(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, "OneBot", i18n.T("对话、写作与文档编辑。配置供应商并勾选需要使用的模型。"))
	conn := a.prefs.AI.Current()

	settingsSection(c, i18n.T("供应商"))
	settingsGroup(c, "provider-group", func() {
		ui.Row(c.Key("provider-actions")).Padding(10, 16).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			selectorLabel := i18n.T("选择供应商")
			if conn != nil && strings.TrimSpace(conn.Name) != "" {
				selectorLabel = conn.Name
			}
			ui.MenuButton(c.Key("connections"), selectorLabel, func(m *ui.Menu) {
				for _, item := range a.prefs.AI.Connections {
					if m.Item(item.Name).Checked(a.prefs.AI.Active == item.ID).Chosen() {
						a.selectAIConnection(item.ID)
					}
				}
			}).Label(i18n.T("选择供应商"))
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
			if conn != nil {
				ui.Spacer(c)
				if ui.Button(c.Key("remove"), i18n.T("移除供应商")).Disabled(conn.Protocol == agent.ChatGPT && a.chatgpt.busyID != "").Clicked() {
					a.removeAIConnection()
				}
			}
		})
		conn = a.prefs.AI.Current()
		if conn == nil {
			settingsSeparator(c)
			ui.Column(c.Key("provider-empty")).Padding(20, 16).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("请先添加供应商")).FontSize(13.5).FontWeight(500).TextColor(t.Text)
				ui.Text(c, i18n.T("点击上方「添加供应商」或从「常用服务」快速创建。")).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
			})
			return
		}
		a.dropStaleModels()
		formRow := func(key, label string, fn func()) {
			ui.Row(c.Key(key)).Padding(settingsRowInset(c), 16).Gap(12).AlignItems(ui.Center).MinHeight(46).Children(func() {
				ui.Text(c, i18n.T(label)).FontSize(13).FontWeight(500).Width(84).Shrink(0)
				ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(fn)
			})
		}
		settingsSeparator(c)
		formRow("name", "供应商名称", func() {
			if ui.TextInput(c.Key("field"), &conn.Name).Grow(1).Label(i18n.T("供应商名称")).Changed() {
				a.savePrefs()
			}
		})
		if conn.Protocol == agent.ChatGPT {
			settingsSeparator(c)
			a.chatGPTAccountView(c)
			return
		}
		settingsSeparator(c)
		formRow("protocol", "接口协议", func() {
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
		settingsSeparator(c)
		formRow("address", "服务地址", func() {
			if ui.TextInput(c.Key("field"), &conn.BaseURL).Grow(1).Placeholder("https://api.example.com/v1").Label(i18n.T("服务地址")).Changed() {
				a.cancelModelFetch()
				a.savePrefs()
			}
		})
		settingsSeparator(c)
		formRow("key", "API Key", func() {
			a.qaSettings()
			placeholder := i18n.T("粘贴 API Key，多个用逗号或换行分隔")
			if agent.IsLocal(conn.BaseURL) {
				placeholder = i18n.T("本机服务可以不填")
			}
			in := ui.TextInput(c.Key("field"), &a.qa.keyInput).Password().Grow(1).Placeholder(placeholder).Label(i18n.T("粘贴 API Key"))
			if in.Submitted() {
				a.saveAIKey()
			}
			if ui.Button(c.Key("save"), i18n.T("添加")).Disabled(strings.TrimSpace(a.qa.keyInput) == "").Clicked() {
				a.saveAIKey()
			}
		})
		// Saved keys show masked; each can be shown in full again, copied or
		// removed. The settings keep them in memory only while open.
		for i, key := range a.qa.keys {
			settingsSeparator(c)
			formRow(fmt.Sprintf("key-%d", i), fmt.Sprintf(i18n.T("Key %d"), i+1), func() {
				shown := a.qa.keyShown[key]
				text := maskAPIKey(key)
				if shown {
					text = key
				}
				ui.Text(c, text).FontSize(13).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
				a.keyTestStatus(c, key)
				if quietOneBotAction(c, "test", i18n.T("测试")).Label(fmt.Sprintf(i18n.T("测试 Key %d"), i+1)).Shrink(0).Clicked() {
					a.testAIKeys(key)
				}
				eye, tip := iconEye, i18n.T("显示 API Key")
				if shown {
					eye, tip = iconEyeOff, i18n.T("隐藏 API Key")
				}
				if iconButton(c, "show", eye, tip, tip, false).Shrink(0).Clicked() {
					if a.qa.keyShown == nil {
						a.qa.keyShown = map[string]bool{}
					}
					a.qa.keyShown[key] = !shown
				}
				if iconButton(c, "copy", iconCopy, i18n.T("复制 API Key"), i18n.T("复制 API Key"), false).Shrink(0).Clicked() {
					c.WriteClipboard(key)
					c.Toast(i18n.T("已复制 API Key"))
				}
				if iconButton(c, "remove", iconTrash, i18n.T("移除 API Key"), i18n.T("移除 API Key"), false).Shrink(0).Clicked() {
					a.removeAIKey(key)
				}
			})
		}
		settingsSeparator(c)
		formRow("key-test", "连接测试", func() {
			// A service on this computer may answer without a key.
			keys := a.qa.keys
			label := i18n.T("测试连接")
			if len(keys) > 1 {
				label = i18n.T("测试全部 Key")
			}
			if len(keys) == 0 {
				keys = []string{""}
				a.keyTestStatus(c, "")
			}
			ui.Text(c, i18n.T("用所选模型发送一条简短消息，收到回复即停止")).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
			busy := a.keyTestsBusy()
			if busy {
				label = i18n.T("测试中…")
			}
			if ui.Button(c.Key("run"), label).Disabled(busy).Shrink(0).Clicked() {
				a.testAIKeys(keys...)
			}
		})
		tested := a.qa.keys
		if len(tested) == 0 {
			tested = []string{""}
		}
		for i, key := range tested {
			if r, ok := a.keyTestOf(key); ok && r.err != nil {
				name := i18n.T("连接测试失败：")
				if key != "" {
					name = fmt.Sprintf(i18n.T("Key %d 测试失败："), i+1)
				}
				ui.Text(c.Key(fmt.Sprintf("key-test-err-%d", i)), name+qaErrorText(r.err)).FontSize(12).TextColor(t.Danger).Padding(0, 16, 10, 16)
			}
		}
		if len(a.qa.keys) > 1 {
			settingsSeparator(c)
			formRow("key-strategy", "多 Key 策略", func() {
				strategies := []string{agent.KeyRoundRobin, agent.KeyRandom, agent.KeyFailover}
				labels := []string{i18n.T("轮询"), i18n.T("随机"), i18n.T("故障转移")}
				current := labels[max(0, slices.Index(strategies, conn.KeyStrategy))]
				ui.Select(c.Key("field"), &current, labels).Grow(1).Label(i18n.T("多 Key 策略"))
				if i := slices.Index(labels, current); i >= 0 && strategies[i] != conn.KeyStrategy {
					conn.KeyStrategy = strategies[i]
					a.savePrefs()
				}
			})
			note := map[string]string{
				agent.KeyRoundRobin: "按顺序轮流使用每个 Key，分摊请求。",
				agent.KeyRandom:     "每次请求随机选用一个 Key。",
				agent.KeyFailover:   "总是先用第一个 Key，失败时再依次换用后面的 Key。",
			}[conn.KeyStrategy]
			ui.Text(c, i18n.T(note)+i18n.T("Key 被拒绝、额度不足、限流或服务出错，且尚未输出内容时，自动换下一个 Key 重试。")).
				FontSize(12).TextColor(t.TextMuted).Padding(0, 16, 10, 16)
		}
		if a.qa.keyErr != nil {
			ui.Text(c, i18n.T("无法读取 API Key：")+a.qa.keyErr.Error()).FontSize(12).TextColor(t.Danger).Padding(0, 16, 10, 16)
		}
	})
	conn = a.prefs.AI.Current()
	if conn == nil {
		return
	}

	settingsSection(c, i18n.T("模型列表"))
	settingsGroup(c, "models-group", func() {
		ui.Row(c.Key("models")).Padding(12, 16, 8, 16).Gap(8).AlignItems(ui.Center).Children(func() {
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
		ui.Row(c.Key("model-actions")).Padding(0, 16, 10, 16).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, a.modelsNote()).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(100)
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
			if ui.Button(c.Key("capabilities"), i18n.T("更新模型能力")).Disabled(a.qa.modelsBusy || conn.Protocol == agent.ChatGPT || conn.Vendor == "" || conn.Vendor == "custom").Clicked() {
				a.fetchModelCapabilities()
			}
		})
		settingsSeparator(c)
		var visible []int
		query := strings.ToLower(a.qa.modelSearch)
		for i, m := range conn.Models {
			if strings.Contains(strings.ToLower(m.ID+" "+m.Name), query) {
				visible = append(visible, i)
			}
		}
		if len(visible) == 0 {
			msg := i18n.T("点击「获取模型」或在下方手动添加模型。")
			if conn.Protocol == agent.ChatGPT {
				msg = i18n.T("登录后点击「获取模型」，显示此账号可用的模型。")
			}
			if len(conn.Models) > 0 {
				msg = i18n.T("没有匹配的模型。")
			}
			ui.Text(c, msg).FontSize(12.5).TextColor(t.TextMuted).Padding(14, 16)
		} else {
			ui.List(c.Key("model-list"), &a.qa.modelsList, len(visible)).Height(min(240, max(44, float32(len(visible))*38+8))).
				Padding(4, 10).ItemKey(func(i int) any { return conn.Models[visible[i]].ID }).Rows(func(row ui.ListRow) {
				idx := visible[row.Index]
				model := &conn.Models[idx]
				c := row.Context
				item := ui.Row(c.Key(model.ID)).Height(36).Padding(0, 6).Radius(controlRadius).Gap(8).AlignItems(ui.Center)
				if item.Hovered() {
					item.Background(hoverFill(t))
				}
				item.Children(func() {
					before := model.Enabled
					label := model.ID
					if conn.Protocol == agent.ChatGPT && model.Name != "" {
						label = model.Name
					}
					ui.Checkbox(c.Key("checked"), &model.Enabled, label).Tooltip(model.ID).Grow(1).MinWidth(0)
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
		}
		for i := range conn.Models {
			if conn.Models[i].ID == a.qa.capabilityModel {
				a.modelCapabilitiesView(c, &conn.Models[i])
				break
			}
		}
		if a.qa.modelsErr != nil {
			ui.Text(c, i18n.T("获取模型列表失败：")+qaErrorText(a.qa.modelsErr)).FontSize(12).TextColor(t.Danger).Padding(6, 16)
		}
		if a.qa.catalogErr != nil {
			ui.Text(c, i18n.T("更新模型能力失败：")+qaErrorText(a.qa.catalogErr)).FontSize(12).TextColor(t.Danger).Padding(6, 16)
		}
		if conn.Protocol == agent.ChatGPT {
			return
		}
		settingsSeparator(c)
		ui.Row(c.Key("manual")).Padding(10, 16).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("手动添加模型")).FontSize(13).FontWeight(500).Width(84).Shrink(0)
			in := ui.TextInput(c.Key("field"), &a.qa.manualName).Grow(1).Placeholder(i18n.T("输入模型名称")).Label(i18n.T("输入模型名称"))
			if in.Submitted() {
				a.addManualAIModel()
			}
			if ui.Button(c.Key("add"), i18n.T("添加模型")).Disabled(strings.TrimSpace(a.qa.manualName) == "").Clicked() {
				a.addManualAIModel()
			}
		})
	})

	settingsSection(c, i18n.T("生成参数"))
	settingsGroup(c, "params-group", func() {
		if conn.Protocol != agent.ChatGPT {
			settingRow(c, "parameters", i18n.T("输出上限"), i18n.T("0 为默认"), func() {
				value := float64(conn.Parameters.MaxOutput)
				if ui.NumberInput(c.Key("tokens"), &value, 0, 1000000, 256).Width(130).Label(i18n.T("输出上限（0 为默认）")).Changed() {
					conn.Parameters.MaxOutput = int(value)
					a.savePrefs()
				}
			})
			settingsSeparator(c)
			settingRow(c, "temperature", i18n.T("温度"), "", func() {
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
				if conn.Parameters.Temperature != nil && ui.NumberInput(c.Key("value"), conn.Parameters.Temperature, 0, 2, 0.1).Width(110).Changed() {
					a.savePrefs()
				}
			})
			settingsSeparator(c)
		}
		note := i18n.T("推理预算（0 为自动）")
		if conn.Protocol == agent.ChatGPT {
			note = i18n.T("以所选模型支持的推理强度为准")
		}
		settingRow(c, "reasoning", i18n.T("推理强度"), note, func() {
			values := []string{"", "off", "minimal", "low", "medium", "high", "xhigh", "max"}
			if m := conn.CurrentModel(); m != nil && len(m.Capabilities.Efforts) > 0 {
				values = append([]string{"", "off"}, m.Capabilities.Efforts...)
			}
			// A select of named levels: the editable combobox listed a blank
			// row for the default and the raw values, its caret in the field.
			if !slices.Contains(values, conn.Parameters.Reasoning) {
				values = append(values, conn.Parameters.Reasoning)
			}
			labels := make([]string, len(values))
			for i, v := range values {
				labels[i] = reasoningLabel(v)
			}
			current := reasoningLabel(conn.Parameters.Reasoning)
			ui.Select(c.Key("field"), &current, labels).Width(120).Label(i18n.T("推理强度"))
			if i := slices.Index(labels, current); i >= 0 && values[i] != conn.Parameters.Reasoning {
				conn.Parameters.Reasoning = values[i]
				a.savePrefs()
			}
			if conn.Protocol == agent.ChatGPT {
				return
			}
			budget := float64(conn.Parameters.ThinkingBudget)
			if ui.NumberInput(c.Key("budget"), &budget, 0, 1000000, 1024).Width(116).Label(i18n.T("推理预算（0 为自动）")).Changed() {
				conn.Parameters.ThinkingBudget = int(budget)
				a.savePrefs()
			}
		})
	})

	ready := a.qaReady() == nil
	status := i18n.T("可以提问了。")
	dotCol, bgCol, textCol := t.Success, t.Success.Alpha(0.08), t.Text.Alpha(0.84)
	if err := a.qaReady(); err != nil {
		status = err.Error()
		dotCol, bgCol, textCol = t.TextMuted, t.Text.Alpha(0.04), t.TextMuted
	}
	if !ready {
		bgCol = t.Text.Alpha(0.04)
	}
	ui.Row(c.Key("ai-status")).Gap(8).Padding(9, 14).Radius(controlRadius).AlignItems(ui.Center).
		Background(bgCol).Border(1, dotCol.Alpha(0.24)).Children(func() {
		ui.Box(c).Size(7, 7).Radius(3.5).Background(dotCol).Shrink(0)
		ui.Text(c, status).FontSize(12.5).TextColor(textCol)
	})
}

// reasoningLabel names a reasoning effort; one the list does not know
// shows as the service calls it.
func reasoningLabel(effort string) string {
	if name, ok := map[string]string{"": "服务默认", "off": "关闭推理", "minimal": "最低", "low": "低", "medium": "中", "high": "高", "xhigh": "超高", "max": "最高"}[effort]; ok {
		return i18n.T(name)
	}
	return effort
}

// keyTestStatus shows the last test of key: running, its time, or failed.
func (a *App) keyTestStatus(c *ui.Context, key string) {
	r, ok := a.keyTestOf(key)
	if !ok {
		return
	}
	t := c.Theme()
	text, ink := i18n.T("测试中…"), t.TextMuted
	switch {
	case r.busy:
	case r.err != nil:
		text, ink = i18n.T("不可用"), t.Danger
	default:
		text, ink = fmt.Sprintf(i18n.T("可用 · %.1f 秒"), r.took.Seconds()), t.Success
	}
	ui.Text(c.Key("test-status"), text).FontSize(12).TextColor(ink).NoWrap().Shrink(0)
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
	t := c.Theme()
	ui.Column(c.Key("model-capabilities")).Gap(8).Margin(4, 16, 12, 16).Padding(12, 14).Radius(8).
		Background(t.Text.Alpha(0.025)).Border(1, t.Border.Alpha(0.6)).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.ID).FontSize(13).FontWeight(600).Grow(1).SingleLine()
			if m.Source != "" || m.Updated != "" {
				ui.Text(c, strings.TrimSpace(i18n.T("能力来源：")+m.Source+" "+m.Updated)).FontSize(11.5).TextColor(t.TextMuted).NoWrap()
			}
		})
		changed := false
		for _, item := range []struct {
			name  string
			value *agent.Support
		}{{"工具调用", &m.Capabilities.Tools}, {"推理", &m.Capabilities.Reasoning}, {"温度参数", &m.Capabilities.Temperature}, {"结构化输出", &m.Capabilities.StructuredOutput}} {
			item := item
			ui.Row(c.Key(item.name)).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T(item.name)).FontSize(12.5).Grow(1)
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
			ui.Row(c.Key(item.name)).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T(item.name)).FontSize(12.5).Grow(1)
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
			ui.Row(c.Key(item.name)).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T(item.name)).FontSize(12.5).Grow(1)
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
		if m.UserOverride && ui.Button(c.Key("reset"), i18n.T("恢复自动识别")).AlignSelf(ui.Start).Clicked() {
			m.UserOverride = false
			m.Capabilities = agent.Capabilities{}
			m.Source = ""
			a.savePrefs()
		}
	})
}

// maskAPIKey hides all of a key but enough of its ends to tell keys apart.
func maskAPIKey(key string) string {
	r := []rune(key)
	if len(r) <= 10 {
		return "••••" + lastRunes(key, 2)
	}
	return string(r[:3]) + "••••••" + lastRunes(key, 4)
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
