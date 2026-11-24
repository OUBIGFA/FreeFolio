package editor

import (
	"math"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func boardDirection(key ui.Key) whiteboard.Point {
	switch key {
	case ui.KeyLeft:
		return whiteboard.Point{-1, 0}
	case ui.KeyRight:
		return whiteboard.Point{1, 0}
	case ui.KeyUp:
		return whiteboard.Point{0, -1}
	default:
		return whiteboard.Point{0, 1}
	}
}
func (b *Board) flowNode(key ui.Key, o BoardOptions) {
	ids := b.SelectedIDs()
	if len(ids) != 1 || b.ReadOnly {
		return
	}
	old := b.Scene.ElementByID(ids[0])
	if old == nil || !(old.Kind == whiteboard.KindRectangle || old.Kind == whiteboard.KindEllipse || old.Kind == whiteboard.KindDiamond || old.Kind == whiteboard.KindNote) {
		return
	}
	dir := boardDirection(key)
	b.edit(o, func() {
		next := old.Clone()
		next.ID = whiteboard.NewID()
		next.Version = 1
		next.VersionNonce = whiteboard.NewSeed()
		next.BoundElements = nil
		next.GroupIDs = nil
		next.FrameID = ""
		next.Link = ""
		next.Index = ""
		next.X += dir[0] * (old.W + 100)
		next.Y += dir[1] * (old.H + 100)
		for attempts := 0; attempts < len(b.Scene.Elements)+1; attempts++ {
			overlaps := false
			for _, e := range b.Scene.Elements {
				if !e.IsDeleted && !e.IsLinear() && whiteboard.ElementBounds(e).Expand(20).Intersects(whiteboard.ElementBounds(next)) {
					overlaps = true
					break
				}
			}
			if !overlaps {
				break
			}
			if dir[0] != 0 {
				next.Y += old.H + 50
			} else {
				next.X += old.W + 50
			}
		}
		start := whiteboard.Point{old.X + old.W*(dir[0]+1)/2, old.Y + old.H*(dir[1]+1)/2}
		end := whiteboard.Point{next.X + next.W*(1-dir[0])/2, next.Y + next.H*(1-dir[1])/2}
		arrow := whiteboard.NewElement(whiteboard.KindArrow, start[0], start[1], math.Abs(end[0]-start[0]), math.Abs(end[1]-start[1]))
		arrow.Elbowed = true
		b.Scene.Elements = append(b.Scene.Elements, next, arrow)
		b.Scene.Bind(arrow.ID, false, old.ID, start)
		b.Scene.Bind(arrow.ID, true, next.ID, end)
		b.selectIDs([]string{next.ID})
	})
}
func (b *Board) navigateNode(key ui.Key) {
	ids := b.SelectedIDs()
	if len(ids) != 1 {
		return
	}
	e := b.Scene.ElementByID(ids[0])
	if e == nil {
		return
	}
	center := whiteboard.ElementBounds(e).Center()
	direction := boardDirection(key)
	best := math.Inf(1)
	id := ""
	for _, candidate := range b.Scene.Elements {
		if candidate.ID == e.ID || candidate.IsDeleted || candidate.IsLinear() || candidate.Kind == whiteboard.KindText {
			continue
		}
		d := whiteboard.Sub(whiteboard.ElementBounds(candidate).Center(), center)
		forward := d[0]*direction[0] + d[1]*direction[1]
		if forward <= 0 {
			continue
		}
		cross := math.Abs(d[0]*direction[1] - d[1]*direction[0])
		score := forward + 2*cross
		if score < best {
			best = score
			id = candidate.ID
		}
	}
	if id != "" {
		b.RevealElement(id)
	}
}
func (b *Board) pastePlain(o BoardOptions) {
	text := b.services.ReadClipboard()
	if text == "" {
		return
	}
	b.edit(o, func() {
		e := b.newElement(whiteboard.KindText, b.hover, 0, 0)
		e.Text, e.OriginalText = text, text
		b.Scene.Elements = append(b.Scene.Elements, e)
		b.Scene.LayoutText(e.ID, boardMeasure)
		b.selectIDs([]string{e.ID})
	})
}
