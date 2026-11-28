package editor

import (
	"encoding/base64"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func boardDataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

type boardToolDef struct {
	tool              BoardTool
	glyph, label, key string
}

var boardTools = []boardToolDef{{ToolHand, "✋", "抓手", "H"}, {ToolSelect, "↖", "选择", "V"}, {ToolRect, "□", "矩形", "R"}, {ToolDiamond, "◇", "菱形", "D"}, {ToolEllipse, "○", "椭圆", "O"}, {ToolArrow, "↗", "箭头", "A"}, {ToolLine, "╱", "直线", "L"}, {ToolFreedraw, "✎", "画笔", "P"}, {ToolText, "T", "文字", "T"}, {ToolNote, "▤", "便签", "N"}, {ToolEraser, "⌫", "橡皮", "E"}, {ToolImage, "▧", "图片", "9"}, {ToolFrame, "▣", "画框", "F"}, {ToolLasso, "◌", "套索", ""}}

func BoardView(c *ui.Context, b *Board, o BoardOptions) ui.Element {
	if b == nil {
		return ui.Box(c)
	}
	if o.Theme == nil {
		o.Theme = c.Theme()
	}
	b.ReadOnly = o.ReadOnly
	b.options = o
	// Visible nodes share one list per role within this view. Discard the
	// cache next view so connection and download changes appear immediately.
	if o.AI != nil && o.AI.Models != nil {
		ai := *o.AI
		models := ai.Models
		lists := make(map[whiteboard.AIRole][]BoardAIModel)
		ai.Models = func(role whiteboard.AIRole) []BoardAIModel {
			if list, ok := lists[role]; ok {
				return list
			}
			list := models(role)
			lists[role] = list
			return list
		}
		o.AI = &ai
	}
	b.services = c.Services()
	b.ShowGrid = b.Scene.AppState.GridModeEnabled
	root := ui.Box(c.Key("whiteboard-root")).Grow(1).WidthPercent(100).MinWidth(0).MinHeight(0).Clip()
	root.Children(func() {
		viewport := ui.Box(c.Key("board-viewport")).Fill()
		viewport.HandleInput(func(ev ui.InputEvent) bool {
			switch ev.Kind {
			case ui.InputZoom:
				return b.input(ev, o)
			case ui.InputScroll:
				if o.ImageAnnotation || ev.Mods&(ui.Cmd|ui.Ctrl) != 0 || !b.ai.hoverTextArea {
					return b.input(ev, o)
				}
			}
			return false
		})
		viewport.Children(func() {
			canvas := ui.Box(c.Key("board-canvas")).Fill().Focusable().FocusRing(false).Role(ui.RoleGroup).Label(i18n.T("手绘白板"))
			if b.RequestFocus {
				canvas.Focus()
				b.RequestFocus = false
			}
			rect := canvas.LayoutBounds()
			if rect.W > 0 && rect.H > 0 {
				b.lastRect = rect
				if !b.fitted {
					if len(b.Scene.Elements) > 0 {
						b.fit(false)
					}
					b.fitted = true
				}
				b.prepareImages(o)
				b.aiPrepareMedia(o)
			} else {
				c.After(0)
			}
			canvas.HandleInput(func(ev ui.InputEvent) bool { return b.input(ev, o) })
			if o.DropFiles != nil && !b.ReadOnly {
				if paths := canvas.DroppedFiles(); len(paths) > 0 {
					o.DropFiles(paths)
				}
			}
			canvas.ContextMenu(func(m *ui.Menu) {
				if o.ImageAnnotation {
					b.annotationMenu(m, o)
				} else {
					b.contextMenu(m, o)
				}
			})
			// Scroll and pinch events have no release event. Keep their feedback
			// briefly between events, then restore the tool cursor without a move.
			now := c.Now()
			if b.gestureChanged {
				b.gestureUntil = now.Add(180 * time.Millisecond)
				b.gestureChanged = false
			}
			gesture := b.gestureCursor != ui.CursorDefault && now.Before(b.gestureUntil)
			if gesture {
				c.After(b.gestureUntil.Sub(now))
			}
			// Reproject the current pointer after pan/zoom as well as pointer moves.
			// PointerPosition also requests frames for hover-only target changes.
			if x, y, over := canvas.PointerPosition(); over {
				b.hover = b.scenePoint(x, y)
			}
			canvas.Cursor(b.canvasCursor(c.Modifiers(), gesture))
			canvas.Draw(func(p *ui.Painter, r ui.Rect) {
				b.lastRect = r
				// The first view has no LayoutBounds yet. Fit the inspector using
				// the actual paint bounds before any full-size pixels are shown.
				if o.ImageAnnotation && !b.fitted && r.W > 0 && r.H > 0 {
					b.fit(false)
				}
				b.paint(p, r, o.Theme.Dark && !o.ImageAnnotation, true)
			})
			b.aiNodeInlineControls(c, o)
			b.aiVideoOverlay(c, o)
			b.aiLinkControls(c, o)
			b.groupControls(c)
		})
		b.textOverlay(c, o)
		if !b.ReadOnly {
			if o.ImageAnnotation {
				b.annotationToolbar(c, o)
			} else {
				b.toolbar(c, o)
			}
			if e, _ := b.selectedAINode(); e == nil && !(b.ai.bar && o.AI != nil) {
				if o.ImageAnnotation {
					b.annotationProperties(c, o)
				} else {
					b.properties(c, o)
				}
			}
			b.aiBar(c, o)
			b.aiSelectionBar(c, o)
			b.aiPickerMenu(c, o)
		}
		b.aiNodeOverlay(c, o)
		b.aiProperties(c, o)
		b.aiErrorDialog(c, o)
		b.footer(c, o)
		b.panelView(c, o)
		if b.cropper != nil {
			b.cropper.page = pageOf(o.Page, o.Theme)
			b.cropper.cropView(c)
			if !b.cropper.cropper.open {
				b.cropper = nil
			}
		}
		if b.err != nil {
			st := boardChromeFor(o.Theme)
			ui.Row(c.Key("board-error")).Absolute().Top(64).Right(14).MaxWidth(400).Padding(10, 12).Gap(8).AlignItems(ui.Center).Background(st.surface).Border(1, st.danger).Radius(10).Shadow(0, 4, 14, 0, st.shadow).Children(func() {
				ui.Text(c, b.err.Error()).FontSize(13).TextColor(st.danger).Grow(1)
				if boardIconButton(c, "board-error-close", "close", i18n.T("关闭"), false).Size(24, 24).Clicked() {
					b.err = nil
				}
			})
		}
	})
	return root
}
func boardButton(c *ui.Context, id, label string, active bool) ui.Element {
	return boardFlatButton(c, id, label, active, boardChromeFor(c.Theme()))
}
func (b *Board) toolbar(c *ui.Context, o BoardOptions) {
	st := boardChromeFor(o.Theme)
	ui.Row(c.Key("board-toolbar-wrap")).Absolute().Top(10).Left(8).Right(8).Justify(ui.Center).PassThrough().Children(func() {
		boardSurface(ui.Row(c.Key("board-toolbar")).Padding(4).Gap(2).AlignItems(ui.Center), st).Role(ui.RoleToolbar).Label(i18n.T("白板工具")).Children(func() {
			lock := "unlocked"
			if b.ToolLocked {
				lock = "locked"
			}
			if boardIconButton(c, "board-tool-lock", lock, i18n.T("保持当前工具")+" Q", b.ToolLocked).Clicked() {
				b.ToolLocked = !b.ToolLocked
				b.RequestFocus = true
			}
			capacity := b.toolCapacity(o)
			last := capacity - 1
			for index := capacity; index < len(boardTools); index++ {
				if boardTools[index].tool == b.Tool {
					last = index
					break
				}
			}
			visible := func(index int) bool { return index < capacity-1 || index == last }
			for index, d := range boardTools {
				if !visible(index) {
					continue
				}
				switch d.tool {
				case ToolRect, ToolArrow, ToolText, ToolImage:
					boardToolbarDivider(c, fmt.Sprintf("board-tool-sep-%d", d.tool), st)
				}
				if boardIconButton(c, fmt.Sprintf("board-tool-%d", d.tool), boardToolIcons[d.tool], i18n.T(d.label)+" "+d.key, b.Tool == d.tool).Clicked() {
					b.finishText(o)
					b.Tool = d.tool
					b.penSelection()
					b.RequestFocus = true
					if d.tool == ToolImage && o.PickImage != nil {
						o.PickImage()
					}
				}
			}
			b.aiToolbar(c, o)
			boardToolbarDivider(c, "board-more-sep", st)
			menu := boardIconButton(c, "board-more", "dots", i18n.T("更多工具"), false)
			menu.Menu(func(m *ui.Menu) {
				if m.Item(i18n.T("dim0 手绘图形")).Chosen() {
					b.libraryQuery = "dim0"
					b.panel = "library"
				}
				if m.Item(i18n.T("大纲转思维导图")).Chosen() {
					b.panel = "outline"
				}
				if m.Item(i18n.T("搜索白板内容")).Chosen() {
					b.RunAction("search", o)
				}
				b.layoutMenu(m, o)
				for index, d := range boardTools {
					if !visible(index) && m.Item(i18n.T(d.label)).Chosen() {
						b.finishText(o)
						b.Tool = d.tool
						b.penSelection()
						b.RequestFocus = true
						if d.tool == ToolImage && o.PickImage != nil {
							o.PickImage()
						}
					}
				}
				m.Separator()
				for _, a := range []struct{ id, label string }{{"library", "组件素材库"}, {"mermaid", "Mermaid 转白板"}, {"background", "画布背景"}, {"help", "白板快捷键"}} {
					if m.Item(i18n.T(a.label)).Chosen() {
						b.panel = a.id
					}
				}
				// Host-owned document actions live here instead of a separate
				// header strip, so standalone boards keep the whole pane as canvas.
				if o.Export != nil || o.History != nil {
					m.Separator()
				}
				if o.Export != nil {
					if m.Item(i18n.T("导出 JSON Canvas")).Chosen() {
						o.Export("canvas")
					}
					if m.Item(i18n.T("导出 SVG")).Chosen() {
						o.Export("svg")
					}
					if m.Item(i18n.T("导出 PNG")).Chosen() {
						o.Export("png")
					}
				}
				if o.History != nil && m.Item(i18n.T("版本历史")).Chosen() {
					o.History()
				}
				m.Separator()
				if m.Item(i18n.T("清空白板")).Chosen() {
					b.panel = "clear"
				}
			})
		})
	})
}

// toolCapacity is how many tools fit in the toolbar beside its fixed
// buttons; the rest are in the More menu.
func (b *Board) toolCapacity(o BoardOptions) int {
	available := b.lastRect.W - 104
	if o.AI != nil {
		available -= 104
	}
	for index, def := range boardTools {
		available -= 34
		switch def.tool {
		case ToolRect, ToolArrow, ToolText, ToolImage:
			available -= 9
		}
		if available < 0 {
			return max(1, index)
		}
	}
	return len(boardTools)
}
func (b *Board) footer(c *ui.Context, o BoardOptions) {
	st := boardChromeFor(o.Theme)
	boardSurface(ui.Row(c.Key("board-footer")).Absolute().Bottom(12).Left(12).Gap(2).Padding(4, 5).AlignItems(ui.Center), st).Children(func() {
		run := func(id string) {
			b.RunAction(id, o)
			b.RequestFocus = true
		}
		if boardIconButton(c, "board-zoom-out", "zoom-out", i18n.T("缩小"), false).Size(28, 28).Clicked() {
			run("zoom-out")
		}
		if boardFlatButton(c, "board-reset-zoom", fmt.Sprintf("%.0f%%", b.Zoom*100), false, st).Height(28).MinWidth(48).PaddingX(6).Tooltip(i18n.T("重置缩放")).Clicked() {
			run("reset-zoom")
		}
		if boardIconButton(c, "board-zoom-in", "zoom-in", i18n.T("放大"), false).Size(28, 28).Clicked() {
			run("zoom-in")
		}
		if boardFlatButton(c, "board-fit", i18n.T("适应画布"), false, st).Height(28).Clicked() {
			run("fit")
		}
		if b.ReadOnly {
			return
		}
		// Undo and redo are a separate group: they change content, zoom does not.
		boardToolbarDivider(c, "board-footer-sep", st)
		if boardIconButton(c, "board-undo", "undo", i18n.T("撤销"), false).Size(28, 28).Disabled(!b.History.CanUndo()).Clicked() {
			run("undo")
		}
		if boardIconButton(c, "board-redo", "redo", i18n.T("重做"), false).Size(28, 28).Disabled(!b.History.CanRedo()).Clicked() {
			run("redo")
		}
		if o.Done != nil && boardFlatButton(c, "board-done", i18n.T("返回文档"), false, st).Height(28).Clicked() {
			b.finishText(o)
			o.Done()
		}
	})
}

var boardRouteLabels = map[string]string{"sharp": "直线", "curved": "贝塞尔曲线", "elbow": "折线"}
var boardArrowLabels = map[string]string{"": "无箭头", "arrow": "空心箭头", "triangle": "实心箭头", "barb": "倒钩箭头"}

func (b *Board) propSelect(c *ui.Context, key, label, current string, options []string, o BoardOptions) {
	boardSectionLabel(c, boardChromeFor(o.Theme), i18n.T(label))
	value := current
	labels := make([]string, len(options))
	for i, v := range options {
		label := v
		if key == "routing" {
			label = boardRouteLabels[v]
		}
		if key == "start-arrow" || key == "end-arrow" {
			if name, ok := boardArrowLabels[v]; ok {
				label = name
			}
		}
		labels[i] = i18n.T(label)
		if v == current {
			value = labels[i]
		}
	}
	before := value
	ui.Select(c.Key("board-property-"+key), &value, labels).WidthPercent(100).Height(28).FontSize(12.5)
	if value != before {
		for i, label := range labels {
			if value == label {
				b.ChangeProperty(key, options[i], o)
				break
			}
		}
	}
}
func (b *Board) properties(c *ui.Context, o BoardOptions) {
	ids := b.SelectedIDs()
	if len(ids) == 0 && (b.Tool == ToolSelect || b.Tool == ToolHand || b.Tool == ToolEraser || b.Tool == ToolLasso) {
		b.compactStyleOpen = false
		return
	}
	e := b.style
	if len(ids) > 0 {
		e = b.Scene.ElementByID(ids[0])
	}
	st := boardChromeFor(o.Theme)
	top := float32(62)
	if b.lastRect.W < 600 {
		boardSurface(ui.Row(c.Key("board-style-toggle")).Absolute().Left(8).Top(top).Padding(2), st).Children(func() {
			if boardFlatButton(c, "toggle", i18n.T("图形样式"), b.compactStyleOpen, st).Clicked() {
				b.compactStyleOpen = !b.compactStyleOpen
			}
		})
		if !b.compactStyleOpen {
			return
		}
		top += 38
	}
	panel := boardSurface(ui.Scroll(c.Key("board-properties")).Absolute().Top(top).Width(min(232, max(1, b.lastRect.W-24))).MaxHeight(max(44, b.lastRect.H-top-56)).Padding(12).Gap(8), st)
	if len(ids) > 0 && b.lastRect.W >= 600 {
		bounds := whiteboard.SelectionBounds(b.Scene, ids)
		center, _, _, _ := b.canvasRect(bounds.X+bounds.W/2, 0, 0, 0)
		if center < b.lastRect.W/2 {
			panel.Right(12)
		} else {
			panel.Left(12)
		}
	} else {
		panel.Left(8)
	}
	panel.Role(ui.RoleGroup).Label(i18n.T("图形样式")).Children(func() {
		for _, which := range []struct{ key, label, current string }{{"stroke", "描边", e.StrokeColor}, {"fill", "填充", e.FillColor}} {
			boardSectionLabel(c, st, i18n.T(which.label))
			ui.Row(c.Key(which.key)).Gap(5).AlignItems(ui.Center).Children(func() {
				for _, color := range []string{"#1e1e1e", "#e03131", "#2f9e44", "#1971c2", "#f08c00", "transparent"} {
					if boardSwatch(c, which.key+color, color, which.label+color, color == which.current, o.Theme.Dark, st).Clicked() {
						b.ChangeProperty(which.key, color, o)
					}
				}
				more := boardFlatButton(c, which.key+"-custom", "…", false, st).Size(24, 24).PaddingX(0).MinWidth(24)
				more.Menu(func(m *ui.Menu) {
					if m.Item(i18n.T("输入颜色值")).Chosen() {
						b.panel = "color:" + which.key
						b.propertyInput = which.current
					}
				})
			})
		}
		ui.Box(c).Height(1).Margin(2, 0).Background(st.divider)
		ui.Row(c.Key("prop-style-row")).Gap(6).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				b.propSelect(c, "fill-style", "填充样式", string(e.FillStyle), []string{"hachure", "cross-hatch", "solid", "zigzag"}, o)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				b.propSelect(c, "stroke-style", "线条样式", string(e.StrokeStyle), []string{"solid", "dashed", "dotted"}, o)
			})
		})
		round := e.Roundness != nil
		beforeRound := round
		ui.Checkbox(c.Key("board-roundness"), &round, i18n.T("圆角"))
		if round != beforeRound {
			b.ChangeProperty("round", round, o)
		}
		ui.Box(c).Height(1).Margin(2, 0).Background(st.divider)
		for _, p := range []struct {
			key, label    string
			value, lo, hi float64
		}{{"width", "线宽", e.StrokeWidth, 1, 12}, {"roughness", "手绘程度", e.Roughness, 0, 2}, {"opacity", "不透明度", e.Opacity, 0, 100}} {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				boardSectionLabel(c, st, i18n.T(p.label))
				ui.Spacer(c)
				ui.Text(c, fmt.Sprintf("%.0f", p.value)).FontSize(11.5).TextColor(st.text)
			})
			v := p.value
			ui.Slider(c.Key(p.key), &v, p.lo, p.hi).WidthPercent(100)
			if v != p.value {
				b.ChangeProperty(p.key, v, o)
			}
		}
		if len(ids) == 0 || e.Kind == whiteboard.KindText || e.BoundTextID() != "" {
			ui.Box(c).Height(1).Margin(2, 0).Background(st.divider)
			v := fmt.Sprint(e.FontFamily)
			if e.FontFamily == 0 {
				v = "5"
			}
			boardSectionLabel(c, st, i18n.T("字体"))
			fonts := []string{"1 Virgil", "2 Sans", "3 Cascadia", "5 Excalifont", "6 Nunito", "7 Lilita One", "8 Comic Shanns", "9 Arial", "10 Assistant"}
			current := v + " " + whiteboard.FontName(e.FontFamily)
			selected := current
			ui.Select(c.Key("board-font"), &selected, fonts).WidthPercent(100).Height(28).FontSize(12.5)
			if selected != current {
				n, _ := strconv.Atoi(strings.Fields(selected)[0])
				b.ChangeProperty("font", n, o)
			}
			ui.Row(c).Gap(4).Children(func() {
				for _, size := range []float64{16, 20, 28, 36} {
					if boardFlatButton(c, fmt.Sprint("font", size), fmt.Sprint(size), e.FontSize == size, st).Grow(1).MinWidth(0).PaddingX(0).Clicked() {
						b.ChangeProperty("font-size", size, o)
					}
				}
			})
			ui.Row(c.Key("prop-align-row")).Gap(6).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					b.propSelect(c, "text-align", "文字对齐", e.TextAlign, []string{"left", "center", "right"}, o)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					b.propSelect(c, "vertical-align", "垂直对齐", e.VerticalAlign, []string{"top", "middle", "bottom"}, o)
				})
			})
		}
		if e.IsLinear() || b.Tool == ToolArrow || b.Tool == ToolLine {
			ui.Box(c).Height(1).Margin(2, 0).Background(st.divider)
			dim := e.DimArrow
			if ui.Checkbox(c.Key("board-dim0-arrow"), &dim, i18n.T("dim0 箭头")).Changed() {
				b.ChangeProperty("dim0-arrow", dim, o)
			}
			routing := "sharp"
			if e.Elbowed {
				routing = "elbow"
			} else if e.Roundness != nil {
				routing = "curved"
			}
			b.propSelect(c, "routing", "连线", routing, []string{"sharp", "curved", "elbow"}, o)
			heads := []string{"", "arrow", "triangle", "barb", "bar", "circle", "circle_outline", "triangle_outline", "diamond", "diamond_outline", "cardinality_one", "cardinality_many", "cardinality_one_or_many", "cardinality_exactly_one", "cardinality_zero_or_one", "cardinality_zero_or_many"}
			ui.Row(c.Key("prop-heads-row")).Gap(6).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					b.propSelect(c, "start-arrow", "起点", string(e.StartArrowhead), heads, o)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					b.propSelect(c, "end-arrow", "终点", string(e.EndArrowhead), heads, o)
				})
			})
		}
		if len(ids) > 0 {
			ui.Box(c).Height(1).Margin(2, 0).Background(st.divider)
			ui.Row(c).Gap(4).Children(func() {
				for _, a := range []struct{ id, label string }{{"duplicate", "复制图形"}, {"delete", "删除"}} {
					if boardFlatButton(c, a.id, i18n.T(a.label), false, st).Grow(1).MinWidth(0).Clicked() {
						b.RunAction(a.id, o)
					}
				}
			})
			b.arrangeButtons(c, o, len(ids))
			if boardFlatButton(c, "board-link", i18n.T("关联文档或链接"), false, st).WidthPercent(100).Clicked() {
				b.panel = "link"
				b.propertyInput = e.Link
			}
			if e.Kind == whiteboard.KindImage && boardFlatButton(c, "board-crop", i18n.T("裁剪图片"), false, st).WidthPercent(100).Clicked() {
				b.beginCrop(e.ID)
			}
		}
	})
}

// boardActionGroup is a menu of related actions on the selection.
type boardActionGroup struct {
	key, label string
	min        int // selected elements the group needs
	actions    []struct{ id, label string }
}

// boardArrange are the selection's layout actions, as an office suite's
// Arrange menu offers them: layers, alignment, spacing, flips, grouping
// and locking.
var boardArrange = []boardActionGroup{
	{"board-layers", "图层", 1, []struct{ id, label string }{{"front", "置于顶层"}, {"forward", "上移一层"}, {"backward", "下移一层"}, {"back", "置于底层"}}},
	{"board-align", "对齐", 2, []struct{ id, label string }{{"align-x-start", "左对齐"}, {"align-x-center", "水平居中"}, {"align-x-end", "右对齐"}, {"align-y-start", "顶部对齐"}, {"align-y-center", "垂直居中"}, {"align-y-end", "底部对齐"}, {"distribute-x", "水平分布"}, {"distribute-y", "垂直分布"}}},
	{"board-arrange", "排列", 1, []struct{ id, label string }{{"flip-horizontal", "水平翻转"}, {"flip-vertical", "垂直翻转"}, {"group", "组合"}, {"ungroup", "取消组合"}, {"lock", "锁定或解锁"}, {"copy-style", "复制样式"}, {"paste-style", "粘贴样式"}}},
}

// arrangeButtons are the selection's layout menus in the properties panel,
// so they need no right click; alignment needs two elements or more.
func (b *Board) arrangeButtons(c *ui.Context, o BoardOptions, selected int) {
	ui.Row(c.Key("board-arrange-row")).Gap(3).Children(func() {
		for _, g := range boardArrange {
			if selected < g.min {
				continue
			}
			boardButton(c, g.key, i18n.T(g.label)+" ▾", false).Tooltip(i18n.T(g.label)).Menu(func(m *ui.Menu) {
				for _, a := range g.actions {
					if m.Item(i18n.T(a.label)).Chosen() {
						b.RunAction(a.id, o)
						b.RequestFocus = true
					}
				}
			})
		}
	})
}

func (b *Board) contextMenu(m *ui.Menu, o BoardOptions) {
	if !b.ReadOnly {
		b.layoutMenu(m, o)
		if b.selectedGroup() != "" && m.Item(i18n.T("重命名分组")).Chosen() {
			b.RunAction("rename-group", o)
		}
	}
	for _, a := range []struct{ id, label string }{{"copy", "复制"}, {"cut", "剪切"}, {"paste", "粘贴"}, {"duplicate", "复制图形"}, {"delete", "删除"}, {"copy-svg", "复制为 SVG"}, {"copy-png", "复制为 PNG"}, {"copy-text", "复制文字"}, {"group", "组合"}, {"ungroup", "取消组合"}, {"lock", "锁定或解锁"}, {"unlock-all", "解锁全部"}, {"front", "置于顶层"}, {"back", "置于底层"}, {"forward", "上移一层"}, {"backward", "下移一层"}, {"flip-horizontal", "水平翻转"}, {"flip-vertical", "垂直翻转"}, {"copy-style", "复制样式"}, {"paste-style", "粘贴样式"}, {"bind-text", "绑定文字"}, {"unbind-text", "解除文字绑定"}, {"wrap-text", "添加文字容器"}, {"auto-text", "文字自动宽度"}, {"edit-line", "编辑折点"}, {"crop", "裁剪图片"}, {"frame", "由选区创建画框"}, {"remove-frame", "移出画框"}, {"select-frame", "选择画框内容"}, {"align-x-start", "左对齐"}, {"align-x-center", "水平居中"}, {"align-x-end", "右对齐"}, {"align-y-start", "顶部对齐"}, {"align-y-center", "垂直居中"}, {"align-y-end", "底部对齐"}, {"distribute-x", "水平分布"}, {"distribute-y", "垂直分布"}, {"grid", "显示网格"}, {"snap", "对象吸附"}, {"arrow-binding", "箭头绑定"}, {"midpoint-snap", "中点吸附"}, {"fit-selection", "适应选区"}, {"help", "白板快捷键"}} {
		if m.Item(i18n.T(a.label)).Chosen() {
			b.RunAction(a.id, o)
		}
	}
	b.aiContextMenu(m, o)
}
func (b *Board) textOverlay(c *ui.Context, o BoardOptions) {
	t := b.editing
	if t == nil {
		return
	}
	e := b.Scene.ElementByID(t.id)
	if e == nil {
		return
	}
	x, y := b.screenPoint(whiteboard.Point{e.X, e.Y})
	w := math.Max(80, e.W+30) * b.Zoom
	h := math.Max(40, e.H+30) * b.Zoom
	f := whiteboard.Font{Size: e.FontSize * b.Zoom, Family: e.FontFamily}
	t.layout = boardShapeText(t.shown, f, 0, e.TextAlign)
	lw, lh := t.layout.Size()
	w = math.Max(w, float64(lw+20))
	h = math.Max(h, float64(lh+12))
	el := ui.Box(c.Key("board-text-input")).Absolute().Left(x-b.lastRect.X).Top(y-b.lastRect.Y).Size(float32(w), float32(h)).Cursor(ui.CursorText).Focusable().FocusRing(false).Role(ui.RoleTextField).Label(i18n.T("白板文字")).HandleTextInput(t)
	if b.textFocus {
		el.Focus()
		b.textFocus = false
	}
	el.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputKeyDown:
			return b.textKey(ev, o)
		case ui.InputCommand:
			return b.textCommand(ev.Text, o)
		case ui.InputPointerDown:
			at, _ := t.IndexForPoint(ui.Point{X: ev.X, Y: ev.Y})
			if ev.Mods&ui.Shift == 0 {
				t.selection.Start = at
			}
			t.selection.End = at
			return true
		}
		return false
	})
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, o.Theme.Background, 2)
		for _, sel := range t.layout.SelectionRects(t.selection) {
			sel.X += r.X
			sel.Y += r.Y
			p.Fill(sel, o.Theme.Accent.Alpha(.2), 0)
		}
		p.TextLayout(t.layout, r.X, r.Y, boardColor(e.StrokeColor, o.Theme.Dark))
		caret := t.layout.Caret(t.selection.End)
		p.Fill(ui.Rect{X: r.X + caret.X, Y: r.Y + caret.Y, W: 1.5, H: caret.H}, o.Theme.Text, 0)
		if t.hasMarked {
			mark := t.layout.Caret(t.marked.Start)
			end := t.layout.Caret(t.marked.End)
			p.Line(r.X+mark.X, r.Y+mark.Y+mark.H, r.X+end.X, r.Y+end.Y+end.H, 1, o.Theme.Text)
		}
	})
}
func (b *Board) beginCrop(id string) {
	e := b.Scene.ElementByID(id)
	if e == nil || e.Kind != whiteboard.KindImage {
		return
	}
	bm := b.images[e.FileID]
	if bm == nil {
		return
	}
	w, h := bm.Size()
	crop := whiteboard.ImageCrop{Width: float64(w), Height: float64(h), NaturalWidth: float64(w), NaturalHeight: float64(h)}
	if e.Crop != nil {
		crop = *e.Crop
	}
	b.cropID = id
	b.cropValues = [4]string{fmt.Sprint(crop.X), fmt.Sprint(crop.Y), fmt.Sprint(crop.Width), fmt.Sprint(crop.Height)}
	// Reuse the production drag/keyboard crop frame and keep original pixels.
	ce := New("")
	ce.openCropper(id, "", bm)
	ce.cropper.x0 = float32(crop.X)
	ce.cropper.y0 = float32(crop.Y)
	ce.cropper.x1 = float32(crop.X + crop.Width)
	ce.cropper.y1 = float32(crop.Y + crop.Height)
	ce.cropImage = func(_, _ string, r image.Rectangle) {
		b.err = b.SetImageCrop(id, whiteboard.ImageCrop{X: float64(r.Min.X), Y: float64(r.Min.Y), Width: float64(r.Dx()), Height: float64(r.Dy()), NaturalWidth: float64(w), NaturalHeight: float64(h)})
	}
	b.cropper = ce
	b.panel = ""
}
func (b *Board) panelTitle() string {
	switch {
	case b.panel == "group-name":
		return i18n.T("重命名分组")
	case b.panel == "search":
		return i18n.T("搜索白板内容")
	case b.panel == "outline":
		return i18n.T("大纲转思维导图")
	case b.panel == "clear":
		return i18n.T("清空白板")
	case b.panel == "help":
		return i18n.T("白板快捷键")
	case strings.HasPrefix(b.panel, "color:"):
		return i18n.T("自定义颜色")
	case b.panel == "link":
		return i18n.T("关联文档或链接")
	case b.panel == "crop":
		return i18n.T("裁剪图片")
	case b.panel == "library":
		return i18n.T("组件素材库")
	case b.panel == "mermaid":
		return i18n.T("Mermaid 转白板")
	case b.panel == "background":
		return i18n.T("画布背景")
	case b.panel == "frame-name":
		return i18n.T("重命名画框")
	}
	return i18n.T("白板")
}

func (b *Board) panelView(c *ui.Context, o BoardOptions) {
	if b.panel == "" {
		return
	}
	st := boardChromeFor(o.Theme)
	boardSurface(ui.Scroll(c.Key("board-panel")).Absolute().Top(66).Right(12).Width(min(336, max(180, b.lastRect.W-24))).MaxHeight(max(150, b.lastRect.H-140)).Padding(14).Gap(10), st).Children(func() {
		boardPanelHeader(c, st, b.panelTitle(), "", "", o.Theme.Dark, func() { b.panel = "" })
		switch {
		case b.panel == "search":
			b.searchPanel(c)
		case b.panel == "outline":
			b.outlinePanel(c, o)
		case b.panel == "clear":
			ui.Text(c, i18n.T("清空当前白板？可以撤销此操作")).FontSize(13).TextColor(st.text)
			if ui.Button(c, i18n.T("清空白板")).Clicked() {
				b.RunAction("clear-confirm", o)
			}
		case b.panel == "help":
			shortcuts := [][2]string{
				{"V / H", "选择与抓手"},
				{"R / D / O", "矩形、菱形、椭圆"},
				{"A / L / P", "箭头、直线、画笔"},
				{"T / N / E / F", "文字、便签、橡皮、画框"},
				{"Space / Ctrl+滚轮", "平移与缩放"},
				{"Ctrl+Z / Shift+Z", "撤销与重做"},
				{"Ctrl+C / X / V", "复制、剪切、粘贴"},
				{"Ctrl+D / Ctrl+G", "复制图形与组合"},
				{"Shift / Alt", "保持比例与拖动复制"},
				{"Enter / Esc", "编辑文字与返回"},
			}
			ui.Column(c).Gap(6).Children(func() {
				for _, sc := range shortcuts {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Padding(2, 6).Radius(4).Background(st.surfaceMuted).Children(func() {
							ui.Text(c, sc[0]).FontSize(11.5).FontWeight(500).TextColor(st.text).NoWrap()
						})
						ui.Text(c, i18n.T(sc[1])).FontSize(12).TextColor(st.muted).Grow(1).MinWidth(0)
					})
				}
			})
		case strings.HasPrefix(b.panel, "color:"):
			ui.TextInput(c.Key("board-color-input"), &b.propertyInput).WidthPercent(100)
			if ui.Button(c, i18n.T("应用颜色")).Clicked() {
				v := strings.TrimSpace(b.propertyInput)
				if len(v) == 7 && v[0] == '#' {
					if _, err := strconv.ParseUint(v[1:], 16, 24); err == nil {
						b.ChangeProperty(strings.TrimPrefix(b.panel, "color:"), v, o)
						b.panel = ""
					}
				}
			}
		case b.panel == "link":
			ui.TextInput(c.Key("board-link-input"), &b.propertyInput).WidthPercent(100)
			if o.WikiSuggest != nil {
				for _, name := range o.WikiSuggest(strings.Trim(b.propertyInput, "[]")) {
					if ui.Button(c, name).Clicked() {
						b.propertyInput = "[[" + name + "]]"
					}
				}
			}
			if ui.Button(c, i18n.T("保存链接")).Clicked() {
				b.ChangeProperty("link", b.propertyInput, o)
				b.panel = ""
			}
		case b.panel == "crop":
			for i, label := range []string{"X", "Y", "宽度", "高度"} {
				boardSectionLabel(c, st, i18n.T(label))
				ui.TextInput(c.Key(fmt.Sprint("crop", i)), &b.cropValues[i]).WidthPercent(100)
			}
			if ui.Button(c, i18n.T("应用裁剪")).Clicked() {
				var vs [4]float64
				valid := true
				for i, v := range b.cropValues {
					n, err := strconv.ParseFloat(v, 64)
					if err != nil {
						b.err = err
						valid = false
					}
					vs[i] = n
				}
				e := b.Scene.ElementByID(b.cropID)
				if valid && e != nil {
					if bm := b.images[e.FileID]; bm != nil {
						w, h := bm.Size()
						b.err = b.SetImageCrop(e.ID, whiteboard.ImageCrop{X: vs[0], Y: vs[1], Width: vs[2], Height: vs[3], NaturalWidth: float64(w), NaturalHeight: float64(h)})
						if b.err == nil {
							b.panel = ""
						}
					}
				}
			}
		case b.panel == "library":
			b.libraryPanel(c, o)
		case b.panel == "mermaid":
			b.mermaidPanel(c, o)
		case b.panel == "background":
			ui.Row(c).Gap(8).Children(func() {
				currentBg := strings.ToLower(b.Scene.AppState.ViewBackgroundColor)
				for _, color := range []string{"#ffffff", "#f8f9fa", "#fff9db", "#e7f5ff", "#f3f0ff", "#000000"} {
					sw := ui.ButtonBase(c.Key(color)).Size(30, 30).Radius(8).Background(boardColor(color, false)).Border(1, st.border).Focusable().Label(color)
					if currentBg == color {
						sw.Border(2, st.activeInk)
					}
					if sw.Clicked() {
						b.edit(o, func() { b.Scene.AppState.ViewBackgroundColor = color })
					}
				}
			})
		case b.panel == "frame-name":
			ui.TextInput(c.Key("board-frame-name"), &b.propertyInput).WidthPercent(100)
			if ui.Button(c, i18n.T("重命名画框")).Clicked() {
				b.edit(o, func() {
					for _, id := range b.SelectedIDs() {
						if e := b.Scene.ElementByID(id); e != nil && e.IsFrame() {
							e.Name = b.propertyInput
							whiteboard.Touch(e)
						}
					}
				})
				b.panel = ""
			}
		case b.panel == "group-name":
			input := ui.TextInput(c.Key("board-group-name"), &b.propertyInput).WidthPercent(100).AutoFocus().Label(i18n.T("分组名称"))
			if input.Shortcut(0, ui.KeyEscape) {
				b.panel = ""
			}
			if ui.Button(c, i18n.T("保存分组名称")).Disabled(b.ReadOnly).Clicked() || input.Submitted() && !b.ReadOnly {
				b.edit(o, func() { b.Scene.RenameGroup(b.groupRename, b.propertyInput) })
				b.panel = ""
			}
		}
	})
}
