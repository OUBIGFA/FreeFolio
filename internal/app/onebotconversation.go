package app

import (
	"fmt"
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// The composer/review hierarchy follows OpenCode's MIT-licensed desktop
// composer and session-review: one message timeline, one input, a quiet model
// selector, and expandable changes attached to assistant messages. See the
// pinned source record in third_party/opencode/README.md.
func (a *App) oneBotHeaderMenu(c *ui.Context) {
	iconButton(c, "onebot-menu", iconEllipsis, i18n.T("OneBot 菜单"), i18n.T("更多"), false).Role(ui.RoleMenuButton).Menu(func(m *ui.Menu) {
		if m.Item(i18n.T("OneBot 设置…")).Chosen() {
			a.openSettings(tabAI)
		}
		if m.Item(i18n.T("编辑记录")).Chosen() {
			a.loadOneBotHistory()
			a.qa.editHistoryOpen = true
		}
		m.Separator()
		if m.Item(i18n.T("直接修改")).Checked(a.prefs.AI.Direct).Chosen() {
			a.prefs.AI.Direct = !a.prefs.AI.Direct
			a.savePrefs()
		}
		if m.Item(i18n.T("清除编辑上下文")).Chosen() {
			a.qa.editMode = false
			a.qa.editTask = nil
			a.qa.editFollow = false
		}
	})
}

func quietOneBotAction(c *ui.Context, key, label string) ui.Element {
	e := ui.Row(c.Key(key)).Height(26).Padding(2, 4).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(label)
	ink := c.Theme().TextMuted
	if e.Hovered() {
		ink = c.Theme().Text
		e.Background(c.Theme().Text.Alpha(0.04)).Radius(4)
	}
	e.Children(func() { ui.Text(c, label).FontSize(12).TextColor(ink).NoWrap() })
	return e
}

// oneBotApplyButton is the one emphasised action in a review row: accent text
// on a faint accent tint, so it reads as the next step without a heavy fill.
func oneBotApplyButton(c *ui.Context, key, label string) ui.Element {
	t := c.Theme()
	e := ui.Row(c.Key(key)).Height(26).Padding(2, 9).Radius(5).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(label)
	fill := t.Accent.Alpha(0.1)
	if e.Hovered() {
		fill = t.Accent.Alpha(0.18)
	}
	e.Background(fill).Children(func() { ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.Accent).NoWrap() })
	return e
}

func (a *App) oneBotComposer(c *ui.Context, s *qaSession) {
	t := c.Theme()
	if a.qa.editHistoryOpen {
		ui.Scroll(c.Key("history-popover")).Height(220).Padding(12).Children(func() { a.oneBotHistoryView(c) })
		if quietOneBotAction(c, "close-history", i18n.T("关闭记录")).Clicked() {
			a.qa.editHistoryOpen = false
		}
	}
	if a.qa.editOptionsOpen {
		ui.Column(c.Key("edit-options")).Padding(12).Gap(6).Children(func() {
			a.oneBotScopeView(c)
			if quietOneBotAction(c, "close-options", i18n.T("完成")).Clicked() {
				a.qa.editOptionsOpen = false
			}
		})
	}
	ui.Column(c.Key("composer-margin")).Padding(10).Shrink(0).Children(func() {
		ui.Column(c.Key("composer")).Radius(10).Border(1, t.Border.Alpha(0.75)).Background(field(t)).Padding(9, 10, 5, 10).Gap(5).Children(func() {
			if task := a.qa.editTask; a.qa.editMode && task != nil && len(task.Targets) > 0 {
				label := task.Targets[0].Path
				if len(task.Targets) > 1 {
					label += fmt.Sprintf(" · %s", oneBotScopeLabel(task.Scope))
				} else {
					excerpt := []rune(strings.TrimSpace(task.Targets[0].Target.Text))
					if len(excerpt) > 48 {
						excerpt = append(excerpt[:48], '…')
					}
					label += " · " + string(excerpt)
				}
				ui.Row(c.Key("selection-context")).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Width(2).Height(22).Background(t.Accent.Alpha(0.55))
					ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
					if quietOneBotAction(c, "clear-context", "×").Tooltip(i18n.T("清除编辑上下文")).Clicked() {
						a.qa.editMode = false
						a.qa.editTask = nil
						a.qa.editFollow = false
					}
				})
			}
			if len(a.qa.attachments) > 0 {
				ui.Row(c.Key("attachments")).Gap(5).Wrap().Children(func() {
					for i, p := range a.qa.attachments {
						if quietOneBotAction(c, fmt.Sprint(i), p.Name+" ×").Clicked() {
							a.qa.attachments = append(a.qa.attachments[:i], a.qa.attachments[i+1:]...)
							break
						}
					}
				})
			}
			in := ui.TextInput(c.Key("qa-question"), &a.qa.input).Multiline(true).Height(60).Border(0, t.Border).Background(ui.Color{}).Padding(0).Label(i18n.T("问题")).Placeholder(i18n.T("发送消息，或描述要修改的内容…"))
			if a.qa.focusInput {
				a.qa.focusInput = false
				in.Focus()
			}
			if in.Submitted() {
				a.askQA()
			}
			if in.Focused() && c.Shortcut(0, ui.KeyEnter) {
				a.askQA()
			}
			ui.Row(c.Key("composer-toolbar")).Gap(5).AlignItems(ui.Center).Children(func() {
				iconButton(c, "context-add", iconPlus, i18n.T("添加上下文"), i18n.T("添加附件或编辑范围"), false).Size(26, 26).Role(ui.RoleMenuButton).Menu(func(m *ui.Menu) {
					if m.Item(i18n.T("添加附件")).Chosen() {
						a.pickOneBotAttachments()
					}
					m.Separator()
					for _, scope := range []string{"selection", "section", "document"} {
						if m.Item(oneBotScopeLabel(scope)).Chosen() {
							a.beginOneBotEdit(scope)
						}
					}
					if m.Item(i18n.T("选定多篇文档")).Chosen() {
						a.qa.editScope = "documents"
						a.qa.editOptionsOpen = true
						a.qa.editDocumentsOpen = true
					}
					if m.Item(i18n.T("创建新文档")).Chosen() {
						a.beginOneBotEdit("new")
						a.qa.editOptionsOpen = true
					}
					m.Separator()
					if m.Item(i18n.T("提供三个方案")).Checked(a.qa.editCount == 3).Chosen() {
						a.qa.editCount = 3
						if !a.qa.editMode {
							a.beginOneBotEdit("document")
						}
					}
					if m.Item(i18n.T("更多编辑选项…")).Chosen() {
						a.qa.editOptionsOpen = true
					}
				})
				a.oneBotCompactModel(c)
				ui.Spacer(c)
				busy := s.running()
				if task := a.qa.editTask; task != nil && (task.Running || task.Applying) {
					busy = true
				}
				if busy {
					if qaRoundButton(c, "qa-stop", iconStop, i18n.T("停止"), false).Clicked() {
						if task := a.qa.editTask; task != nil && (task.Running || task.Applying) {
							a.stopOneBotEdit()
						} else {
							a.stopQA()
						}
					}
				} else if qaRoundButton(c, "qa-send", iconArrowUp, i18n.T("发送 (Enter)"), strings.TrimSpace(a.qa.input) == "").Clicked() {
					a.askQA()
				}
			})
		})
	})
}

func (a *App) oneBotCompactModel(c *ui.Context) {
	label := i18n.T("选择模型")
	if conn := a.prefs.AI.Current(); conn != nil && conn.Model != "" {
		label = conn.Model
	}
	ui.MenuButton(c.Key("onebot-model"), label, func(m *ui.Menu) {
		for i, conn := range a.prefs.AI.Connections {
			if i > 0 {
				m.Separator()
			}
			for _, model := range conn.Models {
				if !model.Enabled {
					continue
				}
				if m.Item(conn.Name + " / " + model.ID).Checked(conn.ID == a.prefs.AI.Active && conn.Model == model.ID).Chosen() {
					if a.prefs.AI.Active != conn.ID {
						a.selectAIConnection(conn.ID)
					}
					a.prefs.AI.Current().Model = model.ID
					a.savePrefs()
				}
			}
		}
		m.Separator()
		if m.Item(i18n.T("管理供应商…")).Chosen() {
			a.openSettings(tabAI)
		}
	}).Height(26).Padding(2, 4).Border(0, c.Theme().Border).Background(ui.Color{}).TextColor(c.Theme().TextMuted).FontSize(12).MinWidth(0).Shrink(1).Label(i18n.T("选择模型"))
}

func (a *App) attachOneBotTask(task *oneBotTask) {
	s := a.qaSession(task.SpaceID)
	for _, turn := range s.turns {
		if turn.edit == task {
			return
		}
	}
	s.turns = append(s.turns, &qaTurn{question: task.Instruction, edit: task})
	s.list.ScrollToEnd()
}

func (a *App) oneBotReply(c *ui.Context, task *oneBotTask) {
	if task == nil {
		return
	}
	t := c.Theme()
	ui.Column(c.Key("onebot-edit-reply")).Gap(8).Children(func() {
		if task.Error != "" {
			ui.Text(c, task.Error).TextColor(t.Danger).FontSize(13)
		}
		if task.Stopped {
			ui.Text(c, i18n.T("已停止，未应用的内容保留为草稿")).FontSize(13).TextColor(t.TextMuted)
		}
		if task.Running || task.Applying {
			label := i18n.T("正在生成修改…")
			if task.Applying {
				label = i18n.T("正在应用并保存…")
			}
			ui.Text(c, label).FontSize(13).TextColor(t.TextMuted).Role(ui.RoleStatus)
			if task.Running && task.Selected >= 0 && task.Selected < len(task.Candidates) {
				candidate := task.Candidates[task.Selected]
				a.oneBotReplyText(c, task, candidate, candidate.Text)
			}
			return
		}
		if len(task.Candidates) == 0 {
			return
		}
		if task.Selected < 0 || task.Selected >= len(task.Candidates) {
			task.Selected = 0
		}
		if len(task.Candidates) > 1 {
			ui.Row(c.Key("alternatives")).Gap(12).Children(func() {
				for i := range task.Candidates {
					label := fmt.Sprintf(i18n.T("方案 %d"), i+1)
					el := quietOneBotAction(c, fmt.Sprint(i), label)
					if i == task.Selected {
						el.Opacity(1)
					}
					if el.Clicked() {
						task.Selected = i
					}
				}
			})
		}
		candidate := task.Candidates[task.Selected]
		if candidate.Error != "" {
			ui.Text(c, candidate.Error).FontSize(13).TextColor(t.Danger)
			if quietOneBotAction(c, "retry", i18n.T("重试")).Clicked() {
				a.qa.editTask = task
				a.qa.editMode = true
				a.qa.input = task.Instruction
				a.qa.focusInput = true
			}
			return
		}
		if !candidate.Complete {
			return
		}
		if len(candidate.Changes) == 0 {
			text := candidate.Text
			if strings.TrimSpace(text) == "" {
				text = i18n.T("没有产生修改")
			}
			a.oneBotReplyText(c, task, candidate, text)
			return
		}
		// The assistant answer previews its wording. JSON protocol text never
		// appears in the conversation, even while partial data is arriving.
		var parts []string
		pending, saved := 0, 0
		paths := map[string]bool{}
		for _, ch := range candidate.Changes {
			paths[ch.Path] = true
			if ch.Status == "pending" {
				pending++
			}
			if ch.Status == "saved" {
				saved++
			}
			content := ch.Operation.Text
			if ch.NewText != "" {
				content = ch.NewText
			}
			if content != "" {
				parts = append(parts, content)
			}
		}
		preview := strings.Join(parts, "\n\n")
		if len([]rune(preview)) > 2400 {
			preview = string([]rune(preview)[:2400]) + "…"
		}
		a.oneBotReplyText(c, task, candidate, preview)
		label := fmt.Sprintf(i18n.T("%d 个文件 · %d 处修改"), len(paths), len(candidate.Changes))
		if saved == len(candidate.Changes) {
			label = i18n.T("已保存") + " · " + label
		}
		ui.Row(c.Key("change-summary")).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconListTree).FontSize(13).TextColor(t.TextMuted)
			if quietOneBotAction(c, "diff", label).Clicked() {
				task.ShowDiff = !task.ShowDiff
			}
			ui.Spacer(c)
			if pending > 0 {
				if oneBotApplyButton(c, "apply", i18n.T("应用修改")).Clicked() {
					if err := a.applyOneBotCandidate(task, task.Selected, -1); err != nil {
						task.Error = err.Error()
					}
				}
			}
			iconButton(c, "more", iconEllipsis, i18n.T("修改操作"), i18n.T("更多"), false).Size(26, 26).Role(ui.RoleMenuButton).Menu(func(m *ui.Menu) {
				if m.Item(i18n.T("继续调整方案")).Chosen() {
					a.qa.editTask = task
					a.qa.editMode = true
					a.qa.editFollow = true
					a.qa.focusInput = true
				}
				if m.Item(i18n.T("基于最新原文重试")).Chosen() {
					a.restartOneBotTask(task)
				}
				if m.Item(i18n.T("拒绝全部")).Chosen() {
					for _, ch := range candidate.Changes {
						if ch.Status == "pending" {
							ch.Status = "rejected"
						}
					}
					a.keepOneBotTask(task)
				}
				if m.Item(i18n.T("回退本方案")).Chosen() {
					a.revertOneBotCandidate(task, candidate)
				}
			})
		})
		if task.ShowDiff {
			for i, ch := range candidate.Changes {
				a.oneBotChangeView(c.Key(i), task, task.Selected, i, ch)
			}
		}
	})
}

func (a *App) oneBotReplyText(c *ui.Context, task *oneBotTask, candidate *oneBotCandidate, text string) {
	if text == "" {
		return
	}
	candidate.preview.answer = text
	candidate.preview.view.parse(text, task.Running)
	session := a.qaSession(task.SpaceID)
	for i := range candidate.preview.view.blocks {
		a.qaAnswerBlock(c.Key(i), task.SpaceID, session, &candidate.preview, &candidate.preview.view.blocks[i])
	}
}
