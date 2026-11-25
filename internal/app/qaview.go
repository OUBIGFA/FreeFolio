package app

import (
	"fmt"
	"github.com/OUBIGFA/FreeFolio/internal/solar"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"math"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/spacefs"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// qaWidth is the initial width; the divider lets the user resize it.
const qaWidth = 380

var (
	iconAsk          = solar.Must("dialog-2")
	iconArrowUp      = solar.Must("arrow-up")
	iconArrowDown    = solar.Must("arrow-down")
	iconArrowUpRight = solar.Must("arrow-right-up")
	iconStop         = solar.Must("stop")
	iconList         = solar.Must("list")
	iconListTree     = solar.Must("structure")
	iconNewChat      = solar.Must("pen-new-square")
)

// qaExamples are questions the empty panel offers.
var qaExamples = []string{
	"这个空间里有哪些还没完成的任务",
	"总结一下最近修改的几篇笔记",
	"有没有前后矛盾的记录",
}

// fileQAExamples are the questions offered on the file browser's files.
var fileQAExamples = []string{
	"总结一下这篇文档",
	"这篇文档里有哪些待办事项",
	"有没有前后矛盾的地方",
}

// oneBotSpaceName names what OneBot reads: the open files in the file
// browser, the space otherwise.
func (a *App) oneBotSpaceName(sp *space.Space) string {
	if a.files != nil && sp == a.files.locations {
		return i18n.T("已打开的文件")
	}
	return sp.Name
}

// fileQAHeadHeight is the height of OneBot's header under the file bar.
const fileQAHeadHeight = 40

// oneBotHeadHeight keeps OneBot's header level with the toolbar beside it
// in the spaces; under the file bar it needs no title bar room.
func (a *App) oneBotHeadHeight(c *ui.Context) float32 {
	if a.files != nil {
		return fileQAHeadHeight
	}
	return max(a.titleBar(c).Height, mainBarHeight)
}

// qaPanel is the question panel right of the document: its header level
// with the toolbar, a card with the conversation and the question field.
// It slides open as the sidebar does.
func (a *App) qaPanel(c *ui.Context, docked float32) {
	t := c.Theme()
	sp := a.qaActiveSpace()
	col := ui.Column(c.Key("qa-dock")).Shrink(0).Clip()
	w := a.qa.shown
	col.Width(w).Hidden(w < 0.5).Children(func() {
		if w < 0.5 || sp == nil {
			return
		}
		bar := a.titleBar(c)
		if !a.prefs.SidebarLeft {
			bar.Right = max(0, bar.Right-docked)
		}
		flat := a.files != nil
		fill := pick(t, white.Alpha(0.88), t.Surface)
		qaCol := ui.Column(c.Key("qa")).Width(a.oneBotWidth(docked)).Grow(1).MinHeight(0)
		if flat {
			qaCol.Background(fill).BorderWidth(0, 0, 0, 1).BorderColor(t.Border)
		} else {
			qaCol.Padding(0, 8, 8, 0)
		}
		qaCol.Children(func() {
			s := a.qaSession(sp.ID)
			name := a.oneBotSpaceName(sp)
			head := ui.Row(c.Key("qa-head")).Height(a.oneBotHeadHeight(c)).Gap(6).AlignItems(ui.Center)
			if flat {
				head.Padding(0, 8, 0, 12).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6))
			} else {
				head.Padding(0, bar.Right, 0, 8).DragWindow()
			}
			head.Children(func() {
				ui.Icon(c, iconAsk).FontSize(15).TextColor(t.Accent.Alpha(0.88))
				ui.Text(c, i18n.T("OneBot")).FontSize(13.5).FontWeight(600).NoWrap()
				ui.Text(c, "·").FontSize(12).TextColor(t.TextMuted.Alpha(0.55)).NoWrap()
				ui.Text(c, name).FontSize(12.5).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
				ui.Spacer(c)
				a.oneBotHeaderMenu(c)
				histBtn := iconButton(c, "qa-history", iconHistory, i18n.T("历史记录"), i18n.T("历史记录"), false)
				if a.qa.historyOpen {
					histBtn.Background(t.Accent.Alpha(0.14)).TextColor(t.Accent)
				}
				if histBtn.Clicked() {
					a.toggleOneBotHistory()
				}
				if iconButton(c, "qa-new", iconNewChat, i18n.T("新对话"), i18n.T("新对话（清空界面并重新开始）"), len(s.turns) == 0 && !a.qa.historyOpen).Clicked() {
					a.clearQA()
				}
				if iconButton(c, "qa-close", iconX, i18n.T("关闭 OneBot"), a.withKey(i18n.T("关闭 OneBot"), "ask"), false).Clicked() {
					a.qa.open = false
				}
			})
			card := ui.Column(c.Key("qa-card")).Grow(1).MinHeight(0).Clip()
			if !flat {
				card.Radius(panelRadius).Background(fill).Border(1, t.Border)
			}
			if !a.qa.historyOpen {
				fileOver := card.FileDragOver()
				droppedFiles := card.DroppedFiles()
				_, appOver := ui.DragOver[dragged](card)
				droppedDrag, isAppDrop := ui.Drop[dragged](card)
				_, favOver := ui.DragOver[favoriteDrag](qaCol)
				droppedFav, isFavDrop := ui.Drop[favoriteDrag](qaCol)
				a.qa.dropHover = fileOver || appOver || favOver
				if len(droppedFiles) > 0 {
					a.addOneBotFiles(droppedFiles)
				}
				if isAppDrop {
					a.dragGrab.consumed = true
					rels := a.oneBotDraggedRels(droppedDrag)
					if len(rels) > 1 {
						a.clearPicks("")
					}
					a.addOneBotSpaceRefs(droppedDrag.space, rels)
				}
				if isFavDrop && droppedFav.index >= 0 && droppedFav.index < len(a.state.Favorites) {
					fav := a.state.Favorites[droppedFav.index]
					a.addOneBotSpaceRefs(fav.Space, []string{fav.Path})
				}
			} else {
				a.qa.dropHover = false
			}
			card.Children(func() {
				if a.qa.historyOpen {
					a.oneBotHistoryPanel(c, sp.ID, s)
					return
				}
				a.qaBody(c, sp.ID, a.oneBotSpaceName(sp), s)
			})
		})
	})
}

func (a *App) qaBody(c *ui.Context, spaceID, name string, s *qaSession) {
	t := c.Theme()
	s.beginCitationFrame(c)
	for _, turn := range s.turns {
		turn.prepareStream(c)
	}
	rows := s.visibleRowsWith(func() *spacefs.FS {
		if fs := a.qa.spaces[spaceID]; fs != nil {
			return fs
		}
		return a.qaSpace(spaceID)
	})
	for _, turn := range s.turns {
		turn.view.animate(c.Now(), turn.animateAnswer(c))
	}
	follow := true
	if n := len(s.turns); n > 0 {
		last := s.turns[n-1]
		if !last.running && !last.revealing && last.stepsOpen() {
			follow = false
		}
	}
	s.list.FollowEnd = follow
	wrap := ui.Column(c.Key("qa-list-wrap")).Grow(1).MinHeight(0)
	wrap.Draw(func(_ *ui.Painter, r ui.Rect) {
		s.preview.panelRect = r
	})
	wrap.Children(func() {
		list := ui.List(c.Key("qa-scroll-"+spaceID+"-"+s.id), &s.list, len(rows)).Grow(1).MinHeight(0).Padding(16, 18).
			Label(i18n.T("OneBot")).ItemKey(func(i int) any { return rows[i] })
		list.Rows(func(row ui.ListRow) {
			c := row.Context
			r := rows[row.Index]
			gap := r.gap()
			if row.Index+1 < len(rows) && rows[row.Index+1].turn != r.turn {
				gap = 22
			}
			ui.Column(c.Key("content")).Padding(0, 0, gap, 0).Children(func() {
				switch r.kind {
				case qaEmptyRow:
					a.qaEmpty(c, name)
				case qaQuestionRow:
					a.qaQuestion(c, spaceID, s, r.turn)
				case qaStepsRow:
					a.qaSteps(c, spaceID, r.turn)
				case qaAnswerRow:
					a.qaAnswerBlock(c, spaceID, s, r.turn, &r.turn.view.blocks[r.index])
				case qaSourcesRow:
					a.qaTurnFooter(c, spaceID, s, r.turn)
				case qaStatusRow:
					a.qaStatus(c, spaceID, r.turn)
				case qaEditRow:
					a.oneBotReply(c, r.turn.edit)
				case qaContextBreakRow:
					a.qaContextBreak(c, spaceID, s, r.turn)
				}
			})
		})
		_, lastVis := s.list.Visible()
		if len(s.turns) > 0 && lastVis >= 0 && !s.list.AtEnd() {
			col := previewColors(t, a.look)
			ui.Row(c.Key("qa-jump-pos")).Absolute().Left(0).Right(0).Bottom(10).Justify(ui.Center).PassThrough().
				Transition(ui.ElementTransition{
					Duration: 150 * time.Millisecond,
					Enter:    &ui.Motion{Y: 6, Opacity: 0},
					Exit:     &ui.Motion{Y: 6, Opacity: 0},
				}).Children(func() {
				pill := ui.Row(c.Key("qa-jump-latest")).Height(28).Padding(0, 11).Gap(5).Radius(14).AlignItems(ui.Center).
					Background(col.fill).Border(1, t.Accent.Alpha(0.42)).Shadow(0, 4, 14, 0, col.shadow).
					Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("回到最新"))
				if pill.Hovered() {
					pill.Background(col.fill.Mix(t.Accent, 0.08)).Border(1, t.Accent.Alpha(0.72))
				}
				pill.Children(func() {
					if s.running() {
						pulse := float32(0.55 + 0.45*math.Sin(float64(c.Now().UnixMilli())/260.0))
						ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Background(t.Accent.Alpha(pulse)).Shrink(0)
						if !c.Preferences().ReduceMotion {
							c.After(33 * time.Millisecond)
						}
					}
					ui.Icon(c, iconArrowDown).FontSize(12).TextColor(t.Accent)
					ui.Text(c, i18n.T("回到最新")).FontSize(11.5).FontWeight(600).TextColor(col.title).NoWrap()
				})
				if pill.Clicked() {
					s.list.ScrollToEnd()
				}
			})
		}
	})
	s.finishCitationFrame(c)
	a.qaCitationPreview(c, s)

	a.oneBotComposer(c, s)
}

// qaRoundButton is the round send or stop button beside the field.
func qaRoundButton(c *ui.Context, key string, icon *ui.SVG, label string, disabled bool) ui.Element {
	t := c.Theme()
	b := ui.Box(c.Key(key)).Size(32, 32).Radius(16).Center().Focusable().Role(ui.RoleButton).Label(label).Tooltip(label).Disabled(disabled)
	fill, ink := t.Accent, t.AccentText
	if disabled {
		fill, ink = t.Text.Alpha(0.08), t.Text.Alpha(0.3)
	} else {
		b.Cursor(ui.CursorPointer)
		if b.Hovered() {
			fill = fill.Mix(white, 0.12)
		}
	}
	b.Background(fill).Children(func() { ui.Icon(c, icon).FontSize(16).TextColor(ink) })
	return b
}

func (a *App) qaEmpty(c *ui.Context, name string) {
	t := c.Theme()
	ui.Column(c.Key("qa-empty")).Gap(12).Padding(28, 6, 8, 6).AlignItems(ui.Center).Children(func() {
		ui.Box(c.Key("qa-badge")).Size(44, 44).Radius(12).Background(t.Accent.Alpha(0.10)).
			Border(1, t.Accent.Alpha(0.18)).Center().Children(func() {
			ui.Icon(c, iconAsk).FontSize(22).TextColor(t.Accent)
		})
		ui.Column(c).Gap(5).AlignItems(ui.Center).Children(func() {
			title, hint := fmt.Sprintf(i18n.T("向「%s」提问"), name), i18n.T("FreeFolio 会像翻资料一样搜索、阅读这个空间里的文档来回答，并标出每句话的出处")
			if a.files != nil {
				title, hint = i18n.T("向打开的文件提问"), i18n.T("FreeFolio 会阅读已打开的文件来回答，并标出每句话的出处")
			}
			ui.Text(c, title).FontSize(15).FontWeight(600).TextAlign(ui.Center)
			ui.Text(c, hint).
				FontSize(12).LineHeight(1.5).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(290)
		})
		if err := a.qaReady(); err != nil {
			ui.Column(c.Key("qa-unset-card")).Gap(10).Padding(12, 16).Radius(10).AlignItems(ui.Center).
				Background(t.Text.Alpha(0.035)).Border(1, t.Border.Alpha(0.5)).Children(func() {
				ui.Text(c, err.Error()).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
				if ui.PrimaryButton(c.Key("qa-setup"), i18n.T("设置模型")).Clicked() {
					a.openSettings(tabAI)
				}
			})
			return
		}
		ui.Column(c).Gap(7).Padding(6, 0, 0, 0).AlignItems(ui.Stretch).MaxWidth(320).WidthPercent(100).Children(func() {
			examples := qaExamples
			if a.files != nil {
				examples = fileQAExamples
			}
			for i, ex := range examples {
				ex := i18n.T(ex)
				row := ui.Row(c.Key(fmt.Sprintf("qa-example-%d", i))).Padding(9, 12).Radius(10).Gap(8).AlignItems(ui.Center).
					Background(t.Text.Alpha(0.032)).Border(1, t.Border.Alpha(0.45)).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(ex)
				if row.Hovered() {
					row.Background(t.Accent.Alpha(0.07)).Border(1, t.Accent.Alpha(0.32))
				}
				row.Children(func() {
					ui.Icon(c, iconSearch).FontSize(13).TextColor(t.TextMuted)
					ui.Text(c, ex).FontSize(12.5).LineHeight(1.4).Shrink(1).MinWidth(0)
				})
				if row.Clicked() {
					a.qa.input = ex
					a.askQA()
				}
			}
		})
		ui.Text(c, i18n.T("提问时，读到的文档片段会发送给你设置的模型服务")).FontSize(11).TextColor(t.TextMuted.Alpha(0.8)).
			TextAlign(ui.Center).MaxWidth(290).Padding(4, 0, 0, 0)
	})
}

func qaQuietIconAction(c *ui.Context, key string, icon *ui.SVG, label, tooltip string, active, disabled bool) ui.Element {
	t := c.Theme()
	b := ui.Box(c.Key(key)).Size(24, 24).Radius(6).Center().Focusable().
		Role(ui.RoleButton).Label(label).Tooltip(tooltip).Disabled(disabled)
	ink := t.TextMuted.Alpha(0.72)
	switch {
	case disabled:
		ink = t.Text.Alpha(0.26)
	case active:
		ink = t.Accent
		b.Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(hoverFill(t))
		}
	default:
		b.Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(hoverFill(t))
			ink = t.Text
		}
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(12.5).TextColor(ink) })
	return b
}

func (a *App) qaQuestion(c *ui.Context, spaceID string, s *qaSession, turn *qaTurn) {
	t := c.Theme()
	ui.Column(c.Key("q")).Gap(5).AlignItems(ui.End).Transition(ui.ElementTransition{
		Duration: 180 * time.Millisecond,
		Enter:    &ui.Motion{Y: 5, Opacity: 0},
	}).Children(func() {
		a.oneBotQuoteBadges(c, "q-quotes", turn.quotes, false)
		a.oneBotAttachmentBadges(c, "q-attachments", turn.attachments, nil)
		if a.qa.editingTurn == turn {
			ui.Column(c.Key("q-edit")).WidthPercent(100).MaxWidth(520).Padding(10, 12).Radius(12).
				Background(field(t)).Border(1, t.Accent.Alpha(0.55)).Gap(8).Children(func() {
				edIn := ui.TextInput(c.Key("edit-turn-input"), &a.qa.editingText).Multiline(true).MinHeight(56).
					Border(0, t.Border).Background(ui.Color{}).Padding(0).Label(i18n.T("编辑问题"))
				if a.qa.focusEditTurn {
					a.qa.focusEditTurn = false
					edIn.Focus()
				}
				if edIn.Focused() && c.Shortcut(0, ui.KeyEscape) {
					a.cancelEditQATurn()
					return
				}
				canSave := strings.TrimSpace(a.qa.editingText) != "" || len(turn.attachments) > 0 || len(turn.quotes) > 0
				if (edIn.Submitted() || (edIn.Focused() && c.Shortcut(ui.Cmd, ui.KeyEnter))) && canSave {
					a.submitEditQATurn(spaceID)
					return
				}
				ui.Row(c.Key("edit-actions")).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
					if ui.Button(c.Key("edit-cancel"), i18n.T("取消")).FontSize(12).Clicked() {
						a.cancelEditQATurn()
					}
					if ui.PrimaryButton(c.Key("edit-save"), i18n.T("保存并发送")).FontSize(12).Disabled(!canSave).Clicked() {
						a.submitEditQATurn(spaceID)
					}
				})
			})
			return
		}
		if turn.question != "" {
			ui.Box(c.Key("bubble")).Padding(9, 13).Radius(14).
				Background(t.Accent.Alpha(0.11)).Border(1, t.Accent.Alpha(0.18)).MaxWidth(520).Children(func() {
				ui.Text(c, turn.question).FontSize(13.5).LineHeight(1.5).Selectable()
			})
			if !turn.running && !turn.revealing {
				copyKey := fmt.Sprintf("qcopy-%p", turn)
				copied := a.isQACopied(c, copyKey)
				ui.Row(c.Key("q-actions")).Gap(2).AlignItems(ui.Center).Children(func() {
					copyTip, copyIcon := i18n.T("复制问题"), iconCopy
					if copied {
						copyTip, copyIcon = i18n.T("已复制"), iconCheck
					}
					if qaQuietIconAction(c, "q-copy", copyIcon, i18n.T("复制问题"), copyTip, copied, false).Clicked() {
						a.copyQAText(c, copyKey, turn.question)
					}
					if qaQuietIconAction(c, "q-edit", iconPencil, i18n.T("编辑并重发"), i18n.T("编辑并重发"), false, s.running()).Clicked() {
						a.beginEditQATurn(turn)
					}
				})
			}
		}
	})
}

func (a *App) qaTurnFooter(c *ui.Context, spaceID string, s *qaSession, turn *qaTurn) {
	t := c.Theme()
	ui.Column(c.Key("turn-footer")).Gap(6).Children(func() {
		if len(turn.view.refs) > 0 {
			a.qaSources(c, spaceID, turn)
		}
		isLast := len(s.turns) > 0 && s.turns[len(s.turns)-1] == turn
		canRegen := turn.answer != "" || turn.stopped || turn.err != nil || len(turn.steps) > 0
		ui.Row(c.Key("qa-actions")).Gap(2).Wrap().AlignItems(ui.Center).Children(func() {
			if turn.answer != "" {
				copyKey := fmt.Sprintf("acopy-%p", turn)
				copied := a.isQACopied(c, copyKey)
				copyIcon, copyTip := iconCopy, i18n.T("复制")
				if copied {
					copyIcon, copyTip = iconCheck, i18n.T("已复制")
				}
				if qaQuietIconAction(c, "copy", copyIcon, i18n.T("复制"), copyTip, copied, false).Clicked() {
					a.copyQAText(c, copyKey, qaCleanCopyMarkdown(turn.answer))
				}
			}
			if canRegen {
				if qaQuietIconAction(c, "regenerate", iconReload, i18n.T("重新生成"), i18n.T("重新生成回复"), false, s.running()).Clicked() {
					a.retryQA(spaceID, turn)
				}
			}
			if !s.running() && isLast && (turn.stopped || turn.finishReason == "length") {
				cont := ui.Row(c.Key("continue")).Height(24).Padding(0, 8).Gap(4).Radius(6).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("继续生成"))
				if cont.Hovered() {
					cont.Background(hoverFill(t))
				}
				cont.Children(func() {
					ui.Icon(c, iconForward).FontSize(12).TextColor(t.Text.Alpha(0.82))
					ui.Text(c, i18n.T("继续生成")).FontSize(11.5).FontWeight(500).TextColor(t.Text.Alpha(0.82)).NoWrap()
				})
				if cont.Clicked() {
					a.continueQA(spaceID, turn)
				}
			}
			if turn.answer != "" && a.canInsertQAToDoc() {
				insKey := fmt.Sprintf("ains-%p", turn)
				inserted := a.isQACopied(c, insKey)
				insIcon, insTip := iconFilePlus, i18n.T("插入到当前文档")
				if inserted {
					insIcon, insTip = iconCheck, i18n.T("已插入到文档")
				}
				if qaQuietIconAction(c, "insert-doc", insIcon, i18n.T("插入到当前文档"), insTip, inserted, false).Clicked() {
					a.insertQATurnToDoc(c, insKey, turn)
				}
			}
			more := qaQuietIconAction(c, "more", iconEllipsis, i18n.T("更多操作"), i18n.T("更多操作"), false, false)
			more.Role(ui.RoleMenuButton).Menu(func(m *ui.Menu) {
				if turn.answer != "" {
					if m.Item(i18n.T("复制纯文本")).Chosen() {
						a.copyQAText(c, fmt.Sprintf("acopy-%p", turn), qaPlainCopyText(turn.answer))
					}
				}
				if isLast && (turn.answer != "" || turn.stopped) {
					if m.Item(i18n.T("从此轮继续生成")).Disabled(s.running()).Chosen() {
						a.continueQA(spaceID, turn)
					}
				}
				if m.Item(i18n.T("从此轮新建分支对话")).Disabled(s.running()).Chosen() {
					a.forkOneBotFromTurn(spaceID, turn)
				}
				if isLast {
					if m.Item(i18n.T("清除上下文（从此处重新开始）")).Disabled(s.running() || turn.contextBreakAfter).Chosen() {
						a.clearOneBotContext(spaceID)
					}
				}
				m.Separator()
				if m.Item(i18n.T("删除此轮及后续")).Disabled(s.running()).Chosen() {
					a.deleteQATurnFrom(spaceID, turn)
				}
			})
			ui.Spacer(c)
			if turn.model != "" {
				ui.Text(c, turn.model).FontSize(11).TextColor(t.TextMuted.Alpha(0.68)).SingleLine().Shrink(1).MinWidth(0).Tooltip(turn.model)
			}
		})
	})
}

func (a *App) qaContextBreak(c *ui.Context, spaceID string, s *qaSession, turn *qaTurn) {
	t := c.Theme()
	isLast := len(s.turns) > 0 && s.turns[len(s.turns)-1] == turn
	ui.Row(c.Key("context-break")).Gap(8).Padding(4, 0).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Grow(1).Height(1).Background(separator(t).Alpha(0.7))
		ui.Row(c.Key("pill")).Gap(6).Padding(2, 9).Radius(999).AlignItems(ui.Center).
			Background(t.Text.Alpha(0.045)).Border(1, t.Border.Alpha(0.35)).Children(func() {
			ui.Icon(c, iconEraser).FontSize(11).TextColor(t.TextMuted)
			ui.Text(c, i18n.T("以上上下文已清除 · 重新开始")).FontSize(11).TextColor(t.TextMuted).NoWrap()
			if isLast && !s.running() {
				undo := ui.Row(c.Key("undo")).Padding(1, 5).Radius(4).Cursor(ui.CursorPointer).
					Focusable().Role(ui.RoleButton).Label(i18n.T("撤销"))
				if undo.Hovered() {
					undo.Background(t.Accent.Alpha(0.12))
				}
				undo.Children(func() {
					ui.Text(c, i18n.T("撤销")).FontSize(11).FontWeight(600).TextColor(t.Accent).NoWrap()
				})
				if undo.Clicked() {
					a.undoOneBotContextBreak(spaceID, turn)
				}
			}
		})
		ui.Box(c).Grow(1).Height(1).Background(separator(t).Alpha(0.7))
	})
}

func (a *App) qaStatus(c *ui.Context, spaceID string, turn *qaTurn) {
	t := c.Theme()
	if (turn.running || turn.revealing) && turn.displayAnswer() == "" && len(turn.steps) == 0 {
		label := i18n.T("正在查找")
		switch {
		case turn.retrying:
			label = i18n.T("正在重试")
		case turn.thinking:
			label = i18n.T("思考中")
		}
		ui.Row(c.Key("working")).Gap(9).Padding(8, 11).Radius(11).AlignItems(ui.Center).
			Background(t.Text.Alpha(0.032)).Border(1, t.Border.Alpha(0.32)).
			Transition(ui.ElementTransition{
				Duration: 160 * time.Millisecond,
				Enter:    &ui.Motion{Y: 4, Opacity: 0},
				Exit:     &ui.Motion{Collapse: true, Opacity: 0},
			}).Children(func() {
			qaLoader(c, &turn.loader, turn.loaderStyle(c.Now()))
			ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(t.Text.Alpha(0.82)).Role(ui.RoleStatus)
			ui.Spacer(c)
			qaPulseDots(c, t.Accent)
		})
	}
	if turn.stopped {
		ui.Row(c.Key("stopped")).Gap(6).AlignItems(ui.Center).Padding(2, 0).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(t.TextMuted.Alpha(0.55))
			ui.Text(c, i18n.T("已停止")).FontSize(12).TextColor(t.TextMuted)
		})
	}
	if turn.err != nil {
		ui.Column(c.Key("error")).Gap(8).Padding(10, 12).Radius(10).
			Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.22)).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
				ui.Icon(c, iconAlert).FontSize(14).TextColor(t.Danger).Padding(2, 0, 0, 0)
				ui.Text(c, qaErrorText(turn.err)).FontSize(13).LineHeight(1.45).TextColor(t.Danger).Selectable().Shrink(1).MinWidth(0)
			})
			ui.Row(c).Gap(8).Children(func() {
				if chatGPTUsageLimit(turn.err) && ui.Button(c.Key("usage"), i18n.T("管理用量")).Clicked() {
					a.openChatGPTUsage()
				}
				if ui.Button(c.Key("retry"), i18n.T("重试")).Clicked() {
					a.retryQA(spaceID, turn)
				}
				if a.qaReady() != nil && ui.Button(c.Key("setup"), i18n.T("设置模型")).Clicked() {
					a.openSettings(tabAI)
				}
			})
		})
	}
}

// qaPulseDots renders three softly breathing dots during live agent work,
// falling back to a static ellipsis when ReduceMotion is enabled.
func qaPulseDots(c *ui.Context, color ui.Color) {
	if c.Preferences().ReduceMotion {
		return
	}
	c.After(45 * time.Millisecond)
	sec := float64(c.Now().UnixNano()) / 1e9
	ui.Row(c.Key("dots")).Gap(3).AlignItems(ui.Center).Children(func() {
		for i := range 3 {
			phase := sec*4.6 - float64(i)*0.65
			wave := float32(0.5 + 0.5*math.Sin(phase))
			alpha := 0.25 + 0.65*wave
			lift := -1.4 * wave
			ui.Box(c.Key(i)).Size(3.5, 3.5).Radius(2).Background(color.Alpha(alpha)).
				Translate(0, lift)
		}
	})
}

// liveStepHeadline describes what the agent is actively doing in the header.
func (t *qaTurn) liveStepHeadline() string {
	if t.retrying {
		return i18n.T("正在重试")
	}
	for i := len(t.steps) - 1; i >= 0; i-- {
		st := t.steps[i]
		if !st.done {
			m := stepMeta(st.call)
			if m.subject != "" {
				return m.active + " " + m.subject
			}
			return m.active
		}
	}
	if t.thinking {
		return i18n.T("思考中")
	}
	return i18n.T("正在查找")
}

// consultedTitles returns up to 3 distinct document titles inspected in steps.
func (t *qaTurn) consultedTitles() []string {
	var out []string
	seen := map[string]bool{}
	for _, st := range t.steps {
		m := stepMeta(st.call)
		if m.docPath == "" || m.subject == "" {
			continue
		}
		key := strings.ToLower(m.docPath)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m.subject)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// qaSteps renders the unified Agent Exploration Card: live Ghost + active
// headline while exploring, compact docIcon-badged summary once answering,
// and a smoothly collapsible vertical timeline of interactive steps.
func (a *App) qaSteps(c *ui.Context, spaceID string, turn *qaTurn) {
	t := c.Theme()
	waiting := (turn.running || turn.revealing) && turn.displayAnswer() == ""
	open := turn.stepsOpen()

	reads := 0
	for _, st := range turn.steps {
		if st.call.Name == agent.ToolRead {
			reads++
		}
	}

	borderCol := t.Border.Alpha(0.34)
	if waiting {
		borderCol = t.Accent.Alpha(0.24)
	}

	card := ui.Column(c.Key("steps")).Radius(11).
		Background(t.Text.Alpha(0.032)).Border(1, borderCol).Clip().
		Transition(ui.ElementTransition{
			Duration: 180 * time.Millisecond,
			Colors:   true,
			Size:     true,
			Enter:    &ui.Motion{Y: 4, Opacity: 0},
		})

	card.Children(func() {
		if waiting {
			ui.Column(c.Key("live-banner")).
				Transition(ui.ElementTransition{
					Duration: 160 * time.Millisecond,
					Enter:    &ui.Motion{Collapse: true, Opacity: 0},
					Exit:     &ui.Motion{Collapse: true, Opacity: 0},
				}).Children(func() {
				ui.Row(c.Key("live-row")).Gap(8).Padding(7, 10).AlignItems(ui.Center).
					Background(t.Accent.Alpha(0.05)).Children(func() {
					qaLoader(c, &turn.loader, turn.loaderStyle(c.Now()))
					ui.Text(c, turn.liveStepHeadline()).FontSize(12.5).FontWeight(550).
						TextColor(t.Text.Alpha(0.88)).SingleLine().Shrink(1).MinWidth(0).Role(ui.RoleStatus)
					ui.Spacer(c)
					qaPulseDots(c, t.Accent)
				})
				ui.Box(c).Height(1).Background(separator(t))
			})
		}

		head := ui.Row(c.Key("steps-head")).Gap(8).Padding(7, 10).MinHeight(32).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("查找过程"))
		if head.Hovered() {
			head.Background(hoverFill(t).Alpha(0.65))
		}
		head.Children(func() {
			titles := turn.consultedTitles()
			if len(titles) > 0 {
				ui.Row(c.Key("doc-stack")).Gap(3).AlignItems(ui.Center).Shrink(0).Children(func() {
					for idx, title := range titles {
						ui.Box(c.Key(idx)).Children(func() { docIcon(c, title, 15) })
					}
				})
			} else {
				ui.Box(c.Key("search-badge")).Size(17, 17).Radius(5).Background(t.Accent.Alpha(0.12)).Center().Shrink(0).Children(func() {
					ui.Icon(c, iconSparkles).FontSize(10.5).TextColor(t.Accent)
				})
			}
			ui.Text(c, fmt.Sprintf(i18n.T("查找了 %d 步 · 阅读 %d 处"), len(turn.steps), reads)).
				FontSize(12).FontWeight(500).TextColor(t.Text.Alpha(0.82)).SingleLine().Shrink(1).MinWidth(0)
			ui.Spacer(c)
			rot := float32(0)
			if open {
				rot = 90
			}
			ui.Box(c.Key("chev")).Size(16, 16).Center().Shrink(0).
				AnimatePose(ui.Pose{Rotate: rot}, ui.Snappy).Children(func() {
				ui.Icon(c, iconChevronRight).FontSize(12).TextColor(t.TextMuted)
			})
		})
		if head.Clicked() {
			turn.expanded = !open
			turn.userToggled = true
		}

		if !open {
			return
		}

		ui.Column(c.Key("steps-body")).
			Transition(ui.ElementTransition{
				Duration: 180 * time.Millisecond,
				Enter:    &ui.Motion{Collapse: true, Y: -4},
				Exit:     &ui.Motion{Collapse: true, Y: -4},
			}).Children(func() {
			ui.Box(c).Height(1).Background(separator(t))
			ui.Column(c.Key("timeline")).Padding(6, 8, 7, 10).Children(func() {
				for i, st := range turn.steps {
					a.qaStepItem(c, spaceID, st, i, len(turn.steps))
				}
			})
		})
	})
}

func (a *App) qaStepItem(c *ui.Context, spaceID string, st *qaStep, index, total int) {
	t := c.Theme()
	meta := stepMeta(st.call)
	hits := st.ensureHits()
	canJump := meta.docPath != "" && !st.failed
	canExpand := meta.docPath == "" && len(hits) > 0
	interactive := canJump || canExpand

	ui.Row(c.Key(fmt.Sprintf("step-%d", index))).Gap(8).AlignItems(ui.Stretch).
		Transition(ui.ElementTransition{
			Duration: 160 * time.Millisecond,
			Enter:    &ui.Motion{Collapse: true, Y: -3, Opacity: 0},
		}).Children(func() {
		// Left timeline rail + circular step node.
		ui.Column(c.Key("rail")).Width(18).AlignItems(ui.Center).Shrink(0).Children(func() {
			topAlpha := float32(0.55)
			if index == 0 {
				topAlpha = 0
			}
			ui.Box(c).Width(1.5).Height(5).Background(t.Border.Alpha(topAlpha))
			qaStepNode(c, st, meta)
			botAlpha := float32(0.55)
			if index == total-1 {
				botAlpha = 0
			}
			ui.Box(c).Width(1.5).Grow(1).MinHeight(5).Background(t.Border.Alpha(botAlpha))
		})

		// Right step body + optional inline hit preview drawer.
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).Padding(1, 0, 2, 0).Children(func() {
			row := ui.Row(c.Key("bar")).Gap(5).Padding(4, 6).Radius(7).MinHeight(24).AlignItems(ui.Center)
			if interactive {
				row.Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
					Label(strings.TrimSpace(meta.verb + " " + meta.subject))
				if row.Hovered() {
					row.Background(hoverFill(t))
				}
			}
			hovered := row.Hovered()
			row.Children(func() {
				verbInk := t.TextMuted
				subjInk := t.Text.Alpha(0.86)
				if st.failed {
					verbInk, subjInk = t.Danger, t.Danger
				}
				ui.Text(c, meta.verb).FontSize(11.5).TextColor(verbInk).NoWrap().Shrink(0)
				if meta.subject != "" {
					ui.Text(c, docName(meta.subject)).FontSize(12).FontWeight(550).TextColor(subjInk).
						SingleLine().Shrink(1).MinWidth(0).Tooltip(meta.subject)
				}
				if meta.scope != "" {
					ui.Box(c.Key("scope")).Padding(1, 5).Radius(4).Background(t.Text.Alpha(0.055)).
						Shrink(2).MinWidth(0).MaxWidth(96).Clip().Children(func() {
						ui.Text(c, meta.scope).FontSize(10.5).TextColor(t.TextMuted).SingleLine().Tooltip(meta.scope)
					})
				}
				ui.Spacer(c)
				if r := stepResult(st); r != "" {
					resInk := t.TextMuted
					resBg := ui.Color{}
					if st.failed {
						resInk = t.Danger
						resBg = t.Danger.Alpha(0.10)
					} else if !strings.HasPrefix(st.result, "No ") && (st.call.Name == agent.ToolSearch || st.call.Name == agent.ToolGrep) {
						resInk = t.Accent
						resBg = t.Accent.Alpha(0.10)
					}
					ui.Box(c.Key("res")).Padding(1, 6).Radius(5).Background(resBg).Shrink(0).Children(func() {
						ui.Text(c, r).FontSize(10.5).FontWeight(500).TextColor(resInk).NoWrap()
					})
				}
				if canExpand {
					rot := float32(0)
					if st.open {
						rot = 90
					}
					ui.Box(c.Key("hit-chev")).Size(13, 13).Center().Shrink(0).
						AnimatePose(ui.Pose{Rotate: rot}, ui.Snappy).Children(func() {
						ui.Icon(c, iconChevronRight).FontSize(11).TextColor(t.TextMuted)
					})
				} else if canJump && hovered {
					ui.Icon(c, iconArrowUpRight).FontSize(11).TextColor(t.Accent).Shrink(0)
				}
			})

			if row.Clicked() {
				switch {
				case canJump:
					a.openCitation(spaceID, agent.Citation{Path: meta.docPath, From: meta.from, To: meta.to})
				case canExpand:
					st.open = !st.open
				}
			}

			if canExpand && st.open {
				ui.Column(c.Key("hits")).Gap(2).Padding(3, 2, 4, 4).
					Transition(ui.ElementTransition{
						Duration: 150 * time.Millisecond,
						Enter:    &ui.Motion{Collapse: true, Y: -3, Opacity: 0},
						Exit:     &ui.Motion{Collapse: true, Y: -3, Opacity: 0},
					}).Children(func() {
					for hi, h := range hits {
						hRow := ui.Column(c.Key(hi)).Gap(2).Padding(4, 7).Radius(6).
							Background(t.Text.Alpha(0.028)).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleLink).Label(h.head)
						if hRow.Hovered() {
							hRow.Background(hoverFill(t))
						}
						hRow.Children(func() {
							iconTitle := h.head
							if h.path != "" {
								iconTitle = vault.Title(h.path)
							}
							ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
								docIcon(c, iconTitle, 13)
								ui.Text(c, docName(h.head)).FontSize(11.5).FontWeight(500).TextColor(t.Text.Alpha(0.84)).
									SingleLine().Shrink(1).MinWidth(0)
								ui.Spacer(c)
								if h.loc != "" {
									ui.Text(c, h.loc).FontSize(10.5).TextColor(t.TextMuted).NoWrap().Shrink(0)
								}
							})
							if h.text != "" {
								ui.Text(c, h.text).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0).Padding(0, 0, 0, 18)
							}
						})
						if hRow.Clicked() && h.path != "" {
							a.openCitation(spaceID, agent.Citation{Path: h.path, From: max(h.line, 1), To: max(h.line, 1)})
						}
					}
				})
			}
		})
	})
}

func qaStepNode(c *ui.Context, st *qaStep, meta qaStepMeta) {
	t := c.Theme()
	switch {
	case !st.done:
		ui.Box(c.Key("spin")).Size(18, 18).Radius(9).Background(t.Accent.Alpha(0.13)).Center().Shrink(0).Children(func() {
			ui.Spinner(c).Size(11, 11)
		})
	case st.failed:
		ui.Box(c.Key("err")).Size(18, 18).Radius(9).Background(t.Danger.Alpha(0.13)).Center().Shrink(0).Children(func() {
			ui.Icon(c, iconAlert).FontSize(11).TextColor(t.Danger)
		})
	case meta.docPath != "" && st.call.Name == agent.ToolRead:
		ui.Box(c.Key("doc")).Size(18, 18).Center().Shrink(0).Children(func() {
			docIcon(c, meta.subject, 16)
		})
	case st.call.Name == agent.ToolSearch:
		ui.Box(c.Key("spark")).Size(18, 18).Radius(9).Background(t.Accent.Alpha(0.12)).Center().Shrink(0).Children(func() {
			ui.Icon(c, iconSparkles).FontSize(11).TextColor(t.Accent)
		})
	default:
		icon := iconSearch
		switch st.call.Name {
		case agent.ToolGlob:
			icon = iconFolder
		case agent.ToolOutline:
			icon = iconListTree
		case agent.ToolLinks:
			icon = iconLink
		}
		ui.Box(c.Key("ic")).Size(18, 18).Radius(9).Background(t.Text.Alpha(0.06)).Center().Shrink(0).Children(func() {
			ui.Icon(c, icon).FontSize(10.5).TextColor(t.TextMuted)
		})
	}
}
