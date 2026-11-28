package app

import (
	"fmt"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// The composer/review hierarchy follows OpenCode's MIT-licensed desktop
// composer and session-review: one message timeline, one input, a quiet model
// selector, and expandable changes attached to assistant messages. See the
// pinned source record in third_party/opencode/README.md.
func (ob *qaState) oneBotHeaderMenu(c *ui.Context) {
	sp := ob.qaActiveSpace()
	var sess *qaSession
	if sp != nil {
		sess = ob.qaSession(sp.ID)
	}
	hasTurns := sess != nil && len(sess.turns) > 0
	busy := sess != nil && sess.running()
	alreadyCleared := !hasTurns || sess.turns[len(sess.turns)-1].contextBreakAfter
	iconButton(c, "onebot-menu", iconEllipsis, i18n.T("OneBot 菜单"), i18n.T("更多"), false).Role(ui.RoleMenuButton).Menu(func(m *ui.Menu) {
		if m.Item(i18n.T("OneBot 设置")).Chosen() {
			ob.host.openSettings(tabAI)
		}
		if m.Item(i18n.T("编辑记录")).Chosen() {
			ob.loadOneBotHistory()
			ob.historyOpen = false
			ob.editHistoryOpen = true
		}
		m.Separator()
		if m.Item(i18n.T("清除上下文")).Disabled(busy || alreadyCleared).Chosen() && sp != nil {
			ob.clearOneBotContext(sp.ID)
		}
		if m.Item(i18n.T("复制完整对话")).Disabled(!hasTurns).Chosen() {
			ob.copyQAText(c, "conv-copy", exportOneBotConversationMarkdown(sess))
		}
		m.Separator()
		if m.Item(i18n.T("直接修改")).Checked(ob.host.preferences().AI.Direct).Chosen() {
			ob.host.preferences().AI.Direct = !ob.host.preferences().AI.Direct
			ob.host.savePrefs()
		}
		if m.Item(i18n.T("清除编辑上下文")).Chosen() {
			ob.editMode = false
			ob.editTask = nil
			ob.editFollow = false
		}
	})
}

func quietOneBotAction(c *ui.Context, key, label string) ui.Element {
	e := ui.Row(c.Key(key)).Height(26).Padding(4, 8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(label)
	ink := c.Theme().TextMuted
	if e.Hovered() {
		ink = c.Theme().Text
		e.Background(c.Theme().Text.Alpha(0.04)).Radius(8)
	}
	e.Children(func() { ui.Text(c, label).FontSize(12).TextColor(ink).NoWrap() })
	return e
}

// oneBotApplyButton is the one emphasised action in a review row: accent text
// on a faint accent tint, so it reads as the next step without a heavy fill.
func oneBotApplyButton(c *ui.Context, key, label string) ui.Element {
	t := c.Theme()
	e := ui.Row(c.Key(key)).Height(26).Padding(4, 12).Radius(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(label)
	fill := t.Accent.Alpha(0.1)
	if e.Hovered() {
		fill = t.Accent.Alpha(0.18)
	}
	e.Background(fill).Children(func() { ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.Accent).NoWrap() })
	return e
}

func (ob *qaState) oneBotComposer(c *ui.Context, s *qaSession) {
	t := c.Theme()
	if ob.editHistoryOpen {
		ui.Column(c.Key("history-drawer")).Background(t.Text.Alpha(0.024)).Children(func() {
			ui.Row(c.Key("history-head")).Padding(12, 20, 8, 20).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("编辑记录与恢复")).FontSize(12.5).FontWeight(600).Grow(1)
				if quietOneBotAction(c, "close-history", i18n.T("关闭记录")).Clicked() {
					ob.editHistoryOpen = false
				}
			})
			ui.Scroll(c.Key("history-popover")).Height(200).Padding(8, 20, 16, 20).Children(func() { ob.oneBotHistoryView(c) })
		})
	}
	if ob.editOptionsOpen {
		ui.Column(c.Key("edit-options")).Background(t.Text.Alpha(0.024)).Padding(16, 20).Gap(12).Children(func() {
			ob.oneBotScopeView(c)
			ui.Row(c).Justify(ui.End).Children(func() {
				if quietOneBotAction(c, "close-options", i18n.T("完成")).Clicked() {
					ob.editOptionsOpen = false
				}
			})
		})
	}
	ui.Column(c.Key("composer-margin")).Padding(12, 20, 20, 20).Shrink(0).Children(func() {
		ob.host.chatGPT().chatGPTUsageView(c)
		composer := ui.Column(c.Key("composer")).Radius(panelRadius).Border(1, t.Border).Background(field(t)).Padding(12, 16, 8, 16).Gap(8)
		if ob.dropHover {
			composer.Border(1.5, t.Accent).Background(t.Accent.Alpha(0.06))
		} else if composer.FocusWithin() {
			composer.Border(1, t.Accent.Alpha(0.45))
		}
		composer.Children(func() {
			if ob.dropHover {
				ui.Row(c.Key("drop-hint")).Gap(8).Padding(8, 12).Radius(8).AlignItems(ui.Center).
					Background(t.Accent.Alpha(0.10)).Children(func() {
					ui.Icon(c, iconPlus).FontSize(13).TextColor(t.Accent)
					ui.Text(c, i18n.T("松开以引用到 OneBot（支持多文件、图片）")).FontSize(12).FontWeight(500).TextColor(t.Accent)
				})
			}
			if task := ob.editTask; ob.editMode && task != nil && len(task.Targets) > 0 {
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
				ui.Row(c.Key("selection-context")).Gap(8).Padding(8, 8, 8, 12).Radius(8).AlignItems(ui.Center).
					Background(t.Accent.Alpha(0.07)).Border(1, t.Accent.Alpha(0.16)).Children(func() {
					ui.Box(c).Width(2.5).Height(18).Radius(8).Background(t.Accent.Alpha(0.75)).Shrink(0)
					ui.Text(c, label).FontSize(12).TextColor(t.Text.Alpha(0.82)).SingleLine().Grow(1).MinWidth(0)
					if quietOneBotAction(c, "clear-context", "×").Height(22).Tooltip(i18n.T("清除编辑上下文")).Clicked() {
						ob.editMode = false
						ob.editTask = nil
						ob.editFollow = false
					}
				})
			}
			if len(ob.quotes) > 0 || len(ob.attachments) > 0 {
				_, height := c.Size()
				ui.Scroll(c.Key("draft-references")).MinHeight(0).MaxHeight(min(108, height/5)).Gap(8).Children(func() {
					ob.oneBotQuoteBadges(c, "quotes", ob.quotes, true)
					badges := make([]oneBotAttachmentBadge, len(ob.attachments))
					for i, item := range ob.attachments {
						badges[i] = item.badge()
					}
					ob.oneBotAttachmentBadges(c, "attachments", badges, func(i int) {
						ob.attachments = append(ob.attachments[:i:i], ob.attachments[i+1:]...)
					})
				})
			}
			svc := c.Services()
			in := ui.TextAreaBase(c.Key("qa-question"), &ob.input).Lines(1, 6).MinHeight(24).FontSize(14).Padding(4, 0).Label(i18n.T("问题")).Placeholder(i18n.T("发送消息，或拖入附件")).OnTextKey(func(key ui.TextKey) bool {
				if key.Key != ui.KeyEnter || key.Mods != 0 {
					return false
				}
				ob.input = key.Text()
				ob.askQA()
				if ob.input != key.Text() {
					key.SetText(ob.input)
				}
				return true
			}).OnPaste(func(text string) bool {
				data := svc.ReadClipboardData(transfer.FileList, transfer.PNG)
				if files, err := data.Files(); err == nil && len(files) > 0 {
					ob.addOneBotFiles(files)
					return true
				}
				if text == "" {
					if raw, err := data.Read(transfer.PNG); err == nil && len(raw) > 0 {
						ob.addOneBotClipboardImage(raw)
						return true
					}
				}
				return false
			})
			if ob.focusInput {
				ob.focusInput = false
				in.Focus()
			}
			ui.Row(c.Key("composer-toolbar")).Gap(8).AlignItems(ui.Center).Children(func() {
				iconButton(c, "context-add", iconPlus, i18n.T("添加上下文"), i18n.T("添加附件或编辑范围"), false).Size(28, 28).Role(ui.RoleMenuButton).Menu(func(m *ui.Menu) {
					if m.Item(i18n.T("添加附件")).Chosen() {
						ob.pickOneBotAttachments()
					}
					m.Separator()
					for _, scope := range []string{"selection", "section", "document"} {
						if m.Item(oneBotScopeLabel(scope)).Chosen() {
							ob.beginOneBotEdit(scope)
						}
					}
					if m.Item(i18n.T("选定多篇文档")).Chosen() {
						ob.editScope = "documents"
						ob.editOptionsOpen = true
						ob.editDocumentsOpen = true
					}
					if m.Item(i18n.T("创建新文档")).Chosen() {
						ob.beginOneBotEdit("new")
						ob.editOptionsOpen = true
					}
					m.Separator()
					if m.Item(i18n.T("提供三个方案")).Checked(ob.editCount == 3).Chosen() {
						ob.editCount = 3
						if !ob.editMode {
							ob.beginOneBotEdit("document")
						}
					}
					if m.Item(i18n.T("更多编辑选项")).Chosen() {
						ob.editOptionsOpen = true
					}
				})
				busy := s.running()
				if task := ob.editTask; task != nil && (task.Running || task.Applying) {
					busy = true
				}
				ctxCleared := len(s.turns) == 0 || s.turns[len(s.turns)-1].contextBreakAfter
				if iconButton(c, "qa-clear-context", iconEraser, i18n.T("清除上下文"), i18n.T("清除上下文（保留上方记录，重新开始对话）"), busy || ctxCleared).Size(28, 28).Clicked() {
					ob.clearOneBotContext(s.spaceID)
				}
				ob.oneBotCompactModel(c)
				ui.Spacer(c)
				if len(s.turns) > 0 {
					ctxLabel := i18n.T("新上下文")
					if n := activeContextTurns(s); n > 0 {
						ctxLabel = fmt.Sprintf(i18n.T("%d 轮上下文"), n)
					}
					ui.Text(c, ctxLabel).FontSize(11).TextColor(t.TextMuted.Alpha(0.68)).NoWrap().Padding(0, 8, 0, 0)
				}
				sendDisabled := strings.TrimSpace(ob.input) == "" && (ob.editMode || len(ob.attachments) == 0)
				if busy {
					if qaRoundButton(c, "qa-stop", iconStop, i18n.T("停止"), false).Clicked() {
						if task := ob.editTask; task != nil && (task.Running || task.Applying) {
							ob.stopOneBotEdit()
						} else {
							ob.stopQA()
						}
					}
				} else if qaRoundButton(c, "qa-send", iconArrowUp, i18n.T("发送 (Enter)"), sendDisabled).Clicked() {
					ob.askQA()
				}
			})
		})
	})
}

func oneBotAttachmentIcon(kind string) *ui.SVG {
	if kind == "image" {
		return iconImage
	}
	return iconFile
}

func (ob *qaState) oneBotAttachmentBadges(c *ui.Context, key string, badges []oneBotAttachmentBadge, onRemove func(int)) {
	if len(badges) == 0 {
		return
	}
	t := c.Theme()
	ui.Row(c.Key(key)).Gap(8).Wrap().Children(func() {
		for i, b := range badges {
			clickable := b.SpaceID != "" && b.RelPath != ""
			var removed bool
			ui.Row(c.Key(fmt.Sprint(i))).Height(24).Radius(8).AlignItems(ui.Center).
				Background(t.Text.Alpha(0.055)).Children(func() {
				var body ui.Element
				if clickable {
					body = ui.Row(c.Key("open")).Height(24).PaddingX(12).Gap(8).Radius(8).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("打开附件：") + b.Name).Tooltip(b.RelPath)
					if body.Hovered() {
						body.Background(hoverFill(t))
					}
				} else {
					body = ui.Row(c.Key("info")).Height(24).PaddingX(12).Gap(8).Radius(8).AlignItems(ui.Center)
					if b.MIME != "" {
						body.Tooltip(b.MIME)
					}
				}
				body.Children(func() {
					if b.Thumb != nil {
						ui.Image(c, b.Thumb).Size(16, 16).Radius(8).Shrink(0)
					} else {
						ui.Icon(c, oneBotAttachmentIcon(b.Kind)).FontSize(12).TextColor(t.TextMuted)
					}
					ui.Text(c, b.Name).FontSize(11.5).TextColor(t.Text.Alpha(0.82)).SingleLine().MaxWidth(160)
				})
				if clickable && body.Clicked() {
					ob.host.openAnywhere(b.SpaceID, b.RelPath)
				}
				if onRemove != nil {
					x := ui.Row(c.Key("remove")).Size(20, 24).Radius(8).Center().Cursor(ui.CursorPointer).
						Focusable().Role(ui.RoleButton).Label(i18n.T("移除附件：") + b.Name)
					if x.Hovered() {
						x.Background(hoverFill(t))
					}
					x.Children(func() { ui.Icon(c, iconX).FontSize(11).TextColor(t.TextMuted) })
					if x.Clicked() {
						onRemove(i)
						removed = true
					}
				}
			})
			if removed {
				break
			}
		}
	})
}

func (ob *qaState) oneBotCompactModel(c *ui.Context) {
	t := c.Theme()
	label := i18n.T("选择模型")
	if conn := ob.host.preferences().AI.Current(); conn != nil && conn.Model != "" {
		label = oneBotModelName(conn.Name, conn.Model)
		if conn.Protocol == agent.ChatGPT {
			if model := conn.CurrentModel(); model != nil && model.Name != "" {
				label = oneBotModelName(conn.Name, model.Name)
			}
		}
	}
	b := ui.MenuButton(c.Key("onebot-model"), label, func(m *ui.Menu) {
		first := true
		for _, conn := range ob.host.preferences().AI.Connections {
			// Image and video services cannot hold a conversation.
			if conn.Protocol == agent.Media {
				continue
			}
			if !first {
				m.Separator()
			}
			first = false
			for _, model := range conn.Models {
				if !model.Enabled {
					continue
				}
				name := model.ID
				if conn.Protocol == agent.ChatGPT && model.Name != "" {
					name = model.Name
				}
				if m.Item(oneBotModelName(conn.Name, name)).Checked(conn.ID == ob.host.preferences().AI.Active && conn.Model == model.ID).Chosen() {
					if ob.host.preferences().AI.Active != conn.ID {
						ob.selectAIConnection(conn.ID)
					}
					ob.host.preferences().AI.Current().Model = model.ID
					ob.host.savePrefs()
				}
			}
		}
		m.Separator()
		if m.Item(i18n.T("管理供应商")).Chosen() {
			ob.host.openSettings(tabAI)
		}
	}).Height(26).Padding(4, 12).Radius(8).Border(0, t.Border).Background(ui.Color{}).TextColor(t.TextMuted).FontSize(12).MinWidth(0).Shrink(1).Label(i18n.T("选择模型")).Tooltip(label)
	if b.Hovered() {
		b.Background(hoverFill(t)).TextColor(t.Text)
	}
}

// oneBotModelName is the full name of a model: its provider, then its ID,
// so models of the same ID from two providers read apart.
func oneBotModelName(provider, model string) string {
	if provider = strings.TrimSpace(provider); provider == "" {
		return model
	}
	return provider + " / " + model
}

func (ob *qaState) attachOneBotTask(task *oneBotTask) {
	s := ob.qaSession(task.SpaceID)
	for _, turn := range s.turns {
		if turn.edit == task {
			return
		}
	}
	s.turns = append(s.turns, &qaTurn{model: ob.qaSettings().Model, start: len(s.history), question: task.Instruction, edit: task})
	s.list.ScrollToEnd()
	ob.saveOneBotConversation(task.SpaceID, s)
}

func (ob *qaState) oneBotReply(c *ui.Context, task *oneBotTask) {
	if task == nil {
		return
	}
	t := c.Theme()
	ui.Column(c.Key("onebot-edit-reply")).Gap(12).Children(func() {
		if task.Error != "" {
			ui.Text(c, task.Error).TextColor(t.Danger).FontSize(13)
		}
		if task.Stopped {
			ui.Text(c, i18n.T("已停止，未应用的内容保留为草稿")).FontSize(13).TextColor(t.TextMuted)
		}
		if task.Running || task.Applying {
			label := i18n.T("正在生成修改")
			if task.Applying {
				label = i18n.T("正在应用并保存")
			}
			ui.Text(c, label).FontSize(13).TextColor(t.TextMuted).Role(ui.RoleStatus)
			if task.Running && task.Selected >= 0 && task.Selected < len(task.Candidates) {
				candidate := task.Candidates[task.Selected]
				ob.oneBotReplyText(c, task, candidate, candidate.Text)
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
			ui.Row(c.Key("alternatives")).Gap(8).Wrap().Children(func() {
				for i := range task.Candidates {
					label := fmt.Sprintf(i18n.T("方案 %d"), i+1)
					selected := i == task.Selected
					el := ui.Row(c.Key(fmt.Sprint(i))).Height(26).Padding(0, 16).Radius(8).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(label)
					ink := t.TextMuted
					if selected {
						ink = t.Accent
						el.Background(t.Accent.Alpha(0.12)).Border(1, t.Accent.Alpha(0.28))
					} else {
						el.Background(t.Text.Alpha(0.035))
						if el.Hovered() {
							ink = t.Text
							el.Background(hoverFill(t))
						}
					}
					el.Children(func() {
						txt := ui.Text(c, label).FontSize(12).TextColor(ink).NoWrap()
						if selected {
							txt.FontWeight(600)
						}
					})
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
				ob.editTask = task
				ob.editMode = true
				ob.input = task.Instruction
				ob.focusInput = true
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
			ob.oneBotReplyText(c, task, candidate, text)
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
		ob.oneBotReplyText(c, task, candidate, preview)
		label := fmt.Sprintf(i18n.T("%d 个文件 · %d 处修改"), len(paths), len(candidate.Changes))
		if saved == len(candidate.Changes) {
			label = i18n.T("已保存") + " · " + label
		}
		ui.Row(c.Key("change-summary")).Gap(8).Padding(8, 12, 8, 16).Radius(8).AlignItems(ui.Center).
			Background(t.Text.Alpha(0.032)).Children(func() {
			ui.Icon(c, iconListTree).FontSize(13).TextColor(t.TextMuted)
			if quietOneBotAction(c, "diff", label).Clicked() {
				task.ShowDiff = !task.ShowDiff
			}
			ui.Spacer(c)
			if pending > 0 {
				if oneBotApplyButton(c, "apply", i18n.T("应用修改")).Clicked() {
					if err := ob.applyOneBotCandidate(task, task.Selected, -1); err != nil {
						task.Error = err.Error()
					}
				}
			}
			iconButton(c, "more", iconEllipsis, i18n.T("修改操作"), i18n.T("更多"), false).Size(26, 26).Role(ui.RoleMenuButton).Menu(func(m *ui.Menu) {
				if m.Item(i18n.T("继续调整方案")).Chosen() {
					ob.editTask = task
					ob.editMode = true
					ob.editFollow = true
					ob.focusInput = true
				}
				if m.Item(i18n.T("基于最新原文重试")).Chosen() {
					ob.restartOneBotTask(task)
				}
				if m.Item(i18n.T("拒绝全部")).Chosen() {
					for _, ch := range candidate.Changes {
						if ch.Status == "pending" {
							ch.Status = "rejected"
						}
					}
					ob.keepOneBotTask(task)
				}
				if m.Item(i18n.T("回退本方案")).Chosen() {
					ob.revertOneBotCandidate(task, candidate)
				}
			})
		})
		if task.ShowDiff {
			for i, ch := range candidate.Changes {
				ob.oneBotChangeView(c.Key(i), task, task.Selected, i, ch)
			}
		}
	})
}

func (ob *qaState) oneBotReplyText(c *ui.Context, task *oneBotTask, candidate *oneBotCandidate, text string) {
	if text == "" {
		return
	}
	candidate.preview.answer = text
	candidate.preview.view.parse(text, task.Running)
	session := ob.qaSession(task.SpaceID)
	for i := range candidate.preview.view.blocks {
		ob.qaAnswerBlock(c.Key(i), task.SpaceID, session, &candidate.preview, &candidate.preview.view.blocks[i])
	}
}
