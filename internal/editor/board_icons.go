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
func boardIconButton(c *ui.Context, key, name, label string, active bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(34, 34).Center().Radius(5).Focusable().Label(label).Tooltip(label)
	if active {
		b.Background(t.Accent.Alpha(.16))
	} else if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		if icon := boardIcon(name); icon != nil {
			ui.Icon(c, icon).FontSize(20).TextColor(t.Text)
		}
	})
	return b
}
