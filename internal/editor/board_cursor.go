package editor

import (
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// canvasCursor follows pointerDown's hit priority. An active gesture keeps its
// cursor even over another target or while a tool modifier is held.
func (b *Board) canvasCursor(mods ui.Modifiers, gesture bool) ui.Cursor {
	switch b.mode {
	case "pan", "move", "rotate", "line-point":
		return ui.CursorGrabbing
	case "resize":
		cursor := boardHandleCursor(b.handle)
		if b.handle == 0 || b.handle == 2 || b.handle == 4 || b.handle == 6 {
			r := b.resizeBounds(b.hover, mods)
			if (r.W < 0) != (r.H < 0) {
				if cursor == ui.CursorResizeNWSE {
					return ui.CursorResizeNESW
				}
				return ui.CursorResizeNWSE
			}
		}
		return cursor
	case "ai-link":
		if d := b.ai.drag; d != nil && d.over && d.target == "" {
			return ui.CursorNotAllowed
		}
		return ui.CursorCrosshair
	case "marquee", "lasso", "erase", "create":
		return ui.CursorCrosshair
	}
	if gesture {
		return b.gestureCursor
	}
	if b.Tool == ToolHand || b.spaceDown {
		return ui.CursorGrab
	}
	hit := b.canvasHit(b.hover)
	if mods&(ui.Ctrl|ui.Cmd) != 0 && hit != nil && hit.Link != "" &&
		(b.options.OpenLink != nil || strings.HasPrefix(hit.Link, "[[") && b.options.OpenWiki != nil) {
		return ui.CursorPointer
	}
	if b.ReadOnly {
		return ui.CursorDefault
	}
	if hit != nil && hit.Locked && (b.Tool == ToolSelect || b.Tool == ToolText || b.Tool == ToolEraser) {
		return ui.CursorNotAllowed
	}
	if b.Tool == ToolSelect {
		if b.options.AI != nil {
			if id, _, _ := b.aiSlotHit(b.hover); id != "" {
				return ui.CursorCrosshair
			}
			if (hit == nil || b.aiNode(hit) == nil) && b.aiLinkHit(b.hover) != nil {
				return ui.CursorPointer
			}
		}
		if point, _ := b.linePointAt(b.hover); point >= 0 {
			return ui.CursorGrab
		}
		if handle, _ := b.selectionHandleAt(b.hover); handle >= 0 {
			return boardHandleCursor(handle)
		}
		if hit != nil {
			if mods&ui.Alt != 0 {
				return ui.CursorCopy
			}
			return ui.CursorGrab
		}
		return ui.CursorDefault
	}
	if b.Tool == ToolText {
		return ui.CursorText
	}
	return ui.CursorCrosshair
}

func (b *Board) canvasHit(p whiteboard.Point) *whiteboard.Element {
	hit := b.hitElement(p, 6/b.Zoom, true)
	// The locked annotation base is the drawing surface. Text and eraser
	// tools act on annotations above it rather than editing the base itself.
	if hit != nil && hit.ID == b.annotationBase && (b.Tool == ToolText || b.Tool == ToolEraser) {
		return nil
	}
	return hit
}

func (b *Board) hitElement(p whiteboard.Point, tolerance float64, includeLocked bool) *whiteboard.Element {
	// Painting already retains versioned world bounds. Reject distant strokes
	// before HitTest would reconstruct their pressure-sensitive outlines.
	for i := len(b.Scene.Elements) - 1; i >= 0; i-- {
		e := b.Scene.Elements[i]
		if e == nil || e.IsDeleted || (!includeLocked && e.Locked) || !b.elementBounds(e).Expand(tolerance+e.StrokeWidth/2).Contains(p) {
			continue
		}
		if whiteboard.HitTest(e, p, tolerance) {
			return e
		}
	}
	return nil
}

func boardHandleCursor(handle int) ui.Cursor {
	switch handle {
	case 0, 4:
		return ui.CursorResizeNWSE
	case 2, 6:
		return ui.CursorResizeNESW
	case 1, 5:
		return ui.CursorResizeNS
	case 3, 7:
		return ui.CursorResizeEW
	default:
		return ui.CursorGrab // The rotation handle is dragged around the selection.
	}
}

type boardSelectionHandle struct {
	index int
	point whiteboard.Point
}

// Drawing, hit testing and cursor feedback share the same visible handles.
func (b *Board) selectionHandles() (whiteboard.Box, []boardSelectionHandle) {
	ids := b.SelectedIDs()
	bounds := whiteboard.SelectionBounds(b.Scene, ids)
	editable := false
	for _, id := range ids {
		if e := b.Scene.ElementByID(id); e != nil && !e.Locked {
			editable = true
			break
		}
	}
	if b.ReadOnly || !editable {
		return bounds, nil
	}
	singleAI := len(ids) == 1 && b.aiNode(b.Scene.ElementByID(ids[0])) != nil
	var handles []boardSelectionHandle
	for i, point := range boardHandles(bounds) {
		if singleAI && (i == 3 || i == 7 || i == 8) {
			continue
		}
		handles = append(handles, boardSelectionHandle{i, point})
	}
	return bounds, handles
}

func (b *Board) selectionHandleAt(p whiteboard.Point) (int, whiteboard.Box) {
	bounds, handles := b.selectionHandles()
	for _, handle := range handles {
		if whiteboard.Distance(p, handle.point) < 8/b.Zoom {
			return handle.index, bounds
		}
	}
	return -1, bounds
}

// Existing points take priority over inserting a point on a segment.
func (b *Board) linePointAt(p whiteboard.Point) (index int, insert bool) {
	e := b.Scene.ElementByID(b.linearEdit)
	if e == nil || e.IsDeleted || e.Locked || b.ReadOnly {
		return -1, false
	}
	points := boardLineControlPoints(e)
	for i, point := range points {
		if whiteboard.Distance(p, point) < 8/b.Zoom {
			return i, false
		}
	}
	for i := 1; i < len(points); i++ {
		if whiteboard.SegmentDistance(p, points[i-1], points[i]) < 6/b.Zoom {
			return i, true
		}
	}
	return -1, false
}

func boardLineLocalPoint(e *whiteboard.Element, p whiteboard.Point) whiteboard.Point {
	return whiteboard.Sub(whiteboard.RotatePoint(p, whiteboard.Point{e.X + e.W/2, e.Y + e.H/2}, -e.Angle), whiteboard.Point{e.X, e.Y})
}

// Curve sampling may add render-only points; only stored points are editable.
func boardLineControlPoints(e *whiteboard.Element) []whiteboard.Point {
	points := make([]whiteboard.Point, len(e.Points))
	center := whiteboard.Point{e.X + e.W/2, e.Y + e.H/2}
	for i, point := range e.Points {
		points[i] = whiteboard.RotatePoint(whiteboard.Add(whiteboard.Point{e.X, e.Y}, point), center, e.Angle)
	}
	return points
}
