package editor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

var (
	aiStatusNames    = map[string]string{"queued": "排队中", "running": "生成中…", "done": "已完成", "cached": "输入未变化，沿用上次结果", "blocked": "上游节点失败，未运行", "stopped": "已停止"}
	aiRoleShortNames = map[whiteboard.AIRole]string{whiteboard.AIPrompt: "文字", whiteboard.AIImageInput: "图片", whiteboard.AIText: "文本", whiteboard.AIImage: "图像", whiteboard.AIVideo: "视频", whiteboard.AIConcat: "拼接", whiteboard.AINote: "笔记"}
	aiRoleIcons      = map[whiteboard.AIRole]string{whiteboard.AIPrompt: "text", whiteboard.AIImageInput: "image", whiteboard.AIText: "text", whiteboard.AIImage: "image", whiteboard.AIVideo: "player-play", whiteboard.AIConcat: "group", whiteboard.AINote: "sticky-note"}
)

type aiNodeColors struct {
	accent, accentSoft         ui.Color
	cardBg, headerBg, footerBg ui.Color
	mediaBg, divider, border   ui.Color
	ink, secondary, muted      ui.Color
	danger, dangerSoft, shadow ui.Color
}

func aiNodePalette(kind string, dark bool) aiNodeColors {
	var accent ui.Color
	if dark {
		switch kind {
		case "image":
			accent = ui.Hex("#4ADE80")
		case "video":
			accent = ui.Hex("#FB923C")
		default:
			accent = ui.Hex("#60A5FA")
		}
		return aiNodeColors{
			accent:     accent,
			accentSoft: accent.Alpha(0.18),
			cardBg:     ui.Hex("#1E2024"),
			headerBg:   ui.Hex("#25282E"),
			footerBg:   ui.Hex("#22252A"),
			mediaBg:    ui.Hex("#16181C"),
			divider:    ui.Hex("#32363F"),
			border:     ui.Hex("#3D424D"),
			ink:        ui.Hex("#E6E8EC"),
			secondary:  ui.Hex("#B0B6C2"),
			muted:      ui.Hex("#8B929E"),
			danger:     ui.Hex("#F85149"),
			dangerSoft: ui.Hex("#F85149").Alpha(0.16),
			shadow:     ui.RGBA(0, 0, 0, 0.24),
		}
	}
	switch kind {
	case "image":
		accent = ui.Hex("#15803D")
	case "video":
		accent = ui.Hex("#C2410C")
	default:
		accent = ui.Hex("#2563EB")
	}
	return aiNodeColors{
		accent:     accent,
		accentSoft: accent.Alpha(0.11),
		cardBg:     ui.Hex("#FFFFFF"),
		headerBg:   ui.Hex("#F7F8FA"),
		footerBg:   ui.Hex("#F8F9FB"),
		mediaBg:    ui.Hex("#F4F5F7"),
		divider:    ui.Hex("#E4E6EB"),
		border:     ui.Hex("#CFD4DC"),
		ink:        ui.Hex("#1F2329"),
		secondary:  ui.Hex("#525866"),
		muted:      ui.Hex("#6E7681"),
		danger:     ui.Hex("#CF222E"),
		dangerSoft: ui.Hex("#CF222E").Alpha(0.10),
		shadow:     ui.RGBA(0, 0, 0, 0.06),
	}
}

// aiStatus is the card's status line and whether it reports a failure.
func (b *Board) aiStatus(e *whiteboard.Element, n *whiteboard.AINode) (string, bool) {
	if n.Role == whiteboard.AIImageInput && n.Error == "" {
		if b.imageErrors[e.FileID] != nil {
			return i18n.T("图片无法读取"), true
		}
		if b.images[e.FileID] == nil {
			return i18n.T("正在加载图片"), false
		}
		return i18n.T("可作为参考图"), false
	}
	r := b.aiState().runs[e.ID]
	if r != nil {
		switch r.status {
		case "error":
			return i18n.T("失败：") + r.err.Error(), true
		case "running":
			if r.progress != "" {
				return i18n.T("生成中…") + " " + r.progress, false
			}
		}
		if name := aiStatusNames[r.status]; name != "" {
			return i18n.T(name), false
		}
	}
	if n.Error != "" {
		return i18n.T("失败：") + n.Error, true
	}
	if len(n.Outputs) > 0 {
		return i18n.T("已生成"), false
	}
	return i18n.T("未运行"), false
}

func (b *Board) aiCardTextFont(id, text string, width float32, font ui.Font) *ui.TextLayout {
	s := b.aiState()
	if len(text) > 6000 {
		text = text[:6000] + "…"
	}
	key := fmt.Sprintf("%s\x00%.0f\x00%.1f\x00%d\x00%.2f", text, width, font.Size, font.Weight, font.LineHeight)
	if c, ok := s.texts[id]; ok && c.key == key {
		return c.layout
	}
	layout := ui.ShapeText(text, font, width)
	s.texts[id] = boardAICardText{key, layout}
	return layout
}

// aiPrepareMedia loads the pictures the cards show, off the main thread.
func (b *Board) aiPrepareMedia(o BoardOptions) {
	if o.AI == nil || o.AI.ReadFile == nil {
		return
	}
	s := b.aiState()
	for _, e := range b.Scene.Elements {
		n := b.aiNode(e)
		if n == nil || e.IsDeleted {
			continue
		}
		out := n.Output()
		if out == nil || out.Kind != "image" || out.Path == "" {
			continue
		}
		rel := out.Path
		if s.media[rel] != nil || s.mediaErr[rel] != nil || s.mediaPending[rel] {
			continue
		}
		s.mediaPending[rel] = true
		read := o.AI.ReadFile
		load := func() (*ui.Bitmap, error) {
			data, err := read(rel)
			if err != nil {
				return nil, err
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			if int64(cfg.Width)*int64(cfg.Height) > 100_000_000 {
				return nil, fmt.Errorf("image dimensions exceed limit")
			}
			return ui.DecodeBitmap(data)
		}
		apply := func(bm *ui.Bitmap, err error) {
			if b.ai.mediaPending == nil {
				return
			}
			delete(s.mediaPending, rel)
			if err != nil {
				s.mediaErr[rel] = err
			} else {
				s.media[rel] = bm
			}
		}
		if o.Dispatch != nil {
			go func() { bm, err := load(); o.Dispatch(func() { apply(bm, err) }) }()
		} else {
			apply(load())
		}
	}
}

// screenRect is a scene box in the canvas's own coordinates.
func (b *Board) canvasRect(x, y, w, h float64) (float32, float32, float32, float32) {
	return float32(x*b.Zoom + b.PanX), float32(y*b.Zoom + b.PanY), float32(w * b.Zoom), float32(h * b.Zoom)
}

func (b *Board) selectedAINode() (*whiteboard.Element, *whiteboard.AINode) {
	ids := b.SelectedIDs()
	if len(ids) != 1 {
		return nil, nil
	}
	e := b.Scene.ElementByID(ids[0])
	if n := b.aiNode(e); n != nil {
		return e, n
	}
	return nil, nil
}

// aiToolbar adds the AI node bar toggle and the whole-board run controls.
func (b *Board) aiToolbar(c *ui.Context, o BoardOptions) {
	if o.AI == nil {
		return
	}
	ui.Box(c.Key("board-ai-sep")).Width(1).Height(20).Margin(0, 3).Background(o.Theme.Border).AlignSelf(ui.Center)
	aiBtn := ui.ButtonBase(c.Key("board-ai")).Height(34).PaddingX(10).Gap(5).Center().Radius(6).Focusable().Label(i18n.T("AI 节点")).Tooltip(i18n.T("AI 节点"))
	if b.ai.bar {
		aiBtn.Background(o.Theme.Accent.Alpha(.16))
	} else if aiBtn.Hovered() {
		aiBtn.Background(o.Theme.SurfaceHover)
	}
	aiBtn.Children(func() {
		if icon := boardIcon("sparkles"); icon != nil {
			ui.Icon(c, icon).FontSize(17).TextColor(o.Theme.Accent)
		}
		ui.Text(c, "AI").FontSize(12.5).FontWeight(600).TextColor(o.Theme.Text).NoWrap()
	})
	if aiBtn.Clicked() {
		b.ai.bar = !b.ai.bar
		b.ai.barFocus = b.ai.bar
	}
	if b.AIRunning() {
		if boardIconButton(c, "board-ai-stop", "player-stop", i18n.T("停止运行"), false).Clicked() {
			b.StopAI()
		}
		return
	}
	has := false
	for _, e := range b.Scene.Elements {
		if b.aiNode(e) != nil {
			has = true
			break
		}
	}
	if has && boardIconButton(c, "board-ai-run-all", "player-play", i18n.T("全部运行"), false).Clicked() {
		b.RunAI(nil, false, false, o)
	}
}

type aiBarItem struct {
	key, label, group, icon, kind string
	run                           func()
}

func aiItemBadge(c *ui.Context, icon, kind string, dark bool) {
	pal := aiNodePalette(kind, dark)
	ui.Box(c).Size(24, 24).Radius(6).Background(pal.accentSoft).Center().Shrink(0).Children(func() {
		if svg := boardIcon(icon); svg != nil {
			ui.Icon(c, svg).FontSize(14).TextColor(pal.accent)
		}
	})
}

// aiBar is the node bar, grouped as Vibe-Workflow's NodesNavbar: inputs,
// text, image, video, templates and exchange, with a search field.
func (b *Board) aiBar(c *ui.Context, o BoardOptions) {
	if !b.ai.bar || o.AI == nil || b.ReadOnly {
		return
	}
	add := func(role whiteboard.AIRole, title string) func() {
		return func() {
			id := b.AddAINode(role, b.viewCenter(), o)
			if title != "" && id != "" {
				b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Title = i18n.T(title) })
			}
			b.RequestFocus = true
		}
	}
	items := []aiBarItem{
		{"ai-prompt", "文字节点", "输入", "text", "text", add(whiteboard.AIPrompt, "")},
		{"note", "便签文字", "输入", "sticky-note", "text", func() {
			at := b.viewCenter()
			b.edit(o, func() {
				e := b.newElement(whiteboard.KindNote, whiteboard.Point{at[0] - 110, at[1] - 80}, 220, 160)
				b.Scene.Elements = append(b.Scene.Elements, e)
				b.selectIDs([]string{e.ID})
			})
			if ids := b.SelectedIDs(); len(ids) == 1 {
				b.startText(b.Scene.ElementByID(ids[0]), at, o)
			}
		}},
		{"picture", "插入图片", "输入", "image", "image", func() {
			if o.PickImage != nil {
				o.PickImage()
			}
		}},
		{"frame", "标注画框", "输入", "frame", "image", func() {
			at := b.viewCenter()
			b.edit(o, func() {
				f := whiteboard.NewElement(whiteboard.KindFrame, at[0]-170, at[1]-160, 340, 320)
				f.Name = i18n.T("标注区：放入底图并圈画")
				b.Scene.Elements = append([]*whiteboard.Element{f}, b.Scene.Elements...)
				b.selectIDs([]string{f.ID})
			})
		}},
		{"ai-note", "笔记引用", "输入", "text", "text", add(whiteboard.AINote, "")},
		{"ai-text", "文本生成", "文本", "text", "text", add(whiteboard.AIText, "")},
		{"ai-concat", "提示词拼接", "文本", "group", "text", add(whiteboard.AIConcat, "")},
		{"ai-image", "生成图像", "图像", "image", "image", add(whiteboard.AIImage, "")},
		{"ai-edit", "编辑图像", "图像", "sparkles", "image", add(whiteboard.AIImage, "编辑图像")},
		{"ai-video", "文生视频", "视频", "player-play", "video", add(whiteboard.AIVideo, "")},
		{"ai-i2v", "图生视频", "视频", "player-play", "video", add(whiteboard.AIVideo, "图生视频")},
	}
	for _, t := range aiTemplates {
		items = append(items, aiBarItem{"tpl-" + t.key, t.label, "模板", "sparkles", "text", func() { b.insertAITemplate(t.key, o) }})
	}
	if o.AI.ImportCanvas != nil {
		items = append(items, aiBarItem{"canvas-import", "导入 JSON Canvas…", "交换", "duplicate", "text", func() {
			o.AI.ImportCanvas(func(data []byte, err error) {
				if err == nil {
					err = b.ImportCanvas(data, o)
				}
				b.err = err
			})
		}})
	}
	if o.Export != nil {
		items = append(items, aiBarItem{"canvas-export", "导出 JSON Canvas…", "交换", "duplicate", "text", func() { o.Export("canvas") }})
	}
	ui.Scroll(c.Key("board-ai-bar")).Absolute().Top(66).Left(12).Width(236).MaxHeight(max(120, b.lastRect.H-132)).Padding(12).Gap(4).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(10).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.12)).Role(ui.RoleGroup).Label(i18n.T("AI 节点")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Margin(0, 0, 4, 0).Children(func() {
			ui.Text(c, i18n.T("AI 节点")).FontSize(14).FontWeight(600).Grow(1)
			if boardIconButton(c, "close", "close", i18n.T("关闭"), false).Size(28, 28).Clicked() {
				b.ai.bar = false
			}
		})
		search := ui.TextInput(c.Key("board-ai-search"), &b.ai.query).Placeholder(i18n.T("搜索节点")).WidthPercent(100)
		if b.ai.barFocus {
			search.Focus()
			b.ai.barFocus = false
		}
		query := strings.ToLower(strings.TrimSpace(b.ai.query))
		group := ""
		for _, it := range items {
			label := i18n.T(it.label)
			if query != "" && !strings.Contains(strings.ToLower(label+" "+i18n.T(it.group)), query) {
				continue
			}
			if it.group != group {
				group = it.group
				if group != "" {
					ui.Box(c).Height(2)
				}
				ui.Text(c.Key("g-"+group), i18n.T(group)).FontSize(11.5).FontWeight(600).TextColor(o.Theme.TextMuted).Margin(6, 4, 2, 4)
			}
			row := ui.ButtonBase(c.Key("board-ai-item-"+it.key)).WidthPercent(100).Padding(5, 8).Gap(8).AlignItems(ui.Center).Radius(6).Focusable().Label(label)
			if row.Hovered() {
				row.Background(o.Theme.SurfaceHover)
			}
			row.Children(func() {
				aiItemBadge(c, it.icon, it.kind, o.Theme.Dark)
				ui.Text(c, label).FontSize(13).TextColor(o.Theme.Text).SingleLine()
			})
			if row.Clicked() {
				it.run()
			}
		}
	})
}

// aiChoice is a labeled select over values; it returns the chosen value.
func aiChoice(c *ui.Context, o BoardOptions, key, label, current string, values, labels []string) string {
	ui.Text(c, i18n.T(label)).FontSize(12).TextColor(o.Theme.TextMuted)
	shown := current
	for i, v := range values {
		if v == current {
			shown = labels[i]
		}
	}
	before := shown
	ui.Select(c.Key("board-ai-"+key), &shown, labels).WidthPercent(100).FontSize(13).Label(i18n.T(label))
	if shown != before {
		for i, l := range labels {
			if l == shown {
				return values[i]
			}
		}
	}
	return current
}

func translated(values ...string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = i18n.T(v)
	}
	return out
}

// aiEditBuffers are the panel's text fields, kept while a node is edited so
// typing and IME composition survive frames.
type aiEditBuffers struct {
	title, instruction, note, template, extra string
}

func aiPropDivider(c *ui.Context, o BoardOptions) {
	ui.Box(c).Height(1).Margin(4, 0).Background(o.Theme.Border.Alpha(0.6))
}

func aiPropSection(c *ui.Context, o BoardOptions, title string) {
	ui.Text(c, i18n.T(title)).FontSize(12).FontWeight(600).TextColor(o.Theme.TextMuted)
}

// aiProperties is the selected node's panel, as Vibe-Workflow's Properties:
// header, run controls, basic config, instruction/inputs, params, and output actions.
func (b *Board) aiProperties(c *ui.Context, o BoardOptions) {
	e, n := b.selectedAINode()
	s := b.aiState()
	if e == nil || o.AI == nil {
		if s.edit != "" {
			b.flushAIEdit()
			s.edit = ""
		}
		return
	}
	if b.panel != "" || !s.panel {
		return
	}
	if s.edit != e.ID {
		b.flushAIEdit()
		s.edit = e.ID
		extra := ""
		if len(n.Params.Extra) > 0 {
			raw, _ := json.Marshal(n.Params.Extra)
			extra = string(raw)
		}
		b.ai.buf = aiEditBuffers{n.Title, n.Instruction, n.Note, n.Params.Template, extra}
		s.extraErr = ""
	}
	id := e.ID
	edit := func(fn func(*whiteboard.AINode)) { b.editAINode(o, id, fn) }
	ui.Scroll(c.Key("board-ai-props")).Absolute().Top(66).Right(12).Width(308).MaxHeight(max(150, b.lastRect.H-140)).Padding(14).Gap(8).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(10).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.12)).Role(ui.RoleGroup).Label(i18n.T("节点属性")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			aiItemBadge(c, aiRoleIcons[n.Role], n.Kind(), o.Theme.Dark)
			ui.Text(c, i18n.T(aiRoleNames[n.Role])).FontSize(15).FontWeight(600).Grow(1)
			if boardIconButton(c, "close", "close", i18n.T("关闭"), false).Size(28, 28).Clicked() {
				s.panel = false
			}
		})
		if n.Role != whiteboard.AIPrompt && n.Role != whiteboard.AIImageInput {
			b.aiRunControls(c, o, id)
		}
		aiPropDivider(c, o)
		aiPropSection(c, o, "基础设置")
		ui.Text(c, i18n.T("名称")).FontSize(12).TextColor(o.Theme.TextMuted)
		if ui.TextInput(c.Key("board-ai-title"), &b.ai.buf.title).Placeholder(i18n.T(aiRoleNames[n.Role])).WidthPercent(100).Changed() {
			edit(func(n *whiteboard.AINode) { n.Title = b.ai.buf.title })
		}
		if n.Role == whiteboard.AIText || n.Role == whiteboard.AIImage || n.Role == whiteboard.AIVideo {
			b.aiModelField(c, o, id, n)
		}
		aiPropDivider(c, o)
		switch n.Role {
		case whiteboard.AIImageInput:
			ui.Text(c, i18n.T("从右侧图片输出拖线到 AI 生图节点的参考图输入。")).FontSize(12).TextColor(o.Theme.TextMuted)
		case whiteboard.AIPrompt:
			aiPropSection(c, o, "文字内容")
			area := ui.TextArea(c.Key("board-ai-instruction"), &b.ai.buf.instruction).Placeholder(i18n.T("输入提示词或文本…")).WidthPercent(100).Height(140).Label(i18n.T("文字内容"))
			if s.focusInstruction {
				area.Focus()
				s.focusInstruction = false
			}
			if area.Changed() {
				b.setAINodeText(o, id, b.ai.buf.instruction)
			}
		case whiteboard.AINote:
			aiPropSection(c, o, "引用笔记")
			if ui.TextInput(c.Key("board-ai-note"), &b.ai.buf.note).Placeholder(i18n.T("文档名称")).WidthPercent(100).Changed() {
				edit(func(n *whiteboard.AINode) { n.Note = strings.Trim(strings.TrimSpace(b.ai.buf.note), "[]") })
			}
			if o.WikiSuggest != nil && strings.TrimSpace(b.ai.buf.note) != "" {
				for i, name := range o.WikiSuggest(strings.Trim(b.ai.buf.note, "[]")) {
					if i >= 5 {
						break
					}
					if ui.Button(c.Key("sug-"+name), name).Clicked() {
						b.ai.buf.note = name
						edit(func(n *whiteboard.AINode) { n.Note = name })
					}
				}
			}
			if n.Note != "" && o.OpenWiki != nil && ui.Button(c.Key("open-note"), i18n.T("打开笔记")).Clicked() {
				o.OpenWiki(n.Note, false)
			}
		case whiteboard.AIConcat:
			aiPropSection(c, o, "拼接模板")
			ui.Text(c, i18n.T("{1}、{2} 为输入，{all} 为全部")).FontSize(12).TextColor(o.Theme.TextMuted)
			if ui.TextArea(c.Key("board-ai-template"), &b.ai.buf.template).WidthPercent(100).Height(88).Changed() {
				edit(func(n *whiteboard.AINode) { n.Params.Template = b.ai.buf.template })
			}
		default:
			aiPropSection(c, o, "指令")
			value, placeholder := &b.ai.buf.instruction, i18n.T("输入指令或提示词…")
			linked := b.aiPromptSource(n)
			if linked != "" {
				// The linked slot feeds the instruction, so the field is
				// disabled and shows the source, as ComfyUI's widget inputs.
				value, placeholder = new(string), fmt.Sprintf(i18n.T("已连接：%s"), linked)
			}
			area := ui.TextArea(c.Key("board-ai-instruction"), value).Placeholder(placeholder).WidthPercent(100).Height(104).Disabled(linked != "" || b.ReadOnly).Label(i18n.T("指令")).Description(placeholder)
			if s.focusInstruction && linked == "" {
				area.Focus()
				s.focusInstruction = false
			}
			if area.Changed() {
				b.setAINodeText(o, id, b.ai.buf.instruction)
			}
		}
		if n.Role != whiteboard.AIPrompt && n.Role != whiteboard.AIImageInput {
			aiPropDivider(c, o)
			inputs := b.Scene.AIInputs(id)
			aiPropSection(c, o, fmt.Sprintf(i18n.T("输入（%d）"), len(inputs)))
			if len(inputs) == 0 && n.Role != whiteboard.AINote {
				ui.Text(c, i18n.T("从其他节点的输出拖线到左侧输入，或从输入拖线到便签、图片、画框。")).FontSize(12).TextColor(o.Theme.TextMuted)
			}
			for i, in := range inputs {
				sl, _ := whiteboard.AISlotByName(n, in.Slot)
				detail := b.aiSourceName(b.Scene.ElementByID(in.From))
				ui.Row(c.Key(fmt.Sprint("in", i))).Padding(6, 8).Gap(6).AlignItems(ui.Center).Background(o.Theme.Surface).Border(1, o.Theme.Border.Alpha(0.6)).Radius(6).Children(func() {
					ui.Text(c, aiSlotLabel(sl)).FontSize(12).FontWeight(500).TextColor(o.Theme.TextMuted).NoWrap()
					ui.Text(c, detail).FontSize(12).TextColor(o.Theme.Text).SingleLine().Grow(1).MinWidth(0)
					if !b.ReadOnly && boardIconButton(c, "disconnect", "close", i18n.T("断开连接"), false).Size(22, 22).Clicked() {
						b.deleteAILink(aiLinkRef{id, in.Slot}, o)
					}
				})
			}
		}
		b.aiParamsFields(c, o, id, n)
		b.aiOutputActions(c, o, id, n)
	})
}

func (b *Board) aiModelField(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode) {
	var models []BoardAIModel
	if o.AI.Models != nil {
		models = o.AI.Models(n.Role)
	}
	ui.Text(c, i18n.T("模型")).FontSize(12).TextColor(o.Theme.TextMuted)
	if len(models) == 0 {
		ui.Text(c, i18n.T("没有可用模型。请在「设置 → OneBot」添加供应商并勾选支持此类输出的模型。")).FontSize(12.5).TextColor(o.Theme.Danger)
		return
	}
	labels := make([]string, len(models))
	current := ""
	for i, m := range models {
		labels[i] = m.label()
		if m.Connection == n.Connection && m.Model == n.Model {
			current = labels[i]
		}
	}
	if current == "" && n.Model != "" {
		current = n.Model
	}
	before := current
	ui.Select(c.Key("board-ai-model"), &current, labels).WidthPercent(100).FontSize(13).Label(i18n.T("模型"))
	if current != before {
		for i, l := range labels {
			if l == current {
				m := models[i]
				b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Connection, n.Model = m.Connection, m.Model })
				if o.AI.Remember != nil {
					o.AI.Remember(n.Role, m)
				}
			}
		}
	}
}

func (b *Board) aiParamsFields(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode) {
	set := func(fn func(*whiteboard.AIParams)) {
		b.updateAINode(o, id, func(n *whiteboard.AINode) { fn(&n.Params) })
	}
	p := n.Params
	switch n.Role {
	case whiteboard.AIText:
		b.aiPresetRow(c, o, id)
		aiPropDivider(c, o)
		aiPropSection(c, o, "输出方式")
		if v := aiChoice(c, o, "write", "结果写到", p.Write, []string{"", "replace", "note"}, translated("仅节点内", "改写第一个文字输入", "节点旁新建便签")); v != p.Write {
			set(func(p *whiteboard.AIParams) { p.Write = v })
		}
	case whiteboard.AIImage:
		aiPropDivider(c, o)
		aiPropSection(c, o, "生成参数")
		ui.Row(c.Key("img-row-1")).Gap(8).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				if v := aiChoice(c, o, "size", "尺寸", p.Size, []string{"", "auto", "1024x1024", "1536x1024", "1024x1536", "1792x1024", "1024x1792"}, translated("服务默认", "自动", "1024×1024 方形", "1536×1024 横向", "1024×1536 纵向", "1792×1024 宽屏", "1024×1792 竖屏")); v != p.Size {
					set(func(p *whiteboard.AIParams) { p.Size = v })
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				if v := aiChoice(c, o, "aspect", "画幅比例", p.AspectRatio, []string{"", "1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}, []string{i18n.T("服务默认"), "1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}); v != p.AspectRatio {
					set(func(p *whiteboard.AIParams) { p.AspectRatio = v })
				}
			})
		})
		ui.Row(c.Key("img-row-2")).Gap(8).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				count := strconv.Itoa(max(1, p.Count))
				if v := aiChoice(c, o, "count", "数量", count, []string{"1", "2", "3", "4"}, []string{"1", "2", "3", "4"}); v != count {
					k, _ := strconv.Atoi(v)
					set(func(p *whiteboard.AIParams) { p.Count = k })
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				if v := aiChoice(c, o, "quality", "质量", p.Quality, []string{"", "low", "medium", "high", "standard", "hd"}, translated("服务默认", "低", "中", "高", "标准", "高清")); v != p.Quality {
					set(func(p *whiteboard.AIParams) { p.Quality = v })
				}
			})
		})
	case whiteboard.AIVideo:
		aiPropDivider(c, o)
		aiPropSection(c, o, "生成参数")
		ui.Row(c.Key("vid-row-1")).Gap(8).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				secs := strconv.Itoa(p.Seconds)
				if v := aiChoice(c, o, "seconds", "时长", secs, []string{"0", "4", "5", "8", "10", "12"}, translated("服务默认", "4 秒", "5 秒", "8 秒", "10 秒", "12 秒")); v != secs {
					k, _ := strconv.Atoi(v)
					set(func(p *whiteboard.AIParams) { p.Seconds = k })
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				if v := aiChoice(c, o, "vsize", "分辨率", p.Size, []string{"", "1280x720", "720x1280", "1792x1024", "1024x1792"}, translated("服务默认", "1280×720 横向", "720×1280 竖向", "1792×1024 横向高清", "1024×1792 竖向高清")); v != p.Size {
					set(func(p *whiteboard.AIParams) { p.Size = v })
				}
			})
		})
		if v := aiChoice(c, o, "vaspect", "画幅比例（Veo）", p.AspectRatio, []string{"", "16:9", "9:16"}, []string{i18n.T("服务默认"), "16:9", "9:16"}); v != p.AspectRatio {
			set(func(p *whiteboard.AIParams) { p.AspectRatio = v })
		}
	default:
		return
	}
	if n.Role == whiteboard.AIText {
		return
	}
	// Services add options of their own; they pass through as given, as
	// Vibe-Workflow renders each model's own input schema.
	ui.Text(c, i18n.T("附加参数（JSON，可选）")).FontSize(12).TextColor(o.Theme.TextMuted)
	if ui.TextInput(c.Key("board-ai-extra"), &b.ai.buf.extra).Placeholder(`{"style": "vivid"}`).WidthPercent(100).Changed() {
		raw := strings.TrimSpace(b.ai.buf.extra)
		var extra map[string]any
		b.ai.extraErr = ""
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &extra); err != nil {
				b.ai.extraErr = i18n.T("不是有效的 JSON 对象")
				return
			}
		}
		b.editAINode(o, id, func(n *whiteboard.AINode) { n.Params.Extra = extra })
	}
	if b.ai.extraErr != "" {
		ui.Text(c, b.ai.extraErr).FontSize(12).TextColor(o.Theme.Danger)
	}
}

func (b *Board) aiPresetRow(c *ui.Context, o BoardOptions, id string) {
	ui.Text(c, i18n.T("常用指令")).FontSize(12).TextColor(o.Theme.TextMuted)
	ui.Row(c.Key("board-ai-presets")).Gap(5).Wrap().Children(func() {
		for _, preset := range aiTextPresets {
			chip := ui.ButtonBase(c.Key("preset-"+preset.label)).Height(26).PaddingX(9).Center().Radius(13).Background(o.Theme.Surface).Border(1, o.Theme.Border).Focusable().Label(i18n.T(preset.label))
			if chip.Hovered() {
				chip.Background(o.Theme.SurfaceHover)
			}
			chip.Children(func() { ui.Text(c, i18n.T(preset.label)).FontSize(12).TextColor(o.Theme.Text).NoWrap() })
			if chip.Clicked() {
				b.ai.buf.instruction = i18n.T(preset.instruction)
				b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Instruction = b.ai.buf.instruction })
			}
		}
	})
}

func (b *Board) aiRunControls(c *ui.Context, o BoardOptions, id string) {
	ui.Column(c.Key("board-ai-run")).Gap(6).Margin(2, 0, 0, 0).Children(func() {
		if b.AIRunning() {
			if ui.Button(c.Key("stop"), i18n.T("停止")).WidthPercent(100).Clicked() {
				b.StopAI()
			}
			return
		}
		if ui.PrimaryButton(c.Key("run"), i18n.T("运行节点")).WidthPercent(100).Clicked() {
			b.RunAI([]string{id}, false, false, o)
		}
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c.Key("run-up"), i18n.T("连同上游运行")).Grow(1).Clicked() {
				b.RunAI([]string{id}, true, false, o)
			}
			if ui.Button(c.Key("force"), i18n.T("强制重新运行")).Grow(1).Clicked() {
				b.RunAI([]string{id}, false, true, o)
			}
		})
	})
}

func (b *Board) aiOutputActions(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode) {
	out := n.Output()
	if out == nil {
		return
	}
	aiPropDivider(c, o)
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		aiPropSection(c, o, fmt.Sprintf(i18n.T("结果 %d/%d"), n.Current+1, len(n.Outputs)))
		ui.Spacer(c)
		if len(n.Outputs) > 1 {
			if boardButton(c, "prop-prev", "‹", false).Height(26).MinWidth(26).Label(i18n.T("上一个结果")).Disabled(n.Current <= 0).Clicked() {
				b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Current-- })
			}
			if boardButton(c, "prop-next", "›", false).Height(26).MinWidth(26).Label(i18n.T("下一个结果")).Disabled(n.Current >= len(n.Outputs)-1).Clicked() {
				b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Current++ })
			}
		}
	})
	if out.Prompt != "" {
		ui.Text(c, i18n.T("服务改写的提示词：")+out.Prompt).FontSize(12).TextColor(o.Theme.TextMuted)
	}
	ui.Row(c.Key("board-ai-out")).Gap(6).Wrap().Children(func() {
		if out.Kind == "text" && ui.Button(c.Key("copy"), i18n.T("复制文字")).Clicked() {
			b.services.WriteClipboard(out.Text)
		}
		if (out.Kind == "text" || out.Kind == "image") && ui.Button(c.Key("place"), i18n.T("放到白板")).Clicked() {
			b.placeAIOutput(id, o)
		}
		if out.Path != "" && o.AI.OpenFile != nil && ui.Button(c.Key("open"), i18n.T("打开文件")).Clicked() {
			o.AI.OpenFile(out.Path)
		}
		if ui.Button(c.Key("remove"), i18n.T("移除此结果")).Tooltip(i18n.T("只从节点移除，文件保留在附件中")).Clicked() {
			i := n.Current
			b.updateAINode(o, id, func(n *whiteboard.AINode) { n.RemoveOutput(i) })
		}
	})
}

func (b *Board) aiInlineModelSelect(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, disabled bool) {
	var models []BoardAIModel
	if o.AI != nil && o.AI.Models != nil {
		models = o.AI.Models(n.Role)
	}
	if len(models) == 0 {
		ui.Text(c, i18n.T("未配置模型")).FontSize(11.5).TextColor(o.Theme.Danger).Grow(1).MinWidth(0).SingleLine()
		return
	}
	labels := make([]string, len(models))
	current := ""
	for i, m := range models {
		labels[i] = m.label()
		if m.Connection == n.Connection && m.Model == n.Model {
			current = labels[i]
		}
	}
	if current == "" {
		if n.Model != "" {
			current = n.Model
		} else {
			current = i18n.T("选择模型")
		}
	}
	before := current
	ui.Select(c.Key("inline-model-"+id), &current, labels).Grow(1).MinWidth(0).Height(28).FontSize(11.5).Disabled(disabled).Label(i18n.T("模型")).Tooltip(i18n.T("模型"))
	if current != before {
		for i, l := range labels {
			if l == current {
				m := models[i]
				b.updateAINode(o, id, func(node *whiteboard.AINode) { node.Connection, node.Model = m.Connection, m.Model })
				if o.AI.Remember != nil {
					o.AI.Remember(n.Role, m)
				}
			}
		}
	}
}

// aiNodeOverlay is the floating action & history bar above the selected node,
// and the player of its video.
func (b *Board) aiNodeOverlay(c *ui.Context, o BoardOptions) {
	s := b.aiState()
	e, n := b.selectedAINode()
	shown := ""
	if e != nil {
		x, y, w, h := b.canvasRect(e.X, e.Y, e.W, e.H)
		out := n.Output()
		{
			// Above the card, or below it where the toolbar would cover it,
			// so the bar never hides the card's title and slots.
			top := y - 44
			if top < 56 {
				top = y + h + 8
			}
			ui.Row(c.Key("board-ai-node-bar")).Absolute().Left(x).Top(top).Gap(4).Padding(4, 6).AlignItems(ui.Center).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(10).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.12)).Role(ui.RoleToolbar).Label(i18n.T("节点操作")).Children(func() {
				// Running is on the card; the bar holds what a card has no
				// room for, as ComfyUI's selection toolbox.
				if boardButton(c, "board-ai-settings", i18n.T("全部设置"), s.panel).Height(26).Label(i18n.T("全部设置")).Clicked() {
					s.panel = !s.panel
				}
				if !b.ReadOnly && n.Role != whiteboard.AIPrompt && n.Role != whiteboard.AINote && n.Role != whiteboard.AIImageInput && !b.AIRunning() {
					if boardButton(c, "board-ai-run-up", i18n.T("连同上游运行"), false).Height(26).Clicked() {
						b.RunAI([]string{e.ID}, true, false, o)
					}
				}
				if len(n.Outputs) > 1 {
					ui.Box(c).Width(1).Height(16).Margin(0, 2).Background(o.Theme.Border)
					if boardButton(c, "board-ai-prev", "‹", false).Height(26).MinWidth(26).Label(i18n.T("上一个结果")).Disabled(n.Current <= 0).Clicked() {
						b.updateAINode(o, e.ID, func(n *whiteboard.AINode) { n.Current-- })
					}
					ui.Text(c, fmt.Sprintf("%d/%d", n.Current+1, len(n.Outputs))).FontSize(12).TextColor(o.Theme.TextMuted).PaddingX(2)
					if boardButton(c, "board-ai-next", "›", false).Height(26).MinWidth(26).Label(i18n.T("下一个结果")).Disabled(n.Current >= len(n.Outputs)-1).Clicked() {
						b.updateAINode(o, e.ID, func(n *whiteboard.AINode) { n.Current++ })
					}
				}
				if out != nil && (out.Kind == "text" || out.Kind == "image") {
					ui.Box(c).Width(1).Height(16).Margin(0, 2).Background(o.Theme.Border)
					if out.Kind == "text" && boardButton(c, "board-ai-copy", i18n.T("复制"), false).Height(26).Label(i18n.T("复制文字")).Clicked() {
						b.services.WriteClipboard(out.Text)
					}
					if !b.ReadOnly && boardButton(c, "board-ai-place", i18n.T("放到白板"), false).Height(26).Clicked() {
						b.placeAIOutput(e.ID, o)
					}
				}
			})
		}
		if out != nil && out.Kind == "video" && o.AI != nil && o.AI.OpenVideo != nil && w > 60 && h > 60 {
			player := s.videos[out.Path]
			if player == nil {
				var err error
				player, err = o.AI.OpenVideo(out.Path)
				if err != nil {
					b.err = err
				} else {
					s.videos[out.Path] = player
				}
			}
			if player != nil {
				shown = out.Path
				r := b.aiLayout(e, n).preview
				z := float32(b.Zoom)
				bx, by, bw, bh := x+r.X*z, y+r.Y*z, r.W*z, r.H*z
				if bh > 24 && bw > 24 {
					v := ui.Video(c.Key("board-ai-video"), player).Absolute().Left(bx).Top(by).Size(bw, bh).Fit(ui.Contain).Label(i18n.T("播放或暂停视频"))
					if v.Clicked() {
						if player.Playing() {
							player.Pause()
						} else {
							player.Play()
						}
					}
				}
			}
		}
	}
	for rel, player := range s.videos {
		if rel != shown && player.Playing() {
			player.Pause()
		}
	}
}

// aiSelectionBar offers to build a node from the selected elements.
func (b *Board) aiSelectionBar(c *ui.Context, o BoardOptions) {
	if o.AI == nil || b.ReadOnly || b.mode != "" || b.editing != nil || b.multiLine {
		return
	}
	if e, _ := b.selectedAINode(); e != nil {
		return
	}
	inputs := b.aiSelectionInputs()
	if len(inputs) == 0 {
		return
	}
	for _, e := range inputs {
		if b.aiNode(e) != nil {
			return
		}
	}
	box := whiteboard.SelectionBounds(b.Scene, b.SelectedIDs())
	x, y, _, h := b.canvasRect(box.X, box.Y, box.W, box.H)
	top := y - 46
	if top < 60 {
		top = y + h + 10
	}
	ui.Row(c.Key("board-ai-selection")).Absolute().Left(max(8, x)).Top(top).Gap(4).Padding(4, 6).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(10).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.12)).Role(ui.RoleToolbar).Label(i18n.T("用选中内容运行 AI")).Children(func() {
		if len(b.selectedImagesForNodes()) > 0 && boardButton(c, "board-ai-convert-image", i18n.T("转为节点"), false).Height(28).Clicked() {
			b.convertImagesToNodes(o)
		}
		for _, a := range []struct {
			role  whiteboard.AIRole
			label string
		}{{whiteboard.AIText, "AI 文本"}, {whiteboard.AIImage, "AI 生图"}, {whiteboard.AIVideo, "AI 视频"}} {
			if boardButton(c, "board-ai-sel-"+string(a.role), i18n.T(a.label), false).Height(28).Clicked() {
				if b.AIFromSelection(a.role, o) != "" {
					b.ai.focusInstruction = true
				}
			}
		}
	})
}

// aiContextMenu adds the AI actions to the board's context menu.
func (b *Board) aiContextMenu(m *ui.Menu, o BoardOptions) {
	if o.AI == nil || b.ReadOnly {
		return
	}
	m.Separator()
	if len(b.selectedImagesForNodes()) > 0 && m.Item(i18n.T("转为节点")).Chosen() {
		b.convertImagesToNodes(o)
	}
	if ref := b.ai.link; ref != nil && m.Item(i18n.T("删除连线")).Chosen() {
		b.deleteAILink(*ref, o)
	}
	if len(b.aiSelectionInputs()) > 0 {
		for _, a := range []struct {
			role  whiteboard.AIRole
			label string
		}{{whiteboard.AIText, "OneBot 文本编辑或汇总选中内容"}, {whiteboard.AIImage, "OneBot 用选中内容生成或编辑图像"}, {whiteboard.AIVideo, "OneBot 用选中内容生成视频"}} {
			if m.Item(i18n.T(a.label)).Chosen() {
				if b.AIFromSelection(a.role, o) != "" {
					b.ai.focusInstruction = true
				}
			}
		}
	}
	var nodes []string
	for _, id := range b.SelectedIDs() {
		if b.aiNode(b.Scene.ElementByID(id)) != nil {
			nodes = append(nodes, id)
		}
	}
	if len(nodes) > 0 && m.Item(i18n.T("运行所选 AI 节点")).Chosen() {
		b.RunAI(nodes, false, false, o)
	}
	if m.Item(i18n.T("运行全部 AI 节点")).Chosen() {
		b.RunAI(nil, false, false, o)
	}
	if m.Item(i18n.T("AI 节点栏")).Chosen() {
		b.ai.bar = true
	}
}

// aiPortCursor reports a hover over a node's slot or a link, or a link
// being dragged.
func (b *Board) aiPortCursor() bool {
	if b.ai.drag != nil {
		return true
	}
	if b.Tool != ToolSelect || b.mode != "" || b.ReadOnly || b.options.AI == nil {
		return false
	}
	id, _, _ := b.aiSlotHit(b.hover)
	return id != ""
}
