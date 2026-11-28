package editor

import (
	"math"
	"slices"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// Background hits are considered after nodes, handles and AI links. Prefer the
// outer group for whole-group moves; editing exposes its members individually.
func (b *Board) groupAt(p whiteboard.Point) *whiteboard.GroupRegion {
	for i := range b.groupRegions() {
		g := &b.groups.regions[i]
		if g.Bounds.Contains(p) {
			return g
		}
	}
	return nil
}

func (b *Board) editGroup(id string) {
	if b.ReadOnly {
		return
	}
	for _, g := range b.groupRegions() {
		if g.ID == id && !g.Locked {
			b.isolatedGroup = id
			b.Selected = map[string]bool{}
			b.RequestFocus = true
			return
		}
	}
}

func (b *Board) exitGroup() {
	id := b.isolatedGroup
	b.isolatedGroup = ""
	for _, g := range b.groupRegions() {
		if g.ID == id {
			b.selectIDs(g.Members)
			break
		}
	}
	b.RequestFocus = true
}

func (b *Board) groupMemberSelection(e *whiteboard.Element) []string {
	return b.Scene.ExpandSelection([]string{e.ID}, b.isolatedGroup == "" || !slices.Contains(e.GroupIDs, b.isolatedGroup))
}

func (b *Board) groupMenu(m *ui.Menu) {
	if b.isolatedGroup != "" {
		if m.Item(i18n.T("完成组内编辑")).Chosen() {
			b.exitGroup()
		}
		return
	}
	if id := b.selectedGroup(); id != "" {
		if m.Item(i18n.T("编辑组内节点")).Disabled(b.ReadOnly).Chosen() {
			b.editGroup(id)
		}
		if m.Item(i18n.T("重命名分组")).Disabled(b.ReadOnly).Chosen() {
			b.beginGroupRename(id)
		}
	}
}

type boardGroupCache struct {
	scene     *whiteboard.Scene
	signature uint64
	regions   []whiteboard.GroupRegion
}

// boardGroupHeader is the on-screen height, in px, of one group title band.
// Titles keep one readable size at every zoom, so each band is this tall on
// screen and the region grows or shrinks in scene units as the zoom changes.
const boardGroupHeader = 28

// boardGroupTitleFont is the group title's on-screen font.
var boardGroupTitleFont = ui.Font{Size: 15, Weight: 500}

func (b *Board) groupRegions() []whiteboard.GroupRegion {
	signature := uint64(len(b.Scene.Elements)) ^ math.Float64bits(b.Zoom)
	for _, e := range b.Scene.Elements {
		signature = (signature ^ uint64(e.Version) ^ uint64(e.VersionNonce)) * 1099511628211
		if e.IsDeleted {
			signature ^= 1
		}
	}
	if b.groups.scene != b.Scene || b.groups.signature != signature {
		regions := b.Scene.GroupRegions()
		// The scene reserves 28 units per header level; resize them to
		// boardGroupHeader px at this zoom. Outer groups keep containing
		// inner ones, as they reserve more levels.
		zoom := max(b.Zoom, 0.01)
		for i := range regions {
			extra := float64(regions[i].Levels) * (boardGroupHeader/zoom - 28)
			regions[i].Bounds.Y -= extra
			regions[i].Bounds.H += extra
		}
		b.groups = boardGroupCache{b.Scene, signature, regions}
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
		if b.isolatedGroup == group.ID {
			p.Stroke(r, ink.Alpha(.8), 16, 2/max(.25, float32(b.Zoom)))
		}
		name := group.Name
		if name == "" {
			name = i18n.T("分组")
		}
		if b.isolatedGroup == group.ID {
			name += " · " + i18n.T("组内编辑中")
		}
		// Draw the title unscaled around the region's corner, so it keeps
		// its on-screen size whatever the zoom.
		layout := b.aiCardTextFont("group-title:"+group.ID, name, 0, boardGroupTitleFont)
		zoom := float32(max(b.Zoom, 0.01))
		p.Transform(ui.Scaling(1/zoom, 1/zoom), r.X, r.Y, func() {
			p.Clip(ui.Rect{X: r.X + 24, Y: r.Y + 2, W: max(0, r.W*zoom-48), H: boardGroupHeader - 4}, 0, func() {
				_, h := layout.Size()
				p.TextLayout(layout, r.X+24, r.Y+(boardGroupHeader-h)/2, ink)
			})
		})
	}
}

// Title controls complement background dragging without covering node ports.
// Group editing remains a view mode and never removes persisted membership.
func (b *Board) groupControls(c *ui.Context) {
	if b.Tool != ToolSelect || b.spaceDown || b.mode != "" || b.editing != nil {
		return
	}
	for _, group := range b.groupRegions() {
		left, y, width, _ := b.canvasRect(group.Bounds.X, group.Bounds.Y, group.Bounds.W, 0)
		x, w := left+24, max(0, width-48)
		if x+w < 0 || y+boardGroupHeader < 0 || x > b.lastRect.W || y > b.lastRect.H {
			continue
		}
		name := group.Name
		if name == "" {
			name = i18n.T("分组")
		}
		button := ui.ButtonBase(c.Key("board-group:"+group.ID)).Absolute().Left(x).Top(y).Size(w, boardGroupHeader).Focusable().FocusRing(false).Label(name)
		if button.Clicked() {
			if b.isolatedGroup == "" {
				b.selectIDs(group.Members)
			}
		}
		if !b.ReadOnly && !group.Locked {
			button.Cursor(ui.CursorPointer).Tooltip(i18n.T("双击重命名分组"))
			if button.DoubleClicked() || button.Focused() && button.Shortcut(0, ui.KeyEnter) {
				b.beginGroupRename(group.ID)
			}
			button.ContextMenu(func(m *ui.Menu) {
				if b.isolatedGroup == group.ID {
					if m.Item(i18n.T("完成组内编辑")).Chosen() {
						b.exitGroup()
					}
				} else if m.Item(i18n.T("编辑组内节点")).Chosen() {
					b.editGroup(group.ID)
				}
				if m.Item(i18n.T("重命名分组")).Chosen() {
					b.beginGroupRename(group.ID)
				}
			})
		}
	}
	if b.isolatedGroup != "" {
		if ui.Button(c.Key("board-group-done"), i18n.T("完成组内编辑")).Absolute().Right(12).Top(64).Clicked() {
			b.exitGroup()
		}
	}
}
