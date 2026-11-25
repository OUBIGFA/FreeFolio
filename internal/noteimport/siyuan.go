package noteimport

// SiYuan's Lute JSON is converted in two passes, following Quiet.md's MIT
// importer: assign every page first, then resolve links while rendering content.
// Unsupported constructs retain readable content and receive a report entry.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type syNode map[string]any

func (n syNode) s(key string) string   { v, _ := n[key].(string); return v }
func (n syNode) number(key string) int { v, _ := n[key].(float64); return int(v) }
func (n syNode) kids() []syNode {
	var out []syNode
	raw, _ := n["Children"].([]any)
	for _, x := range raw {
		if v, ok := x.(map[string]any); ok {
			out = append(out, syNode(v))
		}
	}
	return out
}
func (n syNode) prop(key string) string {
	v, _ := n["Properties"].(map[string]any)
	s, _ := v[key].(string)
	return s
}
func (n syNode) child(kind string) string {
	for _, ch := range n.kids() {
		if ch.s("Type") == kind {
			return ch.s("Data")
		}
	}
	return ""
}
func (n syNode) plain() string {
	if s := n.s("TextMarkTextContent"); s != "" {
		return s
	}
	if s := n.s("Data"); s != "" {
		return s
	}
	var b strings.Builder
	for _, ch := range n.kids() {
		b.WriteString(ch.plain())
	}
	return b.String()
}

type syPage struct {
	file, target, title string
	node                syNode
}
type syTarget struct {
	page int
	node syNode
}
type syConverter struct {
	s         *source
	pages     []syPage
	index     map[string]syTarget
	assets    map[string]string
	files     map[string]bool
	write     func(string, []byte) error
	report    *Report
	at, depth int
	output    int
	attrs     [][3]string
}

func importSiYuan(s *source, write func(string, []byte) error, r *Report) error {
	c := syConverter{s: s, index: map[string]syTarget{}, assets: map[string]string{}, files: map[string]bool{}, write: write, report: r}
	for _, file := range s.files {
		c.files[file] = true
		if !strings.HasSuffix(strings.ToLower(file), ".sy") {
			continue
		}
		raw, e := s.read(file)
		if e != nil {
			r.issue(file, "failed", e.Error())
			continue
		}
		var node syNode
		if e = json.Unmarshal(raw, &node); e != nil || node.s("Type") != "NodeDocument" {
			r.issue(file, "failed", "Invalid SiYuan document; original retained")
			continue
		}
		title := node.prop("title")
		if title == "" {
			title = strings.TrimSuffix(path.Base(file), ".sy")
		}
		c.pages = append(c.pages, syPage{file: file, title: title, node: node})
	}
	titles := map[string]string{}
	for _, p := range c.pages {
		titles[strings.TrimSuffix(path.Base(p.file), ".sy")] = cleanTitle(p.title)
	}
	used := map[string]bool{}
	for i := range c.pages {
		p := &c.pages[i]
		parts := strings.Split(path.Dir(p.file), "/")
		start := 0
		for j, part := range parts {
			if part == "data" {
				start = j + 1
			}
		}
		var dirs []string
		for j := start; j < len(parts); j++ {
			part := parts[j]
			if part == "." {
				continue
			}
			prefix := strings.Join(parts[:j+1], "/")
			if raw, e := s.read(path.Join(prefix, ".siyuan/conf.json")); e == nil {
				var conf map[string]any
				if json.Unmarshal(raw, &conf) == nil {
					if name, ok := conf["name"].(string); ok && name != "" {
						part = name
					}
				}
			} else if title := titles[part]; title != "" {
				part = title
			}
			dirs = append(dirs, cleanTitle(part))
		}
		base := path.Join(append(dirs, cleanTitle(p.title))...)
		target := base + ".md"
		for n := 2; used[strings.ToLower(target)]; n++ {
			target = fmt.Sprintf("%s (%d).md", base, n)
		}
		used[strings.ToLower(target)] = true
		p.target = target
		var visit func(syNode)
		visit = func(node syNode) {
			if id := node.s("ID"); id != "" {
				c.index[id] = syTarget{i, node}
			}
			for _, ch := range node.kids() {
				visit(ch)
			}
		}
		visit(p.node)
	}
	for i, p := range c.pages {
		c.at = i
		c.output = i
		c.attrs = nil
		var body strings.Builder
		icon := p.node.prop("icon")
		if icon != "" {
			if v, e := strconv.ParseInt(icon, 16, 32); e == nil {
				icon = string(rune(v))
			}
			encoded, _ := json.Marshal(icon)
			fmt.Fprintf(&body, "---\nicon: %s\n---\n\n", encoded)
		}
		body.WriteString("# " + p.title + "\n\n")
		if cover := htmlSource(p.node.prop("title-img")); cover != "" {
			body.WriteString("![](" + c.asset(cover) + ")\n\n")
		}
		body.WriteString(c.blocks(p.node.kids(), 0))
		c.attributes(p.node, p.title)
		if len(c.attrs) > 0 {
			body.WriteString("\n<details>\n<summary>思源笔记属性 / SiYuan attributes</summary>\n\n| 块 / Block | 属性 / Attribute | 值 / Value |\n| --- | --- | --- |\n")
			for _, a := range c.attrs {
				fmt.Fprintf(&body, "| %s | %s | %s |\n", cell(a[0]), cell(a[1]), cell(a[2]))
			}
			body.WriteString("\n</details>\n")
		}
		if e := write(p.target, []byte(body.String())); e != nil {
			r.issue(p.file, "failed", e.Error())
		} else {
			r.Documents++
		}
	}
	return nil
}

func (c *syConverter) issue(kind, detail string) { c.report.issue(c.pages[c.at].file, kind, detail) }
func (c *syConverter) attributes(n syNode, label string) {
	props, _ := n["Properties"].(map[string]any)
	var keys []string
	for k := range props {
		if k == "name" || k == "alias" || k == "memo" || k == "bookmark" || k == "tags" || strings.HasPrefix(k, "custom-") {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		v, _ := props[k].(string)
		if v != "" {
			c.attrs = append(c.attrs, [3]string{label, k, v})
		}
	}
}
func cell(s string) string { return strings.NewReplacer("|", "\\|", "\n", "<br>").Replace(s) }
func escape(s string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "`", "\\`", "<", "&lt;", ">", "&gt;").Replace(strings.ReplaceAll(s, "\u200b", ""))
}
func code(s string) string {
	fence := "```"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	return fence
}
func inlineCode(s string) string {
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	return fence + " " + s + " " + fence
}
func quote(s string) string {
	return "> " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n> ") + "\n\n"
}

var srcPattern = regexp.MustCompile(`(?i)(?:src\s*=\s*["']([^"']+)["']|url\(\s*["']?([^"')]+))`)

func htmlSource(s string) string {
	m := srcPattern.FindStringSubmatch(s)
	if len(m) > 1 {
		if m[1] != "" {
			return html.UnescapeString(m[1])
		}
		return html.UnescapeString(m[2])
	}
	return ""
}

func (c *syConverter) asset(href string) string {
	decoded, e := url.PathUnescape(href)
	if e == nil {
		href = decoded
	}
	href = strings.TrimPrefix(href, "./")
	if strings.Contains(href, "://") || strings.HasPrefix(href, "data:") {
		if !strings.HasPrefix(href, "file:") {
			return href
		}
		c.issue("missing-attachment", href)
		return href
	}
	if !safeName(href) {
		c.issue("missing-attachment", href)
		return href
	}
	var found string
	for dir := path.Dir(c.pages[c.at].file); ; dir = path.Dir(dir) {
		candidate := path.Clean(path.Join(dir, href))
		if c.files[candidate] {
			found = candidate
			break
		}
		if dir == "." {
			break
		}
	}
	if found == "" {
		for _, file := range c.s.files {
			if file == href || strings.HasSuffix(file, "/"+href) {
				found = file
				break
			}
		}
	}
	if found == "" {
		c.issue("missing-attachment", href)
		return href
	}
	target, ok := c.assets[found]
	if !ok {
		target = path.Join("assets", found)
		raw, e := c.s.read(found)
		if e == nil {
			e = c.write(target, raw)
		}
		if e != nil {
			c.issue("missing-attachment", href+": "+e.Error())
			return href
		}
		c.assets[found] = target
		c.report.Attachments++
	}
	return strings.ReplaceAll(relative(c.pages[c.output].target, target), " ", "%20")
}
func (c *syConverter) reference(id, text string) string {
	target, ok := c.index[id]
	if !ok {
		c.issue("broken-reference", id)
		if text == "" {
			text = id
		}
		return escape(text)
	}
	p := c.pages[target.page]
	if text == "" {
		text = target.node.plain()
		if text == "" {
			text = p.title
		}
	}
	href := relative(c.pages[c.output].target, p.target)
	if target.node.s("Type") == "NodeHeading" {
		href += "#" + url.PathEscape(target.node.plain())
	} else if target.node.s("Type") != "NodeDocument" {
		c.issue("block-reference", "Reference "+id+" now links to its document")
	}
	return "[" + escape(text) + "](" + strings.ReplaceAll(href, " ", "%20") + ")"
}

func (c *syConverter) inline(nodes []syNode) string {
	var out strings.Builder
	for _, n := range nodes {
		kind := n.s("Type")
		var s string
		switch kind {
		case "NodeText", "NodeLinkText":
			s = escape(n.s("Data"))
		case "NodeTextMark":
			s = escape(n.s("TextMarkTextContent"))
			for _, mark := range strings.Fields(n.s("TextMarkType")) {
				switch mark {
				case "text":
				case "strong":
					s = "**" + s + "**"
				case "em":
					s = "*" + s + "*"
				case "s":
					s = "~~" + s + "~~"
				case "u":
					s = "<u>" + s + "</u>"
				case "sup", "sub", "kbd":
					s = "<" + mark + ">" + s + "</" + mark + ">"
				case "mark":
					s = "<mark>" + s + "</mark>"
				case "code":
					s = inlineCode(n.s("TextMarkTextContent"))
				case "tag":
					s = "#" + s
				case "a":
					s = "[" + s + "](" + c.link(n.s("TextMarkAHref")) + ")"
				case "block-ref":
					s = c.reference(n.s("TextMarkBlockRefID"), n.s("TextMarkTextContent"))
				case "inline-math":
					s = "$" + n.s("TextMarkInlineMathContent") + "$"
				case "inline-memo":
					s += "（" + escape(n.s("TextMarkInlineMemoContent")) + "）"
					c.issue("converted", "Inline memo retained as text")
				default:
					c.issue("converted", mark)
				}
			}
		case "NodeBr", "NodeHardBreak":
			s = "<br>\n"
		case "NodeSoftBreak":
			s = "\n"
		case "NodeImage":
			s = "![" + escape(n.child("NodeLinkText")) + "](" + c.asset(n.child("NodeLinkDest")) + ")"
			if styleAttr(n) != "" {
				s = c.inlineHTML([]syNode{n})
			}
		case "NodeLink":
			s = "[" + escape(n.child("NodeLinkText")) + "](" + c.link(n.child("NodeLinkDest")) + ")"
		case "NodeBlockRef":
			text := n.child("NodeBlockRefText")
			if text == "" {
				text = n.child("NodeBlockRefDynText")
			}
			s = c.reference(n.child("NodeBlockRefID"), text)
		case "NodeStrong":
			s = "**" + c.inline(n.kids()) + "**"
		case "NodeEmphasis":
			s = "*" + c.inline(n.kids()) + "*"
		case "NodeUnderline":
			s = "<u>" + c.inline(n.kids()) + "</u>"
		case "NodeStrikethrough":
			s = "~~" + c.inline(n.kids()) + "~~"
		case "NodeMark":
			s = "<mark>" + c.inline(n.kids()) + "</mark>"
		case "NodeCodeSpan":
			s = inlineCode(n.child("NodeCodeSpanContent"))
		case "NodeInlineMath":
			s = "$" + n.child("NodeInlineMathContent") + "$"
		case "NodeTag":
			s = "#" + escape(n.plain())
		case "NodeFootnotesRef":
			s = "[^" + strings.TrimPrefix(n.s("Data"), "^") + "]"
		case "NodeInlineHTML", "NodeHTMLInline":
			s = n.s("Data")
			c.issue("converted", "Inline HTML retained for compatibility")
		default:
			if strings.HasSuffix(kind, "Marker") || strings.HasSuffix(kind, "OpenMarker") || strings.HasSuffix(kind, "CloseMarker") {
				continue
			}
			if len(n.kids()) > 0 {
				s = c.inline(n.kids())
			} else {
				s = escape(n.s("Data"))
			}
			if s != "" {
				c.issue("converted", kind)
			}
		}
		if kind == "NodeTextMark" && styleAttr(n) != "" {
			s = c.inlineHTML([]syNode{n})
		}
		out.WriteString(s)
	}
	return out.String()
}
func (c *syConverter) link(href string) string {
	if strings.HasPrefix(href, "siyuan://blocks/") {
		id := strings.TrimPrefix(href, "siyuan://blocks/")
		if t, ok := c.index[id]; ok {
			return relative(c.pages[c.output].target, c.pages[t.page].target)
		}
		c.issue("broken-reference", href)
		return href
	}
	if strings.HasPrefix(href, "assets/") {
		return c.asset(href)
	}
	return href
}

func (c *syConverter) blocks(nodes []syNode, depth int) string {
	if depth > 64 {
		c.issue("failed", "Nesting exceeds 64 levels; source retained")
		return ""
	}
	var out strings.Builder
	for _, n := range nodes {
		kind := n.s("Type")
		var s string
		switch kind {
		case "NodeParagraph":
			s = c.inline(n.kids()) + "\n\n"
			if styleAttr(n) != "" {
				s = "<p" + styleAttr(n) + ">" + c.inlineHTML(n.kids()) + "</p>\n\n"
			}
		case "NodeHeading":
			s = strings.Repeat("#", max(1, min(6, n.number("HeadingLevel")))) + " " + c.inline(n.kids()) + "\n\n"
		case "NodeList", "NodeMindmap":
			list, _ := n["ListData"].(map[string]any)
			ordered := list["Typ"] == float64(1)
			task := list["Typ"] == float64(3)
			start := 1
			if n, ok := list["Start"].(float64); ok && n > 0 {
				start = int(n)
			}
			for i, item := range n.kids() {
				if item.s("Type") != "NodeListItem" && item.s("Type") != "NodeMindmapItem" {
					continue
				}
				prefix := "- "
				if ordered {
					prefix = fmt.Sprintf("%d. ", i+start)
				}
				if task {
					prefix = "- [ ] "
					for _, ch := range item.kids() {
						if ch.s("Type") == "NodeTaskListItemMarker" && (ch["TaskListItemChecked"] == true || ch.s("Data") == "X" || ch.s("Data") == "x") {
							prefix = "- [x] "
						}
					}
				}
				body := strings.TrimSpace(c.blocks(item.kids(), depth+1))
				s += prefix + strings.ReplaceAll(body, "\n", "\n    ") + "\n"
			}
			s += "\n"
			if kind == "NodeMindmap" {
				c.issue("converted", "Mind map rendered as a list")
			}
		case "NodeBlockquote":
			s = quote(c.blocks(n.kids(), depth+1))
		case "NodeCallout":
			kind := strings.ToUpper(n.s("CalloutType"))
			if kind == "" {
				kind = "NOTE"
			}
			s = "> [!" + kind + "] " + escape(n.s("CalloutTitle")) + "\n" + quote(c.blocks(n.kids(), depth+1))
		case "NodeCodeBlock":
			language := n.child("NodeCodeBlockFenceInfoMarker")
			info := n.s("CodeBlockInfo")
			if info == "" {
				for _, ch := range n.kids() {
					if ch.s("Type") == "NodeCodeBlockFenceInfoMarker" {
						info = ch.s("CodeBlockInfo")
					}
				}
			}
			if decoded, e := base64.StdEncoding.DecodeString(info); e == nil && len(decoded) > 0 {
				language = string(decoded)
			}
			if fields := strings.Fields(language); len(fields) > 0 {
				language = fields[0]
			}
			body := n.child("NodeCodeBlockCode")
			if body == "" {
				body = n.s("Data")
			}
			f := code(body)
			s = f + language + "\n" + body + "\n" + f + "\n\n"
		case "NodeMathBlock":
			s = "$$\n" + n.child("NodeMathBlockContent") + "\n$$\n\n"
		case "NodeTable":
			s = c.table(n)
		case "NodeThematicBreak":
			s = "---\n\n"
		case "NodeHTMLBlock":
			body := n.s("Data")
			f := code(body)
			s = f + "html\n" + body + "\n" + f + "\n\n"
			c.issue("converted", "HTML block preserved as source")
		case "NodeIFrame", "NodeWidget", "NodeVideo", "NodeAudio":
			href := htmlSource(n.s("Data"))
			if href != "" {
				if kind == "NodeVideo" || kind == "NodeAudio" {
					href = c.asset(href)
				}
				s = "[" + kind + "](" + href + ")\n\n"
			} else {
				s = escape(n.plain()) + "\n\n"
			}
			c.issue("converted", kind+" retained as a link")
		case "NodeAttributeView":
			s = c.database(n)
		case "NodeBlockQueryEmbed":
			body := n.child("NodeBlockQueryEmbedScript")
			if body == "" {
				body = n.plain()
			}
			if embedded, ok := c.staticEmbed(body, depth); ok {
				s = embedded
				break
			}
			f := code(body)
			s = f + "sql\n" + body + "\n" + f + "\n\n"
			c.issue("query", "Query retained; dynamic results were not imported")
		case "NodeTabs":
			for _, tab := range n.kids() {
				s += "**" + escape(tab.s("TabItemTitle")) + "**\n\n" + c.blocks(tab.kids(), depth+1)
			}
			c.issue("converted", "Tabs arranged sequentially")
		case "NodeSuperBlock":
			if n.child("NodeSuperBlockLayoutMarker") == "col" && c.depth == 0 {
				c.depth++
				s = "<div class=\"freefolio-columns\" style=\"display:flex;gap:24px\">"
				for _, ch := range n.kids() {
					if strings.HasSuffix(ch.s("Type"), "Marker") {
						continue
					}
					s += "<div class=\"freefolio-column\" style=\"flex:1\">" + c.blockHTML(ch, depth+1) + "</div>"
				}
				s += "</div>\n\n"
				c.depth--
			} else {
				s = c.blocks(n.kids(), depth+1)
				if n.child("NodeSuperBlockLayoutMarker") == "col" {
					c.issue("converted", "Nested columns arranged sequentially")
				}
			}
		case "NodeYamlFrontMatter":
			body := n.child("NodeYamlFrontMatterContent")
			s = "```yaml\n" + body + "\n```\n\n"
		case "NodeFootnotesDefBlock":
			for _, def := range n.kids() {
				body := strings.TrimSpace(c.blocks(def.kids(), depth+1))
				s += "[^" + strings.TrimPrefix(def.s("Data"), "^") + "]: " + strings.ReplaceAll(body, "\n", "\n    ") + "\n\n"
			}
		case "NodeTaskListItemMarker", "NodeSuperBlockOpenMarker", "NodeSuperBlockLayoutMarker", "NodeSuperBlockCloseMarker":
			continue
		default:
			if strings.HasSuffix(kind, "Marker") {
				continue
			}
			if len(n.kids()) > 0 {
				s = c.blocks(n.kids(), depth+1)
			} else {
				s = escape(n.plain()) + "\n\n"
			}
			c.issue("converted", kind+" retained as text or child blocks")
		}
		c.attributes(n, n.s("ID"))
		if n.prop("fold") == "1" || n.prop("heading-fold") == "1" {
			c.issue("converted", "Folded block is imported expanded; content retained")
		}
		out.WriteString(s)
	}
	return out.String()
}
func (c *syConverter) table(n syNode) string {
	rich := false
	var inspect func(syNode)
	inspect = func(node syNode) {
		if node.prop("colspan") != "" || node.prop("rowspan") != "" || node.prop("style") != "" || node["TableCellRich"] != nil {
			rich = true
		}
		for _, ch := range node.kids() {
			inspect(ch)
		}
	}
	inspect(n)
	if rich {
		return c.tableHTML(n)
	}
	var rows [][]string
	var walk func(syNode)
	walk = func(n syNode) {
		if n.s("Type") == "NodeTableRow" || n.s("Type") == "NodeTableHead" {
			var cells []string
			for _, cell := range n.kids() {
				if cell.s("Type") == "NodeTableCell" {
					cells = append(cells, c.inline(cell.kids()))
				}
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
				return
			}
		}
		for _, ch := range n.kids() {
			walk(ch)
		}
	}
	walk(n)
	return markdownTable(rows)
}
func markdownTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row))
	}
	var out strings.Builder
	for i, row := range rows {
		out.WriteString("|")
		for j := 0; j < width; j++ {
			s := ""
			if j < len(row) {
				s = row[j]
			}
			out.WriteString(" " + cell(s) + " |")
		}
		out.WriteByte('\n')
		if i == 0 {
			out.WriteString("|" + strings.Repeat(" --- |", width) + "\n")
		}
	}
	out.WriteByte('\n')
	return out.String()
}
