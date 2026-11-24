package formats

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// htmlText is the plain text of an HTML fragment, as Draw.io labels hold.
func htmlText(s string) string {
	if !strings.ContainsAny(s, "<&") {
		return oneLine(s)
	}
	doc, err := html.Parse(strings.NewReader("<body>" + s))
	if err != nil {
		return oneLine(s)
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode:
			if n.DataAtom == atom.Br || n.DataAtom == atom.Div || n.DataAtom == atom.P {
				b.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return oneLine(b.String())
}

// htmlMarkdown turns an HTML document (a chapter, a mail body) into
// Markdown: headings, paragraphs, lists, quotes, code, tables and links.
// Scripts, styles and images are left out.
func htmlMarkdown(w *mdWriter, source string) {
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		w.para(source)
		return
	}
	h := &htmlConv{w: w}
	h.walk(doc, 0)
	h.flush()
}

type htmlConv struct {
	w     *mdWriter
	spans []span
	quote bool
}

func (h *htmlConv) flush() {
	md := spansMarkdown(h.spans)
	h.spans = nil
	if md == "" {
		return
	}
	md = lineStart(md)
	if h.quote {
		md = "> " + strings.ReplaceAll(md, "\\\n", "\\\n> ")
	}
	h.w.block(md)
}

func (h *htmlConv) text(s string, st span) {
	st.text = s
	h.spans = append(h.spans, st)
}

func attrOf(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Script, atom.Style:
				return
			case atom.Br:
				b.WriteString("\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func (h *htmlConv) inline(n *html.Node, st span) {
	switch n.Type {
	case html.TextNode:
		if t := strings.Join(strings.Fields(n.Data), " "); t != "" {
			lead := n.Data != "" && strings.ContainsRune(" \t\n\r", rune(n.Data[0]))
			trail := n.Data != "" && strings.ContainsRune(" \t\n\r", rune(n.Data[len(n.Data)-1]))
			if lead {
				t = " " + t
			}
			if trail {
				t += " "
			}
			h.text(t, st)
		} else if n.Data != "" && len(h.spans) > 0 {
			h.text(" ", st)
		}
		return
	case html.ElementNode:
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Head, atom.Img, atom.Svg, atom.Object, atom.Noscript:
			return
		case atom.Br:
			h.text("\n", st)
			return
		case atom.B, atom.Strong:
			st.bold = true
		case atom.I, atom.Em, atom.Cite:
			st.italic = true
		case atom.S, atom.Strike, atom.Del:
			st.strike = true
		case atom.A:
			if href := attrOf(n, "href"); strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "mailto:") {
				st.link = href
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		h.inline(c, st)
	}
}

func isBlockAtom(a atom.Atom) bool {
	switch a {
	case atom.P, atom.Div, atom.Section, atom.Article, atom.Main, atom.Header, atom.Footer, atom.Aside, atom.Nav,
		atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol, atom.Li, atom.Blockquote,
		atom.Pre, atom.Table, atom.Hr, atom.Body, atom.Html, atom.Figure, atom.Figcaption, atom.Dl, atom.Dt, atom.Dd,
		atom.Center, atom.Form, atom.Fieldset, atom.Address:
		return true
	}
	return false
}

func hasBlockChild(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && isBlockAtom(c.DataAtom) {
			return true
		}
	}
	return false
}

func (h *htmlConv) walk(n *html.Node, listDepth int) {
	if h.w.full() {
		return
	}
	if n.Type == html.ElementNode {
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Head, atom.Noscript, atom.Template:
			return
		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			h.flush()
			h.w.heading(int(n.Data[1]-'0'), nodeText(n))
			return
		case atom.Hr:
			h.flush()
			h.w.block("---")
			return
		case atom.Pre:
			h.flush()
			h.w.code("", nodeText(n))
			return
		case atom.Table:
			h.flush()
			h.w.table(htmlTable(n))
			return
		case atom.Blockquote:
			h.flush()
			was := h.quote
			h.quote = true
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				h.walk(c, listDepth)
			}
			h.flush()
			h.quote = was
			return
		case atom.Ul, atom.Ol:
			h.flush()
			k := 0
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.ElementNode || c.DataAtom != atom.Li {
					continue
				}
				k++
				marker := "- "
				if n.DataAtom == atom.Ol {
					marker = itoa(k) + ". "
				}
				// The item's own text, then its nested lists.
				var own []span
				saved := h.spans
				h.spans = nil
				var nested []*html.Node
				for g := c.FirstChild; g != nil; g = g.NextSibling {
					if g.Type == html.ElementNode && (g.DataAtom == atom.Ul || g.DataAtom == atom.Ol) {
						nested = append(nested, g)
						continue
					}
					h.inline(g, span{})
				}
				own, h.spans = h.spans, saved
				if md := strings.ReplaceAll(spansMarkdown(own), "\\\n", " "); md != "" {
					h.w.listItem(strings.Repeat("  ", min(listDepth, 8)) + marker + md)
				}
				for _, g := range nested {
					inList := h.w.inList
					h.walk(g, listDepth+1)
					h.w.inList = inList || h.w.inList
				}
			}
			return
		}
		if isBlockAtom(n.DataAtom) || n.DataAtom == 0 && n.Data == "" {
			if !hasBlockChild(n) {
				h.flush()
				h.inline(n, span{})
				h.flush()
				return
			}
			h.flush()
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				h.walk(c, listDepth)
			}
			h.flush()
			return
		}
		h.inline(n, span{})
		return
	}
	if n.Type == html.TextNode {
		h.inline(n, span{})
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		h.walk(c, listDepth)
	}
}

func htmlTable(n *html.Node) [][]string {
	var rows [][]string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Tr {
			var row []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.DataAtom == atom.Td || c.DataAtom == atom.Th) {
					row = append(row, oneLine(nodeText(c)))
				}
			}
			rows = append(rows, row)
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Table && len(rows) > 0 {
			return // nested tables flatten into their cell's text
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
	return rows
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
