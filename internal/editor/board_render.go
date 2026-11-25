package editor

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard/rough"
	"github.com/egoist/mygo/ui"
)

type boardPath struct {
	path  *ui.Path
	style whiteboard.Primitive
}
type boardPathCache struct {
	version, nonce int64
	paths          []boardPath
}
type boardTextCache struct {
	version, nonce int64
	lines          []*ui.TextLayout
}

type boardBoundsCache struct {
	version, nonce int64
	box            whiteboard.Box
}
type boardViewCache struct {
	layer     *ui.Canvas
	signature uint64
	dark      bool
	zoom      float64
	world     whiteboard.Box
	used      bool
}

func (b *Board) elementBounds(e *whiteboard.Element) whiteboard.Box {
	if b.bounds == nil {
		b.bounds = map[string]boardBoundsCache{}
	}
	if c, ok := b.bounds[e.ID]; ok && c.version == e.Version && c.nonce == e.VersionNonce {
		return c.box
	}
	box := whiteboard.ElementBounds(e)
	b.bounds[e.ID] = boardBoundsCache{e.Version, e.VersionNonce, box}
	return box
}

func boardColor(s string, dark bool) ui.Color {
	if s == "" || s == "none" || s == "transparent" {
		return ui.Color{}
	}
	if len(s) == 4 && s[0] == '#' {
		s = fmt.Sprintf("#%c%c%c%c%c%c", s[1], s[1], s[2], s[2], s[3], s[3])
	}
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if err != nil {
		return ui.RGB(30, 30, 30)
	}
	r, g, b := float64((v>>16)&255)/255, float64((v>>8)&255)/255, float64(v&255)/255
	if dark {
		// Excalidraw's invert(93%) hue-rotate(180deg), applied per color.
		r, g, b = .93-.86*r, .93-.86*g, .93-.86*b
		r, g, b = (-.574*r + 1.43*g + .144*b), (.426*r + .43*g + .144*b), (.426*r + 1.43*g - .856*b)
	}
	clamp := func(x float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(1, x)) * 255)) }
	return ui.RGB(clamp(r), clamp(g), clamp(b))
}
func nativeBoardPath(ops []rough.Op) *ui.Path {
	p := new(ui.Path).Cap(ui.RoundCap).Join(ui.RoundJoin)
	for _, o := range ops {
		d := o.Data
		switch o.Op {
		case "move":
			p.MoveTo(float32(d[0]), float32(d[1]))
		case "lineTo":
			p.LineTo(float32(d[0]), float32(d[1]))
		case "bcurveTo":
			p.CubeTo(float32(d[0]), float32(d[1]), float32(d[2]), float32(d[3]), float32(d[4]), float32(d[5]))
		case "quadTo":
			p.QuadTo(float32(d[0]), float32(d[1]), float32(d[2]), float32(d[3]))
		case "close":
			p.Close()
		}
	}
	return p
}
func (b *Board) elementPaths(e *whiteboard.Element) []boardPath {
	version, nonce := e.ShapeRevision()
	c, ok := b.paths[e.ID]
	if ok && c.version == version && c.nonce == nonce {
		return c.paths
	}
	c = boardPathCache{version: version, nonce: nonce}
	for _, p := range whiteboard.ElementPrimitives(e) {
		np := nativeBoardPath(p.Ops)
		if len(p.Dash) > 0 {
			ds := make([]float32, len(p.Dash))
			for i, d := range p.Dash {
				ds[i] = float32(d)
			}
			np.Dash(0, ds...)
		}
		c.paths = append(c.paths, boardPath{np, p})
	}
	b.paths[e.ID] = c
	return c.paths
}
func (b *Board) elementText(e *whiteboard.Element) []*ui.TextLayout {
	version, nonce := e.ShapeRevision()
	c, ok := b.texts[e.ID]
	if ok && c.version == version && c.nonce == nonce {
		return c.lines
	}
	c = boardTextCache{version: version, nonce: nonce}
	for _, line := range strings.Split(e.Text, "\n") {
		c.lines = append(c.lines, boardShapeText(line, whiteboard.Font{Size: e.FontSize, Family: e.FontFamily}, e.W, e.TextAlign))
	}
	b.texts[e.ID] = c
	return c.lines
}
func boardFileBytes(f *whiteboard.FileData, o BoardOptions) ([]byte, error) {
	if f == nil {
		return nil, fmt.Errorf("missing image")
	}
	if f.DataURL != "" {
		header, data, ok := strings.Cut(f.DataURL, ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return nil, fmt.Errorf("invalid image data")
		}
		if len(data) > 48<<20 {
			return nil, fmt.Errorf("image exceeds size limit")
		}
		return base64.StdEncoding.DecodeString(data)
	}
	if o.ResolveFile != nil {
		return o.ResolveFile(f)
	}
	return nil, fmt.Errorf("image cannot be found")
}
func (b *Board) prepareImages(o BoardOptions) {
	for id, f := range b.Scene.Files {
		if b.images[id] != nil || b.imageErrors[id] != nil || b.imagePending[id] {
			continue
		}
		b.imagePending[id] = true
		f = f.Clone()
		load := func() (*ui.Bitmap, error) {
			data, err := boardFileBytes(f, o)
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
		apply := func(bitmap *ui.Bitmap, err error) {
			if b.imagePending == nil {
				return
			}
			delete(b.imagePending, id)
			if err != nil {
				b.imageErrors[id] = err
			} else {
				b.images[id] = bitmap
			}
		}
		if o.Dispatch != nil {
			go func() { bitmap, err := load(); o.Dispatch(func() { apply(bitmap, err) }) }()
		} else {
			apply(load())
		}
	}
}
func (b *Board) paint(p *ui.Painter, r ui.Rect, dark bool, controls bool) {
	p.Fill(r, boardColor(b.Scene.AppState.ViewBackgroundColor, dark), 0)
	if b.ShowGrid && controls {
		step := b.Scene.AppState.GridSize * b.Zoom
		if step >= 5 {
			for x := math.Mod(b.PanX, step); x < float64(r.W); x += step {
				for y := math.Mod(b.PanY, step); y < float64(r.H); y += step {
					p.Fill(ui.Rect{X: r.X + float32(x), Y: r.Y + float32(y), W: 1, H: 1}, boardColor("#ced4da", dark), 0)
				}
			}
		}
	}
	viewport := whiteboard.Box{X: -b.PanX / b.Zoom, Y: -b.PanY / b.Zoom, W: float64(r.W) / b.Zoom, H: float64(r.H) / b.Zoom}.Expand(40)
	b.aiPainting = controls
	if controls && len(b.Scene.Elements) > 500 && b.paintDenseScene(p, r, dark, viewport) {
		p.Clip(r, 0, func() {
			p.Transform(ui.Scaling(float32(b.Zoom), float32(b.Zoom)).Then(ui.Translation(r.X+float32(b.PanX), r.Y+float32(b.PanY))), 0, 0, func() { b.paintAIOverlay(p, dark) })
		})
		b.paintSelection(p, r, dark)
		return
	}
	p.Clip(r, 0, func() {
		p.Transform(ui.Scaling(float32(b.Zoom), float32(b.Zoom)).Then(ui.Translation(r.X+float32(b.PanX), r.Y+float32(b.PanY))), 0, 0, func() {
			b.paintScene(p, viewport, dark)
			if controls {
				b.paintAIOverlay(p, dark)
			}
		})
	})
	if controls {
		b.paintSelection(p, r, dark)
	}
}

// paintScene draws the board's elements, then the node links over them,
// then the node cards on top, as a node editor layers its canvas: the
// cards carry live widgets that sit above every drawing anyway.
func (b *Board) paintScene(p *ui.Painter, viewport whiteboard.Box, dark bool) {
	paint := func(nodes bool) {
		for _, e := range b.Scene.Elements {
			if e.IsDeleted || (b.aiNode(e) != nil) != nodes || !viewport.Intersects(b.elementBounds(e).Expand(e.StrokeWidth*5)) {
				continue
			}
			if b.editing != nil && b.editing.id == e.ID {
				continue
			}
			draw := func() { b.paintElement(p, e, dark) }
			if frame := b.Scene.ElementByID(e.FrameID); frame != nil {
				cx, cy := float32(frame.X+frame.W/2), float32(frame.Y+frame.H/2)
				p.Transform(ui.Rotation(float32(frame.Angle*180/math.Pi)), cx, cy, func() {
					p.Clip(ui.Rect{X: float32(frame.X), Y: float32(frame.Y), W: float32(frame.W), H: float32(frame.H)}, 0, func() { p.Transform(ui.Rotation(float32(-frame.Angle*180/math.Pi)), cx, cy, draw) })
				})
			} else {
				draw()
			}
		}
	}
	paint(false)
	b.paintAILinks(p, dark, viewport)
	paint(true)
}

// A dense viewport is a retained native render target. Panning inside its
// padded coverage reuses pixels; changed content and scale rebuild it. This
// is the same production painter, not a reduced-detail representation.
func (b *Board) paintDenseScene(p *ui.Painter, r ui.Rect, dark bool, viewport whiteboard.Box) bool {
	signature := uint64(len(b.Scene.Elements))
	for _, ch := range b.Scene.AppState.ViewBackgroundColor {
		signature = signature*31 + uint64(ch)
	}
	visible := 0
	for _, e := range b.Scene.Elements {
		signature = (signature ^ uint64(e.VersionNonce) ^ uint64(e.Version)) * 1099511628211
		if !e.IsDeleted && viewport.Intersects(b.elementBounds(e)) {
			visible++
		}
	}
	if visible < 40 {
		b.viewCache = boardViewCache{}
		return false
	}
	for id := range b.images {
		h := uint64(0)
		for _, ch := range id {
			h = h*31 + uint64(ch)
		}
		signature ^= h
	}
	// Node cards show run state and loaded results outside the scene.
	for id, r := range b.ai.runs {
		h := uint64(len(r.stream))
		for _, ch := range id + r.status + r.progress {
			h = h*31 + uint64(ch)
		}
		signature ^= h * 1099511628211 // order-free: maps iterate at random
	}
	signature = signature*31 + uint64(len(b.ai.media))
	if d := b.ai.drag; d != nil && d.live {
		for _, ch := range d.from + d.to + d.typ + d.target + d.targetSlot {
			signature = signature*31 + uint64(ch)
		}
	}
	if b.editing != nil {
		for _, ch := range b.editing.id {
			signature = signature*31 + uint64(ch)
		}
	}
	c := &b.viewCache
	bucket := math.Pow(2, math.Ceil(math.Log2(b.Zoom)*4)/4)
	coverage := whiteboard.Box{X: -b.PanX / b.Zoom, Y: -b.PanY / b.Zoom, W: float64(r.W) / b.Zoom, H: float64(r.H) / b.Zoom}
	inside := c.world.Contains(whiteboard.Point{coverage.X, coverage.Y}) && c.world.Contains(whiteboard.Point{coverage.X + coverage.W, coverage.Y + coverage.H})
	if c.layer == nil || c.signature != signature || c.dark != dark || c.zoom != bucket || !inside {
		margin := float64(240)
		world := coverage.Expand(margin / b.Zoom)
		w, h := int(math.Ceil(world.W*bucket)), int(math.Ceil(world.H*bucket))
		c.layer = ui.NewCanvas(w, h)
		c.world = world
		c.zoom = bucket
		c.signature = signature
		c.dark = dark
		c.layer.Draw(p, func(cp *ui.Painter) {
			cp.Transform(ui.Scaling(float32(bucket), float32(bucket)).Then(ui.Translation(float32(-world.X*bucket), float32(-world.Y*bucket))), 0, 0, func() {
				b.paintScene(cp, world, dark)
			})
		})
	}
	src := ui.Rect{X: float32((coverage.X - c.world.X) * c.zoom), Y: float32((coverage.Y - c.world.Y) * c.zoom), W: float32(coverage.W * c.zoom), H: float32(coverage.H * c.zoom)}
	p.ImageRegion(c.layer.Bitmap(), src, r, ui.ImageOptions{})
	c.used = true
	return true
}
func (b *Board) paintElement(p *ui.Painter, e *whiteboard.Element, dark bool) {
	p.Transform(ui.Rotation(float32(e.Angle*180/math.Pi)), float32(e.X+e.W/2), float32(e.Y+e.H/2), func() {
		p.Transform(ui.Translation(float32(e.X), float32(e.Y)), 0, 0, func() {
			if n := b.aiNode(e); n != nil {
				b.paintAINode(p, e, n, dark)
				return
			}
			for _, path := range b.elementPaths(e) {
				s := path.style
				if s.Fill != "" && s.Fill != "transparent" {
					p.FillPath(path.path, boardColor(s.Fill, dark).Alpha(float32(s.Opacity)))
				}
				if s.Stroke != "" && s.Stroke != "transparent" && s.Stroke != "none" {
					p.StrokePath(path.path, float32(s.Width), boardColor(s.Stroke, dark).Alpha(float32(s.Opacity)))
				}
			}
			switch e.Kind {
			case whiteboard.KindText:
				if host := b.Scene.ElementByID(e.ContainerID); host != nil && host.Kind == whiteboard.KindArrow {
					p.Fill(ui.Rect{X: -4, Y: -2, W: float32(e.W + 8), H: float32(e.H + 4)}, boardColor(b.Scene.AppState.ViewBackgroundColor, dark), 2)
				}
				lh := e.FontSize * e.LineHeight
				if lh <= 0 {
					lh = e.FontSize * whiteboard.FontLineHeight(e.FontFamily)
				}
				for i, layout := range b.elementText(e) {
					p.TextLayout(layout, 0, float32(float64(i)*lh), boardColor(e.StrokeColor, dark).Alpha(float32(e.Opacity/100)))
				}
			case whiteboard.KindImage:
				if bitmap := b.images[e.FileID]; bitmap != nil {
					src := ui.Rect{}
					if c := e.Crop; c != nil {
						src = ui.Rect{X: float32(c.X), Y: float32(c.Y), W: float32(c.Width), H: float32(c.Height)}
					}
					p.ImageRegion(bitmap, src, ui.Rect{W: float32(e.W), H: float32(e.H)}, ui.ImageOptions{FlipX: e.Scale[0] < 0, FlipY: e.Scale[1] < 0, Tint: ui.RGB(255, 255, 255).Alpha(float32(e.Opacity / 100))})
				} else {
					p.Stroke(ui.Rect{W: float32(e.W), H: float32(e.H)}, boardColor("#e03131", dark), 3, 1)
					message := i18n.T("正在加载图片")
					if b.imageErrors[e.FileID] != nil {
						message = i18n.T("图片无法读取")
					}
					p.Text(8, 8, message, 13, boardColor("#e03131", dark))
				}
			case whiteboard.KindFrame, whiteboard.KindMagicFrame:
				p.Stroke(ui.Rect{W: float32(e.W), H: float32(e.H)}, boardColor("#adb5bd", dark), 0, 1)
				p.Text(0, -22, e.Name, 14, boardColor("#868e96", dark))
			}
		})
	})
}
func (b *Board) paintSelection(p *ui.Painter, r ui.Rect, dark bool) {
	color := boardColor("#6965db", dark)
	ids := b.SelectedIDs()
	if len(ids) > 0 {
		bounds := whiteboard.SelectionBounds(b.Scene, ids)
		x, y := b.screenPoint(whiteboard.Point{bounds.X, bounds.Y})
		p.Stroke(ui.Rect{X: x, Y: y, W: float32(bounds.W * b.Zoom), H: float32(bounds.H * b.Zoom)}, color, 0, 1)
		_, handles := b.selectionHandles()
		for _, handle := range handles {
			x, y := b.screenPoint(handle.point)
			p.Fill(ui.Rect{X: x - 4, Y: y - 4, W: 8, H: 8}, boardColor("#ffffff", dark), 2)
			p.Stroke(ui.Rect{X: x - 4, Y: y - 4, W: 8, H: 8}, color, 2, 1)
		}
	}
	if b.mode == "marquee" {
		bb := b.selectionBox
		x, y := b.screenPoint(whiteboard.Point{bb.X, bb.Y})
		p.Fill(ui.Rect{X: x, Y: y, W: float32(bb.W * b.Zoom), H: float32(bb.H * b.Zoom)}, color.Alpha(.1), 0)
		p.Stroke(ui.Rect{X: x, Y: y, W: float32(bb.W * b.Zoom), H: float32(bb.H * b.Zoom)}, color, 0, 1)
	}
	if len(b.lasso) > 1 {
		for i := 1; i < len(b.lasso); i++ {
			a, z := b.lasso[i-1], b.lasso[i]
			x, y := b.screenPoint(a)
			X, Y := b.screenPoint(z)
			p.Line(x, y, X, Y, 1, color)
		}
	}
	if e := b.Scene.ElementByID(b.linearEdit); e != nil && !e.IsDeleted && !e.Locked && !b.ReadOnly {
		for _, pt := range boardLineControlPoints(e) {
			x, y := b.screenPoint(pt)
			p.Fill(ui.Rect{X: x - 4, Y: y - 4, W: 8, H: 8}, color, 4)
		}
	}
}
func boardHandles(b whiteboard.Box) []whiteboard.Point {
	return []whiteboard.Point{{b.X, b.Y}, {b.X + b.W/2, b.Y}, {b.X + b.W, b.Y}, {b.X + b.W, b.Y + b.H/2}, {b.X + b.W, b.Y + b.H}, {b.X + b.W/2, b.Y + b.H}, {b.X, b.Y + b.H}, {b.X, b.Y + b.H/2}, {b.X + b.W/2, b.Y - 25}}
}

// BoardPNG draws through the same production painter used by the interactive
// board, with content-only framing. Callers run it off the window thread.
func BoardPNG(scene *whiteboard.Scene, o BoardOptions) ([]byte, error) {
	b := NewBoard(scene.Clone())
	o.Dispatch = nil
	b.prepareImages(o)
	for _, err := range b.imageErrors {
		return nil, err
	}
	bounds := whiteboard.SceneBounds(scene).Expand(20)
	if bounds.Empty() {
		bounds = whiteboard.Box{W: 320, H: 180}
	}
	scale := math.Min(2, 4096/math.Max(bounds.W, bounds.H))
	w, h := max(1, int(math.Ceil(bounds.W*scale))), max(1, int(math.Ceil(bounds.H*scale)))
	b.Zoom = scale
	b.PanX = -bounds.X * scale
	b.PanY = -bounds.Y * scale
	img := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { b.paint(p, r, false, false) })
	}, w, h, 1)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	text, err := scene.Encode()
	if err != nil {
		return nil, err
	}
	return whiteboard.EncodePNGMetadata(buf.Bytes(), text)
}
