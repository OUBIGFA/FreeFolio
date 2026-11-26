package editor

import (
	"math"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// The node graph's interaction ports LiteGraph's (Comfy-Org/litegraph.js,
// MIT, revision ea0e1a85), the canvas ComfyUI is built on:
//   - LinkConnector: dragging from an output connects to an input; from a
//     free input, to an output; from a linked input, the link is picked up
//     (MovingInputLink) and dropping it on nothing disconnects it;
//   - a link dropped on a node's body goes to its first free slot of the
//     link's type (findInputByType), on a slot to that slot, replacing its
//     link (connectSlots);
//   - a new link dropped on empty canvas offers the nodes it can feed
//     (release_link_on_empty_shows_menu).
// Board elements that show text, pictures or frames can be sources too, as
// the board is the workflow's material; their links leave their right edge.
// Curve control offsets and arrowheads use dim0/canvas-harness 0.2.1 (MIT).

// Card geometry, in scene units.
const (
	aiHeader     = 40
	aiSlotRow    = 22
	aiFooter     = 24
	aiPad        = 12
	aiPort       = 5
	aiCardRadius = 12
)

// aiCardLayout places a card's parts in its local coordinates.
type aiCardLayout struct {
	slots            []whiteboard.AISlot
	out              string
	slotTop          float32
	headerRun        ui.Rect
	widgets, preview ui.Rect
	history          ui.Rect
}

func aiRunnableRole(role whiteboard.AIRole) bool {
	switch role {
	case whiteboard.AIText, whiteboard.AIImage, whiteboard.AIVideo, whiteboard.AIConcat, whiteboard.AINote:
		return true
	}
	return false
}

// aiWidgetHeightFor is the height of a role's in-node controls.
func aiWidgetHeightFor(role whiteboard.AIRole, linkedPrompt bool) float32 {
	switch role {
	case whiteboard.AINote:
		return 30
	case whiteboard.AIConcat:
		return 68
	case whiteboard.AIPrompt, whiteboard.AIImageInput:
		return 0
	}
	// Prompt/instruction field (collapsed when linked), model row, and settings row.
	promptH := float32(58)
	if linkedPrompt {
		promptH = 30
	}
	return promptH + 6 + 26 + 4 + 26
}

// aiPreviewMin is the least room a role's result needs.
func aiPreviewMin(role whiteboard.AIRole) float32 {
	switch role {
	case whiteboard.AIPrompt:
		return 80
	case whiteboard.AINote:
		return 44
	case whiteboard.AIConcat:
		return 56
	case whiteboard.AIText:
		return 100
	case whiteboard.AIVideo:
		return 150
	}
	return 160
}

func aiSlotRows(slots []whiteboard.AISlot, out string) int {
	rows := len(slots)
	if out != "" {
		rows = max(rows, 1)
	}
	return rows
}

// aiMinHeight is the card height that shows every slot, the controls and
// some of the result.
func aiMinHeight(n *whiteboard.AINode) float64 {
	rows := aiSlotRows(whiteboard.AISlots(n), whiteboard.AIOutputType(n.Role))
	linkedPrompt := n.LinkAt("prompt") != ""
	wh := aiWidgetHeightFor(n.Role, linkedPrompt)
	previewGap := float32(10)
	if wh == 0 {
		previewGap = 4
	}
	return float64(aiHeader + 4 + float32(rows)*aiSlotRow + 6 + wh + previewGap + aiPreviewMin(n.Role) + 8 + aiFooter)
}

func (b *Board) aiLayout(e *whiteboard.Element, n *whiteboard.AINode) aiCardLayout {
	s := b.aiState()
	var slots []whiteboard.AISlot
	cached, ok := s.nodes[e.ID]
	if ok && cached.node == n && cached.layout != nil {
		slots = cached.layout.slots
	} else {
		slots = whiteboard.AISlots(n)
		if ok && cached.node == n {
			cached.layout = &aiCardLayout{slots: slots}
			s.nodes[e.ID] = cached
		}
	}
	l := aiCardLayout{slots: slots, out: whiteboard.AIOutputType(n.Role), slotTop: aiHeader + 4}
	w, h := float32(e.W), float32(e.H)
	innerW := max(0, w-2*aiPad)
	if aiRunnableRole(n.Role) && w >= 180 {
		l.headerRun = ui.Rect{X: w - aiPad - 76, Y: (aiHeader - 26) / 2, W: 76, H: 26}
	}
	linkedPrompt := b.Scene.AILinkSource(n, "prompt") != nil
	top := l.slotTop + float32(aiSlotRows(l.slots, l.out))*aiSlotRow + 6
	wh := aiWidgetHeightFor(n.Role, linkedPrompt)
	l.widgets = ui.Rect{X: aiPad, Y: top, W: innerW, H: wh}
	if n.Role == whiteboard.AIPrompt {
		// A text node's field is its body.
		l.widgets.H = max(0, h-aiFooter-8-top)
		l.preview = ui.Rect{X: aiPad, Y: top + l.widgets.H, W: innerW}
		return l
	}
	outGap := float32(10)
	if wh == 0 {
		outGap = 4
	}
	outTop := top + wh + outGap
	avail := max(0, h-aiFooter-8-outTop)
	if n.Role == whiteboard.AIImage && len(n.Outputs) >= 2 {
		histH := float32(26)
		if b.aiHistory(e, n).compare {
			histH = 56
		}
		gap := float32(6)
		prevH := max(0, avail-histH-gap)
		l.preview = ui.Rect{X: aiPad, Y: outTop, W: innerW, H: prevH}
		l.history = ui.Rect{X: aiPad, Y: outTop + prevH + gap, W: innerW, H: histH}
	} else {
		l.preview = ui.Rect{X: aiPad, Y: outTop, W: innerW, H: avail}
	}
	return l
}

// aiInputPoint is an input slot's point on the board.
func aiInputPoint(e *whiteboard.Element, l aiCardLayout, i int) whiteboard.Point {
	return whiteboard.Point{e.X, e.Y + float64(l.slotTop) + float64(i)*aiSlotRow + aiSlotRow/2}
}

// aiOutputPoint is a node's output slot on the board.
func aiOutputPoint(e *whiteboard.Element, l aiCardLayout) whiteboard.Point {
	return whiteboard.Point{e.X + e.W, e.Y + float64(l.slotTop) + aiSlotRow/2}
}

// aiSourcePoint is where links from an element start: a node's output
// slot, or the middle of another element's right edge.
func (b *Board) aiSourcePoint(e *whiteboard.Element) whiteboard.Point {
	if n := b.aiNode(e); n != nil {
		return aiOutputPoint(e, b.aiLayout(e, n))
	}
	box := b.elementBounds(e)
	return whiteboard.Point{box.X + box.W, box.Y + box.H/2}
}

// aiSpline ports dim0/canvas-harness autoRouteControls, with fixed outward
// normals for the node editor's right output and left input ports.
func aiSpline(a, z whiteboard.Point) [4]whiteboard.Point {
	d := math.Min(200, whiteboard.Distance(a, z)*0.4)
	return [4]whiteboard.Point{a, {a[0] + d, a[1]}, {z[0] - d, z[1]}, z}
}

// aiSplineAt is the curve's point at t: computeConnectionPoint.
func aiSplineAt(c [4]whiteboard.Point, t float64) whiteboard.Point {
	c1, c2, c3, c4 := (1-t)*(1-t)*(1-t), 3*(1-t)*(1-t)*t, 3*(1-t)*t*t, t*t*t
	return whiteboard.Point{c1*c[0][0] + c2*c[1][0] + c3*c[2][0] + c4*c[3][0], c1*c[0][1] + c2*c[1][1] + c3*c[2][1] + c4*c[3][1]}
}

// aiLinkRef names a link by its input.
type aiLinkRef struct{ node, slot string }

// aiLink is a drawn link.
type aiLink struct {
	aiLinkRef
	from  *whiteboard.Element
	typ   string
	curve [4]whiteboard.Point
}

// aiLinks are the board's live links, in scene order of their nodes.
func (b *Board) aiLinks() []aiLink {
	s := b.aiState()
	signature := uint64(len(b.Scene.Elements))
	for _, e := range b.Scene.Elements {
		signature = (signature ^ uint64(e.Version) ^ uint64(e.VersionNonce)) * 1099511628211
		if e.IsDeleted {
			signature ^= 1
		}
	}
	if s.linkScene == b.Scene && s.linkSignature == signature {
		return s.links
	}
	var out []aiLink
	for _, e := range b.Scene.Elements {
		n := b.aiNode(e)
		if n == nil || e.IsDeleted || len(n.Links) == 0 {
			continue
		}
		l := b.aiLayout(e, n)
		for i, sl := range l.slots {
			from := b.Scene.AILinkSource(n, sl.Name)
			if from == nil || from.ID == e.ID {
				continue
			}
			out = append(out, aiLink{aiLinkRef{e.ID, sl.Name}, from, sl.Type, aiSpline(b.aiSourcePoint(from), aiInputPoint(e, l, i))})
		}
	}
	s.linkScene, s.linkSignature, s.links = b.Scene, signature, out
	return s.links
}

// aiLinkHit is the link under a board point, by the curve's distance.
func (b *Board) aiLinkHit(p whiteboard.Point) *aiLink {
	tol := 6 / max(0.25, b.Zoom)
	links := b.aiLinks()
	for i := len(links) - 1; i >= 0; i-- {
		if !whiteboard.PointsBounds(links[i].curve[:]).Expand(tol).Contains(p) {
			continue
		}
		prev := links[i].curve[0]
		for k := 1; k <= 24; k++ {
			next := aiSplineAt(links[i].curve, float64(k)/24)
			if whiteboard.SegmentDistance(p, prev, next) <= tol {
				return &links[i]
			}
			prev = next
		}
	}
	return nil
}

func aiTypeColor(typ string, dark bool) ui.Color {
	kind := "text"
	switch typ {
	case whiteboard.AITypeImage:
		kind = "image"
	case whiteboard.AITypeVideo:
		kind = "video"
	}
	return aiNodePalette(kind, dark).accent
}

func aiSplinePath(c [4]whiteboard.Point) *ui.Path {
	return new(ui.Path).Cap(ui.RoundCap).MoveTo(float32(c[0][0]), float32(c[0][1])).CubeTo(float32(c[1][0]), float32(c[1][1]), float32(c[2][0]), float32(c[2][1]), float32(c[3][0]), float32(c[3][1]))
}

// paintAILink uses dim0's plain cubic body and filled arrowhead. Node links
// remain separate from drawing arrows and retain their typed slot semantics.
func (b *Board) paintAILink(p *ui.Painter, c [4]whiteboard.Point, color ui.Color, width float32, dark bool) {
	path := aiSplinePath(c)
	p.StrokePath(path, width, color)
	direction := whiteboard.Sub(c[3], c[2])
	if length := whiteboard.Distance(c[3], c[2]); length > 0 {
		direction = whiteboard.ScalePoint(direction, 1/length)
		head := whiteboard.DimArrowhead("arrow-filled", c[3], direction, float64(width), "", 1)
		p.FillPath(nativeBoardPath(head.Ops), color)
	}
}

// paintAILinks draws the stored links, under the cards and over the rest.
func (b *Board) paintAILinks(p *ui.Painter, dark bool, viewport whiteboard.Box) {
	s := b.aiState()
	clear(s.linkedOut)
	cardBg := aiNodePalette("text", dark).cardBg
	for _, l := range b.aiLinks() {
		s.linkedOut[l.from.ID] = true
		if !viewport.Intersects(whiteboard.PointsBounds(l.curve[:]).Expand(8)) {
			continue
		}
		color := aiTypeColor(l.typ, dark)
		b.paintAILink(p, l.curve, color.Alpha(0.85), 2.2, dark)
		// LiteGraph's circular link centre with a cutout halo, kept legible at every zoom.
		mid := aiSplineAt(l.curve, .5)
		radius := float32(4.5 / b.Zoom)
		halo := radius + float32(1.5/b.Zoom)
		p.Fill(ui.Rect{X: float32(mid[0]) - halo, Y: float32(mid[1]) - halo, W: halo * 2, H: halo * 2}, cardBg, halo)
		p.Fill(ui.Rect{X: float32(mid[0]) - radius, Y: float32(mid[1]) - radius, W: radius * 2, H: radius * 2}, color, radius)
	}
}

// paintAIOverlay draws, in board units, what is not part of the scene: the
// hovered or selected link and the link being dragged.
func (b *Board) paintAIOverlay(p *ui.Painter, dark bool) {
	s := &b.ai
	if d := s.drag; d != nil && d.live {
		var a, z whiteboard.Point
		switch {
		case d.from != "":
			a, z = b.aiSourcePoint(b.Scene.ElementByID(d.from)), d.at
			if t := b.Scene.ElementByID(d.target); t != nil {
				n := b.aiNode(t)
				l := b.aiLayout(t, n)
				for i, sl := range l.slots {
					if sl.Name == d.targetSlot {
						z = aiInputPoint(t, l, i)
					}
				}
			}
		default:
			to := b.Scene.ElementByID(d.to)
			l := b.aiLayout(to, b.aiNode(to))
			for i, sl := range l.slots {
				if sl.Name == d.slot {
					z = aiInputPoint(to, l, i)
				}
			}
			a = d.at
			if t := b.Scene.ElementByID(d.target); t != nil {
				a = b.aiSourcePoint(t)
			}
		}
		b.paintAILink(p, aiSpline(a, z), aiTypeColor(d.typ, dark), 2.4, dark)
		return
	}
	var shown *aiLink
	if s.link != nil {
		for _, l := range b.aiLinks() {
			if l.aiLinkRef == *s.link {
				shown = &l
			}
		}
		if shown == nil {
			s.link = nil
		}
	}
	if shown == nil && b.mode == "" && b.Tool == ToolSelect {
		shown = b.aiLinkHit(b.hover)
	}
	if shown == nil {
		return
	}
	color := aiTypeColor(shown.typ, dark)
	b.paintAILink(p, shown.curve, color, 3.2, dark)
}

// aiLinkControls gives the small centre marker a screen-sized hit target.
// Clicking selects only; disconnection stays with Delete and the context menu.
func (b *Board) aiLinkControls(c *ui.Context, o BoardOptions) {
	if o.AI == nil || b.ReadOnly || b.Tool != ToolSelect || b.spaceDown || b.mode != "" || b.editing != nil {
		return
	}
	for _, l := range b.aiLinks() {
		mid := aiSplineAt(l.curve, .5)
		x, y, _, _ := b.canvasRect(mid[0], mid[1], 0, 0)
		if x < 0 || y < 0 || x > b.lastRect.W || y > b.lastRect.H {
			continue
		}
		node := b.Scene.ElementByID(l.node)
		button := ui.ButtonBase(c.Key("board-ai-link:"+l.node+":"+l.slot)).
			Absolute().Left(x-14).Top(y-14).Size(28, 28).Radius(14).
			Focusable().Label(i18n.T("选择连线")).Tooltip(i18n.T("选择连线，按 Delete 或右键断开")).
			Cursor(ui.CursorPointer).Disabled(node == nil || node.Locked)
		if node == nil || node.Locked {
			button.Cursor(ui.CursorNotAllowed)
		}
		if button.Hovered() || b.ai.link != nil && *b.ai.link == l.aiLinkRef {
			color := aiTypeColor(l.typ, o.Theme.Dark)
			button.Draw(func(p *ui.Painter, r ui.Rect) {
				p.Stroke(ui.Rect{X: r.X + 5, Y: r.Y + 5, W: 18, H: 18}, color, 9, 2)
			})
		}
		selectLink := func() {
			ref := l.aiLinkRef
			b.ai.link = &ref
			b.Selected = map[string]bool{}
			b.RequestFocus = true
		}
		if button.Clicked() {
			selectLink()
		}
		button.ContextMenu(func(m *ui.Menu) {
			selectLink()
			b.contextMenu(m, o)
		})
	}
}

// aiSlotHit finds the slot under a board point: a node's output, or one of
// its inputs with its label, as LiteGraph's slot bounding boxes.
func (b *Board) aiSlotHit(p whiteboard.Point) (id, slot string, output bool) {
	r := max(10/max(0.25, b.Zoom), aiPort+4)
	for i := len(b.Scene.Elements) - 1; i >= 0; i-- {
		e := b.Scene.Elements[i]
		n := b.aiNode(e)
		if n == nil || e.IsDeleted || e.Locked {
			continue
		}
		if !(whiteboard.Box{X: e.X, Y: e.Y, W: e.W, H: e.H}).Expand(r).Contains(p) {
			continue
		}
		l := b.aiLayout(e, n)
		if l.out != "" {
			op := aiOutputPoint(e, l)
			if whiteboard.Distance(p, op) <= r || p[0] >= e.X+e.W-64 && p[0] <= op[0]+r && math.Abs(p[1]-op[1]) <= aiSlotRow/2 {
				return e.ID, "", true
			}
		}
		for k, sl := range l.slots {
			ip := aiInputPoint(e, l, k)
			if whiteboard.Distance(p, ip) <= r || p[0] >= ip[0]-r && p[0] <= e.X+72 && math.Abs(p[1]-ip[1]) <= aiSlotRow/2 {
				return e.ID, sl.Name, false
			}
		}
		if (whiteboard.Box{X: e.X, Y: e.Y, W: e.W, H: e.H}).Contains(p) {
			return "", "", false
		}
	}
	return "", "", false
}

// aiDrag is a link being dragged. With from set it looks for an input; with
// to and slot set, for a source.
type aiDrag struct {
	from, to, slot, typ string
	// detach is the link picked up from a linked input, taken off once the
	// pointer moves, so a click on a slot changes nothing.
	detach   *aiLinkRef
	detached bool
	live     bool
	at       whiteboard.Point
	// target and targetSlot are where the link would land.
	target, targetSlot string
	// over is an element under the pointer that cannot take the link.
	over bool
}

// aiBeginDrag starts a link drag from a slot under p, if any.
func (b *Board) aiBeginDrag(p whiteboard.Point) bool {
	id, slot, output := b.aiSlotHit(p)
	if id == "" {
		return false
	}
	e := b.Scene.ElementByID(id)
	n := b.aiNode(e)
	b.ai.link = nil
	if output {
		b.ai.drag = &aiDrag{from: id, typ: whiteboard.AIOutputType(n.Role), at: p}
		return true
	}
	sl, _ := whiteboard.AISlotByName(n, slot)
	if from := b.Scene.AILinkSource(n, slot); from != nil {
		b.ai.drag = &aiDrag{from: from.ID, typ: b.Scene.AISourceType(from), detach: &aiLinkRef{id, slot}, at: p}
		return true
	}
	b.ai.drag = &aiDrag{to: id, slot: slot, typ: sl.Type, at: p}
	return true
}

// aiDragTo follows the pointer, picking up a link on the first move and
// finding where the link would land.
func (b *Board) aiDragTo(p whiteboard.Point) {
	d := b.ai.drag
	d.at = p
	if !d.live && whiteboard.Distance(p, b.start) < 4/b.Zoom {
		return
	}
	d.live = true
	if d.detach != nil && !d.detached {
		b.setAILink(d.detach.node, d.detach.slot, "")
		d.detached = true
	}
	d.target, d.targetSlot, d.over = "", "", false
	pad := 8 / max(0.25, b.Zoom)
	for i := len(b.Scene.Elements) - 1; i >= 0; i-- {
		e := b.Scene.Elements[i]
		if e.IsDeleted || e.Kind == whiteboard.KindText && e.ContainerID != "" || !b.elementBounds(e).Expand(pad).Contains(p) {
			continue
		}
		if d.from != "" {
			n := b.aiNode(e)
			if n == nil {
				continue
			}
			d.over = true
			if e.ID == d.from || b.Scene.AICreatesCycle(d.from, e.ID) {
				return
			}
			l := b.aiLayout(e, n)
			for k, sl := range l.slots {
				if math.Abs(p[1]-aiInputPoint(e, l, k)[1]) <= aiSlotRow/2 && p[0] <= e.X+e.W/2 && whiteboard.AIValidConnection(d.typ, sl.Type) {
					d.target, d.targetSlot = e.ID, sl.Name
					return
				}
			}
			if sl, ok := whiteboard.AIFindInputByType(n, d.typ); ok {
				d.target, d.targetSlot = e.ID, sl.Name
			}
			return
		}
		typ := b.Scene.AISourceType(e)
		if typ == "" || e.ID == d.to {
			continue
		}
		d.over = true
		if whiteboard.AIValidConnection(typ, d.typ) && !b.Scene.AICreatesCycle(e.ID, d.to) {
			d.target = e.ID
		}
		return
	}
}

// aiDrop ends a drag: connects where the link would land, disconnects a
// picked-up link dropped on nothing, or offers new nodes on empty canvas.
// It reports whether the scene changed.
func (b *Board) aiDrop(o BoardOptions) bool {
	d := b.ai.drag
	b.ai.drag = nil
	if d == nil || !d.live {
		return false
	}
	switch {
	case d.target != "" && d.from != "":
		b.setAILink(d.target, d.targetSlot, d.from)
		return true
	case d.target != "":
		b.setAILink(d.to, d.slot, d.target)
		return true
	case d.detached || d.over:
		return d.detached
	}
	b.ai.picker = &aiPicker{at: d.at, from: d.from, to: d.to, slot: d.slot, typ: d.typ}
	return false
}

// setAILink links from into a node's slot ("" disconnects it) and grows
// the card to show its slots. The caller records the undo step.
func (b *Board) setAILink(nodeID, slot, from string) {
	e := b.Scene.ElementByID(nodeID)
	n := b.aiNode(e)
	if n == nil {
		return
	}
	next := *n
	if from == "" {
		next.Disconnect(slot)
	} else {
		next.Connect(slot, from)
	}
	if h := aiMinHeight(&next); e.H < h {
		e.H = h
	}
	b.err = whiteboard.SetAINode(e, &next)
}

// connectAI links an element into a node's first free slot of its type,
// as a link dropped on the node's body. Board material (content) goes to
// the first free slot that is not the instruction's, so a selection fed
// to a node never replaces what the user wrote; it takes the instruction
// slot only when nothing else fits. It fails when nothing fits or the link
// would make a cycle.
func (b *Board) connectAI(from, to *whiteboard.Element, content bool) error {
	n := b.aiNode(to)
	typ := b.Scene.AISourceType(from)
	if n == nil || typ == "" {
		return errAI(i18n.T("只能连接到 AI 节点的输入"))
	}
	if b.Scene.AICreatesCycle(from.ID, to.ID) {
		return errAI(i18n.T("连接会形成循环"))
	}
	if content {
		for _, sl := range whiteboard.AISlots(n) {
			if !sl.Widget && n.LinkAt(sl.Name) == "" && whiteboard.AIValidConnection(typ, sl.Type) {
				b.setAILink(to.ID, sl.Name, from.ID)
				return nil
			}
		}
	}
	sl, ok := whiteboard.AIFindInputByType(n, typ)
	if !ok {
		return errAI(i18n.T("节点没有可接收此类型的输入"))
	}
	b.setAILink(to.ID, sl.Name, from.ID)
	return nil
}

type errAI string

func (e errAI) Error() string { return string(e) }

// deleteAILink removes a link as one undo step.
func (b *Board) deleteAILink(ref aiLinkRef, o BoardOptions) {
	b.ai.link = nil
	node := b.Scene.ElementByID(ref.node)
	if b.ReadOnly || node == nil || node.Locked {
		return
	}
	b.edit(o, func() { b.setAILink(ref.node, ref.slot, "") })
}

// aiPicker is the node menu a link dropped on empty canvas opens.
type aiPicker struct {
	at                  whiteboard.Point
	from, to, slot, typ string
}

// aiPickerRoles are the node roles that can take the dropped link.
func aiPickerRoles(pk *aiPicker) []whiteboard.AIRole {
	var out []whiteboard.AIRole
	if pk.from != "" {
		for _, r := range []whiteboard.AIRole{whiteboard.AIText, whiteboard.AIImage, whiteboard.AIVideo, whiteboard.AIConcat} {
			if _, ok := whiteboard.AIFindInputByType(&whiteboard.AINode{Role: r}, pk.typ); ok {
				out = append(out, r)
			}
		}
		return out
	}
	for _, r := range []whiteboard.AIRole{whiteboard.AIPrompt, whiteboard.AINote, whiteboard.AIText, whiteboard.AIConcat, whiteboard.AIImage} {
		if t := whiteboard.AIOutputType(r); t != "" && whiteboard.AIValidConnection(t, pk.typ) {
			out = append(out, r)
		}
	}
	return out
}

// pickAINode adds a node of the role where the link was dropped, linked
// to the link's other end, as one undo step.
func (b *Board) pickAINode(role whiteboard.AIRole, o BoardOptions) {
	pk := b.ai.picker
	b.ai.picker = nil
	if pk == nil || b.ReadOnly {
		return
	}
	b.edit(o, func() {
		e := b.newAINode(role, pk.at, o)
		n := b.aiNode(e)
		l := b.aiLayout(e, n)
		if pk.from != "" {
			sl, _ := whiteboard.AIFindInputByType(n, pk.typ)
			for i, s := range l.slots {
				if s.Name == sl.Name {
					e.Y = pk.at[1] - (aiInputPoint(e, l, i)[1] - e.Y)
				}
			}
			e.X = pk.at[0]
			b.setAILink(e.ID, sl.Name, pk.from)
		} else {
			e.X = pk.at[0] - e.W
			e.Y = pk.at[1] - (aiOutputPoint(e, l)[1] - e.Y)
			b.setAILink(pk.to, pk.slot, e.ID)
		}
		whiteboard.Touch(e)
		b.selectIDs([]string{e.ID})
	})
}
