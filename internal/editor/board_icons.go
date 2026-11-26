package editor

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Excalidraw icons are from Quiet.md e59c49a, originally Excalidraw and
// Tabler (MIT); each SVG retains its source header.
//
//go:embed boardicons/*.svg
var boardIconFiles embed.FS
var boardIcons sync.Map
var boardToolIcons = map[BoardTool]string{ToolHand: "hand", ToolSelect: "selection", ToolRect: "rectangle", ToolDiamond: "diamond", ToolEllipse: "ellipse", ToolArrow: "arrow", ToolLine: "line", ToolFreedraw: "freedraw", ToolText: "text", ToolNote: "sticky-note", ToolEraser: "eraser", ToolImage: "image", ToolFrame: "frame", ToolLasso: "lasso"}

// Compact icons keep at least a one-pixel stroke at the board's normal zoom.
const boardCompactIconSize = 16

func boardIcon(name string) *ui.SVG {
	if value, ok := boardIcons.Load(name); ok {
		return value.(*ui.SVG)
	}
	data, err := boardIconFiles.ReadFile("boardicons/" + name + ".svg")
	if err != nil {
		return nil
	}
	svg, err := ui.ParseSVG(data)
	if err != nil {
		return nil
	}
	boardIcons.Store(name, svg)
	return svg
}

func paintBoardIcon(p *ui.Painter, icon *ui.SVG, rect ui.Rect, color ui.Color, zoom float32) {
	// MyGo 0.10 rasterizes SVG icons before applying the board transform.
	// Request the displayed pixel size, then cancel only that extra scale.
	p.Transform(ui.Scaling(1/zoom, 1/zoom), 0, 0, func() {
		p.Icon(icon, ui.Rect{X: rect.X * zoom, Y: rect.Y * zoom, W: rect.W * zoom, H: rect.H * zoom}, color)
	})
}

func boardScaledIcon(c *ui.Context, icon *ui.SVG, size float32, color ui.Color, zoom float32) ui.Element {
	return ui.Box(c).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, rect ui.Rect) {
		paintBoardIcon(p, icon, rect, color, zoom)
	})
}

func boardIconButton(c *ui.Context, key, name, label string, active bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(32, 32).Center().Radius(7).Focusable().Label(label).Tooltip(label)
	ink := t.Text
	if active {
		b.Background(t.Accent.Alpha(.14))
		ink = t.Accent
	} else if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		if icon := boardIcon(name); icon != nil {
			ui.Icon(c, icon).FontSize(18).TextColor(ink)
		}
	})
	return b
}
