package editor

import (
	"math"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/egoist/mygo/ui"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// An HTML block of the Markdown ("raw") keeps its source as the only truth
// and shows, at rest, what the HTML says, as Obsidian shows it: blocks and
// tables with their alignment, colors, sizes and borders, inline tags and
// the common inline styles. The caret in the block shows the source to
// edit. Nothing runs: scripts and styles are left out, and embedded frames
// and media show as a card naming their source.

// htmlBox is an HTML block laid out at a width: what to paint, relative to
// the block's top left, and how tall it stands.
type htmlBox struct {
	src     string
	width   float32
	base    float32
	dark    bool
	ops     []htmlOp
	h       float32
	loading bool // an image is still loading: lay out again next time
}

type htmlOpKind int

const (
	htmlFill htmlOpKind = iota
	htmlStroke
	htmlText
	htmlImage
)

type htmlOp struct {
	kind   htmlOpKind
	r      ui.Rect
	color  ui.Color
	radius float32
	text   *ui.TextLayout
	image  *ui.Bitmap
}

// htmlStyle is what an element hands down to its content.
type htmlStyle struct {
	size                            float32
	weight                          int
	italic, underline, strike, mono bool
	color, bg                       ui.Color
	hasColor, hasBg                 bool
	link                            string
	align                           ui.Align
	shift                           float32
	preserve                        bool // pre: keep spaces and newlines
	family                          string
	lineHeight                      float32
}

// htmlLayout lays HTML blocks out for the editor.
type htmlLayout struct {
	e       *Editor
	o       Options
	base    ui.Font
	dark    bool
	box     *htmlBox
	spans   []ui.Span
	spanAl  ui.Align
	spanX   float32
	spanW   float32
	y       float32
	pending float32 // margin waiting below the last block
}

// documentParagraph measures the body text HTML is set in.
var documentParagraph = document.Block{Kind: "paragraph"}

// htmlPreview returns the HTML block src laid out at width, kept while
// neither changes.
func (e *Editor) htmlPreview(id, src string, width float32, o Options) *htmlBox {
	base := e.blockMetrics(&documentParagraph, width, false).font
	dark := o.Theme != nil && o.Theme.Dark
	if b, ok := e.htmls[id]; ok && b.src == src && b.width == width && b.base == base.Size && b.dark == dark && !b.loading {
		return b
	}
	l := &htmlLayout{e: e, o: o, base: base, dark: dark, box: &htmlBox{src: src, width: width, base: base.Size, dark: dark}}
	nodes, err := xhtml.ParseFragment(strings.NewReader(src), &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body})
	if err == nil {
		st := htmlStyle{size: base.Size, weight: 400, align: ui.Start, family: base.Family, lineHeight: base.LineHeight}
		for _, n := range nodes {
			l.node(n, st, 0, width)
		}
		l.flush()
	}
	l.box.h = max(l.y, 0)
	if e.htmls == nil {
		e.htmls = map[string]*htmlBox{}
	}
	e.htmls[id] = l.box
	return l.box
}

// watchHTML notes the HTML block the caret is in, which shows its source.
func (e *Editor) watchHTML() {
	e.htmlEditing = ""
	if !e.focused || e.printing || e.ReadOnly {
		return
	}
	if leaf := e.Engine.LeafAt(e.Caret); leaf.Block.Kind == "raw" && e.Anchor >= leaf.Start && e.Anchor <= leaf.End {
		e.htmlEditing = leaf.Block.ID
	}
}

// htmlRow lays out the HTML block of r at rest and returns its height, or
// 0 to show its source while the caret is in it.
func (e *Editor) htmlRow(r *row, o Options) float32 {
	b := r.leaf.Block
	r.html = nil
	if b.ID == e.htmlEditing {
		return 0
	}
	box := e.htmlPreview(b.ID, b.Text(), r.rect.W, o)
	r.html, r.htmlTop = box, 4
	// HTML that shows nothing, as an anchor, keeps a sliver the caret
	// reaches by the keys, as Obsidian shows it.
	return max(box.h+8, 6)
}

// htmlBlocks are the elements that start a block of their own.
var htmlBlocks = map[atom.Atom]bool{
	atom.Div: true, atom.P: true, atom.Center: true, atom.Section: true, atom.Article: true,
	atom.Header: true, atom.Footer: true, atom.Main: true, atom.Aside: true, atom.Nav: true,
	atom.Figure: true, atom.Figcaption: true, atom.Blockquote: true, atom.Pre: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Ul: true, atom.Ol: true, atom.Li: true, atom.Dl: true, atom.Dt: true, atom.Dd: true,
	atom.Table: true, atom.Hr: true, atom.Details: true, atom.Summary: true, atom.Address: true,
	atom.Iframe: true, atom.Video: true, atom.Audio: true, atom.Embed: true, atom.Object: true,
}

// htmlSkipped are never shown.
var htmlSkipped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Head: true, atom.Title: true, atom.Meta: true,
	atom.Link: true, atom.Template: true, atom.Noscript: true,
}

func (l *htmlLayout) node(n *xhtml.Node, st htmlStyle, x, w float32) {
	switch n.Type {
	case xhtml.TextNode:
		l.text(n.Data, st, x, w)
		return
	case xhtml.ElementNode:
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			l.node(c, st, x, w)
		}
		return
	}
	if htmlSkipped[n.DataAtom] {
		return
	}
	st = l.inlineStyle(n, st)
	if !htmlBlocks[n.DataAtom] {
		l.inline(n, st, x, w)
		return
	}
	l.flush()
	l.block(n, st, x, w)
}

// inline adds an inline element's content to the line being written.
func (l *htmlLayout) inline(n *xhtml.Node, st htmlStyle, x, w float32) {
	switch n.DataAtom {
	case atom.Br:
		l.text("\n", htmlStyle{size: st.size, weight: st.weight, family: st.family, lineHeight: st.lineHeight, align: st.align, preserve: true}, x, w)
		return
	case atom.Img:
		l.image(n, st, x, w)
		return
	case atom.Q:
		l.text("“", st, x, w)
		defer l.text("”", st, x, w)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		l.node(c, st, x, w)
	}
}

// text adds text to the line being written, collapsing white space as
// HTML does unless in pre.
func (l *htmlLayout) text(s string, st htmlStyle, x, w float32) {
	if !st.preserve {
		s = collapseHTMLSpace(s)
		if len(l.spans) == 0 || strings.HasSuffix(l.spans[len(l.spans)-1].Text, "\n") {
			s = strings.TrimLeft(s, " ")
		}
		if s == "" {
			return
		}
	}
	if len(l.spans) == 0 {
		l.spanAl, l.spanX, l.spanW = st.align, x, w
	}
	var color ui.Color
	switch {
	case st.hasColor:
		color = st.color
	case st.link != "":
		color = l.o.Theme.Accent
	case st.hasBg:
		color = readableOn(st.bg)
	default:
		color = l.o.Theme.Text
	}
	span := ui.Span{Text: s, Size: st.size, Weight: st.weight, Italic: st.italic, Underline: st.underline, Strikethrough: st.strike, Color: color, BaselineShift: st.shift}
	if st.mono {
		span.Font = "monospace"
		span.Size = st.size * 0.9
		if !st.hasBg {
			span.Background, span.Radius = l.e.page.InlineFill, 3
		}
	}
	if st.hasBg {
		span.Background = st.bg
	}
	l.spans = append(l.spans, span)
}

// flush lays out the line being written as a paragraph.
func (l *htmlLayout) flush() {
	if len(l.spans) == 0 {
		return
	}
	// As in the browser, a line break ending a paragraph opens no line of
	// its own, but a paragraph of line breaks alone keeps each one's line.
	last := &l.spans[len(l.spans)-1]
	last.Text = strings.TrimSuffix(last.Text, "\n")
	if all := spanText(l.spans); all == "" || strings.HasSuffix(all, "\n") {
		last.Text += "\u200b"
	}
	font := l.base
	layout := ui.ShapeRichTextWith(l.spans, font, ui.TextShape{Width: max(1, l.spanW), Align: l.spanAl})
	_, h := layout.Size()
	l.y += l.pending
	l.pending = 0
	l.box.ops = append(l.box.ops, htmlOp{kind: htmlText, r: ui.Rect{X: l.spanX, Y: l.y, W: l.spanW, H: h}, text: layout})
	l.y += h
	l.spans = l.spans[:0]
}

func spanText(spans []ui.Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// margin puts room below the last block, the larger of two meeting
// margins, as CSS collapses them.
func (l *htmlLayout) margin(m float32) { l.pending = max(l.pending, m) }

func (l *htmlLayout) block(n *xhtml.Node, st htmlStyle, x, w float32) {
	em := st.size
	css := parseCSS(attr(n, "style"))
	if a := htmlAlign(attr(n, "align"), css["text-align"]); a >= 0 {
		st.align = ui.Align(a)
	}
	top, bottom := float32(0), float32(0)
	pad := cssBox(css, "padding", em)
	switch n.DataAtom {
	case atom.Center:
		st.align = ui.Center
	case atom.P, atom.Dl, atom.Figure:
		top, bottom = em*0.5, em*0.5
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		st.size = l.base.Size * []float32{2, 1.5, 1.25, 1.1, 1, 0.9}[level-1]
		st.weight = max(st.weight, 600)
		top, bottom = st.size*0.6, st.size*0.3
	case atom.Blockquote:
		top, bottom = em*0.5, em*0.5
		pad[3] = max(pad[3], 14)
	case atom.Pre:
		st.preserve, st.mono = true, true
		top, bottom = em*0.5, em*0.5
		pad = [4]float32{10, 12, 10, 12}
	case atom.Ul, atom.Ol:
		top, bottom = em*0.3, em*0.3
	case atom.Hr:
		l.y += l.pending + em*0.6
		l.pending = 0
		l.box.ops = append(l.box.ops, htmlOp{kind: htmlFill, r: ui.Rect{X: x, Y: l.y, W: w, H: 1}, color: l.e.page.Rule})
		l.y += 1
		l.margin(em * 0.6)
		return
	case atom.Table:
		l.table(n, st, x, w)
		return
	case atom.Iframe, atom.Video, atom.Audio, atom.Embed, atom.Object:
		l.embed(n, st, x, w)
		return
	case atom.Details:
		l.details(n, st, x, w)
		return
	case atom.Dd:
		pad[3] = max(pad[3], 24)
	}
	if m := cssBox(css, "margin", em); m != [4]float32{} {
		top, bottom = m[0], m[2]
	}
	l.margin(top)
	bg, hasBg := cssColor(firstOf(css["background-color"], css["background"], attr(n, "bgcolor")))
	borderC, hasBorder := cssBorder(css["border"], l.o.Theme.Border)
	boxed := hasBg || hasBorder || pad != [4]float32{} || n.DataAtom == atom.Blockquote
	if boxed {
		l.y += l.pending
		l.pending = 0
	}
	start, at := len(l.box.ops), l.y
	if hasBg {
		st.bg, st.hasBg = bg, false // the box shows it, not each word
		if !st.hasColor {
			st.color, st.hasColor = readableOn(bg), true
		}
	}
	l.y += pad[0]
	inner, innerW := x+pad[3], max(1, w-pad[1]-pad[3])
	if n.DataAtom == atom.Ul || n.DataAtom == atom.Ol {
		l.list(n, st, inner, innerW)
	} else {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			l.node(c, st, inner, innerW)
		}
		l.flush()
	}
	l.y += pad[2]
	if boxed {
		r := ui.Rect{X: x, Y: at, W: w, H: l.y - at}
		radius := cssLength(css["border-radius"], em, w)
		var deco []htmlOp
		if hasBg {
			deco = append(deco, htmlOp{kind: htmlFill, r: r, color: bg, radius: radius})
		}
		if hasBorder {
			deco = append(deco, htmlOp{kind: htmlStroke, r: r, color: borderC, radius: radius})
		}
		if n.DataAtom == atom.Blockquote && !hasBorder {
			deco = append(deco, htmlOp{kind: htmlFill, r: ui.Rect{X: x, Y: at, W: 3, H: r.H}, color: l.e.page.QuoteBar, radius: 1.5})
		}
		l.box.ops = append(l.box.ops[:start], append(deco, l.box.ops[start:]...)...)
	}
	l.margin(bottom)
}

// list lays out the items of ul or ol, each with its bullet or number.
func (l *htmlLayout) list(n *xhtml.Node, st htmlStyle, x, w float32) {
	number := 1
	if s, err := strconv.Atoi(attr(n, "start")); err == nil {
		number = s
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode || c.DataAtom != atom.Li {
			l.node(c, st, x+24, w-24)
			continue
		}
		mark := "•"
		if n.DataAtom == atom.Ol {
			mark = strconv.Itoa(number) + "."
			number++
		}
		l.flush()
		l.y += l.pending
		l.pending = 0
		markLayout := ui.ShapeRichText([]ui.Span{{Text: mark, Size: st.size, Color: l.e.page.Marker}}, l.base, 0)
		mw, _ := markLayout.Size()
		l.box.ops = append(l.box.ops, htmlOp{kind: htmlText, r: ui.Rect{X: x + 18 - mw, Y: l.y, W: mw}, text: markLayout})
		item := l.inlineStyle(c, st)
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			l.node(k, item, x+24, max(1, w-24))
		}
		l.flush()
		l.margin(st.size * 0.2)
	}
}

// details shows its summary with a disclosure mark, and its content only
// when it is open, as the browser shows it at rest.
func (l *htmlLayout) details(n *xhtml.Node, st htmlStyle, x, w float32) {
	_, open := attrOK(n, "open")
	mark := "▸ "
	if open {
		mark = "▾ "
	}
	l.margin(st.size * 0.3)
	l.text(mark, st, x, w)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.DataAtom == atom.Summary {
			for k := c.FirstChild; k != nil; k = k.NextSibling {
				l.node(k, l.inlineStyle(c, st), x, w)
			}
		}
	}
	l.flush()
	if open {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode || c.DataAtom != atom.Summary {
				l.node(c, st, x+16, max(1, w-16))
			}
		}
		l.flush()
	}
	l.margin(st.size * 0.3)
}

// embed shows a frame or media element as a card naming its source.
func (l *htmlLayout) embed(n *xhtml.Node, st htmlStyle, x, w float32) {
	src := firstOf(attr(n, "src"), attr(n, "data"))
	if src == "" {
		for c := n.FirstChild; c != nil && src == ""; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && c.DataAtom == atom.Source {
				src = attr(c, "src")
			}
		}
	}
	l.y += l.pending
	l.pending = 0
	label := "<" + n.Data + ">"
	if src != "" {
		label += " " + src
	}
	layout := ui.ShapeRichTextWith([]ui.Span{{Text: label, Size: st.size * 0.875, Color: l.e.page.Caption}}, l.base, ui.TextShape{Width: max(1, w-24)})
	_, h := layout.Size()
	r := ui.Rect{X: x, Y: l.y, W: w, H: h + 20}
	l.box.ops = append(l.box.ops,
		htmlOp{kind: htmlFill, r: r, color: l.e.page.CodeFill, radius: 8},
		htmlOp{kind: htmlStroke, r: r, color: l.e.page.CodeBorder, radius: 8},
		htmlOp{kind: htmlText, r: ui.Rect{X: x + 12, Y: l.y + 10, W: w - 24, H: h}, text: layout})
	l.y += r.H
	l.margin(st.size * 0.5)
}

// image shows an img on a line of its own at its width attribute, or as
// large as it is within the block.
func (l *htmlLayout) image(n *xhtml.Node, st htmlStyle, x, w float32) {
	src := attr(n, "src")
	if src == "" {
		return
	}
	res := l.e.imageResource(src, l.o)
	if res.Loading {
		l.box.loading = true
	}
	css := parseCSS(attr(n, "style"))
	iw := cssLength(firstOf(css["width"], attr(n, "width")), st.size, w)
	ih := cssLength(firstOf(css["height"], attr(n, "height")), st.size, w)
	if res.Bitmap == nil {
		alt := attr(n, "alt")
		if alt == "" {
			alt = imageName(src)
		}
		l.text("🖼 "+alt, htmlStyle{size: st.size * 0.875, weight: 400, family: st.family, lineHeight: st.lineHeight, align: st.align, color: l.e.page.Caption, hasColor: true}, x, w)
		return
	}
	nw, nh := float32(res.Width), float32(res.Height)
	switch {
	case iw > 0 && ih > 0:
	case iw > 0:
		ih = iw * nh / max(1, nw)
	case ih > 0:
		iw = ih * nw / max(1, nh)
	default:
		iw, ih = nw, nh
	}
	if iw > w {
		ih, iw = ih*w/iw, w
	}
	l.flush()
	l.y += l.pending
	l.pending = 0
	ix := x
	switch st.align {
	case ui.Center:
		ix = x + (w-iw)/2
	case ui.End:
		ix = x + w - iw
	}
	l.box.ops = append(l.box.ops, htmlOp{kind: htmlImage, r: ui.Rect{X: ix, Y: l.y, W: iw, H: ih}, image: res.Bitmap, radius: 4})
	l.y += ih
}

// table lays out a table as the browser sizes one without widths: each
// column as wide as its widest cell, the table as wide as its columns,
// narrowed to the block, then placed by the alignment around it.
func (l *htmlLayout) table(n *xhtml.Node, st htmlStyle, x, w float32) {
	type cell struct {
		n       *xhtml.Node
		st      htmlStyle
		head    bool
		bg      ui.Color
		hasBg   bool
		spans   []ui.Span
		natural float32
	}
	var rows [][]*cell
	var collect func(*xhtml.Node)
	collect = func(p *xhtml.Node) {
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Thead, atom.Tbody, atom.Tfoot:
				collect(c)
			case atom.Tr:
				var row []*cell
				rowBg, rowHasBg := cssColor(firstOf(parseCSS(attr(c, "style"))["background-color"], attr(c, "bgcolor")))
				for d := c.FirstChild; d != nil; d = d.NextSibling {
					if d.Type != xhtml.ElementNode || d.DataAtom != atom.Td && d.DataAtom != atom.Th {
						continue
					}
					cs := l.inlineStyle(d, st)
					head := d.DataAtom == atom.Th
					if head {
						cs.weight = max(cs.weight, 600)
					}
					css := parseCSS(attr(d, "style"))
					if a := htmlAlign(attr(d, "align"), css["text-align"]); a >= 0 {
						cs.align = ui.Align(a)
					} else if head {
						cs.align = ui.Center
					} else {
						cs.align = ui.Start
					}
					bg, hasBg := cssColor(firstOf(css["background-color"], css["background"], attr(d, "bgcolor")))
					if !hasBg && rowHasBg {
						bg, hasBg = rowBg, true
					}
					if hasBg {
						cs.hasBg = false
						if !cs.hasColor {
							cs.color, cs.hasColor = readableOn(bg), true
						}
					}
					row = append(row, &cell{n: d, st: cs, head: head, bg: bg, hasBg: hasBg})
				}
				rows = append(rows, row)
			}
		}
	}
	collect(n)
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return
	}
	const padX, padY = 12, 6
	// Each cell's content as one paragraph, measured unwrapped.
	natural := make([]float32, cols)
	for _, r := range rows {
		for i, c := range r {
			sub := &htmlLayout{e: l.e, o: l.o, base: l.base, dark: l.dark, box: &htmlBox{}}
			for k := c.n.FirstChild; k != nil; k = k.NextSibling {
				sub.node(k, c.st, 0, 1e6)
			}
			c.spans = append([]ui.Span(nil), sub.spans...)
			if len(c.spans) > 0 {
				cw, _ := ui.ShapeRichText(c.spans, l.base, 0).Size()
				c.natural = cw
			}
			natural[i] = max(natural[i], c.natural+2*padX)
		}
	}
	total := float32(0)
	for _, v := range natural {
		total += v
	}
	widths := natural
	if total > w {
		widths = make([]float32, cols)
		for i, v := range natural {
			widths[i] = v * w / total
		}
		total = w
	}
	tx := x
	switch {
	case htmlAlign(attr(n, "align"), "") == int(ui.Center), st.align == ui.Center:
		tx = x + (w-total)/2
	case htmlAlign(attr(n, "align"), "") == int(ui.End), st.align == ui.End:
		tx = x + w - total
	}
	l.y += l.pending + st.size*0.5
	l.pending = 0
	border := l.o.Theme.Border
	for _, r := range rows {
		rowH := float32(0)
		layouts := make([]*ui.TextLayout, len(r))
		for i, c := range r {
			if len(c.spans) == 0 {
				continue
			}
			layouts[i] = ui.ShapeRichTextWith(c.spans, l.base, ui.TextShape{Width: max(1, widths[i]-2*padX), Align: c.st.align})
			_, h := layouts[i].Size()
			rowH = max(rowH, h)
		}
		rowH = max(rowH, l.base.Size*l.base.LineHeight) + 2*padY
		cx := tx
		for i := range cols {
			cr := ui.Rect{X: cx, Y: l.y, W: widths[i], H: rowH}
			if i < len(r) {
				c := r[i]
				switch {
				case c.hasBg:
					l.box.ops = append(l.box.ops, htmlOp{kind: htmlFill, r: cr, color: c.bg})
				case c.head:
					l.box.ops = append(l.box.ops, htmlOp{kind: htmlFill, r: cr, color: l.e.page.TableHeader})
				}
				if layouts[i] != nil {
					_, h := layouts[i].Size()
					l.box.ops = append(l.box.ops, htmlOp{kind: htmlText, r: ui.Rect{X: cx + padX, Y: l.y + (rowH-h)/2, W: widths[i] - 2*padX, H: h}, text: layouts[i]})
				}
			}
			l.box.ops = append(l.box.ops, htmlOp{kind: htmlStroke, r: cr, color: border})
			cx += widths[i]
		}
		l.y += rowH
	}
	l.margin(st.size * 0.5)
}

// inlineStyle is st changed by the element n: its tag and its style.
func (l *htmlLayout) inlineStyle(n *xhtml.Node, st htmlStyle) htmlStyle {
	switch n.DataAtom {
	case atom.B, atom.Strong:
		st.weight = max(st.weight, 600)
	case atom.I, atom.Em, atom.Cite, atom.Var, atom.Dfn:
		st.italic = true
	case atom.U, atom.Ins:
		st.underline = true
	case atom.S, atom.Strike, atom.Del:
		st.strike = true
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		st.mono = true
	case atom.Mark:
		if !st.hasBg {
			st.bg, st.hasBg = ui.Hex("#fff3a3"), true
			if !st.hasColor {
				st.color, st.hasColor = ui.Hex("#1f2329"), true
			}
		}
	case atom.Small:
		st.size *= 0.85
	case atom.Big:
		st.size *= 1.2
	case atom.Sub:
		st.size *= 0.8
		st.shift -= st.size * 0.25
	case atom.Sup:
		st.size *= 0.8
		st.shift += st.size * 0.45
	case atom.A:
		if href := attr(n, "href"); href != "" {
			st.link = href
		}
	case atom.Font:
		if c, ok := cssColor(attr(n, "color")); ok {
			st.color, st.hasColor = c, true
		}
		if s := attr(n, "size"); s != "" {
			st.size = fontTagSize(s, l.base.Size)
		}
	}
	css := parseCSS(attr(n, "style"))
	if c, ok := cssColor(css["color"]); ok {
		st.color, st.hasColor = c, true
	}
	if !htmlBlocks[n.DataAtom] {
		if c, ok := cssColor(firstOf(css["background-color"], css["background"])); ok {
			st.bg, st.hasBg = c, true
			if !st.hasColor {
				st.color, st.hasColor = readableOn(c), true
			}
		}
	}
	if v := css["font-size"]; v != "" {
		if s := cssFontSize(v, st.size, l.base.Size); s > 0 {
			st.size = s
		}
	}
	switch v := css["font-weight"]; v {
	case "bold", "bolder":
		st.weight = 600
	case "normal", "lighter":
		st.weight = 400
	default:
		if n, err := strconv.Atoi(v); err == nil {
			st.weight = min(max(n, 100), 900)
		}
	}
	switch css["font-style"] {
	case "italic", "oblique":
		st.italic = true
	case "normal":
		st.italic = false
	}
	deco := firstOf(css["text-decoration-line"], css["text-decoration"])
	if strings.Contains(deco, "underline") {
		st.underline = true
	}
	if strings.Contains(deco, "line-through") {
		st.strike = true
	}
	if strings.Contains(css["font-family"], "mono") {
		st.mono = true
	}
	return st
}

// paintHTML draws a laid out HTML block at (x, y).
func (e *Editor) paintHTML(p *ui.Painter, b *htmlBox, x, y float32, t *ui.Theme) {
	for _, op := range b.ops {
		r := op.r
		r.X += x
		r.Y += y
		switch op.kind {
		case htmlFill:
			p.Fill(r, op.color, op.radius)
		case htmlStroke:
			p.Stroke(r, op.color, op.radius, 1)
		case htmlText:
			p.TextLayout(op.text, r.X, r.Y, t.Text)
		case htmlImage:
			p.Clip(r, op.radius, func() { p.Image(op.image, r, ui.Contain) })
		}
	}
}

func attrOK(n *xhtml.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return strings.TrimSpace(a.Val), true
		}
	}
	return "", false
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// collapseHTMLSpace turns runs of white space into one space.
func collapseHTMLSpace(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// parseCSS reads a style attribute into its properties, lowercased.
func parseCSS(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := map[string]string{}
	for _, decl := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
		out[strings.ToLower(strings.TrimSpace(k))] = strings.ToLower(v)
	}
	return out
}

// htmlAlign reads an align attribute or a text-align value as a ui.Align,
// or -1 for none.
func htmlAlign(attrValue, css string) int {
	switch strings.ToLower(firstOf(css, attrValue)) {
	case "center", "middle":
		return int(ui.Center)
	case "right", "end":
		return int(ui.End)
	case "left", "start", "justify":
		return int(ui.Start)
	}
	return -1
}

// cssLength reads a length in DIPs: px, em, rem, pt or % of whole; a bare
// number is pixels, as HTML's width attribute.
func cssLength(v string, em, whole float32) float32 {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" || v == "auto" {
		return 0
	}
	unit := ""
	for _, u := range []string{"px", "rem", "em", "pt", "%"} {
		if strings.HasSuffix(v, u) {
			unit, v = u, strings.TrimSuffix(v, u)
			break
		}
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 32)
	if err != nil || f < 0 {
		return 0
	}
	n := float32(f)
	switch unit {
	case "em", "rem":
		return n * em
	case "pt":
		return n * 4 / 3
	case "%":
		return n * whole / 100
	}
	return n
}

// cssBox reads padding or margin, in the one-to-four value form or its
// -top, -right, -bottom and -left properties: top, right, bottom, left.
func cssBox(css map[string]string, name string, em float32) [4]float32 {
	var out [4]float32
	if v := css[name]; v != "" {
		parts := strings.Fields(v)
		vals := make([]float32, len(parts))
		for i, p := range parts {
			vals[i] = cssLength(p, em, 0)
		}
		switch len(vals) {
		case 1:
			out = [4]float32{vals[0], vals[0], vals[0], vals[0]}
		case 2:
			out = [4]float32{vals[0], vals[1], vals[0], vals[1]}
		case 3:
			out = [4]float32{vals[0], vals[1], vals[2], vals[1]}
		case 4:
			out = [4]float32{vals[0], vals[1], vals[2], vals[3]}
		}
	}
	for i, side := range []string{"top", "right", "bottom", "left"} {
		if v := css[name+"-"+side]; v != "" {
			out[i] = cssLength(v, em, 0)
		}
	}
	return out
}

// cssBorder reads a border shorthand: its color, and whether it draws.
// Borders draw a hairline whatever their width, as the page's own do.
func cssBorder(v string, fallback ui.Color) (ui.Color, bool) {
	if v == "" || v == "none" || v == "0" {
		return ui.Color{}, false
	}
	color := fallback
	for _, part := range strings.Fields(v) {
		if part == "none" || part == "hidden" {
			return ui.Color{}, false
		}
		if c, ok := cssColor(part); ok && cssLength(part, 16, 0) == 0 {
			color = c
		}
	}
	return color, true
}

// fontTagSize is the size of <font size>: 1 to 7, or relative to 3, as
// browsers map them.
func fontTagSize(s string, base float32) float32 {
	scale := []float32{0.625, 0.8125, 1, 1.125, 1.5, 2, 3}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "+"))
	if err != nil {
		return base
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		n += 3
	}
	return base * scale[min(max(n, 1), 7)-1]
}

// cssFontSize reads font-size against the parent's size and the body's.
func cssFontSize(v string, parent, body float32) float32 {
	keywords := map[string]float32{"xx-small": 0.5625, "x-small": 0.625, "small": 0.8125, "medium": 1, "large": 1.125, "x-large": 1.5, "xx-large": 2, "xxx-large": 3}
	if k, ok := keywords[v]; ok {
		return body * k
	}
	switch v {
	case "smaller":
		return parent * 0.83
	case "larger":
		return parent * 1.2
	}
	if strings.HasSuffix(v, "%") {
		return cssLength(v, parent, parent)
	}
	return cssLength(v, parent, parent)
}

// cssColor reads a CSS or HTML color: a hex value, rgb() or rgba(), or a
// common name.
func cssColor(v string) (ui.Color, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || v == "transparent" || v == "inherit" || v == "initial" || v == "currentcolor" {
		return ui.Color{}, false
	}
	if hex, ok := cssNamedColors[v]; ok {
		v = hex
	}
	if strings.HasPrefix(v, "rgb") {
		open, close := strings.IndexByte(v, '('), strings.IndexByte(v, ')')
		if open < 0 || close < open {
			return ui.Color{}, false
		}
		parts := strings.FieldsFunc(v[open+1:close], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return ui.Color{}, false
		}
		var ch [4]float64
		ch[3] = 1
		for i := range min(len(parts), 4) {
			p := parts[i]
			f, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
			if err != nil {
				return ui.Color{}, false
			}
			if strings.HasSuffix(p, "%") {
				f = f / 100
				if i < 3 {
					f *= 255
				}
			}
			ch[i] = f
		}
		c := ui.RGB(uint8(clamp255(ch[0])), uint8(clamp255(ch[1])), uint8(clamp255(ch[2])))
		return c.Alpha(float32(math.Min(math.Max(ch[3], 0), 1))), true
	}
	if !strings.HasPrefix(v, "#") {
		v = "#" + v // HTML attributes often leave the # out
	}
	hex := v[1:]
	switch len(hex) {
	case 3, 4:
		var b strings.Builder
		for _, r := range hex {
			b.WriteRune(r)
			b.WriteRune(r)
		}
		hex = b.String()
	case 6, 8:
	default:
		return ui.Color{}, false
	}
	if _, err := strconv.ParseUint(hex, 16, 32); err != nil {
		return ui.Color{}, false
	}
	c := ui.Hex("#" + hex[:6])
	if len(hex) == 8 {
		a, _ := strconv.ParseUint(hex[6:], 16, 8)
		c = c.Alpha(float32(a) / 255)
	}
	return c, true
}

func clamp255(f float64) float64 { return math.Min(math.Max(math.Round(f), 0), 255) }

// readableOn is dark or light text, whichever reads on bg, for text an
// author put on a fill without a color of its own: the page's own text
// color may not read on it in the other appearance.
func readableOn(bg ui.Color) ui.Color {
	lum := 0.2126*float64(bg.R) + 0.7152*float64(bg.G) + 0.0722*float64(bg.B)
	if bg.A < 128 || lum > 140 {
		return ui.Hex("#1f2329")
	}
	return ui.Hex("#f5f6f7")
}

var cssNamedColors = map[string]string{
	"black": "#000000", "white": "#ffffff", "red": "#ff0000", "green": "#008000", "blue": "#0000ff",
	"yellow": "#ffff00", "orange": "#ffa500", "purple": "#800080", "pink": "#ffc0cb", "brown": "#a52a2a",
	"gray": "#808080", "grey": "#808080", "silver": "#c0c0c0", "maroon": "#800000", "olive": "#808000",
	"lime": "#00ff00", "aqua": "#00ffff", "cyan": "#00ffff", "teal": "#008080", "navy": "#000080",
	"fuchsia": "#ff00ff", "magenta": "#ff00ff", "gold": "#ffd700", "indigo": "#4b0082", "violet": "#ee82ee",
	"crimson": "#dc143c", "coral": "#ff7f50", "salmon": "#fa8072", "tomato": "#ff6347", "orchid": "#da70d6",
	"skyblue": "#87ceeb", "steelblue": "#4682b4", "royalblue": "#4169e1", "dodgerblue": "#1e90ff",
	"lightblue": "#add8e6", "lightgreen": "#90ee90", "lightgray": "#d3d3d3", "lightgrey": "#d3d3d3",
	"darkgray": "#a9a9a9", "darkgrey": "#a9a9a9", "darkred": "#8b0000", "darkgreen": "#006400",
	"darkblue": "#00008b", "darkorange": "#ff8c00", "beige": "#f5f5dc", "ivory": "#fffff0",
	"khaki": "#f0e68c", "lavender": "#e6e6fa", "tan": "#d2b48c", "turquoise": "#40e0d0",
	"whitesmoke": "#f5f5f5", "gainsboro": "#dcdcdc", "slategray": "#708090", "seagreen": "#2e8b57",
	"forestgreen": "#228b22", "firebrick": "#b22222", "chocolate": "#d2691e", "hotpink": "#ff69b4",
	"deeppink": "#ff1493", "aliceblue": "#f0f8ff", "honeydew": "#f0fff0", "mintcream": "#f5fffa",
	"azure": "#f0ffff", "linen": "#faf0e6", "wheat": "#f5deb3", "plum": "#dda0dd", "rebeccapurple": "#663399",
}
