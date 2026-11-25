package editor

import (
	"math"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func boardToolKind(t BoardTool) whiteboard.ElementKind {
	switch t {
	case ToolRect:
		return whiteboard.KindRectangle
	case ToolDiamond:
		return whiteboard.KindDiamond
	case ToolEllipse:
		return whiteboard.KindEllipse
	case ToolArrow:
		return whiteboard.KindArrow
	case ToolLine:
		return whiteboard.KindLine
	case ToolFreedraw:
		return whiteboard.KindFreedraw
	case ToolFrame:
		return whiteboard.KindFrame
	case ToolNote:
		return whiteboard.KindNote
	case ToolImage:
		return whiteboard.KindImage
	default:
		return whiteboard.KindText
	}
}
func (b *Board) input(ev ui.InputEvent, o BoardOptions) bool {
	if o.ImageAnnotation && o.DismissImage != nil && ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape {
		o.DismissImage()
		return true
	}
	if b.editing != nil {
		if ev.Kind == ui.InputKeyDown {
			return b.textKey(ev, o)
		}
		if ev.Kind == ui.InputPointerDown {
			b.finishText(o)
		} else if ev.Kind == ui.InputCommand {
			return b.textCommand(ev.Text, o)
		}
	}
	switch ev.Kind {
	case ui.InputKeyUp:
		if ev.Key == ui.KeySpace {
			b.spaceDown = false
			return true
		}
	case ui.InputKeyDown:
		return b.key(ev, o)
	case ui.InputCommand:
		action := ev.Text
		if action == "selectAll" {
			action = "select-all"
		}
		b.RunAction(action, o)
		return true
	case ui.InputScroll:
		if o.ImageAnnotation || ev.Mods&(ui.Cmd|ui.Ctrl) != 0 {
			b.zoomAt(ev.X, ev.Y, math.Pow(1.008, float64(-ev.DY)))
		} else {
			b.PanX -= float64(ev.DX)
			b.PanY -= float64(ev.DY)
			if ev.DX != 0 || ev.DY != 0 {
				b.gestureCursor = ui.CursorGrabbing
				b.gestureChanged = true
			}
		}
		b.fitted = true
		return true
	case ui.InputZoom:
		b.zoomAt(ev.X, ev.Y, float64(ev.Scale))
		return true
	case ui.InputPointerDown:
		return b.pointerDown(ev, o)
	case ui.InputPointerMove:
		b.hover = b.scenePoint(ev.X, ev.Y)
		return b.pointerMove(ev, o)
	case ui.InputPointerUp:
		return b.pointerUp(ev, o)
	}
	return false
}
func (b *Board) key(ev ui.InputEvent, o BoardOptions) bool {
	if o.ImageAnnotation && b.editing == nil && ev.Mods&(ui.Cmd|ui.Ctrl|ui.Alt) == 0 {
		switch ev.Key {
		case ui.Key9, ui.KeyF, ui.KeyN, ui.KeyB:
			return true
		}
	}
	if b.multiLine && (ev.Key == ui.KeyEnter || ev.Key == ui.KeyEscape) {
		b.finishLine(o)
		return true
	}
	cmd, shift := ev.Mods&(ui.Cmd|ui.Ctrl) != 0, ev.Mods&ui.Shift != 0
	if cmd {
		action := ""
		switch ev.Key {
		case ui.KeyF:
			action = "search"
		case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
			if shift {
				actions := map[ui.Key]string{ui.KeyLeft: "align-x-start", ui.KeyRight: "align-x-end", ui.KeyUp: "align-y-start", ui.KeyDown: "align-y-end"}
				b.RunAction(actions[ev.Key], o)
			} else {
				b.flowNode(ev.Key, o)
			}
			return true
		case ui.KeyBracketLeft:
			action = "backward"
			if shift || ev.Mods&ui.Alt != 0 {
				action = "back"
			}
		case ui.KeyBracketRight:
			action = "forward"
			if shift || ev.Mods&ui.Alt != 0 {
				action = "front"
			}
		case ui.KeyQuote:
			action = "grid"
		case ui.Key0:
			action = "reset-zoom"
		case ui.KeyDelete, ui.KeyBackspace:
			action = "clear"
		case ui.KeyZ:
			action = "undo"
			if shift {
				action = "redo"
			}
		case ui.KeyY:
			action = "redo"
		case ui.KeyA:
			action = "select-all"
		case ui.KeyC:
			action = "copy"
			if shift {
				action = "copy-png"
			}
			if ev.Mods&ui.Alt != 0 {
				action = "copy-style"
			}
		case ui.KeyX:
			action = "cut"
		case ui.KeyV:
			action = "paste"
			if shift {
				b.pastePlain(o)
				return true
			}
			if ev.Mods&ui.Alt != 0 {
				action = "paste-style"
			}
		case ui.KeyD:
			action = "duplicate"
		case ui.KeyG:
			action = "group"
			if shift {
				action = "ungroup"
			}
		case ui.KeyL:
			action = "lock"
		case ui.KeyEqual:
			action = "zoom-in"
		case ui.KeyMinus:
			action = "zoom-out"
		}
		if action != "" {
			b.RunAction(action, o)
			return true
		}
		return false
	}
	if shift {
		switch ev.Key {
		case ui.KeyH:
			b.RunAction("flip-horizontal", o)
			return true
		case ui.KeyV:
			b.RunAction("flip-vertical", o)
			return true
		case ui.Key1:
			b.RunAction("fit", o)
			return true
		case ui.Key2:
			b.RunAction("fit-selection", o)
			return true
		case ui.Key0:
			b.RunAction("reset-zoom", o)
			return true
		case ui.KeySlash:
			b.RunAction("help", o)
			return true
		}
	}
	if ev.Mods&ui.Alt != 0 {
		switch ev.Key {
		case ui.KeyS:
			b.RunAction("snap", o)
			return true
		case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
			b.navigateNode(ev.Key)
			return true
		}
	}
	switch ev.Key {
	case ui.KeySpace:
		b.spaceDown = true
		return true
	case ui.KeyEscape:
		if b.mode != "" {
			// Cancel the current gesture before changing selection or tools.
			return b.pointerUp(ui.InputEvent{Canceled: true}, o)
		}
		if b.ai.picker != nil {
			b.ai.picker = nil
		} else if b.ai.link != nil {
			b.ai.link = nil
		} else if b.panel != "" {
			b.panel = ""
		} else if b.linearEdit != "" {
			b.linearEdit = ""
		} else if b.isolatedGroup != "" {
			b.isolatedGroup = ""
		} else if len(b.Selected) > 0 {
			b.Selected = map[string]bool{}
		} else if o.Done != nil {
			o.Done()
		}
		b.Tool = ToolSelect
		return true
	case ui.KeyDelete, ui.KeyBackspace:
		if b.linearEdit != "" && b.linearPoint >= 0 {
			b.edit(o, func() {
				e := b.Scene.ElementByID(b.linearEdit)
				if e != nil && len(e.Points) > 2 && b.linearPoint < len(e.Points) {
					e.Points = append(e.Points[:b.linearPoint], e.Points[b.linearPoint+1:]...)
					whiteboard.Touch(e)
				}
			})
		} else if ref := b.ai.link; ref != nil {
			b.deleteAILink(*ref, o)
		} else {
			b.RunAction("delete", o)
		}
		return true
	case ui.KeyEnter:
		ids := b.SelectedIDs()
		if len(ids) == 1 {
			e := b.Scene.ElementByID(ids[0])
			if b.aiNode(e) != nil {
				b.ai.focusInstruction = true
			} else if e.IsFrame() {
				b.panel = "frame-name"
				b.propertyInput = e.Name
			} else if e.IsLinear() {
				b.linearEdit = e.ID
			} else {
				b.startText(e, b.hover, o)
			}
		}
		return true
	case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
		d := whiteboard.Point{}
		step := float64(1)
		if shift {
			step = 10
		}
		switch ev.Key {
		case ui.KeyLeft:
			d[0] = -step
		case ui.KeyRight:
			d[0] = step
		case ui.KeyUp:
			d[1] = -step
		case ui.KeyDown:
			d[1] = step
		}
		b.edit(o, func() { b.Scene.Translate(b.SelectedIDs(), d) })
		return true
	}
	if b.ReadOnly {
		return false
	}
	switch ev.Key {
	case ui.KeyV:
		b.Tool = ToolSelect
	case ui.Key1:
		b.Tool = ToolSelect
	case ui.Key2:
		b.Tool = ToolRect
	case ui.Key3:
		b.Tool = ToolDiamond
	case ui.Key4:
		b.Tool = ToolEllipse
	case ui.Key5:
		b.Tool = ToolArrow
	case ui.Key6:
		b.Tool = ToolLine
	case ui.Key7:
		b.Tool = ToolFreedraw
	case ui.Key8:
		b.Tool = ToolText
	case ui.Key9:
		b.Tool = ToolImage
		if o.PickImage != nil {
			o.PickImage()
		}
	case ui.Key0:
		b.Tool = ToolEraser
	case ui.KeyH:
		b.Tool = ToolHand
	case ui.KeyR:
		b.Tool = ToolRect
	case ui.KeyD:
		b.Tool = ToolDiamond
	case ui.KeyO:
		b.Tool = ToolEllipse
	case ui.KeyA:
		b.Tool = ToolArrow
	case ui.KeyL:
		b.Tool = ToolLine
	case ui.KeyP:
		b.Tool = ToolFreedraw
	case ui.KeyT:
		b.Tool = ToolText
	case ui.KeyN:
		b.Tool = ToolNote
	case ui.KeyE:
		b.Tool = ToolEraser
	case ui.KeyF:
		b.Tool = ToolFrame
	case ui.KeyQ:
		b.ToolLocked = !b.ToolLocked
	case ui.KeyB:
		b.panel = "library"
	case ui.KeyEqual:
		b.RunAction("zoom-in", o)
	case ui.KeyMinus:
		b.RunAction("zoom-out", o)
	default:
		return false
	}
	b.penSelection()
	b.RequestFocus = true
	return true
}

// penSelection drops the selection while the pen is the tool, so strokes are
// drawn without a selection box around what was picked before (ArcMD 2fb223e).
func (b *Board) penSelection() {
	if b.Tool == ToolFreedraw && len(b.Selected) > 0 {
		b.Selected = map[string]bool{}
	}
}

func (b *Board) pointerDown(ev ui.InputEvent, o BoardOptions) bool {
	b.gestureCursor = ui.CursorDefault
	b.gestureChanged = false
	p := b.scenePoint(ev.X, ev.Y)
	if b.multiLine {
		if e := b.Scene.ElementByID(b.creating); e != nil {
			e.Points[len(e.Points)-1] = whiteboard.Sub(p, whiteboard.Point{e.X, e.Y})
			if ev.Clicks >= 2 {
				b.finishLine(o)
			} else {
				e.Points = append(e.Points, e.Points[len(e.Points)-1])
				whiteboard.Touch(e)
			}
		}
		return true
	}
	b.hover = p
	b.start = p
	b.ai.picker = nil
	b.last = whiteboard.Point{float64(ev.X), float64(ev.Y)}
	b.handle = -1
	b.linearPoint = -1
	b.moveApplied = whiteboard.Point{}
	if ev.Button == 2 || b.Tool == ToolHand || b.spaceDown {
		b.mode = "pan"
		return true
	}
	if ev.Button != 0 {
		return false
	}
	hit := b.canvasHit(p)
	if ev.Clicks >= 2 && hit != nil && o.AI != nil {
		if n := b.aiNode(hit); n != nil && (n.Role == whiteboard.AIImageInput || n.Output() != nil && n.Output().Kind == "image") {
			r := b.aiLayout(hit, n).preview
			if p[0] >= hit.X+float64(r.X) && p[0] <= hit.X+float64(r.X+r.W) && p[1] >= hit.Y+float64(r.Y) && p[1] <= hit.Y+float64(r.Y+r.H) {
				b.OpenAIImage(hit.ID, false, o)
				return true
			}
		}
	}
	if ev.Mods&(ui.Cmd|ui.Ctrl) != 0 && hit != nil && hit.Link != "" {
		if strings.HasPrefix(hit.Link, "[[") && o.OpenWiki != nil {
			o.OpenWiki(strings.TrimSuffix(strings.TrimPrefix(hit.Link, "[["), "]]"), ev.Mods&ui.Shift != 0)
		} else if o.OpenLink != nil {
			o.OpenLink(hit.Link)
		}
		return true
	}
	if b.ReadOnly {
		return false
	}
	if hit != nil && hit.Locked && (b.Tool == ToolSelect || b.Tool == ToolText || b.Tool == ToolEraser) {
		// Locked objects can still be selected to unlock them, but cannot
		// start a transform, edit session or eraser gesture.
		if b.Tool == ToolSelect {
			b.selectIDs([]string{hit.ID})
		}
		return true
	}
	if b.Tool == ToolSelect && o.AI != nil {
		// Node links are not board elements: a slot starts a link drag and
		// a click on a link selects it, before the board's own handling of
		// clicks and double-clicks.
		if b.aiBeginDrag(p) {
			b.before = b.Scene.Clone()
			b.History.Begin(b.Scene)
			b.mode = "ai-link"
			return true
		}
		if hit == nil || b.aiNode(hit) == nil {
			if l := b.aiLinkHit(p); l != nil {
				ref := l.aiLinkRef
				b.ai.link = &ref
				b.Selected = map[string]bool{}
				return true
			}
		}
		b.ai.link = nil
	}
	// A node card is edited on the card, never as text.
	if hit != nil && b.aiNode(hit) != nil && (b.Tool == ToolText || b.Tool == ToolSelect && ev.Clicks >= 2) {
		b.selectIDs([]string{hit.ID})
		b.ai.focusInstruction = true
		return true
	}
	if b.Tool == ToolText || (b.Tool == ToolSelect && ev.Clicks >= 2 && (hit == nil || !hit.IsLinear())) {
		if hit != nil && hit.IsFrame() {
			b.selectIDs([]string{hit.ID})
			b.panel = "frame-name"
			b.propertyInput = hit.Name
			return true
		}
		if hit != nil && len(hit.GroupIDs) > 0 && ev.Clicks >= 2 && b.isolatedGroup == "" {
			b.isolatedGroup = hit.GroupIDs[0]
			b.selectIDs([]string{hit.ID})
			return true
		}
		b.startText(hit, p, o)
		return true
	}
	if b.Tool == ToolImage {
		if o.PickImage != nil {
			o.PickImage()
		}
		return true
	}
	b.before = b.Scene.Clone()
	b.History.Begin(b.Scene)
	if b.Tool == ToolEraser {
		b.mode = "erase"
		if hit != nil {
			b.Scene.Delete([]string{hit.ID})
		}
		return true
	}
	if b.Tool == ToolLasso {
		b.mode = "lasso"
		b.lasso = []whiteboard.Point{p}
		return true
	}
	if b.Tool == ToolSelect {
		if i, insert := b.linePointAt(p); i >= 0 {
			if insert {
				e := b.Scene.ElementByID(b.linearEdit)
				local := boardLineLocalPoint(e, p)
				e.Points = append(e.Points[:i], append([]whiteboard.Point{local}, e.Points[i:]...)...)
			}
			b.linearPoint, b.mode = i, "line-point"
			return true
		}
		if i, bounds := b.selectionHandleAt(p); i >= 0 {
			b.handle, b.selectionBox, b.mode = i, bounds, "resize"
			if i == 8 {
				b.mode = "rotate"
				c := bounds.Center()
				b.rotateStart = math.Atan2(p[1]-c[1], p[0]-c[0])
			}
			return true
		}
		if hit != nil {
			if ev.Clicks >= 2 && hit.IsLinear() {
				b.linearEdit = hit.ID
				b.selectIDs([]string{hit.ID})
				b.History.Cancel()
				b.before = nil
				return true
			}
			if ev.Mods&ui.Shift != 0 {
				if b.Selected[hit.ID] {
					delete(b.Selected, hit.ID)
				} else {
					b.Selected[hit.ID] = true
				}
			} else if !b.Selected[hit.ID] {
				b.selectIDs(b.Scene.ExpandSelection([]string{hit.ID}, b.isolatedGroup == ""))
			}
			if ev.Mods&ui.Alt != 0 {
				clip, _ := b.Scene.Clipboard(b.SelectedIDs())
				ids, err := b.Scene.Paste(clip, whiteboard.SelectionBounds(b.Scene, b.SelectedIDs()).Center())
				if err == nil {
					b.selectIDs(ids)
					b.before = b.Scene.Clone()
				}
			}
			b.mode = "move"
			return true
		}
		if ev.Mods&ui.Shift == 0 {
			b.Selected = map[string]bool{}
		}
		b.mode = "marquee"
		b.selectionBox = whiteboard.Box{X: p[0], Y: p[1]}
		return true
	}
	e := b.newElement(boardToolKind(b.Tool), p, 0, 0)
	if b.Tool == ToolNote {
		e.W, e.H = 250, 250
	}
	if b.Tool == ToolFrame {
		e.Name = i18nBoardFrame()
	}
	if b.Tool == ToolFreedraw {
		e.Points = []whiteboard.Point{{0, 0}}
		e.Pressures = []float64{float64(ev.Pressure)}
	}
	if e.IsLinear() {
		e.Points = []whiteboard.Point{{0, 0}, {0, 0}}
	}
	b.Scene.Elements = append(b.Scene.Elements, e)
	b.creating = e.ID
	if e.Kind == whiteboard.KindFreedraw {
		b.Selected = map[string]bool{}
	} else {
		b.selectIDs([]string{e.ID})
	}
	b.mode = "create"
	if e.Kind == whiteboard.KindArrow && b.ArrowBinding && hit != nil {
		b.Scene.Bind(e.ID, false, hit.ID, p)
	}
	return true
}
func i18nBoardFrame() string { return "Frame" }
func (b *Board) pointerMove(ev ui.InputEvent, o BoardOptions) bool {
	if b.mode == "" {
		return false
	}
	p := b.scenePoint(ev.X, ev.Y)
	d := whiteboard.Sub(p, b.start)
	switch b.mode {
	case "ai-link":
		b.aiDragTo(p)
	case "pan":
		b.PanX += float64(ev.X) - b.last[0]
		b.PanY += float64(ev.Y) - b.last[1]
		b.last = whiteboard.Point{float64(ev.X), float64(ev.Y)}
		b.fitted = true
	case "move":
		ids := b.SelectedIDs()
		if b.Snap {
			grid := float64(0)
			if b.ShowGrid {
				grid = b.Scene.AppState.GridSize
			}
			d, _ = b.before.Snap(ids, d, 6/b.Zoom, grid)
		}
		b.Scene.Translate(ids, whiteboard.Sub(d, b.moveApplied))
		b.moveApplied = d
	case "resize":
		b.resetTransform()
		b.Scene.Resize(b.SelectedIDs(), b.resizeBounds(p, ev.Mods))
	case "rotate":
		b.resetTransform()
		c := b.selectionBox.Center()
		angle := math.Atan2(p[1]-c[1], p[0]-c[0]) - b.rotateStart
		if ev.Mods&ui.Shift != 0 {
			angle = math.Round(angle/(math.Pi/12)) * (math.Pi / 12)
		}
		b.Scene.Rotate(b.SelectedIDs(), angle)
	case "marquee":
		b.selectionBox = whiteboard.Box{X: math.Min(b.start[0], p[0]), Y: math.Min(b.start[1], p[1]), W: math.Abs(d[0]), H: math.Abs(d[1])}
	case "lasso":
		b.lasso = append(b.lasso, p)
	case "erase":
		if hit := b.Scene.Hit(p, 8/b.Zoom, false); hit != nil {
			b.Scene.Delete([]string{hit.ID})
		}
	case "line-point":
		e := b.Scene.ElementByID(b.linearEdit)
		if e != nil && b.linearPoint >= 0 {
			e.Points[b.linearPoint] = boardLineLocalPoint(e, p)
			whiteboard.Touch(e)
		}
	case "create":
		e := b.Scene.ElementByID(b.creating)
		if e == nil {
			return false
		}
		if e.Kind == whiteboard.KindFreedraw {
			e.Points = append(e.Points, d)
			e.Pressures = append(e.Pressures, float64(ev.Pressure))
			bb := whiteboard.PointsBounds(e.Points)
			e.W, e.H = bb.W, bb.H
		} else if e.IsLinear() {
			if ev.Mods&ui.Shift != 0 {
				angle := math.Round(math.Atan2(d[1], d[0])/(math.Pi/12)) * (math.Pi / 12)
				l := math.Hypot(d[0], d[1])
				d = whiteboard.Point{math.Cos(angle) * l, math.Sin(angle) * l}
			}
			e.Points[len(e.Points)-1] = d
			e.W, e.H = math.Abs(d[0]), math.Abs(d[1])
		} else {
			if ev.Mods&ui.Shift != 0 {
				size := math.Max(math.Abs(d[0]), math.Abs(d[1]))
				d[0] = math.Copysign(size, d[0])
				d[1] = math.Copysign(size, d[1])
			}
			e.X, e.Y = math.Min(b.start[0], b.start[0]+d[0]), math.Min(b.start[1], b.start[1]+d[1])
			e.W, e.H = math.Abs(d[0]), math.Abs(d[1])
			if e.Kind == whiteboard.KindNote {
				e.W = math.Max(150, e.W)
				e.H = math.Max(150, e.H)
			}
		}
		whiteboard.Touch(e)
	}
	return true
}

// resizeBounds retains signed dimensions so cursor feedback follows crossed
// edges and the same aspect-ratio constraint as the applied transform.
func (b *Board) resizeBounds(p whiteboard.Point, mods ui.Modifiers) whiteboard.Box {
	r := b.selectionBox
	right, bottom := r.X+r.W, r.Y+r.H
	switch b.handle {
	case 0, 6, 7:
		r.X = p[0]
	case 2, 3, 4:
		right = p[0]
	}
	switch b.handle {
	case 0, 1, 2:
		r.Y = p[1]
	case 4, 5, 6:
		bottom = p[1]
	}
	r.W, r.H = right-r.X, bottom-r.Y
	if mods&ui.Shift != 0 && b.selectionBox.H != 0 && b.selectionBox.W != 0 {
		r.H = r.W / (b.selectionBox.W / b.selectionBox.H)
	}
	return r
}

// resetTransform restores only the gesture's elements and attached lines.
// The baseline is immutable; unrelated shapes and image resources stay shared
// with the live scene instead of being cloned for every pointer event.
func (b *Board) resetTransform() {
	ids := b.before.ExpandSelection(b.SelectedIDs(), false)
	changed := make(map[string]bool, len(ids))
	for _, id := range ids {
		changed[id] = true
	}
	for _, e := range b.Scene.Elements {
		attached := e.IsLinear() && (e.StartBinding != nil && changed[e.StartBinding.ElementID] || e.EndBinding != nil && changed[e.EndBinding.ElementID])
		if !changed[e.ID] && !attached {
			continue
		}
		if base := b.before.ElementByID(e.ID); base != nil {
			*e = *base.Clone()
		}
	}
}
func (b *Board) pointerUp(ev ui.InputEvent, o BoardOptions) bool {
	if b.mode == "" {
		return false
	}
	mode := b.mode
	if b.multiLine {
		return true
	}
	if mode == "ai-link" {
		b.mode = ""
		if ev.Canceled {
			b.ai.drag = nil
			b.resetFrom(b.before)
			b.History.Cancel()
		} else if b.aiDrop(o) && b.History.Commit(b.Scene) {
			b.changed(o)
		} else {
			b.History.Cancel()
		}
		b.before = nil
		return true
	}
	if mode == "create" {
		if e := b.Scene.ElementByID(b.creating); e != nil && e.IsLinear() && whiteboard.Distance(b.start, b.scenePoint(ev.X, ev.Y)) < 3/b.Zoom && !ev.Canceled {
			b.multiLine = true
			return true
		}
	}
	b.mode = ""
	p := b.scenePoint(ev.X, ev.Y)
	if ev.Canceled {
		if b.before != nil {
			b.resetFrom(b.before)
		}
		b.History.Cancel()
		b.before = nil
		b.creating = ""
		b.lasso = nil
		return true
	}
	if mode == "marquee" {
		r := b.selectionBox
		ps := []whiteboard.Point{{r.X, r.Y}, {r.X + r.W, r.Y}, {r.X + r.W, r.Y + r.H}, {r.X, r.Y + r.H}}
		for _, id := range b.Scene.Lasso(ps, ev.Mods&ui.Alt != 0) {
			b.Selected[id] = true
		}
	}
	if mode == "lasso" {
		if ev.Mods&ui.Shift == 0 {
			b.Selected = map[string]bool{}
		}
		for _, id := range b.Scene.Lasso(b.lasso, true) {
			b.Selected[id] = true
		}
		b.lasso = nil
	}
	var note *whiteboard.Element
	if mode == "create" {
		e := b.Scene.ElementByID(b.creating)
		if e != nil {
			if e.Kind == whiteboard.KindArrow && b.ArrowBinding {
				for i := len(b.Scene.Elements) - 1; i >= 0; i-- {
					target := b.Scene.Elements[i]
					if target.ID != e.ID && target.IsBindable() && whiteboard.HitTest(target, p, 10/b.Zoom) {
						b.Scene.Bind(e.ID, true, target.ID, p)
						break
					}
				}
			}
			if e != nil && e.Kind == whiteboard.KindNote {
				note = e
			}
			if e != nil && e.W < 2 && e.H < 2 && e.Kind != whiteboard.KindFreedraw {
				boardDeleteElement(b.Scene, e.ID)
			}
			if e != nil && e.IsFrame() {
				for _, child := range b.Scene.Elements {
					if child.ID != e.ID && !child.IsFrame() && (whiteboard.Box{X: e.X, Y: e.Y, W: e.W, H: e.H}).Contains(whiteboard.ElementBounds(child).Center()) {
						child.FrameID = e.ID
						whiteboard.Touch(child)
					}
				}
			}
		}
		b.lastCreated = b.creating
		b.creating = ""
		if !b.ToolLocked && (e == nil || e.Kind != whiteboard.KindFreedraw) {
			b.Tool = ToolSelect
		}
	}
	if b.History.Commit(b.Scene) {
		b.changed(o)
	}
	b.before = nil
	b.cleanSelection()
	if note != nil {
		b.startText(note, p, o)
	}
	return true
}

func (b *Board) finishLine(o BoardOptions) {
	if !b.multiLine {
		return
	}
	b.multiLine = false
	if e := b.Scene.ElementByID(b.creating); e != nil {
		if len(e.Points) > 2 && whiteboard.Distance(e.Points[len(e.Points)-1], e.Points[len(e.Points)-2]) < 1e-6 {
			e.Points = e.Points[:len(e.Points)-1]
		}
		bounds := whiteboard.PointsBounds(e.Points)
		e.W, e.H = bounds.W, bounds.H
		whiteboard.Touch(e)
		if len(e.Points) < 2 || bounds.W < 1 && bounds.H < 1 {
			boardDeleteElement(b.Scene, e.ID)
		}
	}
	b.mode = ""
	b.creating = ""
	b.before = nil
	if b.History.Commit(b.Scene) {
		b.changed(o)
	}
	if !b.ToolLocked {
		b.Tool = ToolSelect
	}
	b.cleanSelection()
}
