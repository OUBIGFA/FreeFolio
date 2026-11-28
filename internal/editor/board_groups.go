package editor

import (
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

type boardGroupCache struct {
	scene     *whiteboard.Scene
	signature uint64
	regions   []whiteboard.GroupRegion
}

func (b *Board) groupRegions() []whiteboard.GroupRegion {
	signature := uint64(len(b.Scene.Elements))
	for _, e := range b.Scene.Elements {
		signature = (signature ^ uint64(e.Version) ^ uint64(e.VersionNonce)) * 1099511628211
		if e.IsDeleted {
			signature ^= 1
		}
	}
	if b.groups.scene != b.Scene || b.groups.signature != signature {
		b.groups = boardGroupCache{b.Scene, signature, b.Scene.GroupRegions()}
	}
	return b.groups.regions
}

func (b *Board) selectedGroup() string {
	ids := b.SelectedIDs()
	if len(ids) == 0 {
		return ""
	}
	e := b.Scene.ElementByID(ids[0])
	if e == nil || len(e.GroupIDs) == 0 {
		return ""
	}
	id := e.GroupIDs[len(e.GroupIDs)-1]
	for _, group := range b.groupRegions() {
		if group.ID != id || group.Locked || len(group.Members) != len(ids) {
			continue
		}
		for _, member := range group.Members {
			if !b.Selected[member] {
				return ""
			}
		}
		return id
	}
	return ""
}

func (b *Board) beginGroupRename(id string) {
	if b.ReadOnly {
		return
	}
	for _, group := range b.groupRegions() {
		if group.ID == id && !group.Locked {
			b.groupRename = id
			b.propertyInput = group.Name
			b.panel = "group-name"
			return
		}
	}
}

func (b *Board) paintGroups(p *ui.Painter, viewport whiteboard.Box, dark bool) {
	color := ui.Hex("#687CAB")
	ink := ui.Hex("#536585")
	alpha := float32(.12)
	if dark {
		color = ui.Hex("#A1B3D9")
		ink = ui.Hex("#CBD6ED")
		alpha = .14
	}
	for _, group := range b.groupRegions() {
		box := group.Bounds
		if !viewport.Intersects(box) {
			continue
		}
		r := ui.Rect{X: float32(box.X), Y: float32(box.Y), W: float32(box.W), H: float32(box.H)}
		p.Fill(r, color.Alpha(alpha), 16)
		p.Stroke(r, color.Alpha(.25), 16, 1)
		name := group.Name
		if name == "" {
			name = i18n.T("分组")
		}
		layout := b.aiCardTextFont("group-title:"+group.ID, name, 0, ui.Font{Size: 15, Weight: 500})
		p.Clip(ui.Rect{X: r.X + 24, Y: r.Y + 10, W: max(0, r.W-48), H: 26}, 0, func() { p.TextLayout(layout, r.X+24, r.Y+12, ink) })
	}
}

// Only the title takes input; the background does not intercept empty-canvas
// selection, node dragging or links. The title also works from the keyboard.
func (b *Board) groupControls(c *ui.Context) {
	if b.Tool != ToolSelect || b.spaceDown || b.mode != "" || b.editing != nil {
		return
	}
	for _, group := range b.groupRegions() {
		x, y, w, _ := b.canvasRect(group.Bounds.X+24, group.Bounds.Y+8, group.Bounds.W-48, 28)
		if x+w < 0 || y+28 < 0 || x > b.lastRect.W || y > b.lastRect.H {
			continue
		}
		name := group.Name
		if name == "" {
			name = i18n.T("分组")
		}
		button := ui.ButtonBase(c.Key("board-group:"+group.ID)).Absolute().Left(x).Top(y).Size(w, float32(28*b.Zoom)).Focusable().FocusRing(false).Label(name)
		if button.Clicked() {
			b.selectIDs(group.Members)
		}
		if !b.ReadOnly && !group.Locked {
			button.Cursor(ui.CursorPointer).Tooltip(i18n.T("双击重命名分组"))
			if button.DoubleClicked() || button.Focused() && button.Shortcut(0, ui.KeyEnter) {
				b.beginGroupRename(group.ID)
			}
			button.ContextMenu(func(m *ui.Menu) {
				if m.Item(i18n.T("重命名分组")).Chosen() {
					b.beginGroupRename(group.ID)
				}
			})
		}
	}
}
