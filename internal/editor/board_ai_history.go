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
	split    float32
	view     int
	current  int
	revision int64
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
	if n.Role == whiteboard.AIImage && len(n.Outputs) > 0 {
		h := b.aiHistory(e, n)
		if h.compare {
			return []string{n.Outputs[h.a].Path, n.Outputs[h.b].Path}
		}
		return []string{n.Outputs[b.aiHistoryCurrent(e, n)].Path}
	}
	if out := n.Output(); out != nil && out.Kind == "image" && out.Path != "" {
		return []string{out.Path}
	}
	return nil
}

func aiHistoryLabels(n *whiteboard.AINode) ([]string, []string) {
	values, labels := make([]string, len(n.Outputs)), make([]string, len(n.Outputs))
	for i, out := range n.Outputs {
		values[i] = strconv.Itoa(i)
		labels[i] = fmt.Sprintf(i18n.T("第 %d 张 · 第 %d 次"), i+1, out.Run)
	}
	return values, labels
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
				h.compare = !h.compare
				if h.compare {
					h.b, h.a = current, max(0, current-1)
					if h.a == h.b {
						h.a = min(current+1, len(n.Outputs)-1)
					}
				}
			}
		})
		if !h.compare {
			return
		}
		ui.Row(c).Height(26).Gap(4).AlignItems(ui.Center).Children(func() {
			for _, field := range []struct {
				label, description string
				value              *int
			}{{"A", i18n.T("对比图片 A"), &h.a}, {"B", i18n.T("对比图片 B"), &h.b}} {
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
func (b *Board) paintAIImageHistory(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, r ui.Rect, pal aiNodeColors) {
	h := b.aiHistory(e, n)
	paths := b.aiPreviewPaths(e, n)
	draw := func(rel string) {
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
	draw(paths[0])
	if h.compare {
		splitX := r.X + r.W*h.split
		p.Clip(ui.Rect{X: splitX, Y: r.Y, W: r.W * (1 - h.split), H: r.H}, 0, func() { draw(paths[1]) })
		p.Line(splitX, r.Y, splitX, r.Y+r.H, 3, ui.RGBA(0, 0, 0, 0.42))
		p.Line(splitX, r.Y, splitX, r.Y+r.H, 1.5, ui.RGB(255, 255, 255))
		pill := ui.Rect{X: splitX - 10, Y: r.Y + r.H/2 - 14, W: 20, H: 28}
		p.Fill(pill, ui.RGB(255, 255, 255), 10)
		p.Stroke(pill, ui.RGBA(0, 0, 0, 0.22), 10, 1)
		p.Text(splitX-6, r.Y+r.H/2-8, "↔", 12, ui.Hex("#1F2329"))
		badge := func(bx float32, text string) {
			box := ui.Rect{X: bx, Y: r.Y + 8, W: 20, H: 18}
			p.Fill(box, ui.RGBA(17, 19, 24, 0.72), 4)
			p.Text(bx+6, r.Y+10, text, 11, ui.RGB(255, 255, 255))
		}
		badge(r.X+8, "A")
		badge(r.X+r.W-28, "B")
	}
	p.Stroke(r, pal.divider.Alpha(0.75), 8, 1)
}
