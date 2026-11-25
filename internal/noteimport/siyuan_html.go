package noteimport

import (
	"encoding/base64"
	"html"
	"regexp"
	"strconv"
	"strings"
)

var syBlockID = regexp.MustCompile(`\b[0-9]{14}-[a-z0-9]{7}\b`)

func headingSection(nodes []syNode, id string) []syNode {
	for i, n := range nodes {
		if n.s("ID") == id {
			end := i + 1
			for end < len(nodes) {
				if nodes[end].s("Type") == "NodeHeading" && nodes[end].number("HeadingLevel") <= n.number("HeadingLevel") {
					break
				}
				end++
			}
			return nodes[i:end]
		}
		if found := headingSection(n.kids(), id); len(found) > 0 {
			return found
		}
	}
	return nil
}

func (c *syConverter) staticEmbed(query string, depth int) (string, bool) {
	q := strings.ToLower(query)
	ids := syBlockID.FindAllString(query, -1)
	if c.depth >= 3 || len(ids) == 0 || !strings.Contains(q, "id") || strings.Contains(q, " like ") || (!strings.Contains(q, "=") && !strings.Contains(q, " in ")) {
		return "", false
	}
	for _, id := range ids {
		if _, ok := c.index[id]; !ok {
			return "", false
		}
	}
	var out strings.Builder
	c.depth++
	defer func() { c.depth-- }()
	for _, id := range ids {
		target := c.index[id]
		at := c.at
		c.at = target.page
		if target.node.s("Type") == "NodeDocument" {
			out.WriteString(c.blocks(target.node.kids(), depth+1))
		} else if target.node.s("Type") == "NodeHeading" {
			out.WriteString(c.blocks(headingSection(c.pages[target.page].node.kids(), id), depth+1))
		} else {
			out.WriteString(c.blocks([]syNode{target.node}, depth+1))
		}
		c.at = at
	}
	return out.String(), true
}

// HTML is used only for Markdown's existing rich-table and column extensions.
// It carries semantic tags instead of embedding editor state or a second format.
func (c *syConverter) inlineHTML(nodes []syNode) string {
	var out strings.Builder
	for _, n := range nodes {
		var s string
		switch n.s("Type") {
		case "NodeText", "NodeLinkText":
			s = html.EscapeString(n.s("Data"))
		case "NodeTextMark":
			s = html.EscapeString(n.s("TextMarkTextContent"))
			for _, mark := range strings.Fields(n.s("TextMarkType")) {
				tag := ""
				switch mark {
				case "strong", "em", "u", "s", "code", "kbd", "sup", "sub", "mark":
					tag = mark
				case "a":
					s = `<a href="` + html.EscapeString(c.link(n.s("TextMarkAHref"))) + `">` + s + `</a>`
				case "tag":
					s = "#" + s
				case "inline-math":
					s = "$" + html.EscapeString(n.s("TextMarkInlineMathContent")) + "$"
				case "inline-memo":
					s += "（" + html.EscapeString(n.s("TextMarkInlineMemoContent")) + "）"
				case "block-ref":
					id := n.s("TextMarkBlockRefID")
					if t, ok := c.index[id]; ok {
						s = `<a href="` + html.EscapeString(relative(c.pages[c.output].target, c.pages[t.page].target)) + `">` + s + `</a>`
					} else {
						c.issue("broken-reference", id)
					}
				default:
					if mark != "text" {
						c.issue("converted", mark)
					}
				}
				if tag != "" {
					s = "<" + tag + ">" + s + "</" + tag + ">"
				}
			}
		case "NodeBr", "NodeHardBreak":
			s = "<br>"
		case "NodeImage":
			s = `<img src="` + html.EscapeString(c.asset(n.child("NodeLinkDest"))) + `" alt="` + html.EscapeString(n.child("NodeLinkText")) + `"` + styleAttr(n) + `>`
		case "NodeLink":
			s = `<a href="` + html.EscapeString(c.link(n.child("NodeLinkDest"))) + `">` + html.EscapeString(n.child("NodeLinkText")) + `</a>`
		case "NodeStrong", "NodeEmphasis", "NodeUnderline", "NodeStrikethrough", "NodeMark":
			tag := map[string]string{"NodeStrong": "strong", "NodeEmphasis": "em", "NodeUnderline": "u", "NodeStrikethrough": "s", "NodeMark": "mark"}[n.s("Type")]
			s = "<" + tag + ">" + c.inlineHTML(n.kids()) + "</" + tag + ">"
		case "NodeCodeSpan":
			s = "<code>" + html.EscapeString(n.child("NodeCodeSpanContent")) + "</code>"
		default:
			if strings.HasSuffix(n.s("Type"), "Marker") {
				continue
			}
			if len(n.kids()) > 0 {
				s = c.inlineHTML(n.kids())
			} else {
				s = html.EscapeString(n.plain())
			}
		}
		if n.s("Type") == "NodeTextMark" && styleAttr(n) != "" {
			s = "<span" + styleAttr(n) + ">" + s + "</span>"
		}
		out.WriteString(s)
	}
	return out.String()
}

func (c *syConverter) blockHTML(n syNode, depth int) string {
	if depth > 64 {
		c.issue("failed", "Column nesting exceeds 64 levels")
		return ""
	}
	kind := n.s("Type")
	switch kind {
	case "NodeParagraph":
		return "<p" + styleAttr(n) + ">" + c.inlineHTML(n.kids()) + "</p>"
	case "NodeHeading":
		tag := "h" + strconv.Itoa(max(1, min(6, n.number("HeadingLevel"))))
		return "<" + tag + ">" + c.inlineHTML(n.kids()) + "</" + tag + ">"
	case "NodeCodeBlock":
		lang := n.child("NodeCodeBlockFenceInfoMarker")
		if b, e := base64.StdEncoding.DecodeString(n.s("CodeBlockInfo")); e == nil && len(b) > 0 {
			lang = string(b)
		}
		return `<pre><code class="language-` + html.EscapeString(lang) + `">` + html.EscapeString(n.child("NodeCodeBlockCode")) + `</code></pre>`
	case "NodeList", "NodeMindmap":
		tag := "ul"
		if object(n["ListData"])["Typ"] == float64(1) {
			tag = "ol"
		}
		s := "<" + tag + ">"
		for _, item := range n.kids() {
			if item.s("Type") != "NodeListItem" && item.s("Type") != "NodeMindmapItem" {
				continue
			}
			s += "<li>"
			for _, ch := range item.kids() {
				s += c.blockHTML(ch, depth+1)
			}
			s += "</li>"
		}
		return s + "</" + tag + ">"
	case "NodeBlockquote", "NodeCallout":
		s := "<blockquote>"
		for _, ch := range n.kids() {
			s += c.blockHTML(ch, depth+1)
		}
		return s + "</blockquote>"
	case "NodeTable":
		return c.tableHTML(n)
	case "NodeThematicBreak":
		return "<hr>"
	case "NodeSuperBlock":
		s := ""
		for _, ch := range n.kids() {
			if !strings.HasSuffix(ch.s("Type"), "Marker") {
				s += c.blockHTML(ch, depth+1)
			}
		}
		return s
	default:
		if strings.HasSuffix(kind, "Marker") {
			return ""
		}
		c.issue("converted", kind+" in a column retained as text")
		return "<p>" + html.EscapeString(n.plain()) + "</p>"
	}
}

func (c *syConverter) tableHTML(n syNode) string {
	var out strings.Builder
	out.WriteString("<table>")
	if widths := n.prop("colgroup"); widths != "" {
		out.WriteString("<colgroup>")
		for _, width := range strings.Split(widths, "|") {
			style := syStyle(width)
			if _, value, ok := strings.Cut(style, ":"); ok {
				out.WriteString(`<col width="` + html.EscapeString(strings.TrimSuffix(value, "px")) + `">`)
			} else {
				out.WriteString(`<col width="120">`)
			}
		}
		out.WriteString("</colgroup>")
	}
	var walk func(syNode, bool)
	walk = func(node syNode, header bool) {
		switch node.s("Type") {
		case "NodeTableHead":
			for _, ch := range node.kids() {
				walk(ch, true)
			}
		case "NodeTableRow":
			out.WriteString("<tr>")
			column := 0
			for _, cell := range node.kids() {
				if cell.s("Type") != "NodeTableCell" {
					continue
				}
				tag := "td"
				if header {
					tag = "th"
				}
				out.WriteString("<" + tag)
				out.WriteString(styleAttr(cell))
				aligns := array(n["TableAligns"])
				if column < len(aligns) {
					align := ""
					switch aligns[column] {
					case float64(2):
						align = "center"
					case float64(3):
						align = "right"
					}
					if align != "" {
						out.WriteString(` align="` + align + `"`)
					}
				}
				for _, k := range []string{"colspan", "rowspan"} {
					if v, e := strconv.Atoi(cell.prop(k)); e == nil && v > 1 {
						out.WriteString(" " + k + `="` + strconv.Itoa(min(v, 1000)) + `"`)
					}
				}
				out.WriteString(">")
				if rich := object(cell["TableCellRich"]); rich["format"] == "kramdown" && stringValue(rich["content"]) != "" {
					out.WriteString(html.EscapeString(stringValue(rich["content"])))
					c.issue("converted", "Rich table cell Markdown retained as text")
				} else {
					out.WriteString(c.inlineHTML(cell.kids()))
				}
				out.WriteString("</" + tag + ">")
				span, _ := strconv.Atoi(cell.prop("colspan"))
				column += max(1, span)
			}
			out.WriteString("</tr>")
		default:
			for _, ch := range node.kids() {
				walk(ch, header)
			}
		}
	}
	walk(n, false)
	out.WriteString("</table>\n\n")
	return out.String()
}
