package editor

import (
	"bytes"
	"fmt"
	"image"
	"math"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
)

var (
	compareModeValues = []string{"slide", "side", "overlay", "diff", "toggle"}
	compareModeLabels = []string{"卷帘", "并排", "叠化", "差异", "切换"}
	compareBgValues   = []string{"checker", "dark", "light"}
	compareBgLabels   = []string{"棋盘", "深底", "浅底"}
)

type aiCompareState struct {
	split        float32
	holdA        bool
	modeOverride string
	bgOverride   string
}

type aiCompareDiffCache struct {
	bitmap  *ui.Bitmap
	err     error
	pending bool
	wanted  bool
}

type aiCompareInput struct {
	ok    bool
	bm    *ui.Bitmap
	err   error
	file  *whiteboard.FileData
	rel   string
	crop  *whiteboard.ImageCrop
	flipX bool
	flipY bool
	key   string
}

func (b *Board) aiCompare(e *whiteboard.Element) *aiCompareState {
	s := b.aiState()
	if s.compares == nil {
		s.compares = map[string]*aiCompareState{}
	}
	cs := s.compares[e.ID]
	if cs == nil {
		cs = &aiCompareState{split: 0.5}
		s.compares[e.ID] = cs
	}
	return cs
}

func (b *Board) aiCompareMode(e *whiteboard.Element, n *whiteboard.AINode) string {
	cs := b.aiCompare(e)
	if (b.ReadOnly || e.Locked) && cs.modeOverride != "" {
		return cs.modeOverride
	}
	switch n.Params.CompareMode {
	case "slide", "side", "overlay", "diff", "toggle":
		return n.Params.CompareMode
	}
	return "slide"
}

func (b *Board) aiCompareBg(e *whiteboard.Element, n *whiteboard.AINode) string {
	cs := b.aiCompare(e)
	if (b.ReadOnly || e.Locked) && cs.bgOverride != "" {
		return cs.bgOverride
	}
	switch n.Params.CompareBg {
	case "checker", "dark", "light":
		return n.Params.CompareBg
	}
	return "checker"
}

func (b *Board) aiCompareInputAt(n *whiteboard.AINode, slot string) aiCompareInput {
	from := b.Scene.AILinkSource(n, slot)
	if from == nil {
		return aiCompareInput{}
	}
	if from.Kind == whiteboard.KindImage {
		cropKey := ""
		if from.Crop != nil {
			cropKey = fmt.Sprintf("%.2f,%.2f,%.2f,%.2f", from.Crop.X, from.Crop.Y, from.Crop.Width, from.Crop.Height)
		}
		return aiCompareInput{
			ok:    true,
			bm:    b.images[from.FileID],
			err:   b.imageErrors[from.FileID],
			file:  b.Scene.Files[from.FileID].Clone(),
			crop:  from.Crop,
			flipX: from.Scale[0] < 0,
			flipY: from.Scale[1] < 0,
			key:   fmt.Sprintf("file:%s:%s:%v,%v", from.FileID, cropKey, from.Scale[0] < 0, from.Scale[1] < 0),
		}
	}
	if fn := b.aiNode(from); fn != nil && (fn.Role == whiteboard.AIImage || fn.Role == whiteboard.AICutout || fn.Role == whiteboard.AIUpscale) {
		if rel := b.DisplayedAIImage(from.ID); rel != "" {
			return aiCompareInput{
				ok:  true,
				bm:  b.ai.media[rel],
				err: b.ai.mediaErr[rel],
				rel: rel,
				key: "rel:" + rel,
			}
		}
	}
	return aiCompareInput{}
}

func (b *Board) aiCompareDiffKey(e *whiteboard.Element, n *whiteboard.AINode) (string, aiCompareInput, aiCompareInput, bool) {
	inA := b.aiCompareInputAt(n, "a")
	inB := b.aiCompareInputAt(n, "b")
	if !inA.ok || !inB.ok {
		return "", inA, inB, false
	}
	return inA.key + "\x00" + inB.key, inA, inB, true
}

func (b *Board) prepareCompareMedia(o BoardOptions, e *whiteboard.Element, n *whiteboard.AINode, needed map[string]bool) {
	if n.Role != whiteboard.AICompare {
		return
	}
	s := b.aiState()
	if s.compareDiffs == nil {
		s.compareDiffs = map[string]*aiCompareDiffCache{}
	}
	for _, slot := range []string{"a", "b"} {
		from := b.Scene.AILinkSource(n, slot)
		if from == nil {
			continue
		}
		if fn := b.aiNode(from); fn != nil && (fn.Role == whiteboard.AIImage || fn.Role == whiteboard.AICutout || fn.Role == whiteboard.AIUpscale) {
			if rel := b.DisplayedAIImage(from.ID); rel != "" {
				needed[rel] = true
			}
		}
	}
	if b.aiCompareMode(e, n) != "diff" {
		return
	}
	key, inA, inB, ok := b.aiCompareDiffKey(e, n)
	if !ok {
		return
	}
	entry := s.compareDiffs[key]
	if entry == nil {
		entry = &aiCompareDiffCache{}
		s.compareDiffs[key] = entry
	}
	entry.wanted = true
	if entry.bitmap != nil || entry.err != nil || entry.pending {
		return
	}
	if o.Dispatch != nil && b.imageLoads >= boardImageLoadLimit {
		return
	}
	readInput := func(in aiCompareInput) (image.Image, error) {
		var data []byte
		var err error
		if in.rel != "" {
			if o.AI == nil || o.AI.ReadFile == nil {
				return nil, fmt.Errorf("missing reader")
			}
			data, err = o.AI.ReadFile(in.rel)
		} else {
			data, err = boardFileBytes(in.file, o)
		}
		if err != nil {
			return nil, err
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	}
	entry.pending = true
	load := func() (*ui.Bitmap, error) {
		imgA, err := readInput(inA)
		if err != nil {
			return nil, err
		}
		imgB, err := readInput(inB)
		if err != nil {
			return nil, err
		}
		diff := computeCompareDiffImage(imgA, imgB, inA.crop, inB.crop, inA.flipX, inA.flipY, inB.flipX, inB.flipY)
		return ui.NewBitmap(diff), nil
	}
	apply := func(bm *ui.Bitmap, err error) {
		entry.pending = false
		if !entry.wanted {
			return
		}
		if err != nil {
			entry.err = err
		} else {
			entry.bitmap = bm
		}
	}
	if !b.loadBitmap(o, load, apply) {
		entry.pending = false
	}
}

func cropAndFlipImage(src image.Image, crop *whiteboard.ImageCrop, flipX, flipY bool) image.Image {
	b := src.Bounds()
	rect := b
	if crop != nil && crop.Width > 0 && crop.Height > 0 {
		cx, cy := int(math.Round(crop.X)), int(math.Round(crop.Y))
		cw, ch := int(math.Round(crop.Width)), int(math.Round(crop.Height))
		sub := image.Rect(b.Min.X+cx, b.Min.Y+cy, b.Min.X+cx+cw, b.Min.Y+cy+ch)
		rect = sub.Intersect(b)
		if rect.Empty() {
			rect = b
		}
	}
	w, h := rect.Dx(), rect.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	if !flipX && !flipY {
		draw.Draw(out, out.Bounds(), src, rect.Min, draw.Src)
		return out
	}
	for y := range h {
		sy := rect.Min.Y + y
		if flipY {
			sy = rect.Max.Y - 1 - y
		}
		for x := range w {
			sx := rect.Min.X + x
			if flipX {
				sx = rect.Max.X - 1 - x
			}
			out.Set(x, y, src.At(sx, sy))
		}
	}
	return out
}

func computeCompareDiffImage(imgA, imgB image.Image, cropA, cropB *whiteboard.ImageCrop, flipXA, flipYA, flipXB, flipYB bool) *image.NRGBA {
	a := cropAndFlipImage(imgA, cropA, flipXA, flipYA)
	b := cropAndFlipImage(imgB, cropB, flipXB, flipYB)
	ba, bb := a.Bounds(), b.Bounds()
	w, h := max(1, max(ba.Dx(), bb.Dx())), max(1, max(ba.Dy(), bb.Dy()))
	if long := max(w, h); long > 1024 {
		scale := 1024 / float64(long)
		w = max(1, int(math.Round(float64(w)*scale)))
		h = max(1, int(math.Round(float64(h)*scale)))
	}
	ra := image.NewNRGBA(image.Rect(0, 0, w, h))
	rb := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(ra, ra.Bounds(), a, ba, draw.Src, nil)
	draw.BiLinear.Scale(rb, rb.Bounds(), b, bb, draw.Src, nil)
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	absDiff := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	for y := range h {
		for x := range w {
			off := ra.PixOffset(x, y)
			dr := absDiff(ra.Pix[off+0], rb.Pix[off+0])
			dg := absDiff(ra.Pix[off+1], rb.Pix[off+1])
			db := absDiff(ra.Pix[off+2], rb.Pix[off+2])
			da := absDiff(ra.Pix[off+3], rb.Pix[off+3])
			out.Pix[off+0] = uint8(min(255, dr*3+da*2))
			out.Pix[off+1] = uint8(min(255, dg*3))
			out.Pix[off+2] = uint8(min(255, db*3+da*2))
			out.Pix[off+3] = 255
		}
	}
	return out
}

func paintCompareBackground(p *ui.Painter, r ui.Rect, bg string) {
	switch bg {
	case "dark":
		p.Fill(r, ui.Hex("#181A20"), 0)
	case "light":
		p.Fill(r, ui.Hex("#F5F6F8"), 0)
	default:
		p.Fill(r, ui.Hex("#ECEEF2"), 0)
		p.Clip(r, 0, func() {
			step := max(float32(12), min(float32(24), r.W/36))
			cols := int(math.Ceil(float64(r.W / step)))
			rows := int(math.Ceil(float64(r.H / step)))
			tile := ui.Hex("#D5DAE2")
			for row := range rows {
				for col := range cols {
					if (row+col)%2 == 1 {
						p.Fill(ui.Rect{X: r.X + float32(col)*step, Y: r.Y + float32(row)*step, W: step, H: step}, tile, 0)
					}
				}
			}
		})
	}
}

func paintCompareBadge(p *ui.Painter, x, y float32, text string) {
	w := float32(max(20, len([]rune(text))*9+12))
	box := ui.Rect{X: x, Y: y, W: w, H: 18}
	p.Fill(box, ui.RGBA(17, 19, 24, 0.74), 4)
	p.Text(x+6, y+2, text, 11, ui.RGB(255, 255, 255))
}

func (b *Board) paintCompareInput(p *ui.Painter, e *whiteboard.Element, r ui.Rect, in aiCompareInput, alpha float32, pal aiNodeColors, tag string) {
	if in.bm == nil {
		msg := i18n.T("正在加载图片")
		if in.err != nil {
			msg = i18n.T("图片无法读取")
		}
		p.TextLayout(b.aiCardTextFont(e.ID+"#cmp-"+tag, msg, max(0, r.W-20), ui.Font{Size: 12}), r.X+10, r.Y+16, pal.muted)
		return
	}
	iw, ih := in.bm.Size()
	src := ui.Rect{W: float32(iw), H: float32(ih)}
	if in.crop != nil && in.crop.Width > 0 && in.crop.Height > 0 {
		src = ui.Rect{X: float32(in.crop.X), Y: float32(in.crop.Y), W: float32(in.crop.Width), H: float32(in.crop.Height)}
	}
	scale := min(r.W/src.W, r.H/src.H)
	w, h := src.W*scale, src.H*scale
	opts := ui.ImageOptions{FlipX: in.flipX, FlipY: in.flipY}
	if alpha < 0.999 {
		opts.Tint = ui.RGB(255, 255, 255).Alpha(alpha)
	}
	p.ImageRegion(in.bm, src, ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}, opts)
}

func (b *Board) paintAIComparePreview(p *ui.Painter, e *whiteboard.Element, n *whiteboard.AINode, r ui.Rect, pal aiNodeColors, zoom float32) {
	bg := b.aiCompareBg(e, n)
	paintCompareBackground(p, r, bg)
	inA := b.aiCompareInputAt(n, "a")
	inB := b.aiCompareInputAt(n, "b")
	drawBorder := func() { p.Stroke(r, pal.divider.Alpha(0.75), 8, 1) }

	if !inA.ok && !inB.ok {
		hint := i18n.T("连接图片 A 与图片 B 进行对比")
		hl := b.aiCardTextFont(e.ID+"#cmp-hint", hint, 0, ui.Font{Size: 12})
		hw, hh := hl.Size()
		if icon := boardIcon("image"); icon != nil && r.H >= 72 {
			paintBoardIcon(p, icon, ui.Rect{X: r.X + (r.W-20)/2, Y: r.Y + r.H/2 - 20, W: 20, H: 20}, pal.muted, zoom)
			p.TextLayout(hl, r.X+(r.W-hw)/2, r.Y+r.H/2+6, pal.muted)
		} else {
			p.TextLayout(hl, r.X+(r.W-hw)/2, r.Y+(r.H-hh)/2, pal.muted)
		}
		drawBorder()
		return
	}
	if inA.ok && !inB.ok {
		b.paintCompareInput(p, e, r, inA, 1, pal, "a")
		paintCompareBadge(p, r.X+8, r.Y+8, "A")
		paintCompareBadge(p, r.X+r.W-76, r.Y+8, i18n.T("缺图片 B"))
		drawBorder()
		return
	}
	if !inA.ok && inB.ok {
		b.paintCompareInput(p, e, r, inB, 1, pal, "b")
		paintCompareBadge(p, r.X+8, r.Y+8, i18n.T("缺图片 A"))
		paintCompareBadge(p, r.X+r.W-28, r.Y+8, "B")
		drawBorder()
		return
	}

	cs := b.aiCompare(e)
	switch b.aiCompareMode(e, n) {
	case "side":
		half := r.W / 2
		left := ui.Rect{X: r.X, Y: r.Y, W: max(0, half-1), H: r.H}
		right := ui.Rect{X: r.X + half + 1, Y: r.Y, W: max(0, half-1), H: r.H}
		p.Clip(left, 0, func() { b.paintCompareInput(p, e, left, inA, 1, pal, "a") })
		p.Clip(right, 0, func() { b.paintCompareInput(p, e, right, inB, 1, pal, "b") })
		p.Line(r.X+half, r.Y, r.X+half, r.Y+r.H, 1.5, pal.divider)
		paintCompareBadge(p, r.X+8, r.Y+8, "A")
		paintCompareBadge(p, r.X+half+8, r.Y+8, "B")
	case "overlay":
		b.paintCompareInput(p, e, r, inA, 1, pal, "a")
		b.paintCompareInput(p, e, r, inB, cs.split, pal, "b")
		paintCompareBadge(p, r.X+8, r.Y+8, fmt.Sprintf("B %.0f%%", cs.split*100))
	case "diff":
		key, _, _, _ := b.aiCompareDiffKey(e, n)
		entry := b.ai.compareDiffs[key]
		if entry == nil || entry.bitmap == nil {
			msg := i18n.T("正在计算差异")
			if entry != nil && entry.err != nil {
				msg = i18n.T("无法计算差异")
			}
			p.TextLayout(b.aiCardTextFont(e.ID+"#cmp-diff", msg, max(0, r.W-20), ui.Font{Size: 12}), r.X+10, r.Y+16, pal.muted)
		} else {
			iw, ih := entry.bitmap.Size()
			scale := min(r.W/float32(iw), r.H/float32(ih))
			w, h := float32(iw)*scale, float32(ih)*scale
			p.ImageRegion(entry.bitmap, ui.Rect{}, ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}, ui.ImageOptions{})
		}
		paintCompareBadge(p, r.X+8, r.Y+8, i18n.T("差异"))
	case "toggle":
		if cs.holdA {
			b.paintCompareInput(p, e, r, inA, 1, pal, "a")
			paintCompareBadge(p, r.X+8, r.Y+8, i18n.T("当前 A"))
		} else {
			b.paintCompareInput(p, e, r, inB, 1, pal, "b")
			paintCompareBadge(p, r.X+8, r.Y+8, i18n.T("B · 按住看 A"))
		}
	default:
		b.paintCompareInput(p, e, r, inA, 1, pal, "a")
		splitX := r.X + r.W*cs.split
		p.Clip(ui.Rect{X: splitX, Y: r.Y, W: r.W * (1 - cs.split), H: r.H}, 0, func() {
			paintCompareBackground(p, r, bg)
			b.paintCompareInput(p, e, r, inB, 1, pal, "b")
		})
		p.Line(splitX, r.Y, splitX, r.Y+r.H, 3, ui.RGBA(0, 0, 0, 0.42))
		p.Line(splitX, r.Y, splitX, r.Y+r.H, 1.5, ui.RGB(255, 255, 255))
		pill := ui.Rect{X: splitX - 10, Y: r.Y + r.H/2 - 14, W: 20, H: 28}
		p.Fill(pill, ui.RGB(255, 255, 255), 10)
		p.Stroke(pill, ui.RGBA(0, 0, 0, 0.22), 10, 1)
		p.Text(splitX-6, r.Y+r.H/2-8, "↔", 12, ui.Hex("#1F2329"))
		paintCompareBadge(p, r.X+8, r.Y+8, "A")
		paintCompareBadge(p, r.X+r.W-28, r.Y+8, "B")
	}
	drawBorder()
}

func (b *Board) setCompareMode(o BoardOptions, e *whiteboard.Element, mode string) {
	cs := b.aiCompare(e)
	if b.ReadOnly || e.Locked {
		cs.modeOverride = mode
		return
	}
	cs.modeOverride = ""
	b.updateAINode(o, e.ID, func(n *whiteboard.AINode) { n.Params.CompareMode = mode })
}

func (b *Board) setCompareBg(o BoardOptions, e *whiteboard.Element, bg string) {
	cs := b.aiCompare(e)
	if b.ReadOnly || e.Locked {
		cs.bgOverride = bg
		return
	}
	cs.bgOverride = ""
	b.updateAINode(o, e.ID, func(n *whiteboard.AINode) { n.Params.CompareBg = bg })
}

func (b *Board) swapCompareInputs(o BoardOptions, e *whiteboard.Element) {
	if b.ReadOnly || e.Locked {
		return
	}
	b.updateAINode(o, e.ID, func(n *whiteboard.AINode) {
		a, bl := n.LinkAt("a"), n.LinkAt("b")
		n.Disconnect("a")
		n.Disconnect("b")
		if bl != "" {
			n.Connect("a", bl)
		}
		if a != "" {
			n.Connect("b", a)
		}
	})
}

func (b *Board) aiCompareControls(c *ui.Context, o BoardOptions, e *whiteboard.Element, n *whiteboard.AINode, l aiCardLayout, x, y, z float32, disabled bool) {
	st := boardChromeFor(o.Theme)
	cs := b.aiCompare(e)
	mode := b.aiCompareMode(e, n)
	bg := b.aiCompareBg(e, n)
	w := l.widgets
	if w.W >= 120 && w.H >= 24 {
		ui.Row(c.Key("ai-compare-bar-"+e.ID)).Absolute().Left(x+w.X*z).Top(y+w.Y*z).Size(w.W, w.H).TransformOrigin(0, 0).Scale(z, z).Gap(4).AlignItems(ui.Center).Children(func() {
			if v, ok := aiInlineSelect(c, "cmp-mode-"+e.ID, i18n.T("对比模式"), mode, compareModeValues, translated(compareModeLabels...), false); ok {
				b.setCompareMode(o, e, v)
			}
			if v, ok := aiInlineSelect(c, "cmp-bg-"+e.ID, i18n.T("对比背景"), bg, compareBgValues, translated(compareBgLabels...), false); ok {
				b.setCompareBg(o, e, v)
			}
			if boardFlatButton(c, "cmp-swap-"+e.ID, i18n.T("对调"), false, st).Height(26).PaddingX(8).Disabled(disabled).Tooltip(i18n.T("交换图片 A 与图片 B")).Clicked() {
				b.swapCompareInputs(o, e)
			}
		})
	}
	r := l.preview
	if r.H < 12 {
		return
	}
	inA := b.aiCompareInputAt(n, "a")
	inB := b.aiCompareInputAt(n, "b")
	if !inA.ok || !inB.ok {
		cs.holdA = false
		return
	}
	switch mode {
	case "slide", "overlay":
		label := i18n.T("图片对比分隔线")
		if mode == "overlay" {
			label = i18n.T("叠化透明度")
		}
		handle := ui.Box(c.Key("ai-compare-handle-"+e.ID)).Absolute().Left(x+r.X*z).Top(y+r.Y*z).Size(r.W, r.H).TransformOrigin(0, 0).Scale(z, z).Focusable().Role(ui.RoleSlider).Label(label).Value(fmt.Sprintf("%.0f%%", cs.split*100)).Cursor(ui.CursorResizeEW)
		if _, _, held := handle.Dragged(); held {
			if px, _, ok := handle.PointerPosition(); ok {
				cs.split = max(0, min(1, px/r.W))
			}
		}
		if handle.Shortcut(0, ui.KeyLeft) {
			cs.split = max(0, cs.split-.05)
		}
		if handle.Shortcut(0, ui.KeyRight) {
			cs.split = min(1, cs.split+.05)
		}
		if handle.Shortcut(0, ui.KeyHome) {
			cs.split = 0
		}
		if handle.Shortcut(0, ui.KeyEnd) {
			cs.split = 1
		}
	case "toggle":
		handle := ui.Box(c.Key("ai-compare-toggle-"+e.ID)).Absolute().Left(x+r.X*z).Top(y+r.Y*z).Size(r.W, r.H).TransformOrigin(0, 0).Scale(z, z).Focusable().Role(ui.RoleButton).Label(i18n.T("按住查看图片 A")).Cursor(ui.CursorPointer)
		cs.holdA = handle.Pressed()
	default:
		cs.holdA = false
	}
}
