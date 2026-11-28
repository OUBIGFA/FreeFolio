package editor

import (
	"fmt"
	"strconv"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// Comparison is view state only. Current remains the image supplied to
// downstream nodes; read-only browsing has a temporary selection instead.
type aiImageHistory struct {
	compare  bool
	a, b     int
	c, d     int
	mode     string
	split    float32
	view     int
	current  int
	revision int64
}

func aiImageBatchCount(n *whiteboard.AINode) int {
	if !n.ImageHistory() {
		return 1
	}
	count := max(1, n.Params.Count)
	if len(n.Outputs) == 0 {
		return min(4, count)
	}
	current := max(0, min(n.Current, len(n.Outputs)-1))
	run := n.Outputs[current].Run
	if run > 0 {
		start, end := current, current+1
		for start > 0 && n.Outputs[start-1].Run == run {
			start--
		}
		for end < len(n.Outputs) && n.Outputs[end].Run == run {
			end++
		}
		count = max(count, end-start)
	}
	return min(4, count)
}

// Expand in the same undo step as changing the batch or receiving results.
// Keep the board viewport and the node's position stable, and never shrink a
// node the user made larger. Each tile gets the area of a full single image.
func growAIImageGallery(e *whiteboard.Element, n *whiteboard.AINode) {
	count := aiImageBatchCount(n)
	if count < 2 {
		return
	}
	e.W = max(e.W, 720)
	rows := 1
	if count > 2 {
		rows = 2
	}
	tileWidth := (e.W - 2*aiPad - 6) / 2
	preview := float64(rows)*(tileWidth+26) + float64(rows-1)*6
	e.H = max(e.H, aiMinHeight(n)-float64(aiPreviewMin(n.Role))+preview+62)
}

func (b *Board) aiHistory(e *whiteboard.Element, n *whiteboard.AINode) *aiImageHistory {
	s := b.aiState()
	if s.history == nil {
		s.history = map[string]*aiImageHistory{}
	}
	h := s.history[e.ID]
	if h == nil {
		h = &aiImageHistory{a: max(0, n.Current-1), b: n.Current, split: .5, view: -1, current: n.Current}
		s.history[e.ID] = h
	}
	_, nonce := e.ShapeRevision()
	if h.revision != nonce {
		h.view = -1
		h.revision = nonce
	}
	if h.current != n.Current {
		// Selecting another output (or completing a run) returns to that
		// image so the displayed reference agrees with downstream input.
		h.compare, h.current = false, n.Current
	}
	h.a = max(0, min(h.a, len(n.Outputs)-1))
	h.b = max(0, min(h.b, len(n.Outputs)-1))
	h.c = max(0, min(h.c, len(n.Outputs)-1))
	h.d = max(0, min(h.d, len(n.Outputs)-1))
	if len(n.Outputs) < 2 {
		h.compare = false
	}
	return h
}

func (b *Board) aiHistoryCurrent(e *whiteboard.Element, n *whiteboard.AINode) int {
	h := b.aiHistory(e, n)
	if (b.ReadOnly || e.Locked) && h.view >= 0 {
		return h.view
	}
	return max(0, min(n.Current, len(n.Outputs)-1))
}

func (b *Board) aiSelectImage(o BoardOptions, e *whiteboard.Element, n *whiteboard.AINode, index int) {
	if index < 0 || index >= len(n.Outputs) {
		return
	}
	b.aiHistory(e, n).compare = false
	if b.ReadOnly || e.Locked {
		b.aiHistory(e, n).view = index
		return
	}
	b.updateAINode(o, e.ID, func(n *whiteboard.AINode) { n.Current = index })
}

func (b *Board) aiPreviewPaths(e *whiteboard.Element, n *whiteboard.AINode) []string {
	if n.Role == whiteboard.AICompare {
		var paths []string
		for _, slot := range []string{"a", "b"} {
			from := b.Scene.AILinkSource(n, slot)
			if from == nil {
				continue
			}
			if fn := b.aiNode(from); fn != nil && (fn.ImageHistory() || fn.Role == whiteboard.AICutout || fn.Role == whiteboard.AIUpscale) {
				if rel := b.DisplayedAIImage(from.ID); rel != "" {
					paths = append(paths, rel)
				}
			}
		}
		return paths
	}
	if n.ImageHistory() && len(n.Outputs) > 0 {
		var paths []string
		for _, index := range b.aiHistoryImages(e, n) {
			paths = append(paths, n.Outputs[index].Path)
		}
		return paths
	}
	if out := n.Output(); out != nil && out.Kind == "image" && out.Path != "" {
		return []string{out.Path}
	}
	return nil
}

// The current output selects a batch, while comparison picks remain local to
// the view. Legacy outputs without run metadata are still browsed individually.
func (b *Board) aiHistoryImages(e *whiteboard.Element, n *whiteboard.AINode) []int {
	h := b.aiHistory(e, n)
	if h.compare {
		if h.mode == "four" {
			return []int{h.a, h.b, h.c, h.d}
		}
		return []int{h.a, h.b}
	}
	current := b.aiHistoryCurrent(e, n)
	run := n.Outputs[current].Run
	start, end := current, current+1
	if run > 0 {
		for start > 0 && n.Outputs[start-1].Run == run {
			start--
		}
		for end < len(n.Outputs) && n.Outputs[end].Run == run {
			end++
		}
	}
	// Providers normally return at most four. Keep larger legacy batches
	// reachable through history, four at a time, without discarding results.
	start += (current - start) / 4 * 4
	var indices []int
	for i := start; i < min(start+4, end); i++ {
		indices = append(indices, i)
	}
	return indices
}

// Shared geometry keeps the painted tiles and their click targets aligned.
func aiHistoryCell(r ui.Rect, count, index int) ui.Rect {
	if count <= 1 {
		return r
	}
	rows := 1
	if count > 2 {
		rows = 2
	}
	w, h := (r.W-6)/2, (r.H-float32(rows-1)*6)/float32(rows)
	return ui.Rect{X: r.X + float32(index%2)*(w+6), Y: r.Y + float32(index/2)*(h+6), W: w, H: h}
}

func aiHistoryLabels(n *whiteboard.AINode) ([]string, []string) {
	values, labels := make([]string, len(n.Outputs)), make([]string, len(n.Outputs))
	for i, out := range n.Outputs {
		values[i] = strconv.Itoa(i)
		labels[i] = fmt.Sprintf(i18n.T("第 %d 张 · 第 %d 次"), i+1, out.Run)
	}
	return values, labels
}

func (b *Board) aiHistoryImageControls(c *ui.Context, o BoardOptions, e *whiteboard.Element, n *whiteboard.AINode, l aiCardLayout, x, y, z float32) {
	if !n.ImageHistory() || len(n.Outputs) == 0 || l.preview.H <= 12 {
		return
	}
	h := b.aiHistory(e, n)
	if h.compare && h.mode != "two" && h.mode != "four" {
		return
	}
	images := b.aiHistoryImages(e, n)
	if len(images) <= 1 {
		return
	}
	st := boardChromeFor(o.Theme)
	values, labels := aiHistoryLabels(n)
	for i, index := range images {
		r := aiHistoryCell(l.preview, len(images), i)
		ui.Row(c.Key(fmt.Sprintf("ai-image-cell-%s-%d", e.ID, i))).Absolute().Left(x+r.X*z).Top(y+(r.Y+r.H-26)*z).Size(r.W, 26).TransformOrigin(0, 0).Scale(z, z).Gap(4).Children(func() {
			label := strconv.Itoa(index + 1)
			if h.compare {
				field := []string{"对比图片 A", "对比图片 B", "对比图片 C", "对比图片 D"}[i]
				ui.Text(c, string(rune('A'+i))).FontSize(12).TextColor(st.muted).PaddingX(4)
				if value, ok := aiInlineSelect(c, "image", i18n.T(field), strconv.Itoa(index), values, labels, false); ok {
					selected, _ := strconv.Atoi(value)
					*[]*int{&h.a, &h.b, &h.c, &h.d}[i] = selected
				}
			} else {
				selected := index == b.aiHistoryCurrent(e, n)
				if selected {
					label += " · " + i18n.T("输出")
				}
				if boardFlatButton(c, "select", label, selected, st).Height(26).Grow(1).MinWidth(0).PaddingX(6).Label(fmt.Sprintf(i18n.T("使用第 %d 张图片"), index+1)).Clicked() {
					b.aiSelectImage(o, e, n, index)
				}
			}
			if o.AI.OpenHistoryImage != nil && boardFlatButton(c, "open", i18n.T("查看"), false, st).Height(26).PaddingX(6).Label(fmt.Sprintf(i18n.T("查看第 %d 张图片"), index+1)).Clicked() {
				o.AI.OpenHistoryImage(e.ID, index)
			}
		})
	}
}

func (b *Board) aiHistoryControls(c *ui.Context, o BoardOptions, e *whiteboard.Element, n *whiteboard.AINode, l aiCardLayout, x, y, z float32) {
	if l.history.H == 0 {
		return
	}
	h := b.aiHistory(e, n)
	current := b.aiHistoryCurrent(e, n)
	values, labels := aiHistoryLabels(n)
	st := boardChromeFor(o.Theme)
	ui.Column(c.Key("ai-history-"+e.ID)).Absolute().Left(x+l.history.X*z).Top(y+l.history.Y*z).Size(l.history.W, l.history.H).TransformOrigin(0, 0).Scale(z, z).Gap(4).Children(func() {
		ui.Row(c).Height(26).Gap(4).AlignItems(ui.Center).Children(func() {
			if boardFlatButton(c, "previous", "‹", false, st).Width(24).Height(26).PaddingX(0).Label(i18n.T("上张图片")).Disabled(current == 0).Clicked() {
				b.aiSelectImage(o, e, n, current-1)
			}
			if v, ok := aiInlineSelect(c, "current", i18n.T("图片历史"), strconv.Itoa(current), values, labels, false); ok {
				index, _ := strconv.Atoi(v)
				b.aiSelectImage(o, e, n, index)
			}
			if boardFlatButton(c, "next", "›", false, st).Width(24).Height(26).PaddingX(0).Label(i18n.T("下张图片")).Disabled(current == len(n.Outputs)-1).Clicked() {
				b.aiSelectImage(o, e, n, current+1)
			}
			text, label := i18n.T("对比"), i18n.T("对比图片")
			if h.compare {
				text, label = i18n.T("退出"), i18n.T("退出对比")
			}
			if boardFlatButton(c, "compare", text, h.compare, st).Height(26).PaddingX(8).Label(label).Disabled(len(n.Outputs) < 2).Clicked() {
				images := b.aiHistoryImages(e, n)
				h.compare = !h.compare
				if h.compare {
					h.mode = "slider"
					h.b, h.a = current, max(0, current-1)
					if h.a == h.b {
						h.a = min(current+1, len(n.Outputs)-1)
					}
					h.c, h.d = min(current+2, len(n.Outputs)-1), min(current+3, len(n.Outputs)-1)
					if len(images) > 1 {
						h.a, h.b, h.mode = images[0], images[1], "two"
						if len(images) > 2 {
							h.c, h.d, h.mode = images[2], images[len(images)-1], "four"
						}
					}
				}
			}
		})
		if !h.compare {
			return
		}
		ui.Row(c).Height(26).Gap(4).Children(func() {
			for _, mode := range []struct{ value, label string }{{"slider", "滑杆"}, {"two", "两格"}, {"four", "四格"}} {
				if boardFlatButton(c, mode.value, i18n.T(mode.label), h.mode == mode.value, st).Height(26).Grow(1).Label(i18n.T(mode.label)).Clicked() {
					h.mode = mode.value
				}
			}
		})
		if h.mode == "two" || h.mode == "four" {
			return
		}
		fields := []struct {
			label, description string
			value              *int
		}{{"A", i18n.T("对比图片 A"), &h.a}, {"B", i18n.T("对比图片 B"), &h.b}}
		ui.Row(c).Height(26).Gap(4).AlignItems(ui.Center).Children(func() {
			for _, field := range fields {
				ui.Box(c).Size(18, 18).Radius(4).Background(st.surfaceMuted).Center().Shrink(0).Children(func() {
					ui.Text(c, field.label).FontSize(11).FontWeight(600).TextColor(st.muted)
				})
				if v, ok := aiInlineSelect(c, field.label, field.description, strconv.Itoa(*field.value), values, labels, false); ok {
					*field.value, _ = strconv.Atoi(v)
				}
			}
		})
	})
	if !h.compare || l.preview.H < 12 {
		return
	}
	b.aiHistoryCompareHandle(c, e, n, l, x, y, z)
}

func (b *Board) aiHistoryCompareHandle(c *ui.Context, e *whiteboard.Element, n *whiteboard.AINode, l aiCardLayout, x, y, z float32) {
	if !n.ImageHistory() || len(n.Outputs) < 2 || l.preview.H < 12 {
		return
	}
	h := b.aiHistory(e, n)
	if !h.compare || h.mode == "two" || h.mode == "four" {
		return
	}
	r := l.preview
	// A fixed full-preview hit area keeps pointer capture stable while the
	// divider moves and prevents the gesture from dragging the board node.
	handle := ui.Box(c.Key("ai-compare-handle-"+e.ID)).Absolute().Left(x+r.X*z).Top(y+r.Y*z).Size(r.W, r.H).TransformOrigin(0, 0).Scale(z, z).Focusable().Role(ui.RoleSlider).Label(i18n.T("图片对比分隔线")).Value(fmt.Sprintf("%.0f%%", h.split*100)).Cursor(ui.CursorResizeEW)
	if _, _, held := handle.Dragged(); held {
		if px, _, ok := handle.PointerPosition(); ok {
			h.split = max(0, min(1, px/r.W))
		}
	}
	if handle.Shortcut(0, ui.KeyLeft) {
		h.split = max(0, h.split-.05)
	}
	if handle.Shortcut(0, ui.KeyRight) {
		h.split = min(1, h.split+.05)
	}
	if handle.Shortcut(0, ui.KeyHome) {
		h.split = 0
	}
	if handle.Shortcut(0, ui.KeyEnd) {
		h.split = 1
	}
}

// The node-local A/B selection and clipped, aspect-preserving comparison
// follow rgthree Image Comparer's interaction, implemented with MyGo.
func (b *Board) paintAIImageHistory(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, r ui.Rect, pal aiNodeColors, zoom float32) {
	h := b.aiHistory(e, n)
	paths := b.aiPreviewPaths(e, n)
	draw := func(rel string, r ui.Rect) {
		p.Fill(r, pal.mediaBg, 0)
		bm := b.ai.media[rel]
		if bm == nil {
			msg := i18n.T("正在加载图片")
			if b.ai.mediaErr[rel] != nil {
				msg = i18n.T("图片无法读取：") + rel
			}
			p.TextLayout(b.aiCardTextFont(e.ID+"#history-"+rel, msg, max(0, r.W-20), ui.Font{Size: 12}), r.X+10, r.Y+16, pal.muted)
			return
		}
		iw, ih := bm.Size()
		scale := min(r.W/float32(iw), r.H/float32(ih))
		w, height := float32(iw)*scale, float32(ih)*scale
		p.ImageRegion(bm, ui.Rect{}, ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-height)/2, W: w, H: height}, ui.ImageOptions{})
	}
	if h.compare && h.mode != "two" && h.mode != "four" {
		draw(paths[0], r)
		splitX := r.X + r.W*h.split
		p.Clip(ui.Rect{X: splitX, Y: r.Y, W: r.W * (1 - h.split), H: r.H}, 0, func() { draw(paths[1], r) })
		paintCompareDivider(p, r, h.split, zoom)
		s := compareBadgeScale(zoom)
		pad := 8 * s
		paintCompareBadge(p, r.X+pad, r.Y+pad, zoom, "A")
		paintCompareBadgeRight(p, r.X+r.W-pad, r.Y+pad, zoom, "B")
	} else {
		indices := b.aiHistoryImages(e, n)
		for i, rel := range paths {
			cell := aiHistoryCell(r, len(paths), i)
			picture := cell
			if len(paths) > 1 {
				picture.H = max(0, picture.H-26)
			}
			p.Clip(picture, 4, func() { draw(rel, picture) })
			if len(paths) > 1 {
				color := pal.divider
				if !h.compare && indices[i] == b.aiHistoryCurrent(e, n) {
					color = pal.accent
				}
				p.Stroke(cell, color, 4, 1)
			}
		}
	}
	p.Stroke(r, pal.divider.Alpha(0.75), 8, 1)
}
