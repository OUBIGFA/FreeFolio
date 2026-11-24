package editor

import (
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

type boardBlock struct {
	group  string
	source string
	board  *Board
	err    error
}

// SemanticDocumentText exposes board labels rather than serialized scene data
// to search. The editor's positional text buffer still retains original bytes.
func SemanticDocumentText(doc *document.Document) string {
	var out []string
	for _, leaf := range doc.Leaves() {
		b := leaf.Block
		if isWhiteboardBlock(b) {
			if s, err := whiteboard.Decode(b.Text()); err == nil {
				out = append(out, s.PlainText())
			}
			continue
		}
		out = append(out, b.Text())
	}
	return strings.Join(out, "\n")
}
func HasInlineWhiteboards(doc *document.Document) bool {
	for _, leaf := range doc.Leaves() {
		if isWhiteboardBlock(leaf.Block) {
			return true
		}
	}
	return false
}

func isWhiteboardBlock(b *document.Block) bool {
	return b != nil && b.Kind == "code" && (strings.EqualFold(strings.TrimSpace(b.Language), "excalidraw") || strings.EqualFold(strings.TrimSpace(b.Language), "whiteboard"))
}
func isWhiteboardURL(url string) bool {
	low := strings.ToLower(strings.Split(url, "#")[0])
	return strings.HasSuffix(low, ".excalidraw.svg") || strings.HasSuffix(low, ".excalidraw")
}

// isWhiteboardEmbed reports a picture block showing a whiteboard file.
func isWhiteboardEmbed(b *document.Block) bool {
	return b != nil && b.Kind == "image" && isWhiteboardURL(b.URL)
}

// isAtomicVisualBlock reports a block that is one object on the page, not
// text: an inline or embedded whiteboard. Copy, cut and delete take it whole
// (ArcMD 14e92c1).
func isAtomicVisualBlock(b *document.Block) bool {
	return isWhiteboardEmbed(b) || isWhiteboardBlock(b)
}

// isAtomicOrHiddenImage adds pictures whose description is hidden, which
// the reader also sees as one object.
func (e *Editor) isAtomicOrHiddenImage(b *document.Block) bool {
	return isAtomicVisualBlock(b) || (b != nil && b.Kind == "image" && !e.captionVisible(b))
}

// deleteAtomicBlock removes block id as a whole. Backward puts the caret at
// the end of the block before; otherwise the next block takes focus.
func (e *Editor) deleteAtomicBlock(id string, backward bool) {
	if e.ReadOnly || id == "" {
		return
	}
	if e.imageCaptionID == id {
		e.imageCaptionID = ""
	}
	e.clearBlocks()
	e.Engine.RemoveBlock(id, backward)
	e.sync()
	e.ready = false
	e.reveal = true
	e.vertical = false
}

// copySingleBlock writes block b to the clipboard as a one-block fragment.
func (e *Editor) copySingleBlock(b *document.Block, write func(string)) {
	if b == nil {
		return
	}
	doc := document.New()
	doc.Blocks = []*document.Block{b.Clone()}
	e.writeFragment(doc, "", write)
}
func (e *Editor) InsertWhiteboard() {
	if e.ReadOnly {
		return
	}
	if e.createWhiteboard != nil {
		e.createWhiteboard()
		return
	}
	s := whiteboard.NewScene()
	raw, _ := s.Encode()
	b := document.NewBlock("code", raw)
	b.Language = "excalidraw"
	e.Engine.Insert(b)
	e.sync()
	e.openInlineBoard(b.ID)
}
func (e *Editor) boardBlockFor(b *document.Block) *boardBlock {
	if e.boardBlocks == nil {
		e.boardBlocks = map[string]*boardBlock{}
	}
	entry := e.boardBlocks[b.ID]
	if entry != nil && entry.source == b.Text() {
		return entry
	}
	s, err := whiteboard.Decode(b.Text())
	entry = &boardBlock{source: b.Text(), err: err}
	if err == nil {
		entry.board = NewBoard(s)
		entry.board.prepareImages(e.boardHostOptions)
	}
	e.boardBlocks[b.ID] = entry
	return entry
}
func (e *Editor) layoutWhiteboardBlock(b *document.Block, leaf document.Leaf, x, y, w float32, o Options) float32 {
	entry := e.boardBlockFor(b)
	h := float32(330)
	fake := document.NewBlock("paragraph", "")
	fake.ID = b.ID
	m := e.blockMetrics(fake, w, false)
	key := e.blockShapeKey(fake, m.font, false)
	layout := e.shapeBlock(fake, key, w, o.Theme).layout
	e.rows = append(e.rows, row{leafRevision: e.Revision(), leaf: leaf, layout: layout, rect: ui.Rect{X: x, Y: y, W: w, H: h}, x: x, y: y, board: entry})
	return h
}
func (e *Editor) paintWhiteboardBlock(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) {
	dx, dy := e.visualOffset(r)
	box := ui.Rect{X: bounds.X + r.rect.X + dx, Y: bounds.Y + r.rect.Y + dy + 8, W: r.rect.W, H: r.rect.H - 24}
	if r.board.err != nil {
		p.Stroke(box, t.Danger, 6, 1)
		p.Text(box.X+12, box.Y+12, i18n.T("白板无法读取"), 14, t.Danger)
		return
	}
	b := r.board.board
	b.lastRect = box
	b.fit(false)
	b.paint(p, box, t.Dark, false)
	p.Stroke(box, t.Border, 6, 1)
}
func (e *Editor) whiteboardControls(c *ui.Context, o Options) {
	if e.printing {
		return
	}
	for i := range e.visibleRows(0) {
		r := *e.indexedRow(i)
		if r.board == nil {
			continue
		}
		id := r.leaf.Block.ID
		box := ui.Box(c.Key("inline-board:"+id)).Absolute().Left(r.rect.X).Top(r.rect.Y+8).Size(r.rect.W, r.rect.H-24).Focusable().Role(ui.RoleButton).Label(i18n.T("编辑白板")).Cursor(ui.CursorPointer)
		open := func() { e.openInlineBoard(id) }
		if box.DoubleClicked() || box.Focused() && box.Shortcut(0, ui.KeyEnter) {
			open()
		}
		if box.Hovered() || box.Focused() {
			button := e.imageButton(c.Key("inline-board-edit:"+id), i18n.T("编辑白板"), ui.Rect{X: r.rect.X + r.rect.W - 92, Y: r.rect.Y + 16, W: 82, H: 28})
			button.Background(c.Theme().Background)
			if button.Clicked() {
				open()
			}
		}
	}
}
func (e *Editor) openInlineBoard(id string) {
	b := e.blockByID(id)
	if !isWhiteboardBlock(b) {
		return
	}
	entry := e.boardBlockFor(b)
	if entry.err != nil {
		return
	}
	e.UnmarkText()
	e.activeBoard = id
	entry.group = whiteboard.NewID()
	entry.board.RequestFocus = true
}
func (e *Editor) activeWhiteboardView(c *ui.Context, o Options) (ui.Element, bool) {
	if e.activeBoard == "" {
		return ui.Element{}, false
	}
	block := e.blockByID(e.activeBoard)
	if block == nil {
		e.activeBoard = ""
		return ui.Element{}, false
	}
	entry := e.boardBlockFor(block)
	if entry.err != nil {
		return ui.Element{}, false
	}
	bo := o.BoardOptions
	bo.Theme = o.Theme
	bo.ReadOnly = e.ReadOnly
	bo.Done = func() { entry.board.finishText(bo); e.activeBoard = ""; e.RequestFocus = true; e.ready = false }
	bo.OnChange = func() {
		raw, err := entry.board.Snapshot().Encode()
		if err != nil {
			entry.board.err = err
			return
		}
		e.Engine.EditGrouped(entry.group, func() { block.Runs = []document.Run{{Text: raw}} })
		entry.source = raw
		e.sync()
		e.ready = false
	}
	return BoardView(c, entry.board, bo), true
}
