package editor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"path"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// Card measurements, in scene units.
const (
	aiHeader = 34
	aiFooter = 24
	aiPort   = 6
)

var aiStatusNames = map[string]string{"queued": "排队中", "running": "生成中…", "done": "已完成", "cached": "输入未变化，沿用上次结果", "blocked": "上游节点失败，未运行", "stopped": "已停止"}

// aiStatus is the card's status line and whether it reports a failure.
func (b *Board) aiStatus(e *whiteboard.Element, n *whiteboard.AINode) (string, bool) {
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
		return fmt.Sprintf(i18n.T("结果 %d/%d"), n.Current+1, len(n.Outputs)), false
	}
	return i18n.T("未运行"), false
}

func (b *Board) aiCardText(id, text string, width float32, size float32) *ui.TextLayout {
	s := b.aiState()
	if len(text) > 6000 {
		text = text[:6000] + "…"
	}
	key := fmt.Sprintf("%s\x00%.0f\x00%.0f", text, width, size)
	if c, ok := s.texts[id]; ok && c.key == key {
		return c.layout
	}
	layout := ui.ShapeText(text, ui.Font{Size: size}, width)
	s.texts[id] = boardAICardText{key, layout}
	return layout
}

// paintAINode draws a node as a card in the element's local coordinates:
// a header with its type and model, its current result, a status line and
// typed ports, after Vibe-Workflow's nodes.
func (b *Board) paintAINode(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, dark bool) {
	s := b.aiState()
	w, h := float32(e.W), float32(e.H)
	kind := n.Kind()
	accent := boardColor(aiKindColors[kind], dark)
	ink, muted := boardColor("#1e1e1e", dark), boardColor("#6c757d", dark)
	card := ui.Rect{W: w, H: h}
	run := s.runs[e.ID]
	border := boardColor("#adb5bd", dark)
	status, failed := b.aiStatus(e, n)
	if run != nil && (run.status == "running" || run.status == "queued") {
		border = accent
	} else if failed {
		border = boardColor("#e03131", dark)
	}
	p.Fill(card, boardColor("#ffffff", dark), 10)
	p.Clip(card, 10, func() {
		p.Fill(ui.Rect{W: w, H: aiHeader}, boardColor("#f1f3f5", dark), 0)
		p.Fill(ui.Rect{X: 0, Y: 0, W: 4, H: h}, accent, 0)
		title := cmpOr(n.Title, i18n.T(aiRoleNames[n.Role]))
		model := n.Model
		if n.Role == whiteboard.AINote {
			model = ""
		}
		var mw float32
		if model != "" {
			layout := b.aiCardText(e.ID+"#model", model, 0, 12)
			mw, _ = layout.Size()
			mw = min(mw, w*.45)
			p.Clip(ui.Rect{X: w - 12 - mw, Y: 0, W: mw, H: aiHeader}, 0, func() { p.TextLayout(layout, w-12-mw, 10, muted) })
		}
		p.Clip(ui.Rect{X: 14, Y: 0, W: max(0, w-mw-34), H: aiHeader}, 0, func() { p.Text(14, 9, title, 14, ink) })
		body := ui.Rect{X: 12, Y: aiHeader + 8, W: w - 24, H: h - aiHeader - aiFooter - 12}
		p.Clip(body, 4, func() { b.paintAIBody(p, e, n, body, dark) })
		color := muted
		if failed {
			color = boardColor("#c92a2a", dark)
		}
		p.Clip(ui.Rect{X: 12, Y: h - aiFooter, W: w - 24, H: aiFooter}, 0, func() { p.Text(12, h-aiFooter+4, status, 12, color) })
	})
	p.Stroke(card, border, 10, 1.5)
	if n.Role != whiteboard.AINote {
		in := ui.Rect{X: -aiPort, Y: h/2 - aiPort, W: 2 * aiPort, H: 2 * aiPort}
		p.Fill(in, boardColor("#ffffff", dark), aiPort)
		p.Stroke(in, muted, aiPort, 1.5)
	}
	out := ui.Rect{X: w - aiPort, Y: h/2 - aiPort, W: 2 * aiPort, H: 2 * aiPort}
	p.Fill(out, accent, aiPort)
	p.Stroke(out, boardColor("#ffffff", dark), aiPort, 1.5)
}

func (b *Board) paintAIBody(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, r ui.Rect, dark bool) {
	s := b.aiState()
	ink, muted := boardColor("#343a40", dark), boardColor("#868e96", dark)
	text := func(t string, color ui.Color) {
		p.TextLayout(b.aiCardText(e.ID, t, r.W, 13.5), r.X, r.Y, color)
	}
	if run := s.runs[e.ID]; run != nil && run.status == "running" && run.stream != "" {
		stream := run.stream
		if len(stream) > 4000 {
			stream = "…" + stream[len(stream)-4000:]
		}
		text(stream, ink)
		return
	}
	out := n.Output()
	if out == nil {
		hint := i18n.T("用箭头把文字、图片、画框或其他节点连到左侧端口，然后运行。")
		switch n.Role {
		case whiteboard.AINote:
			hint = i18n.T("在属性面板中填写要引用的笔记。")
			if n.Note != "" {
				hint = "[[" + n.Note + "]]"
			}
		case whiteboard.AIConcat:
			hint = i18n.T("模板：") + n.Params.Template
		}
		if n.Instruction != "" {
			hint = n.Instruction
		}
		text(hint, muted)
		return
	}
	switch out.Kind {
	case "text":
		text(out.Text, ink)
	case "image":
		bm := s.media[out.Path]
		if bm == nil {
			msg := i18n.T("正在加载图片")
			if s.mediaErr[out.Path] != nil {
				msg = i18n.T("图片无法读取：") + out.Path
			}
			text(msg, muted)
			return
		}
		iw, ih := bm.Size()
		scale := min(r.W/float32(iw), r.H/float32(ih))
		dw, dh := float32(iw)*scale, float32(ih)*scale
		p.Fill(r, boardColor("#f8f9fa", dark), 4)
		p.ImageRegion(bm, ui.Rect{}, ui.Rect{X: r.X + (r.W-dw)/2, Y: r.Y + (r.H-dh)/2, W: dw, H: dh}, ui.ImageOptions{})
	case "video":
		p.Fill(r, boardColor("#212529", dark), 4)
		label := "▶  " + path.Base(out.Path)
		p.Text(r.X+12, r.Y+r.H/2-8, label, 13, boardColor("#f8f9fa", dark))
		p.Text(r.X+12, r.Y+r.H/2+12, i18n.T("选中节点后播放"), 12, boardColor("#adb5bd", dark))
	}
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

// aiPortAt is the node whose output port is at a scene point.
func (b *Board) aiPortAt(p whiteboard.Point) string {
	for i := len(b.Scene.Elements) - 1; i >= 0; i-- {
		e := b.Scene.Elements[i]
		if e.IsDeleted || e.Locked || b.aiNode(e) == nil {
			continue
		}
		if whiteboard.Distance(p, whiteboard.Point{e.X + e.W, e.Y + e.H/2}) <= (aiPort+4)/b.Zoom+2 {
			return e.ID
		}
	}
	return ""
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
	if boardIconButton(c, "board-ai", "sparkles", i18n.T("AI 节点"), b.ai.bar).Clicked() {
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
	key, label, group string
	run               func()
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
		{"note", "便签文字", "输入", func() {
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
		{"picture", "插入图片", "输入", func() {
			if o.PickImage != nil {
				o.PickImage()
			}
		}},
		{"frame", "标注画框", "输入", func() {
			at := b.viewCenter()
			b.edit(o, func() {
				f := whiteboard.NewElement(whiteboard.KindFrame, at[0]-170, at[1]-160, 340, 320)
				f.Name = i18n.T("标注区：放入底图并圈画")
				b.Scene.Elements = append([]*whiteboard.Element{f}, b.Scene.Elements...)
				b.selectIDs([]string{f.ID})
			})
		}},
		{"ai-note", "笔记引用", "输入", add(whiteboard.AINote, "")},
		{"ai-text", "文本生成", "文本", add(whiteboard.AIText, "")},
		{"ai-concat", "提示词拼接", "文本", add(whiteboard.AIConcat, "")},
		{"ai-image", "生成图像", "图像", add(whiteboard.AIImage, "")},
		{"ai-edit", "编辑图像", "图像", add(whiteboard.AIImage, "编辑图像")},
		{"ai-video", "文生视频", "视频", add(whiteboard.AIVideo, "")},
		{"ai-i2v", "图生视频", "视频", add(whiteboard.AIVideo, "图生视频")},
	}
	for _, t := range aiTemplates {
		items = append(items, aiBarItem{"tpl-" + t.key, t.label, "模板", func() { b.insertAITemplate(t.key, o) }})
	}
	if o.AI.ImportCanvas != nil {
		items = append(items, aiBarItem{"canvas-import", "导入 JSON Canvas…", "交换", func() {
			o.AI.ImportCanvas(func(data []byte, err error) {
				if err == nil {
					err = b.ImportCanvas(data, o)
				}
				b.err = err
			})
		}})
	}
	if o.Export != nil {
		items = append(items, aiBarItem{"canvas-export", "导出 JSON Canvas…", "交换", func() { o.Export("canvas") }})
	}
	ui.Scroll(c.Key("board-ai-bar")).Absolute().Top(66).Left(12).Width(220).MaxHeight(max(120, b.lastRect.H-132)).Padding(10).Gap(4).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(8).Role(ui.RoleGroup).Label(i18n.T("AI 节点")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("AI 节点")).FontSize(14).FontWeight(600).Grow(1)
			if ui.Button(c.Key("close"), "×").Label(i18n.T("关闭")).Clicked() {
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
				ui.Text(c.Key("g-"+group), i18n.T(group)).FontSize(12).TextColor(o.Theme.TextMuted).Margin(6, 2, 0, 2)
			}
			row := ui.ButtonBase(c.Key("board-ai-item-"+it.key)).WidthPercent(100).Padding(7, 10).Radius(5).Focusable().Label(label)
			if row.Hovered() {
				row.Background(o.Theme.SurfaceHover)
			}
			row.Children(func() { ui.Text(c, label).FontSize(13).TextColor(o.Theme.Text) })
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

// aiProperties is the selected node's panel, as Vibe-Workflow's Properties:
// model, instruction, options, inputs, run controls and the result.
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
	if b.panel != "" {
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
	ui.Scroll(c.Key("board-ai-props")).Absolute().Top(66).Right(12).Width(320).MaxHeight(max(150, b.lastRect.H-140)).Padding(14).Gap(8).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(8).Role(ui.RoleGroup).Label(i18n.T("节点属性")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T(aiRoleNames[n.Role])).FontSize(15).FontWeight(600).Grow(1)
			if ui.Button(c.Key("close"), "×").Label(i18n.T("关闭")).Clicked() {
				b.Selected = map[string]bool{}
			}
		})
		ui.Text(c, i18n.T("名称")).FontSize(12).TextColor(o.Theme.TextMuted)
		if ui.TextInput(c.Key("board-ai-title"), &b.ai.buf.title).Placeholder(i18n.T(aiRoleNames[n.Role])).WidthPercent(100).Changed() {
			edit(func(n *whiteboard.AINode) { n.Title = b.ai.buf.title })
		}
		if n.Role == whiteboard.AIText || n.Role == whiteboard.AIImage || n.Role == whiteboard.AIVideo {
			b.aiModelField(c, o, id, n)
		}
		switch n.Role {
		case whiteboard.AINote:
			ui.Text(c, i18n.T("引用的笔记")).FontSize(12).TextColor(o.Theme.TextMuted)
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
			ui.Text(c, i18n.T("拼接模板（{1}、{2} 为输入，{all} 为全部）")).FontSize(12).TextColor(o.Theme.TextMuted)
			if ui.TextArea(c.Key("board-ai-template"), &b.ai.buf.template).WidthPercent(100).Height(80).Changed() {
				edit(func(n *whiteboard.AINode) { n.Params.Template = b.ai.buf.template })
			}
		default:
			ui.Text(c, i18n.T("指令")).FontSize(12).TextColor(o.Theme.TextMuted)
			area := ui.TextArea(c.Key("board-ai-instruction"), &b.ai.buf.instruction).WidthPercent(100).Height(110).Label(i18n.T("指令"))
			if s.focusInstruction {
				area.Focus()
				s.focusInstruction = false
			}
			if area.Changed() {
				edit(func(n *whiteboard.AINode) { n.Instruction = b.ai.buf.instruction })
			}
		}
		b.aiParamsFields(c, o, id, n)
		inputs := b.Scene.AIInputs(id)
		ui.Text(c, fmt.Sprintf(i18n.T("输入（%d）"), len(inputs))).FontSize(12).TextColor(o.Theme.TextMuted).Margin(4, 0, 0, 0)
		if len(inputs) == 0 && n.Role != whiteboard.AINote {
			ui.Text(c, i18n.T("从文字、图片、画框或其他节点拉箭头到此节点。")).FontSize(12.5).TextColor(o.Theme.TextMuted)
		}
		kinds := map[string]string{"text": "文字", "image": "图片", "frame": "画框", "node": "节点", "note": "笔记"}
		for i, in := range inputs {
			detail := in.Text
			switch in.Kind {
			case "node":
				if up := b.aiNode(b.Scene.ElementByID(in.From)); up != nil {
					detail = cmpOr(up.Title, i18n.T(aiRoleNames[up.Role]))
				}
			case "note":
				detail = "[[" + in.Note + "]]"
			case "image":
				detail = ""
			}
			detail = strings.ReplaceAll(detail, "\n", " ")
			if r := []rune(detail); len(r) > 40 {
				detail = string(r[:40]) + "…"
			}
			line := fmt.Sprintf("%d. %s", i+1, i18n.T(kinds[in.Kind]))
			if in.Mask {
				line += " · " + i18n.T("遮罩")
			} else if in.Label != "" {
				line += " · " + in.Label
			}
			if detail != "" {
				line += "：" + detail
			}
			ui.Text(c.Key(fmt.Sprint("in", i)), line).FontSize(12.5).TextColor(o.Theme.Text)
		}
		b.aiRunControls(c, o, id)
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
		if v := aiChoice(c, o, "write", "结果写到", p.Write, []string{"", "replace", "note"}, translated("仅节点内", "改写第一个文字输入", "节点旁新建便签")); v != p.Write {
			set(func(p *whiteboard.AIParams) { p.Write = v })
		}
	case whiteboard.AIImage:
		if v := aiChoice(c, o, "size", "尺寸", p.Size, []string{"", "auto", "1024x1024", "1536x1024", "1024x1536", "1792x1024", "1024x1792"}, translated("服务默认", "自动", "1024×1024 方形", "1536×1024 横向", "1024×1536 纵向", "1792×1024 宽屏", "1024×1792 竖屏")); v != p.Size {
			set(func(p *whiteboard.AIParams) { p.Size = v })
		}
		if v := aiChoice(c, o, "aspect", "画幅比例（Gemini、OpenRouter）", p.AspectRatio, []string{"", "1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}, []string{i18n.T("服务默认"), "1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}); v != p.AspectRatio {
			set(func(p *whiteboard.AIParams) { p.AspectRatio = v })
		}
		count := strconv.Itoa(max(1, p.Count))
		if v := aiChoice(c, o, "count", "数量", count, []string{"1", "2", "3", "4"}, []string{"1", "2", "3", "4"}); v != count {
			k, _ := strconv.Atoi(v)
			set(func(p *whiteboard.AIParams) { p.Count = k })
		}
		if v := aiChoice(c, o, "quality", "质量", p.Quality, []string{"", "low", "medium", "high", "standard", "hd"}, translated("服务默认", "低", "中", "高", "标准", "高清")); v != p.Quality {
			set(func(p *whiteboard.AIParams) { p.Quality = v })
		}
	case whiteboard.AIVideo:
		secs := strconv.Itoa(p.Seconds)
		if v := aiChoice(c, o, "seconds", "时长", secs, []string{"0", "4", "5", "8", "10", "12"}, translated("服务默认", "4 秒", "5 秒", "8 秒", "10 秒", "12 秒")); v != secs {
			k, _ := strconv.Atoi(v)
			set(func(p *whiteboard.AIParams) { p.Seconds = k })
		}
		if v := aiChoice(c, o, "vsize", "分辨率", p.Size, []string{"", "1280x720", "720x1280", "1792x1024", "1024x1792"}, translated("服务默认", "1280×720 横向", "720×1280 竖向", "1792×1024 横向高清", "1024×1792 竖向高清")); v != p.Size {
			set(func(p *whiteboard.AIParams) { p.Size = v })
		}
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
	ui.Row(c.Key("board-ai-presets")).Gap(4).Wrap().Children(func() {
		for _, preset := range aiTextPresets {
			if boardButton(c, "preset-"+preset.label, i18n.T(preset.label), false).Height(28).Clicked() {
				b.ai.buf.instruction = i18n.T(preset.instruction)
				b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Instruction = b.ai.buf.instruction })
			}
		}
	})
}

func (b *Board) aiRunControls(c *ui.Context, o BoardOptions, id string) {
	ui.Row(c.Key("board-ai-run")).Gap(6).Wrap().Margin(6, 0, 0, 0).Children(func() {
		if b.AIRunning() {
			if ui.Button(c.Key("stop"), i18n.T("停止")).Clicked() {
				b.StopAI()
			}
			return
		}
		if ui.PrimaryButton(c.Key("run"), i18n.T("运行节点")).Clicked() {
			b.RunAI([]string{id}, false, false, o)
		}
		if ui.Button(c.Key("run-up"), i18n.T("连同上游运行")).Clicked() {
			b.RunAI([]string{id}, true, false, o)
		}
		if ui.Button(c.Key("force"), i18n.T("强制重新运行")).Clicked() {
			b.RunAI([]string{id}, false, true, o)
		}
	})
}

func (b *Board) aiOutputActions(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode) {
	out := n.Output()
	if out == nil {
		return
	}
	ui.Text(c, fmt.Sprintf(i18n.T("结果 %d/%d"), n.Current+1, len(n.Outputs))).FontSize(12).TextColor(o.Theme.TextMuted).Margin(6, 0, 0, 0)
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

// aiNodeOverlay is the floating history bar above the selected node, as
// Vibe-Workflow shows it, and the player of its video.
func (b *Board) aiNodeOverlay(c *ui.Context, o BoardOptions) {
	s := b.aiState()
	e, n := b.selectedAINode()
	shown := ""
	if e != nil && b.mode == "" {
		x, y, w, h := b.canvasRect(e.X, e.Y, e.W, e.H)
		ui.Row(c.Key("board-ai-node-bar")).Absolute().Left(x).Top(max(56, y-42)).Gap(2).Padding(3).AlignItems(ui.Center).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(16).Children(func() {
			if !b.ReadOnly && !b.AIRunning() && boardIconButton(c, "board-ai-node-run", "player-play", i18n.T("运行节点"), false).Size(28, 28).Clicked() {
				b.RunAI([]string{e.ID}, false, false, o)
			}
			if len(n.Outputs) > 1 {
				if boardButton(c, "board-ai-prev", "‹", false).Height(28).Label(i18n.T("上一个结果")).Disabled(n.Current <= 0).Clicked() {
					b.updateAINode(o, e.ID, func(n *whiteboard.AINode) { n.Current-- })
				}
				ui.Text(c, fmt.Sprintf("%d/%d", n.Current+1, len(n.Outputs))).FontSize(12).TextColor(o.Theme.TextMuted)
				if boardButton(c, "board-ai-next", "›", false).Height(28).Label(i18n.T("下一个结果")).Disabled(n.Current >= len(n.Outputs)-1).Clicked() {
					b.updateAINode(o, e.ID, func(n *whiteboard.AINode) { n.Current++ })
				}
			}
		})
		if out := n.Output(); out != nil && out.Kind == "video" && o.AI != nil && o.AI.OpenVideo != nil && w > 60 && h > 60 {
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
				bx, by := x+12*float32(b.Zoom), y+(aiHeader+8)*float32(b.Zoom)
				bw, bh := w-24*float32(b.Zoom), h-(aiHeader+aiFooter+12)*float32(b.Zoom)
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
	top := y - 44
	if top < 60 {
		top = y + h + 10
	}
	ui.Row(c.Key("board-ai-selection")).Absolute().Left(max(8, x)).Top(top).Gap(2).Padding(3).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(8).Role(ui.RoleToolbar).Label(i18n.T("用选中内容运行 AI")).Children(func() {
		for _, a := range []struct {
			role  whiteboard.AIRole
			label string
		}{{whiteboard.AIText, "AI 文本"}, {whiteboard.AIImage, "AI 生图"}, {whiteboard.AIVideo, "AI 视频"}} {
			if boardButton(c, "board-ai-sel-"+string(a.role), i18n.T(a.label), false).Height(30).Clicked() {
				if b.AIFromSelection(a.role, o) != "" {
					b.ai.focusInstruction = true
				}
			}
		}
	})
}

// aiPicker offers node types where a line from an output port was let go.
func (b *Board) aiPicker(c *ui.Context, o BoardOptions) {
	s := b.aiState()
	if s.pickerAt == nil || o.AI == nil {
		return
	}
	from := b.Scene.ElementByID(s.pickerFrom)
	if from == nil {
		s.pickerAt = nil
		return
	}
	at := *s.pickerAt
	x, y, _, _ := b.canvasRect(at[0], at[1], 0, 0)
	kind := b.aiOutputKind(from)
	roles := []whiteboard.AIRole{whiteboard.AIText, whiteboard.AIImage, whiteboard.AIVideo, whiteboard.AIConcat}
	if kind != "text" {
		roles = []whiteboard.AIRole{whiteboard.AIImage, whiteboard.AIVideo, whiteboard.AIText}
	}
	ui.Column(c.Key("board-ai-picker")).Absolute().Left(x+8).Top(y).Width(180).Padding(6).Gap(2).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(8).Role(ui.RoleMenu).Label(i18n.T("接入新节点")).Children(func() {
		ui.Text(c, i18n.T("接入新节点")).FontSize(12).TextColor(o.Theme.TextMuted).Margin(2, 6)
		for _, role := range roles {
			label := i18n.T(aiRoleNames[role])
			row := ui.ButtonBase(c.Key("pick-"+string(role))).WidthPercent(100).Padding(7, 10).Radius(5).Focusable().Label(label)
			if row.Hovered() {
				row.Background(o.Theme.SurfaceHover)
			}
			row.Children(func() { ui.Text(c, label).FontSize(13).TextColor(o.Theme.Text) })
			if row.Clicked() {
				b.removeAIPickerLine(o)
				w, h := whiteboard.AINodeSize(role)
				id := b.AddAINode(role, whiteboard.Point{at[0] + w/2, at[1] + h/2 - h/2 + 0}, o)
				if node := b.Scene.ElementByID(id); node != nil {
					node.Y = at[1] - h/2
					b.edit(o, func() { b.connectAI(from, node) })
				}
				s.pickerAt = nil
			}
		}
		if boardButton(c, "pick-cancel", i18n.T("取消"), false).Height(28).Clicked() {
			b.removeAIPickerLine(o)
			s.pickerAt = nil
		}
	})
}

// removeAIPickerLine drops the loose line the picker was opened for.
func (b *Board) removeAIPickerLine(o BoardOptions) {
	if id := b.ai.pickerLine; id != "" {
		b.ai.pickerLine = ""
		if e := b.Scene.ElementByID(id); e != nil && e.EndBinding == nil {
			b.edit(o, func() { b.Scene.Delete([]string{id}); b.cleanSelection() })
		}
	}
}

// aiContextMenu adds the AI actions to the board's context menu.
func (b *Board) aiContextMenu(m *ui.Menu, o BoardOptions) {
	if o.AI == nil || b.ReadOnly {
		return
	}
	m.Separator()
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

// aiPortCursor reports a hover over an output port, where dragging draws
// a data line.
func (b *Board) aiPortCursor() bool {
	return b.Tool == ToolSelect && b.mode == "" && !b.ReadOnly && b.aiPortAt(b.hover) != ""
}
