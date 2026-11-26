package editor

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// A card reads top to bottom as a ComfyUI node: title bar, slot rows
// (inputs on the left, the output on the right), the node's own controls,
// its result, and a status line. The controls are real widgets laid out in
// card units and scaled with the board, so they never vanish or jump while
// the board pans, zooms or a selection box is dragged.

// aiInlineMinZoom is the zoom below which cards are drawn without widgets.
const aiInlineMinZoom = 0.3

func (b *Board) aiInlineActive() bool {
	return b.options.AI != nil && b.Zoom >= aiInlineMinZoom
}

// aiSlotLabel is a slot's name in the interface.
func aiSlotLabel(sl whiteboard.AISlot) string {
	if sl.Number > 0 {
		return fmt.Sprintf("%s %d", i18n.T(sl.Label), sl.Number)
	}
	return i18n.T(sl.Label)
}

// aiSourceName names an element that feeds a node.
func (b *Board) aiSourceName(e *whiteboard.Element) string {
	if e == nil {
		return ""
	}
	if n := b.aiNode(e); n != nil {
		return cmpOr(n.Title, i18n.T(aiRoleNames[n.Role]))
	}
	switch {
	case e.Kind == whiteboard.KindImage:
		return i18n.T("图片")
	case e.IsFrame():
		return cmpOr(e.Name, i18n.T("画框"))
	}
	text := strings.Join(strings.Fields(b.Scene.ElementText(e)), " ")
	if text == "" {
		if note := whiteboard.WikiTarget(e.Link); note != "" {
			return "[[" + note + "]]"
		}
		return i18n.T("文字")
	}
	if r := []rune(text); len(r) > 24 {
		text = string(r[:24]) + "…"
	}
	return text
}

// aiPromptSource names what is linked into a node's instruction slot, or "".
func (b *Board) aiPromptSource(n *whiteboard.AINode) string {
	return b.aiSourceName(b.Scene.AILinkSource(n, "prompt"))
}

// aiSlotUsable reports whether a slot can take the link being dragged;
// LiteGraph fades the slots that cannot.
func (b *Board) aiSlotUsable(nodeID string, typ string, input bool) bool {
	d := b.ai.drag
	if d == nil || !d.live {
		return true
	}
	if input {
		return d.from != "" && d.from != nodeID && whiteboard.AIValidConnection(d.typ, typ)
	}
	return d.to != "" && d.to != nodeID && whiteboard.AIValidConnection(typ, d.typ)
}

// paintAINode draws a node as a card in the element's local coordinates.
func (b *Board) paintAINode(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, dark bool) {
	s := b.aiState()
	w, h := float32(e.W), float32(e.H)
	pal := aiNodePalette(n.Kind(), dark)
	l := b.aiLayout(e, n)
	card := ui.Rect{W: w, H: h}
	run := s.runs[e.ID]
	running := run != nil && (run.status == "running" || run.status == "queued")
	status, failed := b.aiStatus(e, n)

	p.Shadow(card, aiCardRadius, 0, 3, 12, 0, pal.shadow)
	p.Fill(card, pal.cardBg, aiCardRadius)
	p.Clip(card, aiCardRadius, func() {
		// Clean header: icon badge + title; action sits on the right in inline controls.
		p.Fill(ui.Rect{W: w, H: aiHeader}, pal.headerBg, 0)
		p.Line(0, aiHeader, w, aiHeader, 1, pal.divider.Alpha(0.75))
		badge := ui.Rect{X: aiPad, Y: (aiHeader - 22) / 2, W: 22, H: 22}
		p.Fill(badge, pal.accentSoft, 6)
		if icon := boardIcon(aiRoleIcons[n.Role]); icon != nil {
			p.Icon(icon, ui.Rect{X: badge.X + 4, Y: badge.Y + 4, W: 14, H: 14}, pal.accent)
		}
		tx := float32(aiPad + 30)
		maxW := w - tx - aiPad
		if l.headerRun.W > 0 {
			maxW = l.headerRun.X - tx - 8
		}
		title := b.aiCardTextFont(e.ID+"#title", cmpOr(n.Title, i18n.T(aiRoleNames[n.Role])), 0, ui.Font{Size: 13, Weight: 600})
		_, th := title.Size()
		p.Clip(ui.Rect{X: tx, Y: 4, W: max(0, maxW), H: aiHeader - 8}, 0, func() {
			p.TextLayout(title, tx, (aiHeader-th)/2, pal.ink)
		})

		// Slot rows: what flows in and out.
		for i, sl := range l.slots {
			y := l.slotTop + float32(i)*aiSlotRow
			color := pal.muted
			if b.Scene.AILinkSource(n, sl.Name) != nil {
				color = pal.secondary
			}
			if !b.aiSlotUsable(e.ID, sl.Type, true) {
				color = color.Alpha(0.30)
			}
			slLayout := b.aiCardTextFont(e.ID+"#slot"+sl.Name, aiSlotLabel(sl), 0, ui.Font{Size: 11.5, Weight: 500})
			_, sh := slLayout.Size()
			p.TextLayout(slLayout, aiPad, y+(aiSlotRow-sh)/2, color)
		}
		if l.out != "" {
			label := b.aiCardTextFont(e.ID+"#out", aiTypeName(l.out), 0, ui.Font{Size: 11.5, Weight: 500})
			lw, lh := label.Size()
			color := pal.muted
			if s.linkedOut[e.ID] {
				color = pal.secondary
			}
			if !b.aiSlotUsable(e.ID, l.out, false) {
				color = color.Alpha(0.30)
			}
			p.TextLayout(label, w-aiPad-lw, l.slotTop+(aiSlotRow-lh)/2, color)
		}
		if (len(l.slots) > 0 || l.out != "") && l.widgets.H > 0 {
			y := l.widgets.Y - 4
			p.Line(aiPad, y, w-aiPad, y, 1, pal.divider.Alpha(0.6))
		}

		// Without widgets (far out, or in an export) the controls' values
		// are drawn in their place.
		if !b.aiInlineActive() || !b.aiPainting {
			b.paintAIWidgetValues(p, e, n, l.widgets, pal)
		}
		if l.preview.H > 12 {
			p.Clip(l.preview, 8, func() { b.paintAIPreview(p, e, n, l.preview, pal) })
		}

		// Slim integrated status footer.
		fy := h - aiFooter
		p.Fill(ui.Rect{Y: fy, W: w, H: aiFooter}, pal.footerBg, 0)
		p.Line(0, fy, w, fy, 1, pal.divider.Alpha(0.55))
		var meta string
		if len(n.Outputs) > 0 {
			cur := n.Current
			if n.Role == whiteboard.AIImage {
				cur = b.aiHistoryCurrent(e, n)
			}
			cur = max(0, min(cur, len(n.Outputs)-1))
			meta = fmt.Sprintf("%d/%d", cur+1, len(n.Outputs))
			if n.Role == whiteboard.AIImage && n.Outputs[cur].Run > 0 {
				meta = fmt.Sprintf(i18n.T("第 %d 次 · %d/%d"), n.Outputs[cur].Run, cur+1, len(n.Outputs))
			}
		}
		var metaW float32
		if meta != "" {
			ml := b.aiCardTextFont(e.ID+"#meta", meta, 0, ui.Font{Size: 11, Weight: 500})
			mw, mh := ml.Size()
			metaW = mw + 8
			p.TextLayout(ml, w-aiPad-mw, fy+(aiFooter-mh)/2, pal.muted)
		}
		sl := b.aiCardTextFont(e.ID+"#status", status, 0, ui.Font{Size: 11, Weight: 500})
		_, sh := sl.Size()
		color := pal.muted
		if running {
			color = pal.accent
		} else if failed {
			color = pal.danger
		}
		p.Clip(ui.Rect{X: aiPad, Y: fy, W: max(0, w-2*aiPad-metaW), H: aiFooter}, 0, func() {
			p.TextLayout(sl, aiPad, fy+(aiFooter-sh)/2, color)
		})
	})

	border, bw := pal.border, float32(1)
	switch {
	case failed:
		border, bw = pal.danger, 2
	case b.Selected[e.ID] || running:
		border, bw = pal.accent, 2
	}
	p.Stroke(card, border, aiCardRadius, bw)

	// Slots: filled when linked, hollow when free, as LiteGraph draws them.
	d := s.drag
	for i, sl := range l.slots {
		y := float32(aiInputPoint(e, l, i)[1] - e.Y)
		c := aiTypeColor(sl.Type, dark)
		if !b.aiSlotUsable(e.ID, sl.Type, true) {
			c = c.Alpha(0.30)
		}
		lit := d != nil && d.target == e.ID && d.targetSlot == sl.Name
		b.paintAISlot(p, 0, y, c, pal.cardBg, b.Scene.AILinkSource(n, sl.Name) != nil, lit)
	}
	if l.out != "" {
		c := aiTypeColor(l.out, dark)
		if !b.aiSlotUsable(e.ID, l.out, false) {
			c = c.Alpha(0.30)
		}
		lit := d != nil && d.to != "" && d.target == e.ID
		b.paintAISlot(p, w, l.slotTop+aiSlotRow/2, c, pal.cardBg, s.linkedOut[e.ID], lit)
	}
}

func (b *Board) paintAISlot(p *ui.Painter, x, y float32, c, bg ui.Color, filled, lit bool) {
	if lit {
		r := float32(aiPort + 5)
		p.Fill(ui.Rect{X: x - r, Y: y - r, W: 2 * r, H: 2 * r}, c.Alpha(0.24), r)
	}
	cut := float32(aiPort + 1.5)
	p.Fill(ui.Rect{X: x - cut, Y: y - cut, W: 2 * cut, H: 2 * cut}, bg, cut)
	r := float32(aiPort)
	box := ui.Rect{X: x - r, Y: y - r, W: 2 * r, H: 2 * r}
	if filled {
		p.Fill(box, c, r)
		return
	}
	p.Fill(box, bg, r)
	p.Stroke(box, c, r, 1.75)
}

func aiTypeName(typ string) string {
	switch typ {
	case whiteboard.AITypeImage:
		return i18n.T("图片")
	case whiteboard.AITypeVideo:
		return i18n.T("视频")
	}
	return i18n.T("文本")
}

// paintAIWidgetValues draws the controls' values where the widgets are not
// shown.
func (b *Board) paintAIWidgetValues(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, r ui.Rect, pal aiNodeColors) {
	if r.H < 16 {
		return
	}
	value := n.Instruction
	switch n.Role {
	case whiteboard.AINote:
		value = "[[" + n.Note + "]]"
	case whiteboard.AIConcat:
		value = n.Params.Template
	}
	color := pal.secondary
	if src := b.aiPromptSource(n); src != "" {
		value, color = fmt.Sprintf(i18n.T("已连接：%s"), src), pal.muted
	}
	p.Fill(r, pal.mediaBg, 6)
	if n.Model != "" && n.Role != whiteboard.AIPrompt && n.Role != whiteboard.AINote && n.Role != whiteboard.AIConcat {
		value = n.Model + "\n" + value
	}
	p.Clip(r, 6, func() {
		p.TextLayout(b.aiCardTextFont(e.ID+"#values", value, r.W-16, ui.Font{Size: 12.5, LineHeight: 1.4}), r.X+8, r.Y+6, color)
	})
}

// paintAIPreview draws the node's result, its streamed text while it runs,
// or what it will show.
func (b *Board) paintAIPreview(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, r ui.Rect, pal aiNodeColors) {
	if n.Role == whiteboard.AIImage && len(n.Outputs) > 0 {
		b.paintAIImageHistory(p, e, n, r, pal)
		return
	}
	s := b.aiState()
	drawBox := func() {
		p.Fill(r, pal.mediaBg, 8)
	}
	drawBorder := func() {
		p.Stroke(r, pal.divider.Alpha(0.75), 8, 1)
	}
	text := func(id, t string, color ui.Color) {
		p.TextLayout(b.aiCardTextFont(id, t, max(0, r.W-20), ui.Font{Size: 12.5, LineHeight: 1.5}), r.X+10, r.Y+8, color)
	}
	if n.Role == whiteboard.AIImageInput {
		drawBox()
		bm := b.images[e.FileID]
		if bm == nil {
			message, _ := b.aiStatus(e, n)
			text(e.ID+"#picture", message, pal.muted)
			drawBorder()
			return
		}
		iw, ih := bm.Size()
		src := ui.Rect{W: float32(iw), H: float32(ih)}
		if crop := e.Crop; crop != nil {
			src = ui.Rect{X: float32(crop.X), Y: float32(crop.Y), W: float32(crop.Width), H: float32(crop.Height)}
		}
		scale := min(r.W/src.W, r.H/src.H)
		w, h := src.W*scale, src.H*scale
		p.ImageRegion(bm, src, ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}, ui.ImageOptions{FlipX: e.Scale[0] < 0, FlipY: e.Scale[1] < 0, Tint: ui.RGB(255, 255, 255).Alpha(float32(e.Opacity / 100))})
		drawBorder()
		return
	}
	if run := s.runs[e.ID]; run != nil && run.status == "running" && run.stream != "" {
		drawBox()
		stream := run.stream
		if len(stream) > 4000 {
			stream = "…" + stream[len(stream)-4000:]
		}
		text(e.ID+"#stream", stream, pal.ink)
		drawBorder()
		return
	}
	out := n.Output()
	if out == nil {
		hint := i18n.T("运行后显示文本")
		switch n.Role {
		case whiteboard.AIImage:
			hint = i18n.T("运行后显示图像")
		case whiteboard.AIVideo:
			hint = i18n.T("运行后显示视频")
		case whiteboard.AINote:
			hint = i18n.T("运行后读取笔记正文")
		}
		drawBox()
		hl := b.aiCardTextFont(e.ID+"#hint", hint, 0, ui.Font{Size: 12})
		hw, hh := hl.Size()
		if icon := boardIcon(aiRoleIcons[n.Role]); icon != nil && r.H >= 72 {
			p.Icon(icon, ui.Rect{X: r.X + (r.W-20)/2, Y: r.Y + r.H/2 - 20, W: 20, H: 20}, pal.muted.Alpha(0.55))
			p.TextLayout(hl, r.X+(r.W-hw)/2, r.Y+r.H/2+6, pal.muted)
		} else {
			p.TextLayout(hl, r.X+(r.W-hw)/2, r.Y+(r.H-hh)/2, pal.muted)
		}
		drawBorder()
		return
	}
	switch out.Kind {
	case "text":
		drawBox()
		text(e.ID+"#out-text", out.Text, pal.ink)
		drawBorder()
	case "image":
		drawBox()
		bm := s.media[out.Path]
		if bm == nil {
			msg := i18n.T("正在加载图片")
			if s.mediaErr[out.Path] != nil {
				msg = i18n.T("图片无法读取：") + out.Path
			}
			text(e.ID+"#img-msg", msg, pal.muted)
			drawBorder()
			return
		}
		iw, ih := bm.Size()
		scale := min(r.W/float32(iw), r.H/float32(ih))
		dw, dh := float32(iw)*scale, float32(ih)*scale
		p.ImageRegion(bm, ui.Rect{}, ui.Rect{X: r.X + (r.W-dw)/2, Y: r.Y + (r.H-dh)/2, W: dw, H: dh}, ui.ImageOptions{})
		drawBorder()
	case "video":
		p.Fill(r, ui.Hex("#14161A"), 8)
		cx, cy := r.X+r.W/2, r.Y+r.H/2-12
		p.Fill(ui.Rect{X: cx - 19, Y: cy - 19, W: 38, H: 38}, ui.RGBA(255, 255, 255, 0.18), 19)
		if icon := boardIcon("player-play"); icon != nil {
			p.Icon(icon, ui.Rect{X: cx - 8, Y: cy - 8, W: 16, H: 16}, ui.Hex("#F8F9FA"))
		}
		p.Text(r.X+12, r.Y+r.H-40, path.Base(out.Path), 12, ui.Hex("#E6E8EC"))
		p.Text(r.X+12, r.Y+r.H-20, i18n.T("选中节点后播放"), 11, ui.Hex("#9CA3AF"))
		drawBorder()
	}
}

// aiInlineSelect is a compact select over values; it returns the chosen
// value and whether it changed.
func aiInlineSelect(c *ui.Context, key, label, current string, values, labels []string, disabled bool) (string, bool) {
	shown := labels[0]
	for i, v := range values {
		if v == current {
			shown = labels[i]
		}
	}
	before := shown
	ui.Select(c.Key(key), &shown, labels).Grow(1).MinWidth(0).Height(26).FontSize(11.5).Disabled(disabled).Label(label).Tooltip(label)
	if shown != before {
		for i, l := range labels {
			if l == shown {
				return values[i], true
			}
		}
	}
	return current, false
}

// aiNodeInlineControls puts each visible card's controls on it: header run
// action, prompt/instruction, model, settings, and image history. The
// instruction field is disabled while its slot is linked.
func (b *Board) aiNodeInlineControls(c *ui.Context, o BoardOptions) {
	s := b.aiState()
	if !b.aiInlineActive() {
		if s.inlineFocused != "" {
			s.inlineFocused = ""
			b.flushAIEdit()
		}
		return
	}
	z := float32(b.Zoom)
	st := boardChromeFor(o.Theme)
	focusedNow := ""
	for _, e := range b.Scene.Elements {
		n := b.aiNode(e)
		if n == nil || e.IsDeleted || e.Angle != 0 {
			continue
		}
		x, y, w, h := b.canvasRect(e.X, e.Y, e.W, e.H)
		if x+w < 0 || y+h < 0 || x > b.lastRect.W || y > b.lastRect.H {
			continue
		}
		l := b.aiLayout(e, n)
		id := e.ID
		disabled := b.ReadOnly || e.Locked
		if l.headerRun.W > 0 {
			hr := l.headerRun
			ui.Row(c.Key("ai-header-run-"+id)).Absolute().Left(x+hr.X*z).Top(y+hr.Y*z).Size(hr.W, hr.H).TransformOrigin(0, 0).Scale(z, z).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				b.aiInlineRun(c, o, id, n, disabled)
			})
		}
		b.aiHistoryControls(c, o, e, n, l, x, y, z)
		r := l.widgets
		if r.W < 120 || r.H < 24 {
			continue
		}
		buf := b.aiNodeBuf(e, n)
		focus := func(el ui.Element) {
			if s.focusInstruction && b.Selected[id] && len(b.Selected) == 1 && !disabled {
				el.Focus()
				s.focusInstruction = false
			}
			if el.Focused() {
				focusedNow = id
				if !b.Selected[id] || len(b.Selected) != 1 {
					b.selectIDs([]string{id})
				}
			}
			if el.Changed() {
				b.setAINodeText(o, id, buf.text)
			}
			if el.Shortcut(0, ui.KeyEscape) {
				b.RequestFocus = true
			}
		}
		ui.Column(c.Key("ai-inline-"+id)).Absolute().Left(x+r.X*z).Top(y+r.Y*z).Size(r.W, r.H).TransformOrigin(0, 0).Scale(z, z).Gap(6).Children(func() {
			switch n.Role {
			case whiteboard.AIPrompt:
				focus(ui.TextArea(c.Key("board-ai-inline-text-"+id), &buf.text).Placeholder(i18n.T("输入提示词或文本，从右侧输出连到其他节点")).FontSize(13).WidthPercent(100).Grow(1).MinHeight(0).Disabled(disabled).Label(i18n.T("文字内容")))
			case whiteboard.AINote:
				ui.Row(c).Height(28).Gap(4).AlignItems(ui.Center).Children(func() {
					focus(ui.TextInput(c.Key("board-ai-inline-text-"+id), &buf.text).Placeholder(i18n.T("输入引用的笔记名称")).FontSize(12).Grow(1).MinWidth(0).Height(28).Disabled(disabled).Label(i18n.T("引用的笔记")))
					if n.Note != "" && o.OpenWiki != nil && boardFlatButton(c, "inline-open-note-"+id, i18n.T("打开"), false, st).Height(28).Clicked() {
						o.OpenWiki(n.Note, false)
					}
				})
			case whiteboard.AIConcat:
				focus(ui.TextArea(c.Key("board-ai-inline-text-"+id), &buf.text).Placeholder(i18n.T("{1}、{2} 为输入，{all} 为全部")).FontSize(12).WidthPercent(100).Grow(1).MinHeight(0).Disabled(disabled).Label(i18n.T("拼接模板")))
			default:
				src := b.aiPromptSource(n)
				if src != "" {
					// Linked: the upstream text is the instruction. The
					// field shows the source, never the hidden own value.
					empty := ""
					note := fmt.Sprintf(i18n.T("已连接：%s"), src)
					ui.TextInput(c.Key("board-ai-inline-linked-"+id), &empty).Placeholder(note).FontSize(12).WidthPercent(100).Height(30).Disabled(true).Label(i18n.T("指令")).Description(note)
				} else {
					ph := i18n.T("输入指令或要求")
					switch n.Role {
					case whiteboard.AIImage:
						ph = i18n.T("描述画面或输入提示词")
					case whiteboard.AIVideo:
						ph = i18n.T("描述视频画面或动作")
					}
					focus(ui.TextArea(c.Key("board-ai-inline-text-"+id), &buf.text).Placeholder(ph).FontSize(12).WidthPercent(100).Height(58).Disabled(disabled).Label(i18n.T("指令")))
				}
				ui.Column(c.Key("inline-cfg-" + id)).Gap(4).Children(func() {
					ui.Row(c.Key("inline-bar-" + id)).Height(26).Gap(4).AlignItems(ui.Center).Children(func() {
						b.aiInlineModelSelect(c, o, id, n, disabled)
					})
					ui.Row(c.Key("inline-params-" + id)).Height(26).Gap(4).AlignItems(ui.Center).Children(func() {
						b.aiInlineParams(c, o, id, n, disabled)
					})
				})
			}
		})
	}
	if s.inlineFocused != "" && s.inlineFocused != focusedNow {
		b.flushAIEdit()
	}
	s.inlineFocused = focusedNow
}

func (b *Board) aiInlineRun(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, disabled bool) {
	pal := aiNodePalette(n.Kind(), o.Theme.Dark)
	run := b.ai.runs[id]
	if b.AIRunning() && run != nil && (run.status == "running" || run.status == "queued") {
		btn := ui.ButtonBase(c.Key("inline-stop-" + id)).Height(26).PaddingX(9).Gap(3).Center().Radius(6).Background(pal.dangerSoft).Focusable().Label(i18n.T("停止运行"))
		btn.Children(func() {
			if icon := boardIcon("player-stop"); icon != nil {
				ui.Icon(c, icon).FontSize(12).TextColor(pal.danger)
			}
			ui.Text(c, i18n.T("停止")).FontSize(11.5).FontWeight(600).TextColor(pal.danger).NoWrap()
		})
		if btn.Clicked() {
			b.StopAI()
		}
		return
	}
	btn := ui.ButtonBase(c.Key("inline-run-" + id)).Height(26).PaddingX(9).Gap(3).Center().Radius(6).Background(pal.accentSoft).Focusable().Disabled(disabled || b.AIRunning()).Label(i18n.T("运行此节点")).Tooltip(i18n.T("运行此节点"))
	if btn.Hovered() {
		btn.Background(pal.accent.Alpha(0.20))
	}
	btn.Children(func() {
		if icon := boardIcon("player-play"); icon != nil {
			ui.Icon(c, icon).FontSize(12).TextColor(pal.accent)
		}
		ui.Text(c, i18n.T("运行")).FontSize(11.5).FontWeight(600).TextColor(pal.accent).NoWrap()
	})
	if btn.Clicked() {
		b.selectIDs([]string{id})
		// A deliberate image run requests a new variation, even with the
		// same prompt. Whole-workflow runs still use the input cache.
		force := b.aiNode(b.Scene.ElementByID(id)).Role == whiteboard.AIImage
		b.RunAI([]string{id}, false, force, o)
	}
}

// aiInlineParams are a role's common settings on its card; the rest are in
// the node's full settings.
func (b *Board) aiInlineParams(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode, disabled bool) {
	set := func(fn func(*whiteboard.AIParams)) {
		b.updateAINode(o, id, func(n *whiteboard.AINode) { fn(&n.Params) })
	}
	p := n.Params
	key := func(k string) string { return "inline-" + k + "-" + id }
	switch n.Role {
	case whiteboard.AIText:
		values, labels := []string{""}, []string{i18n.T("常用指令")}
		for _, pr := range aiTextPresets {
			values, labels = append(values, i18n.T(pr.instruction)), append(labels, i18n.T(pr.label))
		}
		if v, ok := aiInlineSelect(c, key("preset"), i18n.T("常用指令"), "", values, labels, disabled || b.aiPromptSource(n) != ""); ok {
			b.setAINodeText(o, id, v)
			b.flushAIEdit()
		}
		if v, ok := aiInlineSelect(c, key("write"), i18n.T("结果写到"), p.Write, []string{"", "replace", "note"}, translated("仅节点内", "改写第一个文字输入", "节点旁新建便签"), disabled); ok {
			set(func(p *whiteboard.AIParams) { p.Write = v })
		}
	case whiteboard.AIImage:
		if v, ok := aiInlineSelect(c, key("ratio"), i18n.T("画幅比例"), p.AspectRatio, []string{"", "1:1", "16:9", "9:16", "4:3", "3:4"}, []string{i18n.T("比例"), "1:1", "16:9", "9:16", "4:3", "3:4"}, disabled); ok {
			set(func(p *whiteboard.AIParams) { p.AspectRatio = v })
		}
		values, labels, nativeSize := aiImageSizeChoices(o, n, true)
		if v, ok := aiInlineSelect(c, key("size"), i18n.T("尺寸"), p.Size, values, labels, disabled); ok {
			set(func(p *whiteboard.AIParams) {
				p.Size = v
				if nativeSize {
					p.Quality = ""
				}
			})
		}
		count := strconv.Itoa(max(1, p.Count))
		if v, ok := aiInlineSelect(c, key("count"), i18n.T("数量"), count, []string{"1", "2", "3", "4"}, []string{"×1", "×2", "×3", "×4"}, disabled); ok {
			k, _ := strconv.Atoi(v)
			set(func(p *whiteboard.AIParams) { p.Count = k })
		}
		if nativeSize {
			return
		}
		if v, ok := aiInlineSelect(c, key("quality"), i18n.T("质量"), p.Quality, []string{"", "low", "medium", "high"}, translated("质量", "低", "中", "高"), disabled); ok {
			set(func(p *whiteboard.AIParams) { p.Quality = v })
		}
	case whiteboard.AIVideo:
		secs := strconv.Itoa(p.Seconds)
		if v, ok := aiInlineSelect(c, key("seconds"), i18n.T("时长"), secs, []string{"0", "4", "5", "8", "10", "12"}, []string{i18n.T("时长"), "4s", "5s", "8s", "10s", "12s"}, disabled); ok {
			k, _ := strconv.Atoi(v)
			set(func(p *whiteboard.AIParams) { p.Seconds = k })
		}
		if v, ok := aiInlineSelect(c, key("vsize"), i18n.T("分辨率"), p.Size, []string{"", "1280x720", "720x1280"}, []string{i18n.T("分辨率"), "1280×720", "720×1280"}, disabled); ok {
			set(func(p *whiteboard.AIParams) { p.Size = v })
		}
		if v, ok := aiInlineSelect(c, key("vratio"), i18n.T("画幅比例"), p.AspectRatio, []string{"", "16:9", "9:16"}, []string{i18n.T("比例"), "16:9", "9:16"}, disabled); ok {
			set(func(p *whiteboard.AIParams) { p.AspectRatio = v })
		}
	}
}

// aiPickerMenu offers the nodes a link dropped on empty canvas can feed or
// come from.
func (b *Board) aiPickerMenu(c *ui.Context, o BoardOptions) {
	s := b.aiState()
	pk := s.picker
	if pk == nil || o.AI == nil {
		return
	}
	if b.Scene.ElementByID(pk.from+pk.to) == nil {
		s.picker = nil
		return
	}
	x, y, _, _ := b.canvasRect(pk.at[0], pk.at[1], 0, 0)
	left := x + 8
	if pk.to != "" {
		left = max(8, x-212)
	}
	st := boardChromeFor(o.Theme)
	boardSurface(ui.Column(c.Key("board-ai-picker")).Absolute().Left(left).Top(y).Width(204).Padding(8).Gap(3), st).Role(ui.RoleMenu).Label(i18n.T("接入新节点")).Children(func() {
		ui.Box(c).Padding(2, 6, 4, 6).Children(func() {
			boardSectionLabel(c, st, i18n.T("接入新节点"))
		})
		for _, role := range aiPickerRoles(pk) {
			label := i18n.T(aiRoleNames[role])
			kind := (&whiteboard.AINode{Role: role}).Kind()
			row := ui.ButtonBase(c.Key("pick-"+string(role))).WidthPercent(100).Padding(5, 8).Gap(8).AlignItems(ui.Center).Radius(6).Focusable().Label(label)
			if row.Hovered() {
				row.Background(st.hover)
			}
			row.Children(func() {
				aiItemBadge(c, aiRoleIcons[role], kind, o.Theme.Dark)
				ui.Text(c, label).FontSize(13).TextColor(st.text)
			})
			if row.Clicked() {
				b.pickAINode(role, o)
			}
		}
		if boardFlatButton(c, "pick-cancel", i18n.T("取消"), false, st).WidthPercent(100).Height(28).Clicked() {
			s.picker = nil
		}
	})
}
