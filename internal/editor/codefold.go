package editor

import (
	"fmt"
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// A code block of more than codeFoldLines lines shows only its first
// codeFoldLines until the reader opens it; the fold is the editing
// session's and never changes the Markdown. Its toggle sits at the foot of
// the block, or at the foot of the view while the block runs past it, so
// it stays at hand for the block being read and no other.
const codeFoldLines = 15

// codeLines is how many lines the code block b holds, counting no further
// than limit.
func codeLines(b *document.Block, limit int) int {
	n := 1
	for _, r := range b.Runs {
		n += strings.Count(r.Text, "\n")
		if n > limit {
			return n
		}
	}
	return n
}

// codeFoldable reports whether code block b is long enough to fold.
// Printing and Mermaid diagrams show whole.
func (e *Editor) codeFoldable(b *document.Block) bool {
	return b != nil && b.Kind == "code" && !e.printing && !isMermaidBlock(b) && codeLines(b, codeFoldLines) > codeFoldLines
}

// codeFolded reports whether code block b shows only its first lines.
func (e *Editor) codeFolded(b *document.Block) bool {
	return e.codeFoldable(b) && !e.codeOpen[b.ID]
}

// foldedCodeHeight is the height a folded code block's text keeps.
func foldedCodeHeight(font ui.Font) float32 {
	return codeFoldLines * font.Size * font.LineHeight
}

// ToggleCodeFold opens a folded code block or folds an open one. Folding
// with the caret in the part going out of sight brings the caret to the
// block's start, which would otherwise open it again at once.
func (e *Editor) ToggleCodeFold(id string) {
	b := e.blockByID(id)
	if !e.codeFoldable(b) {
		return
	}
	if e.codeOpen[id] {
		delete(e.codeOpen, id)
		if leaf := e.Engine.LeafAt(e.Caret); leaf.Block.ID == id {
			e.SetSelection(leaf.Start, leaf.Start)
		}
	} else {
		if e.codeOpen == nil {
			e.codeOpen = map[string]bool{}
		}
		e.codeOpen[id] = true
	}
	e.ready = false
}

// openCodeAtCaret opens the folded code block b when the caret, at local
// in its layout, is in the part out of sight, as while typing past the
// last line shown or stepping down into it.
func (e *Editor) openCodeAtCaret(b *document.Block, laid *textLayout, local int, font ui.Font) {
	if laid == nil || !e.codeFolded(b) {
		return
	}
	if c := laid.Caret(local); c.Y+c.H > foldedCodeHeight(font)+0.5 {
		if e.codeOpen == nil {
			e.codeOpen = map[string]bool{}
		}
		e.codeOpen[b.ID] = true
	}
}

// openFoldAtCaret opens the folded code block holding the caret when the
// caret moved, without an edit, into its part out of sight, as by the
// arrow keys or find; the layout then runs again.
func (e *Editor) openFoldAtCaret() {
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block == nil || leaf.Block.Kind != "code" {
		return
	}
	idx, ok := e.rowIDs[leaf.Block.ID]
	if !ok {
		return
	}
	r := e.indexedRow(idx)
	if r.layout == nil || r.foldH == 0 {
		return
	}
	if c := r.layout.Caret(e.Caret - leaf.Start); c.Y+c.H > r.foldH+0.5 {
		if e.codeOpen == nil {
			e.codeOpen = map[string]bool{}
		}
		e.codeOpen[leaf.Block.ID] = true
		e.ready = false
	}
}

// codeFoldButton is where a long code block shows its toggle.
func (e *Editor) codeFoldButton(d decoration) ui.Rect {
	w, h := float32(96), float32(24)
	bottom := d.rect.Y + d.rect.H - 2
	if e.viewport > 0 {
		bottom = min(bottom, e.Scroll.Y+e.viewport-12)
	}
	bottom = max(bottom, d.rect.Y+48+h)
	return ui.Rect{X: d.rect.X + (d.rect.W-w)/2, Y: bottom - h, W: w, H: h}
}

// foldClip is the box a folded code row's text and marks paint within.
func foldClip(r row) ui.Rect {
	return ui.Rect{X: r.rect.X, Y: r.rect.Y, W: r.rect.W, H: max(0, r.y-r.rect.Y+r.foldH)}
}

// paintCodeFolds draws, over the text, the fade at the foot of each folded
// code block in view and the toggle of every foldable one.
func (e *Editor) paintCodeFolds(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	for i := range e.visibleDecorations(1) {
		d := *e.indexedDecoration(i)
		if d.kind != "code" || !d.foldable {
			continue
		}
		ddx, ddy := e.decorationOffset(d)
		off := func(r ui.Rect) ui.Rect { r.X += bounds.X + ddx; r.Y += bounds.Y + ddy; return r }
		pg := e.page
		if d.folded {
			if idx, ok := e.rowIDs[d.id]; ok {
				r := *e.indexedRow(idx)
				fade := ui.Rect{X: d.rect.X + 1, Y: r.y + r.foldH - 44, W: d.rect.W - 2, H: 44}
				p.FillGradient(off(fade), ui.LinearGradient{From: pg.CodeFill.Alpha(0), To: pg.CodeFill, Angle: 180}, 0)
			}
		}
		b := e.codeFoldButton(d)
		hover := e.hovered(b, 0)
		r := off(b)
		fill := pg.Popover
		if hover {
			fill = pg.Hover
		}
		p.Fill(r, fill, r.H/2)
		p.Stroke(r, pg.CodeBorder, r.H/2, 1)
		icon, label := iconChevron, fmt.Sprintf(i18n.T("展开 %d 行"), d.lines)
		if !d.folded {
			icon, label = iconChevronUp, i18n.T("收起")
		}
		fg := pg.Caption
		if hover {
			fg = t.Text
		}
		span := ui.Span{Text: label, Size: 12, Color: fg}
		tw, th := p.MeasureText(0, span)
		x := r.X + (r.W-tw-18)/2
		p.Icon(icon, ui.Rect{X: x, Y: r.Y + (r.H-14)/2, W: 14, H: 14}, fg)
		p.RichText(x+18, r.Y+(r.H-th)/2, 0, span)
	}
}
