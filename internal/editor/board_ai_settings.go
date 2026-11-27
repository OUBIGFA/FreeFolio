package editor

import (
	"fmt"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func aiSettingsTextHeight(c *ui.Context, preferred float32) float32 {
	_, height := c.Size()
	if height < 600 {
		return min(preferred, 48)
	}
	if height < 800 {
		return min(preferred, 72)
	}
	return preferred
}

// Keep every setting in the same popover. Content, model parameters and native
// sampling have separate columns instead of a height-limited scrolling form.
func (b *Board) aiSettingsPopup(c *ui.Context, o BoardOptions, e *whiteboard.Element, n *whiteboard.AINode) {
	s := b.aiState()
	id := e.ID
	st := boardChromeFor(o.Theme)
	disabled := b.ReadOnly || e.Locked
	effective := b.Scene.AIInputNode(n)
	simple := n.Role == whiteboard.AIPrompt || n.Role == whiteboard.AIImageInput || n.Role == whiteboard.AINote || n.Role == whiteboard.AIModel || n.Role == whiteboard.AILoRA
	details := strings.HasPrefix(effective.Connection, "comfyui") && !simple && (effective.Connection != "comfyui" || n.Role == whiteboard.AICutout)
	columns := 2
	if simple {
		columns = 1
	} else if details {
		columns = 3
	}
	inputs := b.Scene.AIInputs(id)
	if len(inputs) > 3 {
		columns++
	}
	resultColumn := n.Output() != nil && !simple
	if resultColumn {
		columns++
	}
	winW, winH := c.Size()
	width := min(float32(columns)*300+28, max(240, winW-24))
	gap := float32(10)
	padding := float32(12)
	if winH < 600 {
		gap = 2
		padding = 8
	}
	ui.PopoverBase(c.Key("board-ai-properties-popup"), s.settingsAnchor, &s.panel, func(popup ui.Element) {
		boardSurface(popup, st).Width(width)
		popup.AttachTo(s.settingsAnchor, ui.AnchorStart, ui.AnchorStart)
		ui.Column(c.Key("board-ai-props")).WidthPercent(100).Padding(padding).Gap(gap).Role(ui.RoleGroup).Label(i18n.T("节点属性")).Children(func() {
			boardPanelHeader(c, st, i18n.T(aiRoleNames[n.Role]), aiRoleIcons[n.Role], n.Kind(), o.Theme.Dark, func() { s.panel = false })
			ui.Row(c.Key("settings-identity")).Gap(10).AlignItems(ui.Start).Children(func() {
				ui.Column(c.Key("identity-name")).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, i18n.T("名称")).FontSize(12).TextColor(st.muted)
					if ui.TextInput(c.Key("board-ai-title"), &s.buf.title).WidthPercent(100).Height(28).Disabled(disabled).Label(i18n.T("名称")).Changed() {
						b.editAINode(o, id, func(n *whiteboard.AINode) { n.Title = s.buf.title })
					}
				})
				if n.Role != whiteboard.AIPrompt && n.Role != whiteboard.AIImageInput && n.Role != whiteboard.AINote && n.Role != whiteboard.AIConcat && n.Role != whiteboard.AICompare {
					ui.Column(c.Key("identity-model")).Grow(2).MinWidth(0).Gap(3).Children(func() { b.aiModelField(c, o, id, n) })
				}
			})
			ui.Row(c.Key("settings-columns")).Gap(14).AlignItems(ui.Start).Children(func() {
				ui.Column(c.Key("settings-content")).Grow(1).MinWidth(0).Gap(gap).Children(func() {
					b.aiSettingsContent(c, o, e, n)
					b.aiLyricsField(c, o, id, n, disabled)
					if len(inputs) <= 3 {
						b.aiSettingsInputs(c, o, id, n, inputs, disabled)
					}
					if simple {
						b.aiMainParamsFields(c, o, id, n)
					}
				})
				if !simple {
					ui.Column(c.Key("settings-parameters")).Grow(1).MinWidth(0).Gap(gap).Children(func() { b.aiMainParamsFields(c, o, id, n) })
					if details {
						ui.Column(c.Key("settings-details")).Grow(1).MinWidth(0).Gap(gap).Children(func() { b.aiComfySamplingParams(c, o, id, n, disabled) })
					}
				}
				if len(inputs) > 3 {
					ui.Column(c.Key("settings-inputs")).Grow(1).MinWidth(0).Gap(gap).Children(func() { b.aiSettingsInputs(c, o, id, n, inputs, disabled) })
				}
				if resultColumn {
					ui.Column(c.Key("settings-result")).Grow(1).MinWidth(0).Gap(gap).Children(func() { b.aiOutputActions(c, o, id, n) })
				}
			})
			if !resultColumn {
				b.aiOutputActions(c, o, id, n)
			}
			if aiRunnableRole(n.Role) {
				b.aiRunControls(c, o, id)
			}
		})
	})
}

func (b *Board) aiSettingsInputs(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, inputs []whiteboard.AIInput, disabled bool) {
	if len(inputs) == 0 {
		return
	}
	st := boardChromeFor(o.Theme)
	aiPropSection(c, o, fmt.Sprintf(i18n.T("输入（%d）"), len(inputs)))
	_, height := c.Size()
	columns := 1
	if height < 600 && len(inputs) > 8 {
		columns = 2
	}
	ui.Grid(c.Key("input-rows")).Columns(columns).Gap(2).WidthPercent(100).Children(func() {
		for i, in := range inputs {
			sl, _ := whiteboard.AISlotByName(b.Scene.AIInputNode(n), in.Slot)
			ui.Row(c.Key(fmt.Sprint("in", i))).Height(24).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, aiSlotLabel(sl)).FontSize(12).TextColor(st.muted).SingleLine()
				ui.Text(c, b.aiSourceName(b.Scene.ElementByID(in.From))).FontSize(12).TextColor(st.text).SingleLine().Grow(1).MinWidth(0)
				if boardIconButton(c, "disconnect", "close", i18n.T("断开连接"), false).Size(22, 22).Disabled(disabled).Clicked() {
					b.deleteAILink(aiLinkRef{id, in.Slot}, o)
				}
			})
		}
	})
}
