package editor

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

var (
	aiStatusNames = map[string]string{"queued": "排队中", "running": "生成中", "done": "已完成", "cached": "输入未变化，沿用上次结果", "blocked": "上游节点失败，未运行", "stopped": "已停止"}
	aiRoleIcons   = map[whiteboard.AIRole]string{whiteboard.AIPrompt: "text", whiteboard.AIImageInput: "image", whiteboard.AIText: "text", whiteboard.AIImage: "image", whiteboard.AIEnhance: "image", whiteboard.AIModel: "image", whiteboard.AILoRA: "sparkles", whiteboard.AIVideo: "player-play", whiteboard.AIAudio: "player-play", whiteboard.AIConcat: "group", whiteboard.AINote: "sticky-note", whiteboard.AICutout: "image", whiteboard.AIUpscale: "image", whiteboard.AICompare: "image"}
)

// aiStatus is the card's status line and whether it reports a failure.
func (b *Board) aiStatus(e *whiteboard.Element, n *whiteboard.AINode) (string, bool) {
	if n.Role == whiteboard.AIModel || n.Role == whiteboard.AILoRA {
		if _, err := b.Scene.AIModelFlow(e.ID); err != nil {
			return i18n.T(err.Error()), true
		}
		return i18n.T("随下游节点运行"), false
	}
	if n.Role == whiteboard.AICompare {
		if b.aiCompareInputAt(n, "a").ok && b.aiCompareInputAt(n, "b").ok {
			return i18n.T("对比中"), false
		}
		return i18n.T("待连接图片"), false
	}
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
				return i18n.T("生成中") + " " + r.progress, false
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
	if c, ok := s.texts[id]; ok && c.text == text && c.width == width && c.font == font {
		return c.layout
	}
	layout := ui.ShapeText(text, font, width)
	s.texts[id] = boardAICardText{text: text, width: width, font: font, layout: layout}
	return layout
}

// aiPrepareMedia loads the pictures the cards show, off the main thread.
func (b *Board) aiPrepareMedia(o BoardOptions) {
	if o.AI == nil || o.AI.ReadFile == nil {
		return
	}
	s := b.aiState()
	if s.mediaWanted == nil {
		s.mediaWanted = map[string]bool{}
	}
	needed := s.mediaWanted
	clear(needed)
	for _, e := range b.Scene.Elements {
		if e == nil || e.IsDeleted || len(e.CustomData) == 0 || !b.mediaVisible(e) {
			continue
		}
		n := b.aiNode(e)
		if n == nil || e.IsDeleted {
			continue
		}
		for _, rel := range b.aiPreviewPaths(e, n) {
			needed[rel] = true
			if s.media[rel] != nil || s.mediaErr[rel] != nil || s.mediaPending[rel] {
				continue
			}
			if o.Dispatch != nil && b.imageLoads >= boardImageLoadLimit {
				continue
			}
			s.mediaPending[rel] = true
			read := o.AI.ReadFile
			load := func() (*ui.Bitmap, error) {
				data, err := read(rel)
				if err != nil {
					return nil, err
				}
				return decodeBoardBitmap(data)
			}
			apply := func(bm *ui.Bitmap, err error) {
				if b.ai.mediaPending == nil {
					return
				}
				delete(s.mediaPending, rel)
				if !s.mediaWanted[rel] {
					return
				}
				if err != nil {
					s.mediaErr[rel] = err
				} else {
					s.media[rel] = bm
				}
			}
			if !b.loadBitmap(o, load, apply) {
				delete(s.mediaPending, rel)
			}
		}
	}
	// History stores lightweight references. Retain decoded images only
	// while a node displays them, so browsing does not retain every bitmap.
	for rel := range s.media {
		if !needed[rel] {
			delete(s.media, rel)
		}
	}
	for rel := range s.mediaErr {
		if !needed[rel] {
			delete(s.mediaErr, rel)
		}
	}
	for _, entry := range s.compareDiffs {
		entry.wanted = false
	}
	for _, e := range b.Scene.Elements {
		if e == nil || e.IsDeleted || len(e.CustomData) == 0 || !b.mediaVisible(e) {
			continue
		}
		if n := b.aiNode(e); n != nil && n.Role == whiteboard.AICompare {
			b.prepareCompareMedia(o, e, n, needed)
		}
	}
	for key, entry := range s.compareDiffs {
		if !entry.wanted && !entry.pending {
			delete(s.compareDiffs, key)
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
	st := boardChromeFor(o.Theme)
	boardToolbarDivider(c, "board-ai-sep", st)
	aiBtn := ui.ButtonBase(c.Key("board-ai")).Height(32).PaddingX(9).Gap(5).Center().Radius(7).Focusable().Label(i18n.T("AI 节点")).Tooltip(i18n.T("AI 节点"))
	aiInk := st.text
	if b.ai.bar {
		aiBtn.Background(st.activeBg)
		aiInk = st.activeInk
	} else if aiBtn.Hovered() {
		aiBtn.Background(st.hover)
	}
	aiBtn.Children(func() {
		if icon := boardIcon("sparkles"); icon != nil {
			ui.Icon(c, icon).FontSize(16).TextColor(aiInk)
		}
		ui.Text(c, "AI").FontSize(12.5).FontWeight(600).TextColor(aiInk).NoWrap()
	})
	if aiBtn.Clicked() {
		b.ai.bar = !b.ai.bar
		b.ai.barFocus = b.ai.bar
	}
	if b.AIRunning() {
		stop := ui.ButtonBase(c.Key("board-ai-stop")).Size(32, 32).Center().Radius(7).Background(st.dangerSoft).Focusable().Label(i18n.T("停止运行")).Tooltip(i18n.T("停止运行"))
		stop.Children(func() {
			if icon := boardIcon("player-stop"); icon != nil {
				ui.Icon(c, icon).FontSize(16).TextColor(st.danger)
			}
		})
		if stop.Clicked() {
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
			ui.Icon(c, svg).FontSize(boardCompactIconSize).TextColor(pal.accent)
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
	unavailable := map[string]string{}
	descriptions := map[string]string{}
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
		{"ai-base", "加载底模", "ComfyUI 模型", "image", "model", add(whiteboard.AIModel, "")},
		{"ai-lora", "加载 LoRA", "ComfyUI 模型", "sparkles", "model", add(whiteboard.AILoRA, "")},
		{"ai-image", "生成图像", "图像", "image", "image", add(whiteboard.AIImage, "")},
		{"ai-cutout", "本地抠图", "图像", "image", "image", add(whiteboard.AICutout, "")},
		{"ai-upscale", "图片放大", "图像", "image", "image", add(whiteboard.AIUpscale, "")},
		{"ai-enhance", "图片增强", "图像", "image", "image", add(whiteboard.AIEnhance, "")},
		{"ai-compare", "图片对比", "图像", "image", "image", add(whiteboard.AICompare, "")},
		{"ai-edit", "编辑图像", "图像", "sparkles", "image", add(whiteboard.AIImage, "编辑图像")},
		{"ai-video", "文生视频", "视频", "player-play", "video", add(whiteboard.AIVideo, "")},
		{"ai-i2v", "图生视频", "视频", "player-play", "video", add(whiteboard.AIVideo, "图生视频")},
	}
	if o.AI.Unavailable != nil {
		for key, role := range map[string]whiteboard.AIRole{"ai-text": whiteboard.AIText, "ai-image": whiteboard.AIImage, "ai-edit": whiteboard.AIImage, "ai-base": whiteboard.AIModel, "ai-lora": whiteboard.AILoRA, "ai-cutout": whiteboard.AICutout, "ai-upscale": whiteboard.AIUpscale, "ai-enhance": whiteboard.AIEnhance, "ai-video": whiteboard.AIVideo, "ai-i2v": whiteboard.AIVideo} {
			unavailable[key] = o.AI.Unavailable(role)
		}
		for _, template := range aiTemplates {
			for _, step := range template.steps {
				if reason := o.AI.Unavailable(step.role); reason != "" {
					unavailable["tpl-"+template.key] = reason
					break
				}
			}
		}
	}
	if o.AI.Presets != nil {
		for _, isLoRA := range []bool{true, false} {
			for _, preset := range o.AI.Presets() {
				if (preset.Model.BaseModel != "") != isLoRA {
					continue
				}
				m := preset.Model
				label := m.Name
				if m.Category != "" {
					label = i18n.T(m.Category) + " · " + label
				}
				group := "图片处理"
				if preset.Role == whiteboard.AIAudio {
					group = "音频"
				}
				if preset.Role == whiteboard.AIVideo {
					group = "视频"
				}
				if m.BaseModel != "" {
					group = "LoRA 流程（自动连线）"
				}
				unavailable["model-"+m.Model] = m.Unavailable
				if m.NeedsDownload {
					unavailable["model-"+m.Model] = i18n.T("请先下载模型及依赖")
				}
				descriptions["model-"+m.Model] = m.Description
				items = append(items, aiBarItem{"model-" + m.Model, label, group, "image", "image", func() { b.insertAIModelPreset(preset, label, o) }})
			}
		}

	}
	for _, t := range aiTemplates {
		items = append(items, aiBarItem{"tpl-" + t.key, t.label, "模板", "sparkles", "text", func() { b.insertAITemplate(t.key, o) }})
	}
	if o.AI.ImportCanvas != nil {
		items = append(items, aiBarItem{"canvas-import", "导入 JSON Canvas", "交换", "duplicate", "text", func() {
			o.AI.ImportCanvas(func(data []byte, err error) {
				if err == nil {
					err = b.ImportCanvas(data, o)
				}
				b.err = err
			})
		}})
	}
	if o.Export != nil {
		items = append(items, aiBarItem{"canvas-export", "导出 JSON Canvas", "交换", "duplicate", "text", func() { o.Export("canvas") }})
	}
	groupOrder := map[string]int{}
	for _, item := range items {
		if _, ok := groupOrder[item.group]; !ok {
			groupOrder[item.group] = len(groupOrder)
		}
	}
	slices.SortStableFunc(items, func(a, b aiBarItem) int { return groupOrder[a.group] - groupOrder[b.group] })
	st := boardChromeFor(o.Theme)
	boardSurface(ui.Scroll(c.Key("board-ai-bar")).Absolute().Top(66).Left(12).Width(min(360, max(220, b.lastRect.W-24))).MaxHeight(max(120, b.lastRect.H-96)).Padding(12).Gap(4).AlignItems(ui.Stretch), st).Role(ui.RoleGroup).Label(i18n.T("AI 节点")).Children(func() {
		boardPanelHeader(c, st, i18n.T("AI 节点"), "", "", o.Theme.Dark, func() { b.ai.bar = false })
		ui.Text(c, i18n.T("模型线：底模 → LoRA → 生成；提示词接生成节点")).FontSize(12).WidthPercent(100).TextColor(st.muted)
		ui.Row(c).WidthPercent(100).Gap(10).Justify(ui.Start).Children(func() {
			for _, source := range []struct{ label, connection string }{{"本地", "local"}, {"ComfyUI", "comfyui"}, {"供应商", "cloud"}} {
				ui.Row(c.Key(source.connection)).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background((BoardAIModel{Connection: source.connection}).sourceColor())
					ui.Text(c, i18n.T(source.label)).FontSize(11.5).TextColor(st.muted)
				})
			}
		})
		search := ui.TextInput(c.Key("board-ai-search"), &b.ai.query).Placeholder(i18n.T("搜索节点")).Label(i18n.T("搜索节点")).WidthPercent(100)
		if b.ai.barFocus {
			search.Focus()
			b.ai.barFocus = false
		}
		query := strings.ToLower(strings.TrimSpace(b.ai.query))
		group := ""
		for _, it := range items {
			label := i18n.T(it.label)
			if query != "" && !strings.Contains(strings.ToLower(label+" "+i18n.T(it.group)+" "+descriptions[it.key]), query) {
				continue
			}
			if it.group != group {
				group = it.group
				if group != "" {
					ui.Box(c).Height(2)
				}
				ui.Row(c.Key("g-"+group)).WidthPercent(100).Justify(ui.Start).Padding(6, 4, 2, 4).Children(func() {
					boardSectionLabel(c, st, i18n.T(group))
				})
			}
			row := ui.ButtonBase(c.Key("board-ai-item-"+it.key)).WidthPercent(100).Padding(7, 8).Gap(8).Justify(ui.Start).AlignItems(ui.Center).Radius(6).Focusable().Label(label).Disabled(unavailable[it.key] != "").Description(unavailable[it.key]).Tooltip(unavailable[it.key])
			if row.Hovered() {
				row.Background(st.hover)
			}
			row.Children(func() {
				aiItemBadge(c, it.icon, it.kind, o.Theme.Dark)
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, label).FontSize(13).TextColor(st.text).WidthPercent(100).TextAlign(ui.Start).SingleLine()
					if desc := descriptions[it.key]; desc != "" {
						ui.Text(c, desc).FontSize(12).TextColor(st.muted).WidthPercent(100).TextAlign(ui.Start)
					}
					if reason := unavailable[it.key]; reason != "" {
						ui.Text(c, reason).FontSize(12).TextColor(st.muted).WidthPercent(100).TextAlign(ui.Start)
					}
				})
			})
			if row.Clicked() && unavailable[it.key] == "" {
				it.run()
			}
		}
	})
}

// aiChoice is a labeled select over values; it returns the chosen value.
func aiChoice(c *ui.Context, o BoardOptions, key, label, current string, values, labels []string) string {
	return aiChoiceControl(c, o, key, label, current, values, labels, false)
}

func aiChoiceControl(c *ui.Context, o BoardOptions, key, label, current string, values, labels []string, disabled bool) string {
	shown := current
	for i, v := range values {
		if v == current {
			shown = labels[i]
		}
	}
	before := shown
	_, height := c.Size()
	if height < 600 {
		ui.Row(c.Key("field-" + key)).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T(label)).FontSize(12).TextColor(o.Theme.TextMuted).Grow(1).MinWidth(0)
			ui.Select(c.Key("board-ai-"+key), &shown, labels).WidthPercent(64).Height(28).FontSize(12).Disabled(disabled).Label(i18n.T(label))
		})
	} else {
		ui.Text(c, i18n.T(label)).FontSize(12).TextColor(o.Theme.TextMuted)
		ui.Select(c.Key("board-ai-"+key), &shown, labels).WidthPercent(100).FontSize(13).Disabled(disabled).Label(i18n.T(label))
	}
	if shown != before && !disabled {
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
	title, instruction, note, template, extra, lyrics, voiceStyle, negative string
}

func aiPropDivider(c *ui.Context, o BoardOptions) {
	ui.Box(c).Height(1).Margin(4, 0).Background(boardChromeFor(o.Theme).divider)
}

func aiPropSection(c *ui.Context, o BoardOptions, title string) {
	boardSectionLabel(c, boardChromeFor(o.Theme), i18n.T(title))
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
		b.ai.buf = aiEditBuffers{n.Title, n.Instruction, n.Note, n.Params.Template, extra, n.Params.Lyrics, n.Params.VoiceStyle, defaultNegativePrompt(n)}
		s.extraErr = ""
	}
	b.aiSettingsPopup(c, o, e, n)
}

func (b *Board) aiSettingsContent(c *ui.Context, o BoardOptions, e *whiteboard.Element, n *whiteboard.AINode) {
	id := e.ID
	s := b.aiState()
	edit := func(fn func(*whiteboard.AINode)) { b.editAINode(o, id, fn) }
	switch n.Role {
	case whiteboard.AIPrompt:
		aiPropSection(c, o, "文字内容")
		area := ui.TextArea(c.Key("board-ai-instruction"), &b.ai.buf.instruction).Placeholder(i18n.T("输入提示词或文本")).WidthPercent(100).Height(aiSettingsTextHeight(c, 140)).Label(i18n.T("文字内容"))
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
		if ui.TextArea(c.Key("board-ai-template"), &b.ai.buf.template).WidthPercent(100).Height(aiSettingsTextHeight(c, 88)).Changed() {
			edit(func(n *whiteboard.AINode) { n.Params.Template = b.ai.buf.template })
		}
	}
	if n.Role == whiteboard.AIText || n.Role == whiteboard.AIImage || n.Role == whiteboard.AIEnhance || n.Role == whiteboard.AIVideo || n.Role == whiteboard.AIAudio || n.Role == whiteboard.AICutout {
		aiPropSection(c, o, "指令")
		value, placeholder := &b.ai.buf.instruction, i18n.T("输入指令或提示词")
		if n.Role == whiteboard.AICutout {
			placeholder = i18n.T("可选：指定对象或材质，如「左边的人」或「透明玻璃」")
		}
		linked := b.aiPromptSource(n)
		unsupported := !n.AcceptsPrompt()
		if unsupported {
			value, placeholder = new(string), i18n.T("此模型不支持提示词")
		} else if linked != "" {
			// The linked slot feeds the instruction, so the field is
			// disabled and shows the source, as ComfyUI's widget inputs.
			value, placeholder = new(string), fmt.Sprintf(i18n.T("已连接：%s"), linked)
		}
		area := ui.TextArea(c.Key("board-ai-instruction"), value).Placeholder(placeholder).WidthPercent(100).Height(aiSettingsTextHeight(c, 104)).Disabled(unsupported || linked != "" || b.ReadOnly || e.Locked).Label(i18n.T("指令")).Description(placeholder)
		if s.focusInstruction && !unsupported && linked == "" && !b.ReadOnly && !e.Locked {
			area.Focus()
			s.focusInstruction = false
		}
		if area.Changed() {
			b.setAINodeText(o, id, b.ai.buf.instruction)
		}
	}
}

func (b *Board) aiModelField(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode) {
	ui.Text(c, i18n.T("模型")).FontSize(12).TextColor(o.Theme.TextMuted)
	ui.Row(c).WidthPercent(100).Children(func() {
		b.aiModelSelect(c, o, id, n, "board-ai-model", b.ReadOnly || b.Scene.ElementByID(id).Locked, 30)
	})
}

// aiImageSizeChoices shares provider-specific resolutions between the card
// and its settings panel; stored values remain the provider's API values.
func aiImageSizeChoices(o BoardOptions, n *whiteboard.AINode, compact bool) ([]string, []string, bool) {
	if o.AI != nil && o.AI.Models != nil {
		for _, m := range o.AI.Models(whiteboard.AIImage) {
			if m.Connection == n.Connection && m.Model == n.Model && len(m.ImageSizes) > 0 {
				return append([]string{""}, m.ImageSizes...), append([]string{i18n.T("跟随输入图")}, m.ImageSizes...), true
			}
		}
	}
	if compact {
		return []string{"", "1024x1024", "1536x1024", "1024x1536"}, []string{i18n.T("跟随输入图"), "1024 x 1024", "1536×1024", "1024×1536"}, false
	}
	return []string{"", "auto", "1024x1024", "1536x1024", "1024x1536", "1792x1024", "1024x1792"}, translated("跟随输入图", "自动", "1024×1024 方形", "1536×1024 横向", "1024×1536 纵向", "1792×1024 宽屏", "1024×1792 竖屏"), false
}

func (b *Board) aiParamsFields(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode) {
	b.aiMainParamsFields(c, o, id, n)
	b.aiLyricsField(c, o, id, n, b.ReadOnly || b.Scene.ElementByID(id).Locked)
	b.aiComfySamplingParams(c, o, id, n, b.ReadOnly || b.Scene.ElementByID(id).Locked)
}

func (b *Board) aiMainParamsFields(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode) {
	n = b.Scene.AIInputNode(n)
	if n.Connection == "comfyui-media" {
		aiPropSection(c, o, "生成参数")
		b.aiLocalMediaFields(c, o, id, n, false, b.ReadOnly || b.Scene.ElementByID(id).Locked)
		return
	}
	set := func(fn func(*whiteboard.AIParams)) {
		b.updateAINode(o, id, func(n *whiteboard.AINode) { fn(&n.Params) })
	}
	p := n.Params
	if n.Params.LoRAStrength != nil {
		ui.Row(c.Key("lora-strength-prop")).Height(28).Gap(6).AlignItems(ui.Center).Children(func() { b.aiLoRAStrength(c, o, id, n, b.ReadOnly || b.Scene.ElementByID(id).Locked) })
	}
	switch n.Role {
	case whiteboard.AIEnhance:
		ui.Text(c, i18n.T("按原图比例增强，输出约 2MP 的新图片")).FontSize(12).TextColor(o.Theme.TextMuted)
		return
	case whiteboard.AIText:
		b.aiPresetRow(c, o, id)
		aiPropDivider(c, o)
		aiPropSection(c, o, "输出方式")
		if v := aiChoice(c, o, "write", "结果写到", p.Write, []string{"", "replace", "note"}, translated("仅节点内", "改写第一个文字输入", "节点旁新建便签")); v != p.Write {
			set(func(p *whiteboard.AIParams) { p.Write = v })
		}
	case whiteboard.AIImage:
		values, labels, nativeSize := aiImageSizeChoices(o, n, false)
		aiPropDivider(c, o)
		aiPropSection(c, o, "生成参数")
		ui.Row(c.Key("img-row-1")).Gap(8).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				if v := aiChoice(c, o, "size", "尺寸", p.Size, values, labels); v != p.Size {
					set(func(p *whiteboard.AIParams) {
						p.Size = v
						if nativeSize {
							p.Quality = ""
						}
					})
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				if v := aiChoice(c, o, "aspect", "画幅比例", p.AspectRatio, []string{"", "1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}, []string{i18n.T("跟随输入图"), "1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}); v != p.AspectRatio {
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
				if nativeSize || strings.HasPrefix(n.Connection, "comfyui") {
					return
				}
				if v := aiChoice(c, o, "quality", "质量", p.Quality, []string{"", "low", "medium", "high", "standard", "hd"}, translated("服务默认", "低", "中", "高", "标准", "高清")); v != p.Quality {
					set(func(p *whiteboard.AIParams) { p.Quality = v })
				}
			})
		})
	case whiteboard.AIUpscale:
		aiPropDivider(c, o)
		aiPropSection(c, o, "放大设置")
		scale := strconv.Itoa(aiUpscaleFactor(p.Scale))
		if v := aiChoice(c, o, "upscale", "放大倍数", scale, []string{"2", "4"}, []string{"2×", "4×"}); v != scale {
			k, _ := strconv.Atoi(v)
			set(func(p *whiteboard.AIParams) { p.Scale = k })
		}
	case whiteboard.AICompare:
		aiPropDivider(c, o)
		aiPropSection(c, o, "对比设置")
		mode := b.aiCompareMode(b.Scene.ElementByID(id), n)
		if v := aiChoice(c, o, "cmp-mode", "对比模式", mode, compareModeValues, translated(compareModeLabels...)); v != mode {
			b.setCompareMode(o, b.Scene.ElementByID(id), v)
		}
		bg := b.aiCompareBg(b.Scene.ElementByID(id), n)
		if v := aiChoice(c, o, "cmp-bg", "对比背景", bg, compareBgValues, translated(compareBgLabels...)); v != bg {
			b.setCompareBg(o, b.Scene.ElementByID(id), v)
		}
		if !b.ReadOnly && !b.Scene.ElementByID(id).Locked && ui.Button(c.Key("cmp-swap"), i18n.T("交换图片 A 与图片 B")).Clicked() {
			b.swapCompareInputs(o, b.Scene.ElementByID(id))
		}
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
	if n.Role == whiteboard.AIText || strings.HasPrefix(n.Connection, "comfyui") || n.Connection == "local" || n.Connection == "local-ncnn" {
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
	ui.Row(c.Key("board-ai-run")).Gap(6).Margin(2, 0, 0, 0).Children(func() {
		if b.AIRunning() {
			if ui.Button(c.Key("stop"), i18n.T("停止")).WidthPercent(100).Clicked() {
				b.StopAI()
			}
			return
		}
		if ui.PrimaryButton(c.Key("run"), i18n.T("运行节点")).Grow(1).Clicked() {
			b.RunAI([]string{id}, false, b.aiNode(b.Scene.ElementByID(id)).Role == whiteboard.AIImage, o)
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
	if n.Role == whiteboard.AIImageInput {
		b.aiImageButtons(c, o, id, false)
		return
	}
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
		prompt := out.Prompt
		ui.TextArea(c.Key("output-prompt"), &prompt).ReadOnly(true).Height(aiSettingsTextHeight(c, 64)).WidthPercent(100).FontSize(12).Label(i18n.T("服务改写的提示词："))
	}
	ui.Row(c.Key("board-ai-out")).Gap(6).Wrap().Children(func() {
		if out.Kind == "image" {
			b.aiImageButtons(c, o, id, false)
		}
		if out.Kind == "text" && ui.Button(c.Key("copy"), i18n.T("复制文字")).Clicked() {
			b.services.WriteClipboard(out.Text)
		}
		if (out.Kind == "text" || out.Kind == "image") && ui.Button(c.Key("place"), i18n.T("放到白板")).Clicked() {
			b.placeAIOutput(id, o)
		}
		if out.Kind == "audio" && out.Path != "" && o.AI.OpenFile != nil && ui.Button(c.Key("play-audio"), i18n.T("播放音频")).Clicked() {
			o.AI.OpenFile(out.Path)
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

func (b *Board) chooseAIModel(o BoardOptions, id string, n *whiteboard.AINode, model BoardAIModel) {
	if model.NeedsDownload || model.Unavailable != "" {
		return
	}
	_, _, wasNative := aiImageSizeChoices(o, n, false)
	b.updateAINode(o, id, func(node *whiteboard.AINode) {
		if (node.Connection == "comfyui-media" || model.Connection == "comfyui-media") && (node.Connection != model.Connection || node.Model != model.Model) {
			// Speech and music use different language codes; each model also
			// has its own duration limits. Keep saved lyrics when switching.
			node.Params.Language, node.Params.Seconds = "", 0
			node.Params.FPS = 0
			node.Params.Size = ""
		}
		if node.Connection != model.Connection || node.Model != model.Model {
			node.Params.Steps = 0
			node.Params.Guidance = nil
			node.Params.NegativePrompt = nil
			b.ai.buf.negative = ""
		}
		node.Connection, node.Model = model.Connection, model.Model
		b.ai.buf.negative = defaultNegativePrompt(node)
		node.Params.LoRAStrength = model.LoRAStrength
		if node.Instruction == "" {
			node.Instruction = model.Instruction
		}
		if node.Role == whiteboard.AIImage && wasNative != (len(model.ImageSizes) > 0) {
			node.Params.Size, node.Params.Quality = "", ""
		}
	})
	if o.AI.Remember != nil {
		o.AI.Remember(n.Role, model)
	}
}

func (b *Board) aiInlineModelSelect(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, disabled bool) {
	b.aiModelSelect(c, o, id, n, "inline-model-"+id, disabled, 26)
}

// aiNodeOverlay is the floating action & history bar above the selected node.
func (b *Board) aiNodeOverlay(c *ui.Context, o BoardOptions) {
	s := b.aiState()
	e, n := b.selectedAINode()
	if e == nil {
		return
	}
	x, y, w, h := b.canvasRect(e.X, e.Y, e.W, e.H)
	out := n.Output()
	st := boardChromeFor(o.Theme)
	b.selectionToolbar(c, "board-ai-node-bar", i18n.T("节点操作"), ui.Rect{X: x, Y: y, W: w, H: h}, st).Children(func() {
		if n.Role == whiteboard.AIImageInput || out != nil && out.Kind == "image" {
			b.aiImageButtons(c, o, e.ID, true)
		}
		// Running is on the card; the bar holds what a card has no
		// room for, as ComfyUI's selection toolbox.
		settingsButton := boardFlatButton(c, "board-ai-settings", i18n.T("全部设置"), s.panel, st).Height(26).Label(i18n.T("全部设置"))
		s.settingsAnchor = settingsButton
		if s.edit != "" && s.edit != e.ID {
			s.panel = false
		}
		if settingsButton.Clicked() {
			s.panel = !s.panel
		}
		if !b.ReadOnly && aiRunnableRole(n.Role) && !b.AIRunning() {
			if boardFlatButton(c, "board-ai-run-up", i18n.T("连同上游运行"), false, st).Height(26).Clicked() {
				b.RunAI([]string{e.ID}, true, false, o)
			}
		}
		if len(n.Outputs) > 1 && n.Role != whiteboard.AIImage {
			boardToolbarDivider(c, "node-bar-sep-1", st)
			if boardFlatButton(c, "board-ai-prev", "‹", false, st).Height(26).MinWidth(24).PaddingX(6).Label(i18n.T("上一个结果")).Disabled(n.Current <= 0).Clicked() {
				b.updateAINode(o, e.ID, func(n *whiteboard.AINode) { n.Current-- })
			}
			ui.Text(c, fmt.Sprintf("%d/%d", n.Current+1, len(n.Outputs))).FontSize(11.5).TextColor(st.muted).PaddingX(2)
			if boardFlatButton(c, "board-ai-next", "›", false, st).Height(26).MinWidth(24).PaddingX(6).Label(i18n.T("下一个结果")).Disabled(n.Current >= len(n.Outputs)-1).Clicked() {
				b.updateAINode(o, e.ID, func(n *whiteboard.AINode) { n.Current++ })
			}
		}
		if out != nil && (out.Kind == "text" || out.Kind == "image") {
			boardToolbarDivider(c, "node-bar-sep-2", st)
			if out.Kind == "text" && boardFlatButton(c, "board-ai-copy", i18n.T("复制"), false, st).Height(26).Label(i18n.T("复制文字")).Clicked() {
				b.services.WriteClipboard(out.Text)
			}
			if !b.ReadOnly && boardFlatButton(c, "board-ai-place", i18n.T("放到白板"), false, st).Height(26).Clicked() {
				b.placeAIOutput(e.ID, o)
			}
		}
	})
}

// aiVideoOverlay renders the selected node's video player inside the board viewport.
func (b *Board) aiVideoOverlay(c *ui.Context, o BoardOptions) {
	s := b.aiState()
	e, n := b.selectedAINode()
	shown := ""
	if e != nil {
		x, y, w, h := b.canvasRect(e.X, e.Y, e.W, e.H)
		out := n.Output()
		if out != nil && out.Kind == "audio" && o.AI != nil && o.AI.OpenFile != nil {
			r := b.aiLayout(e, n).preview
			z := float32(b.Zoom)
			if ui.Button(c.Key("board-ai-play-audio"), i18n.T("播放音频")).Absolute().Left(x + r.X*z + 12).Top(y + r.Y*z + 12).Clicked() {
				o.AI.OpenFile(out.Path)
			}
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
	x, y, w, h := b.canvasRect(box.X, box.Y, box.W, box.H)
	st := boardChromeFor(o.Theme)
	b.selectionToolbar(c, "board-ai-selection", i18n.T("用选中内容运行 AI"), ui.Rect{X: x, Y: y, W: w, H: h}, st).Children(func() {
		if len(b.selectedImagesForNodes()) > 0 && boardFlatButton(c, "board-ai-convert-image", i18n.T("转为节点"), false, st).Height(26).Clicked() {
			b.convertImagesToNodes(o)
		}
		for _, a := range []struct {
			role  whiteboard.AIRole
			label string
		}{{whiteboard.AIText, "AI 文本"}, {whiteboard.AIImage, "AI 生图"}, {whiteboard.AIVideo, "AI 视频"}} {
			if boardFlatButton(c, "board-ai-sel-"+string(a.role), i18n.T(a.label), false, st).Height(26).Clicked() {
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
	if e, _ := b.selectedAINode(); b.canConvertNodeToImage(e, o) && m.Item(i18n.T("转为图片")).Chosen() {
		b.convertNodeToImage(e.ID, o)
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
