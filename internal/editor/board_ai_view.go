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
	aiHeader = 40
	aiFooter = 28
	aiPort   = 7
)

var (
	aiStatusNames    = map[string]string{"queued": "排队中", "running": "生成中…", "done": "已完成", "cached": "输入未变化，沿用上次结果", "blocked": "上游节点失败，未运行", "stopped": "已停止"}
	aiRoleShortNames = map[whiteboard.AIRole]string{whiteboard.AIPrompt: "文字", whiteboard.AIText: "文本", whiteboard.AIImage: "图像", whiteboard.AIVideo: "视频", whiteboard.AIConcat: "拼接", whiteboard.AINote: "笔记"}
	aiRoleIcons      = map[whiteboard.AIRole]string{whiteboard.AIPrompt: "text", whiteboard.AIText: "text", whiteboard.AIImage: "image", whiteboard.AIVideo: "player-play", whiteboard.AIConcat: "group", whiteboard.AINote: "sticky-note"}
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

func (b *Board) aiCardText(id, text string, width float32, size float32) *ui.TextLayout {
	return b.aiCardTextFont(id, text, width, ui.Font{Size: size})
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

// paintAINode draws a node as a card in the element's local coordinates:
// a header with role badge, title and model pill, a clear content body,
// a status/metadata footer, and typed ports.
func (b *Board) paintAINode(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, dark bool) {
	s := b.aiState()
	w, h := float32(e.W), float32(e.H)
	kind := n.Kind()
	pal := aiNodePalette(kind, dark)
	card := ui.Rect{W: w, H: h}
	run := s.runs[e.ID]
	running := run != nil && (run.status == "running" || run.status == "queued")
	status, failed := b.aiStatus(e, n)
	inputs := b.Scene.AIInputs(e.ID)

	p.Shadow(card, 12, 0, 2, 10, 0, pal.shadow)
	p.Fill(card, pal.cardBg, 12)
	p.Clip(card, 12, func() {
		p.Fill(ui.Rect{W: w, H: aiHeader}, pal.headerBg, 0)
		p.Fill(ui.Rect{W: w, H: 3}, pal.accent, 0)
		p.Line(0, aiHeader, w, aiHeader, 1, pal.divider)

		shortRole := i18n.T(aiRoleShortNames[n.Role])
		roleLayout := b.aiCardTextFont(e.ID+"#role", shortRole, 0, ui.Font{Size: 11, Weight: 600})
		rw, _ := roleLayout.Size()
		badgeW := rw + 12
		p.Fill(ui.Rect{X: 12, Y: 12, W: badgeW, H: 19}, pal.accentSoft, 4)
		p.TextLayout(roleLayout, 18, 14, pal.accent)

		title := cmpOr(n.Title, i18n.T(aiRoleNames[n.Role]))
		model := n.Model
		if n.Role == whiteboard.AINote {
			model = ""
		}
		var modelReserve float32
		if model != "" {
			ml := b.aiCardTextFont(e.ID+"#model", model, 0, ui.Font{Size: 11.5, Weight: 500})
			mw, _ := ml.Size()
			mw = min(mw, w*0.36)
			pillW := mw + 12
			pillX := w - 12 - pillW
			p.Fill(ui.Rect{X: pillX, Y: 11, W: pillW, H: 20}, pal.divider.Alpha(0.55), 5)
			p.Clip(ui.Rect{X: pillX + 6, Y: 11, W: mw, H: 20}, 0, func() {
				p.TextLayout(ml, pillX+6, 14, pal.secondary)
			})
			modelReserve = pillW + 8
		}
		titleX := 12 + badgeW + 8
		titleW := max(0, w-12-modelReserve-titleX)
		tl := b.aiCardTextFont(e.ID+"#title", title, 0, ui.Font{Size: 13.5, Weight: 600})
		p.Clip(ui.Rect{X: titleX, Y: 4, W: titleW, H: aiHeader - 4}, 0, func() {
			p.TextLayout(tl, titleX, 13, pal.ink)
		})

		body := ui.Rect{X: 14, Y: aiHeader + 10, W: max(0, w-28), H: max(0, h-aiHeader-aiFooter-18)}
		p.Clip(body, 4, func() { b.paintAIBody(p, e, n, body, pal) })

		fy := h - aiFooter
		p.Fill(ui.Rect{Y: fy, W: w, H: aiFooter}, pal.footerBg, 0)
		p.Line(0, fy, w, fy, 1, pal.divider)

		var metaParts []string
		if len(inputs) > 0 {
			metaParts = append(metaParts, fmt.Sprintf(i18n.T("%d 输入"), len(inputs)))
		}
		if len(n.Outputs) > 0 {
			metaParts = append(metaParts, fmt.Sprintf("%d/%d", n.Current+1, len(n.Outputs)))
		}
		meta := strings.Join(metaParts, " · ")
		var metaW float32
		if meta != "" {
			ml := b.aiCardTextFont(e.ID+"#meta", meta, 0, ui.Font{Size: 11.5, Weight: 500})
			mw, _ := ml.Size()
			metaW = mw + 8
			p.TextLayout(ml, w-12-mw, fy+7, pal.muted)
		}
		statusW := max(0, w-24-metaW)
		sl := b.aiCardTextFont(e.ID+"#status", status, 0, ui.Font{Size: 11.5, Weight: 500})
		sw, _ := sl.Size()
		switch {
		case running:
			pw := min(statusW, sw+12)
			p.Fill(ui.Rect{X: 12, Y: fy + 4, W: pw, H: 20}, pal.accentSoft, 4)
			p.Clip(ui.Rect{X: 18, Y: fy + 4, W: max(0, pw-12), H: 20}, 0, func() {
				p.TextLayout(sl, 18, fy+7, pal.accent)
			})
		case failed:
			pw := min(statusW, sw+12)
			p.Fill(ui.Rect{X: 12, Y: fy + 4, W: pw, H: 20}, pal.dangerSoft, 4)
			p.Clip(ui.Rect{X: 18, Y: fy + 4, W: max(0, pw-12), H: 20}, 0, func() {
				p.TextLayout(sl, 18, fy+7, pal.danger)
			})
		default:
			p.Clip(ui.Rect{X: 12, Y: fy, W: statusW, H: aiFooter}, 0, func() {
				p.TextLayout(sl, 12, fy+7, pal.muted)
			})
		}
	})

	borderCol := pal.border
	borderW := float32(1.5)
	switch {
	case failed:
		borderCol, borderW = pal.danger, 2
	case b.Selected[e.ID] || running:
		borderCol, borderW = pal.accent, 2
	}
	p.Stroke(card, borderCol, 12, borderW)

	hoverID, hoverDir := b.aiPortHit(b.hover)
	showPorts := !b.ReadOnly && b.Tool == ToolSelect && (b.mode == "" || b.ai.portFrom != "")
	if n.Role != whiteboard.AINote && n.Role != whiteboard.AIPrompt {
		in := ui.Rect{X: -aiPort, Y: h/2 - aiPort, W: 2 * aiPort, H: 2 * aiPort}
		inLit := showPorts && ((hoverID == e.ID && hoverDir == "in") || (s.snapID == e.ID && s.snapDir == "in"))
		if inLit {
			halo := float32(aiPort + 5)
			p.Fill(ui.Rect{X: -halo, Y: h/2 - halo, W: 2 * halo, H: 2 * halo}, pal.accent.Alpha(0.26), halo)
		}
		inStroke := pal.muted
		if len(inputs) > 0 || inLit {
			inStroke = pal.accent
		}
		p.Fill(in, pal.cardBg, aiPort)
		p.Stroke(in, inStroke, aiPort, 1.75)
	}
	out := ui.Rect{X: w - aiPort, Y: h/2 - aiPort, W: 2 * aiPort, H: 2 * aiPort}
	outLit := showPorts && ((hoverID == e.ID && hoverDir == "out") || (s.snapID == e.ID && s.snapDir == "out"))
	if outLit {
		halo := float32(aiPort + 5)
		p.Fill(ui.Rect{X: w - halo, Y: h/2 - halo, W: 2 * halo, H: 2 * halo}, pal.accent.Alpha(0.26), halo)
	}
	p.Fill(out, pal.accent, aiPort)
	p.Stroke(out, pal.cardBg, aiPort, 2)
}

func (b *Board) aiInlineActive() bool {
	return b.options.AI != nil && !b.ReadOnly && b.mode == "" && b.Zoom >= 0.55 && b.editing == nil
}

func (b *Board) aiInlineReserve(n *whiteboard.AINode, bodyH float32) float32 {
	if !b.aiInlineActive() {
		return 0
	}
	switch n.Role {
	case whiteboard.AIPrompt:
		return bodyH
	case whiteboard.AINote:
		return min(bodyH, 42/float32(b.Zoom))
	case whiteboard.AIConcat:
		return min(bodyH*0.5, 96/float32(b.Zoom))
	default:
		// Compact control row (28) + gap (6) + prompt area (54) + breathing room (20) = 108px.
		return min(bodyH*0.58, 108/float32(b.Zoom))
	}
}

func (b *Board) paintAIBody(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, r ui.Rect, pal aiNodeColors) {
	s := b.aiState()
	drawText := func(id, t string, y float32, color ui.Color, font ui.Font) {
		p.TextLayout(b.aiCardTextFont(id, t, r.W, font), r.X, y, color)
	}
	if reserve := b.aiInlineReserve(n, r.H); reserve > 0 {
		r.Y += reserve
		r.H = max(0, r.H-reserve)
		if r.H < 18 {
			return
		}
	}
	if run := s.runs[e.ID]; run != nil && run.status == "running" && run.stream != "" {
		stream := run.stream
		if len(stream) > 4000 {
			stream = "…" + stream[len(stream)-4000:]
		}
		drawText(e.ID, stream, r.Y, pal.ink, ui.Font{Size: 13, LineHeight: 1.45})
		return
	}
	out := n.Output()
	if out == nil {
		if b.aiInlineActive() {
			hint := i18n.T("输入指令或连接左侧输入后点击运行")
			if n.Role == whiteboard.AINote || n.Role == whiteboard.AIPrompt {
				return
			}
			drawText(e.ID+"#inline-hint", hint, r.Y+4, pal.muted, ui.Font{Size: 12, LineHeight: 1.4})
			return
		}
		label, value := "", ""
		switch {
		case n.Instruction != "":
			label, value = i18n.T("指令"), n.Instruction
			if n.Role == whiteboard.AIPrompt {
				label = i18n.T("文字内容")
			}
		case n.Role == whiteboard.AINote && n.Note != "":
			label, value = i18n.T("引用笔记"), "[["+n.Note+"]]"
		case n.Role == whiteboard.AIConcat && n.Params.Template != "":
			label, value = i18n.T("拼接模板"), n.Params.Template
		}
		if value != "" {
			drawText(e.ID+"#cap", label, r.Y, pal.muted, ui.Font{Size: 11.5, Weight: 500})
			drawText(e.ID, value, r.Y+18, pal.secondary, ui.Font{Size: 13, LineHeight: 1.45})
			return
		}
		hint := i18n.T("用箭头把文字、图片、画框或其他节点连到左侧端口，然后运行。")
		switch n.Role {
		case whiteboard.AINote:
			hint = i18n.T("在属性面板中填写要引用的笔记。")
		case whiteboard.AIPrompt:
			hint = i18n.T("输入提示词或文本，拖出右侧端口连接到其他节点。")
		}
		drawText(e.ID, hint, r.Y, pal.muted, ui.Font{Size: 12.5, LineHeight: 1.45})
		return
	}
	switch out.Kind {
	case "text":
		drawText(e.ID, out.Text, r.Y, pal.ink, ui.Font{Size: 13, LineHeight: 1.45})
	case "image":
		bm := s.media[out.Path]
		if bm == nil {
			msg := i18n.T("正在加载图片")
			if s.mediaErr[out.Path] != nil {
				msg = i18n.T("图片无法读取：") + out.Path
			}
			drawText(e.ID, msg, r.Y, pal.muted, ui.Font{Size: 12.5, LineHeight: 1.45})
			return
		}
		iw, ih := bm.Size()
		scale := min(r.W/float32(iw), r.H/float32(ih))
		dw, dh := float32(iw)*scale, float32(ih)*scale
		p.Fill(r, pal.mediaBg, 6)
		p.ImageRegion(bm, ui.Rect{}, ui.Rect{X: r.X + (r.W-dw)/2, Y: r.Y + (r.H-dh)/2, W: dw, H: dh}, ui.ImageOptions{})
		p.Stroke(r, pal.divider, 6, 1)
	case "video":
		p.Fill(r, ui.Hex("#16181C"), 6)
		cx, cy := r.X+r.W/2, r.Y+r.H/2-14
		p.Fill(ui.Rect{X: cx - 18, Y: cy - 18, W: 36, H: 36}, ui.RGBA(255, 255, 255, 0.16), 18)
		p.Text(cx-5, cy-8, "▶", 14, ui.Hex("#F8F9FA"))
		p.Text(r.X+12, r.Y+r.H-42, path.Base(out.Path), 12.5, ui.Hex("#E6E8EC"))
		p.Text(r.X+12, r.Y+r.H-22, i18n.T("选中节点后播放"), 11.5, ui.Hex("#9CA3AF"))
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

// aiPortHit returns the node ID and port direction ("out" or "in") at a
// scene point, or ("", "") if none.
func (b *Board) aiPortHit(p whiteboard.Point) (string, string) {
	radius := max(14, float64(aiPort)+7) / max(0.25, b.Zoom)
	for i := len(b.Scene.Elements) - 1; i >= 0; i-- {
		e := b.Scene.Elements[i]
		n := b.aiNode(e)
		if e.IsDeleted || e.Locked || n == nil {
			continue
		}
		if whiteboard.Distance(p, whiteboard.Point{e.X + e.W, e.Y + e.H/2}) <= radius {
			return e.ID, "out"
		}
		if n.Role != whiteboard.AINote && n.Role != whiteboard.AIPrompt {
			if whiteboard.Distance(p, whiteboard.Point{e.X, e.Y + e.H/2}) <= radius {
				return e.ID, "in"
			}
		}
	}
	return "", ""
}

// aiPortAt is the node whose output port is at a scene point.
func (b *Board) aiPortAt(p whiteboard.Point) string {
	id, dir := b.aiPortHit(p)
	if dir == "out" {
		return id
	}
	return ""
}

// aiSnapTarget finds a compatible element to connect to while dragging from
// (fromID, fromDir), snapping magnetically to its matching port.
func (b *Board) aiSnapTarget(p whiteboard.Point, fromID, fromDir string) (*whiteboard.Element, whiteboard.Point, string) {
	portRadius := 32 / max(0.25, b.Zoom)
	bodyPad := 12 / max(0.25, b.Zoom)
	var hitBody *whiteboard.Element
	for i := len(b.Scene.Elements) - 1; i >= 0; i-- {
		e := b.Scene.Elements[i]
		if e.IsDeleted || e.ID == fromID || !e.IsBindable() {
			continue
		}
		n := b.aiNode(e)
		if fromDir == "out" {
			if n == nil || n.Role == whiteboard.AINote || n.Role == whiteboard.AIPrompt {
				continue
			}
			pt := whiteboard.Point{e.X, e.Y + e.H/2}
			if whiteboard.Distance(p, pt) <= portRadius {
				return e, pt, "in"
			}
			if hitBody == nil && whiteboard.HitTest(e, p, bodyPad) {
				hitBody = e
			}
		} else {
			if n != nil {
				pt := whiteboard.Point{e.X + e.W, e.Y + e.H/2}
				if whiteboard.Distance(p, pt) <= portRadius {
					return e, pt, "out"
				}
			}
			if hitBody == nil && whiteboard.HitTest(e, p, bodyPad) {
				hitBody = e
			}
		}
	}
	if hitBody == nil {
		return nil, p, ""
	}
	if fromDir == "out" {
		return hitBody, whiteboard.Point{hitBody.X, hitBody.Y + hitBody.H/2}, "in"
	}
	return hitBody, whiteboard.Point{hitBody.X + hitBody.W, hitBody.Y + hitBody.H/2}, "out"
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
	ui.Scroll(c.Key("board-ai-props")).Absolute().Top(66).Right(12).Width(308).MaxHeight(max(150, b.lastRect.H-140)).Padding(14).Gap(8).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(10).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.12)).Role(ui.RoleGroup).Label(i18n.T("节点属性")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			aiItemBadge(c, aiRoleIcons[n.Role], n.Kind(), o.Theme.Dark)
			ui.Text(c, i18n.T(aiRoleNames[n.Role])).FontSize(15).FontWeight(600).Grow(1)
			if boardIconButton(c, "close", "close", i18n.T("关闭"), false).Size(28, 28).Clicked() {
				b.Selected = map[string]bool{}
			}
		})
		if n.Role != whiteboard.AIPrompt {
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
			area := ui.TextArea(c.Key("board-ai-instruction"), &b.ai.buf.instruction).Placeholder(i18n.T("输入指令或提示词…")).WidthPercent(100).Height(104).Label(i18n.T("指令"))
			if s.focusInstruction {
				area.Focus()
				s.focusInstruction = false
			}
			if area.Changed() {
				b.setAINodeText(o, id, b.ai.buf.instruction)
			}
		}
		if n.Role != whiteboard.AIPrompt {
			aiPropDivider(c, o)
			inputs := b.Scene.AIInputs(id)
			aiPropSection(c, o, fmt.Sprintf(i18n.T("输入（%d）"), len(inputs)))
			if len(inputs) == 0 && n.Role != whiteboard.AINote {
				ui.Text(c, i18n.T("从文字、图片、画框或其他节点拉箭头到此节点，或从左侧端口反拉连线。")).FontSize(12).TextColor(o.Theme.TextMuted)
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
				if r := []rune(detail); len(r) > 36 {
					detail = string(r[:36]) + "…"
				}
				tag := i18n.T(kinds[in.Kind])
				if in.Mask {
					tag += " · " + i18n.T("遮罩")
				} else if in.Label != "" {
					tag += " · " + in.Label
				}
				ui.Row(c.Key(fmt.Sprint("in", i))).Padding(6, 8).Gap(6).AlignItems(ui.Center).Background(o.Theme.Surface).Border(1, o.Theme.Border.Alpha(0.6)).Radius(6).Children(func() {
					ui.Text(c, fmt.Sprintf("%d. %s", i+1, tag)).FontSize(12).FontWeight(500).TextColor(o.Theme.TextMuted).NoWrap()
					if detail != "" {
						ui.Text(c, detail).FontSize(12).TextColor(o.Theme.Text).SingleLine().Grow(1).MinWidth(0)
					} else {
						ui.Spacer(c)
					}
					if !b.ReadOnly && boardIconButton(c, "disconnect", "close", i18n.T("断开连接"), false).Size(22, 22).Clicked() {
						b.disconnectAI(in.From, id, o)
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

// disconnectAI deletes all bound arrows linking fromID to toID.
func (b *Board) disconnectAI(fromID, toID string, o BoardOptions) {
	if b.ReadOnly {
		return
	}
	var del []string
	for _, e := range b.Scene.Elements {
		if e.IsDeleted || e.Kind != whiteboard.KindArrow || e.StartBinding == nil || e.EndBinding == nil {
			continue
		}
		if e.StartBinding.ElementID == fromID && e.EndBinding.ElementID == toID {
			del = append(del, e.ID)
		}
	}
	if len(del) > 0 {
		b.edit(o, func() {
			b.Scene.Delete(del)
			b.cleanSelection()
		})
	}
}

// aiNodeInlineControls renders live model/param selectors, prompt editors, and
// run buttons directly inside visible AI nodes on the canvas.
func (b *Board) aiNodeInlineControls(c *ui.Context, o BoardOptions) {
	if !b.aiInlineActive() {
		if b.ai.inlineFocused != "" {
			b.ai.inlineFocused = ""
			b.flushAIEdit()
		}
		return
	}
	s := b.aiState()
	focusedNow := ""
	for _, e := range b.Scene.Elements {
		n := b.aiNode(e)
		if n == nil || e.IsDeleted || e.Locked || e.Angle != 0 {
			continue
		}
		x, y, w, h := b.canvasRect(e.X, e.Y, e.W, e.H)
		if x+w < 0 || y+h < 0 || x > b.lastRect.W || y > b.lastRect.H {
			continue
		}
		padX := 14 * float32(b.Zoom)
		bx := x + padX
		by := y + float32(aiHeader+10)*float32(b.Zoom)
		bw := w - 2*padX
		bh := h - float32(aiHeader+aiFooter+18)*float32(b.Zoom)
		if bw < 140 || bh < 42 {
			continue
		}
		id := e.ID
		buf := b.aiNodeBuf(e, n)
		colH := bh
		if n.Role != whiteboard.AIPrompt {
			colH = b.aiInlineReserve(n, float32(e.H-aiHeader-aiFooter-18)) * float32(b.Zoom)
		}
		ui.Column(c.Key("ai-inline-"+id)).Absolute().Left(bx).Top(by).Size(bw, colH).Gap(6).Children(func() {
			switch n.Role {
			case whiteboard.AIPrompt:
				area := ui.TextArea(c.Key("board-ai-inline-text-"+id), &buf.text).Placeholder(i18n.T("输入提示词或文本，拖出右侧端口连接到其他节点…")).FontSize(13).WidthPercent(100).Grow(1).MinHeight(0).Label(i18n.T("文字内容"))
				if area.Focused() {
					focusedNow = id
					if !b.Selected[id] || len(b.Selected) != 1 {
						b.selectIDs([]string{id})
					}
				}
				if area.Changed() {
					b.setAINodeText(o, id, buf.text)
				}
				if area.Shortcut(0, ui.KeyEscape) {
					b.RequestFocus = true
				}
			case whiteboard.AINote:
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					in := ui.TextInput(c.Key("board-ai-inline-text-"+id), &buf.text).Placeholder(i18n.T("输入引用的笔记名称…")).FontSize(12.5).Grow(1).MinWidth(0).Height(30).Label(i18n.T("引用的笔记"))
					if in.Focused() {
						focusedNow = id
						if !b.Selected[id] || len(b.Selected) != 1 {
							b.selectIDs([]string{id})
						}
					}
					if in.Changed() {
						b.setAINodeText(o, id, buf.text)
					}
					if in.Shortcut(0, ui.KeyEscape) {
						b.RequestFocus = true
					}
					if n.Note != "" && o.OpenWiki != nil && boardButton(c, "inline-open-note-"+id, i18n.T("打开"), false).Height(30).Clicked() {
						o.OpenWiki(n.Note, false)
					}
				})
			case whiteboard.AIConcat:
				area := ui.TextArea(c.Key("board-ai-inline-text-"+id), &buf.text).Placeholder(i18n.T("{1}、{2} 为输入，{all} 为全部")).FontSize(12.5).WidthPercent(100).Height(max(44, colH-8)).Label(i18n.T("拼接模板"))
				if area.Focused() {
					focusedNow = id
					if !b.Selected[id] || len(b.Selected) != 1 {
						b.selectIDs([]string{id})
					}
				}
				if area.Changed() {
					b.setAINodeText(o, id, buf.text)
				}
				if area.Shortcut(0, ui.KeyEscape) {
					b.RequestFocus = true
				}
			default:
				ui.Row(c.Key("inline-bar-" + id)).Height(28).Gap(4).AlignItems(ui.Center).Children(func() {
					b.aiInlineModelSelect(c, o, id, n)
					if n.Role == whiteboard.AIImage && bw >= 240 {
						ratios := []string{"", "1:1", "16:9", "9:16", "4:3", "3:4"}
						labels := []string{i18n.T("比例"), "1:1", "16:9", "9:16", "4:3", "3:4"}
						shown := labels[0]
						for i, v := range ratios {
							if v == n.Params.AspectRatio {
								shown = labels[i]
							}
						}
						before := shown
						ui.Select(c.Key("inline-ratio-"+id), &shown, labels).Width(70).Height(28).FontSize(11.5).Label(i18n.T("画幅比例"))
						if shown != before {
							for i, l := range labels {
								if l == shown {
									v := ratios[i]
									b.updateAINode(o, id, func(node *whiteboard.AINode) { node.Params.AspectRatio = v })
								}
							}
						}
					} else if n.Role == whiteboard.AIVideo && bw >= 240 {
						secs := []string{"0", "4", "5", "8", "10"}
						labels := []string{i18n.T("时长"), "4s", "5s", "8s", "10s"}
						cur := strconv.Itoa(n.Params.Seconds)
						shown := labels[0]
						for i, v := range secs {
							if v == cur {
								shown = labels[i]
							}
						}
						before := shown
						ui.Select(c.Key("inline-sec-"+id), &shown, labels).Width(64).Height(28).FontSize(11.5).Label(i18n.T("时长"))
						if shown != before {
							for i, l := range labels {
								if l == shown {
									k, _ := strconv.Atoi(secs[i])
									b.updateAINode(o, id, func(node *whiteboard.AINode) { node.Params.Seconds = k })
								}
							}
						}
					}
					run := s.runs[id]
					nodeRunning := b.AIRunning() && run != nil && (run.status == "running" || run.status == "queued")
					if nodeRunning {
						stopBtn := ui.ButtonBase(c.Key("inline-stop-" + id)).Height(28).PaddingX(8).Gap(3).Center().Radius(6).Background(o.Theme.Danger.Alpha(0.14)).Focusable().Label(i18n.T("停止运行"))
						stopBtn.Children(func() {
							if icon := boardIcon("player-stop"); icon != nil {
								ui.Icon(c, icon).FontSize(12).TextColor(o.Theme.Danger)
							}
							ui.Text(c, i18n.T("停止")).FontSize(11.5).FontWeight(600).TextColor(o.Theme.Danger).NoWrap()
						})
						if stopBtn.Clicked() {
							b.StopAI()
						}
					} else {
						runBtn := ui.ButtonBase(c.Key("inline-run-" + id)).Height(28).PaddingX(8).Gap(3).Center().Radius(6).Background(o.Theme.Accent.Alpha(0.14)).Focusable().Disabled(b.AIRunning()).Label(i18n.T("运行此节点"))
						if runBtn.Hovered() && !b.AIRunning() {
							runBtn.Background(o.Theme.Accent.Alpha(0.22))
						}
						runBtn.Children(func() {
							if icon := boardIcon("player-play"); icon != nil {
								ui.Icon(c, icon).FontSize(12).TextColor(o.Theme.Accent)
							}
							ui.Text(c, i18n.T("运行")).FontSize(11.5).FontWeight(600).TextColor(o.Theme.Accent).NoWrap()
						})
						if runBtn.Clicked() {
							b.selectIDs([]string{id})
							b.RunAI([]string{id}, false, false, o)
						}
					}
				})
				area := ui.TextArea(c.Key("board-ai-inline-text-"+id), &buf.text).Placeholder(i18n.T("输入指令或提示词（也可连接左侧节点）…")).FontSize(12.5).WidthPercent(100).Height(max(42, colH-52)).Label(i18n.T("指令"))
				if area.Focused() {
					focusedNow = id
					if !b.Selected[id] || len(b.Selected) != 1 {
						b.selectIDs([]string{id})
					}
				}
				if area.Changed() {
					b.setAINodeText(o, id, buf.text)
				}
				if area.Shortcut(0, ui.KeyEscape) {
					b.RequestFocus = true
				}
			}
		})
	}
	if s.inlineFocused != "" && s.inlineFocused != focusedNow {
		b.flushAIEdit()
	}
	s.inlineFocused = focusedNow
}

func (b *Board) aiInlineModelSelect(c *ui.Context, o BoardOptions, id string, n *whiteboard.AINode) {
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
	ui.Select(c.Key("inline-model-"+id), &current, labels).Grow(1).MinWidth(0).Height(28).FontSize(11.5).Label(i18n.T("模型"))
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
	if e != nil && b.mode == "" {
		x, y, w, h := b.canvasRect(e.X, e.Y, e.W, e.H)
		out := n.Output()
		if n.Role != whiteboard.AIPrompt || len(n.Outputs) > 1 || out != nil {
			ui.Row(c.Key("board-ai-node-bar")).Absolute().Left(x).Top(max(56, y-44)).Gap(4).Padding(4, 6).AlignItems(ui.Center).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(10).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.12)).Children(func() {
				if !b.ReadOnly && n.Role != whiteboard.AIPrompt {
					if b.AIRunning() {
						btn := ui.ButtonBase(c.Key("board-ai-node-stop")).Height(28).PaddingX(8).Gap(4).Center().Radius(6).Focusable().Label(i18n.T("停止运行")).Tooltip(i18n.T("停止运行"))
						if btn.Hovered() {
							btn.Background(o.Theme.SurfaceHover)
						}
						btn.Children(func() {
							if icon := boardIcon("player-stop"); icon != nil {
								ui.Icon(c, icon).FontSize(14).TextColor(o.Theme.Danger)
							}
							ui.Text(c, i18n.T("停止")).FontSize(12).FontWeight(500).TextColor(o.Theme.Text).NoWrap()
						})
						if btn.Clicked() {
							b.StopAI()
						}
					} else {
						btn := ui.ButtonBase(c.Key("board-ai-node-run")).Height(28).PaddingX(8).Gap(4).Center().Radius(6).Background(o.Theme.Accent.Alpha(0.14)).Focusable().Label(i18n.T("运行节点")).Tooltip(i18n.T("运行节点"))
						if btn.Hovered() {
							btn.Background(o.Theme.Accent.Alpha(0.22))
						}
						btn.Children(func() {
							if icon := boardIcon("player-play"); icon != nil {
								ui.Icon(c, icon).FontSize(14).TextColor(o.Theme.Accent)
							}
							ui.Text(c, i18n.T("运行")).FontSize(12).FontWeight(600).TextColor(o.Theme.Accent).NoWrap()
						})
						if btn.Clicked() {
							b.RunAI([]string{e.ID}, false, false, o)
						}
					}
				}
				if len(n.Outputs) > 1 {
					if !b.ReadOnly && n.Role != whiteboard.AIPrompt {
						ui.Box(c).Width(1).Height(16).Margin(0, 2).Background(o.Theme.Border)
					}
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
				reserve := b.aiInlineReserve(n, float32(e.H-aiHeader-aiFooter-18))
				bx, by := x+14*float32(b.Zoom), y+(float32(aiHeader+10)+reserve)*float32(b.Zoom)
				bw, bh := w-28*float32(b.Zoom), h-(float32(aiHeader+aiFooter+18)+reserve)*float32(b.Zoom)
				if bh > 24 {
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

// aiPicker offers node types where a line from a port was let go.
func (b *Board) aiPicker(c *ui.Context, o BoardOptions) {
	s := b.aiState()
	if s.pickerAt == nil || o.AI == nil {
		return
	}
	origin := b.Scene.ElementByID(s.pickerFrom)
	if origin == nil {
		s.pickerAt = nil
		return
	}
	at := *s.pickerAt
	x, y, _, _ := b.canvasRect(at[0], at[1], 0, 0)
	backward := s.pickerDir == "in"
	var roles []whiteboard.AIRole
	if backward {
		roles = []whiteboard.AIRole{whiteboard.AIPrompt, whiteboard.AINote, whiteboard.AIText, whiteboard.AIImage}
	} else {
		kind := b.aiOutputKind(origin)
		roles = []whiteboard.AIRole{whiteboard.AIText, whiteboard.AIImage, whiteboard.AIVideo, whiteboard.AIConcat}
		if kind != "text" {
			roles = []whiteboard.AIRole{whiteboard.AIImage, whiteboard.AIVideo, whiteboard.AIText}
		}
	}
	left := x + 8
	if backward {
		left = max(8, x-204)
	}
	ui.Column(c.Key("board-ai-picker")).Absolute().Left(left).Top(y).Width(196).Padding(8).Gap(3).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(10).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.14)).Role(ui.RoleMenu).Label(i18n.T("接入新节点")).Children(func() {
		ui.Text(c, i18n.T("接入新节点")).FontSize(12).FontWeight(600).TextColor(o.Theme.TextMuted).Margin(2, 6, 4, 6)
		for _, role := range roles {
			label := i18n.T(aiRoleNames[role])
			nodeKind := "text"
			if role == whiteboard.AIImage {
				nodeKind = "image"
			} else if role == whiteboard.AIVideo {
				nodeKind = "video"
			}
			row := ui.ButtonBase(c.Key("pick-"+string(role))).WidthPercent(100).Padding(5, 8).Gap(8).AlignItems(ui.Center).Radius(6).Focusable().Label(label)
			if row.Hovered() {
				row.Background(o.Theme.SurfaceHover)
			}
			row.Children(func() {
				aiItemBadge(c, aiRoleIcons[role], nodeKind, o.Theme.Dark)
				ui.Text(c, label).FontSize(13).TextColor(o.Theme.Text)
			})
			if row.Clicked() {
				b.removeAIPickerLine(o)
				w, h := whiteboard.AINodeSize(role)
				cx := at[0] + w/2
				if backward {
					cx = at[0] - w/2
				}
				id := b.AddAINode(role, whiteboard.Point{cx, at[1]}, o)
				if node := b.Scene.ElementByID(id); node != nil {
					node.Y = at[1] - h/2
					b.edit(o, func() {
						if backward {
							b.connectAI(node, origin)
						} else {
							b.connectAI(origin, node)
						}
					})
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

// aiPortCursor reports a hover over a node's input or output port.
func (b *Board) aiPortCursor() bool {
	if b.Tool != ToolSelect || b.mode != "" || b.ReadOnly {
		return false
	}
	id, _ := b.aiPortHit(b.hover)
	return id != ""
}
