package formats

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

// ---- OpenDocument ----

type odfPara struct {
	b     strings.Builder
	level int
}

type odfTable struct {
	name                 string
	rows                 [][]string
	row, cell            []string
	rowRepeat, colRepeat int
	value                string
}

func convertODF(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	if m, err := z.read("META-INF/manifest.xml"); err == nil && bytes.Contains(m, []byte("encryption-data")) {
		return "", ErrEncrypted
	}
	content, err := z.read("content.xml")
	if err != nil {
		return "", err
	}
	return odfMarkdown(content), nil
}

func convertFlatODF(data []byte) (string, error) { return odfMarkdown(data), nil }

func odfMarkdown(data []byte) string {
	var w mdWriter
	d := newXMLDecoder(bytes.NewReader(data))
	var paras []*odfPara
	var tables []*odfTable
	listDepth, skip, slide := 0, 0, 0
	spreadsheet := false
	lastPara := func() *odfPara {
		if len(paras) == 0 {
			return nil
		}
		return paras[len(paras)-1]
	}
	lastTb := func() *odfTable {
		if len(tables) == 0 {
			return nil
		}
		return tables[len(tables)-1]
	}
	for !w.full() {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			if skip > 0 {
				skip++
				continue
			}
			switch name {
			case "spreadsheet":
				spreadsheet = true
			case "tracked-changes", "annotation", "note", "notes", "forms", "scripts", "font-face-decls",
				"automatic-styles", "styles", "master-styles", "sequence-decls", "named-expressions", "settings", "meta":
				skip = 1
			case "page":
				if strings.Contains(t.Name.Space, "drawing") {
					slide++
					w.heading(2, fmt.Sprintf(i18n.T("幻灯片 %d"), slide))
				}
			case "h":
				paras = append(paras, &odfPara{level: max(atoiDefault(xmlAttr(t, "outline-level"), 1), 1)})
			case "p":
				paras = append(paras, &odfPara{})
			case "s":
				if p := lastPara(); p != nil {
					p.b.WriteString(strings.Repeat(" ", min(max(atoiDefault(xmlAttr(t, "c"), 1), 1), 64)))
				}
			case "tab":
				if p := lastPara(); p != nil {
					p.b.WriteString(" ")
				}
			case "line-break":
				if p := lastPara(); p != nil {
					p.b.WriteString("\n")
				}
			case "list":
				listDepth++
			case "table":
				if strings.Contains(t.Name.Space, "table") {
					tables = append(tables, &odfTable{name: xmlAttr(t, "name")})
				}
			case "table-row":
				if tb := lastTb(); tb != nil {
					tb.row, tb.rowRepeat = nil, max(atoiDefault(xmlAttr(t, "number-rows-repeated"), 1), 1)
				}
			case "table-cell", "covered-table-cell":
				if tb := lastTb(); tb != nil {
					tb.cell, tb.colRepeat = nil, max(atoiDefault(xmlAttr(t, "number-columns-repeated"), 1), 1)
					tb.value = ""
					for _, k := range []string{"string-value", "value", "date-value", "time-value", "boolean-value"} {
						if v := xmlAttr(t, k); v != "" {
							tb.value = v
							break
						}
					}
				}
			}
		case xml.CharData:
			if skip == 0 {
				if p := lastPara(); p != nil {
					p.b.Write(t)
				}
			}
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			switch t.Name.Local {
			case "list":
				listDepth = max(listDepth-1, 0)
			case "h", "p":
				if len(paras) == 0 {
					continue
				}
				p := paras[len(paras)-1]
				paras = paras[:len(paras)-1]
				text := p.b.String()
				if outer := lastPara(); outer != nil {
					outer.b.WriteString(" " + text)
					continue
				}
				if tb := lastTb(); tb != nil {
					if s := strings.TrimSpace(text); s != "" {
						tb.cell = append(tb.cell, s)
					}
					continue
				}
				switch {
				case p.level > 0:
					w.heading(p.level, text)
				case listDepth > 0:
					if s := oneLine(text); s != "" {
						w.listItem(strings.Repeat("  ", min(listDepth-1, 8)) + "- " + escapeInlineMid(s))
					}
				default:
					w.para(text)
				}
			case "table-cell", "covered-table-cell":
				tb := lastTb()
				if tb == nil {
					continue
				}
				text := strings.Join(tb.cell, "\n")
				if text == "" {
					text = tb.value
				}
				for i := 0; i < tb.colRepeat && len(tb.row) < maxColumns; i++ {
					tb.row = append(tb.row, text)
				}
			case "table-row":
				tb := lastTb()
				if tb == nil {
					continue
				}
				for i := 0; i < tb.rowRepeat && len(tb.rows) <= maxRows; i++ {
					tb.rows = append(tb.rows, tb.row)
				}
			case "table":
				if !strings.Contains(t.Name.Space, "table") || len(tables) == 0 {
					continue
				}
				tb := tables[len(tables)-1]
				tables = tables[:len(tables)-1]
				if outer := lastTb(); outer != nil {
					for _, r := range tb.rows {
						if !emptyRow(r) {
							outer.cell = append(outer.cell, strings.Join(r, " · "))
						}
					}
					continue
				}
				if spreadsheet {
					w.heading(2, tb.name)
					if len(trimTable(tb.rows)) == 0 {
						w.para(i18n.T("（空工作表）"))
						continue
					}
				}
				w.table(tb.rows)
			}
		}
	}
	return w.String()
}

// ---- AbiWord ----

func gunzipBounded(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	b, err := io.ReadAll(io.LimitReader(zr, maxZipEntry+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxZipEntry {
		return nil, ErrTooLarge
	}
	return b, nil
}

func convertAbiWord(data []byte) (string, error) {
	if bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		var err error
		if data, err = gunzipBounded(data); err != nil {
			return "", err
		}
	}
	var w mdWriter
	d := newXMLDecoder(bytes.NewReader(data))
	var para *odfPara
	skip, tableDepth := 0, 0
	var rows [][]string
	rowOf := -1
	var cell []string
	for !w.full() {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if skip > 0 {
				skip++
				continue
			}
			switch t.Name.Local {
			case "styles", "pagesize", "data", "history", "revisions", "ignoredwords", "lists", "metadata", "rdf":
				skip = 1
			case "p":
				para = &odfPara{}
				if s := strings.ToLower(xmlAttr(t, "style")); strings.HasPrefix(s, "heading ") {
					para.level = atoiDefault(strings.TrimPrefix(s, "heading "), 0)
				}
			case "br", "cbr", "pbr":
				if para != nil {
					para.b.WriteString("\n")
				}
			case "table":
				tableDepth++
				if tableDepth == 1 {
					rows, rowOf = nil, -1
				}
			case "cell":
				cell = nil
				if tableDepth == 1 {
					top := -1
					for prop := range strings.SplitSeq(xmlAttr(t, "props"), ";") {
						if k, v, ok := strings.Cut(prop, ":"); ok && strings.TrimSpace(k) == "top-attach" {
							top = atoiDefault(v, -1)
						}
					}
					if top != rowOf || top < 0 {
						rows = append(rows, nil)
						rowOf = top
					}
				}
			}
		case xml.CharData:
			if skip == 0 && para != nil {
				para.b.Write(t)
			}
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			switch t.Name.Local {
			case "p":
				if para == nil {
					continue
				}
				text := para.b.String()
				switch {
				case tableDepth > 0:
					if s := strings.TrimSpace(text); s != "" {
						cell = append(cell, s)
					}
				case para.level > 0:
					w.heading(para.level, text)
				default:
					w.para(text)
				}
				para = nil
			case "cell":
				if tableDepth == 1 && len(rows) > 0 {
					rows[len(rows)-1] = append(rows[len(rows)-1], strings.Join(cell, "\n"))
				}
			case "table":
				tableDepth--
				if tableDepth == 0 {
					w.table(rows)
				}
			}
		}
	}
	return w.String(), nil
}

// ---- FictionBook ----

func convertFB2(data []byte) (string, error) {
	var w mdWriter
	d := newXMLDecoder(bytes.NewReader(data))
	var stack []string
	var title, buf strings.Builder
	var bookTitle string
	var authors []string
	var author []string
	inTitle, inText, inInfo := false, false, false
	depth, skip := 0, 0
	for !w.full() {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			stack = append(stack, name)
			if skip > 0 {
				skip++
				continue
			}
			switch name {
			case "binary", "stylesheet", "document-info", "publish-info", "custom-info", "annotation":
				skip = 1
			case "title-info":
				inInfo = true
			case "author":
				author = nil
			case "section":
				depth++
			case "title":
				if !inInfo {
					inTitle = true
					title.Reset()
				}
			case "p", "v", "subtitle", "text-author":
				if !inInfo {
					inText = true
					buf.Reset()
				}
			}
		case xml.CharData:
			if skip > 0 || len(stack) == 0 {
				continue
			}
			switch top := stack[len(stack)-1]; {
			case inInfo && top == "book-title":
				bookTitle += string(t)
			case inInfo && (top == "first-name" || top == "middle-name" || top == "last-name" || top == "nickname"):
				if s := strings.TrimSpace(string(t)); s != "" {
					author = append(author, s)
				}
			case inText:
				buf.Write(t)
			}
		case xml.EndElement:
			name := t.Name.Local
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if skip > 0 {
				skip--
				continue
			}
			switch name {
			case "author":
				if inInfo && len(author) > 0 {
					authors = append(authors, strings.Join(author, " "))
				}
			case "title-info":
				inInfo = false
				w.heading(1, bookTitle)
				if len(authors) > 0 {
					w.fields([][2]string{{i18n.T("作者"), strings.Join(authors, ", ")}})
				}
			case "section":
				depth = max(depth-1, 0)
			case "title":
				if inTitle {
					inTitle = false
					w.heading(min(depth+1, 6), title.String())
				}
			case "p", "v", "subtitle", "text-author":
				if !inText {
					continue
				}
				inText = false
				if inTitle {
					title.WriteString(" " + buf.String())
					continue
				}
				text := oneLine(buf.String())
				if name == "subtitle" && text != "" {
					w.block("**" + escapeInlineMid(text) + "**")
					continue
				}
				w.para(text)
			}
		}
	}
	return w.String(), nil
}

// ---- Outlines and mind maps ----

func outlineItem(w *mdWriter, depth int, text string) {
	if text = oneLine(text); text == "" {
		return
	}
	w.listItem(strings.Repeat("  ", min(max(depth, 0), 8)) + "- " + escapeInlineMid(text))
}

func convertOPML(data []byte) (string, error) {
	var w mdWriter
	d := newXMLDecoder(bytes.NewReader(data))
	depth := 0
	inTitle := false
	for !w.full() {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "title":
				inTitle = depth == 0
			case "outline":
				text := xmlAttr(t, "text")
				if text == "" {
					text = xmlAttr(t, "title")
				}
				if u := xmlAttr(t, "xmlUrl"); u != "" {
					text += " (" + u + ")"
				}
				outlineItem(&w, depth, text)
				depth++
			}
		case xml.CharData:
			if inTitle {
				w.heading(1, string(t))
				inTitle = false
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "outline":
				depth = max(depth-1, 0)
			case "title":
				inTitle = false
			}
		}
	}
	return w.String(), nil
}

func convertFreeMind(data []byte) (string, error) {
	var w mdWriter
	d := newXMLDecoder(bytes.NewReader(data))
	depth := 0
	for !w.full() {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "node" {
				continue
			}
			text := xmlAttr(t, "TEXT")
			if depth == 0 {
				w.heading(1, text)
			} else {
				outlineItem(&w, depth-1, text)
			}
			depth++
		case xml.EndElement:
			if t.Name.Local == "node" {
				depth = max(depth-1, 0)
			}
		}
	}
	return w.String(), nil
}

type xmindTopic struct {
	Title    string `json:"title"`
	Children struct {
		Attached []xmindTopic `json:"attached"`
		Detached []xmindTopic `json:"detached"`
	} `json:"children"`
	Notes struct {
		Plain struct {
			Content string `json:"content"`
		} `json:"plain"`
	} `json:"notes"`
}

type xmindSheet struct {
	Title     string     `json:"title"`
	RootTopic xmindTopic `json:"rootTopic"`
}

func convertXMind(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	var w mdWriter
	if b, err := z.read("content.json"); err == nil {
		var sheets []xmindSheet
		if err := json.Unmarshal(b, &sheets); err != nil {
			return "", fmt.Errorf("formats: XMind: %w", err)
		}
		var topic func(t xmindTopic, depth int)
		topic = func(t xmindTopic, depth int) {
			if depth > 64 || w.full() {
				return
			}
			text := t.Title
			if n := oneLine(t.Notes.Plain.Content); n != "" {
				text += " — " + n
			}
			outlineItem(&w, depth, text)
			for _, c := range append(t.Children.Attached, t.Children.Detached...) {
				topic(c, depth+1)
			}
		}
		for _, s := range sheets {
			if len(sheets) > 1 {
				w.heading(2, s.Title)
			}
			w.heading(3, s.RootTopic.Title)
			for _, c := range append(s.RootTopic.Children.Attached, s.RootTopic.Children.Detached...) {
				topic(c, 0)
			}
		}
		return w.String(), nil
	}
	b, err := z.read("content.xml")
	if err != nil {
		return "", err
	}
	d := newXMLDecoder(bytes.NewReader(b))
	var stack []string
	depth := 0
	for !w.full() {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			if t.Name.Local == "topic" {
				depth++
			}
		case xml.CharData:
			n := len(stack)
			if n < 2 || stack[n-1] != "title" {
				continue
			}
			switch stack[n-2] {
			case "sheet":
				w.heading(2, string(t))
			case "topic":
				if depth == 1 {
					w.heading(3, string(t))
				} else {
					outlineItem(&w, depth-2, string(t))
				}
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if t.Name.Local == "topic" {
				depth = max(depth-1, 0)
			}
		}
	}
	return w.String(), nil
}

// ---- Draw.io ----

type drawioFile struct {
	Diagrams []struct {
		Name  string `xml:"name,attr"`
		Inner []byte `xml:",innerxml"`
	} `xml:"diagram"`
}

func convertDrawio(data []byte) (string, error) {
	var w mdWriter
	var f drawioFile
	if err := newXMLDecoder(bytes.NewReader(data)).Decode(&f); err != nil {
		return "", fmt.Errorf("formats: Draw.io: %w", err)
	}
	if len(f.Diagrams) == 0 && bytes.Contains(data, []byte("<mxGraphModel")) {
		drawioModel(&w, data)
		return w.String(), nil
	}
	for i, dg := range f.Diagrams {
		name := dg.Name
		if name == "" {
			name = fmt.Sprintf(i18n.T("第 %d 页"), i+1)
		}
		w.heading(2, name)
		inner := bytes.TrimSpace(dg.Inner)
		if !bytes.HasPrefix(inner, []byte("<")) {
			model, err := drawioInflate(string(inner))
			if err != nil {
				w.para(i18n.T("（无法解压此页）"))
				continue
			}
			inner = model
		}
		drawioModel(&w, inner)
	}
	return w.String(), nil
}

func drawioInflate(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(raw)), maxZipEntry))
	if err != nil {
		return nil, err
	}
	u, err := url.PathUnescape(string(b))
	if err != nil {
		return b, nil
	}
	return []byte(u), nil
}

func drawioModel(w *mdWriter, model []byte) {
	type edge struct{ src, dst, label string }
	labels := map[string]string{}
	var order []string
	var edges []edge
	d := newXMLDecoder(bytes.NewReader(model))
	objID, objLabel := "", ""
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "UserObject", "object":
				objID, objLabel = xmlAttr(t, "id"), xmlAttr(t, "label")
			case "mxCell":
				id, value := xmlAttr(t, "id"), xmlAttr(t, "value")
				if id == "" {
					id, value = objID, objLabel
				}
				text := htmlText(value)
				if xmlAttr(t, "edge") == "1" {
					edges = append(edges, edge{xmlAttr(t, "source"), xmlAttr(t, "target"), text})
					continue
				}
				if text != "" {
					labels[id] = text
					order = append(order, id)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "UserObject" || t.Name.Local == "object" {
				objID, objLabel = "", ""
			}
		}
	}
	var shapes []string
	for _, id := range order {
		shapes = append(shapes, labels[id])
	}
	w.bullets(shapes)
	var links []string
	for _, e := range edges {
		a, b := labels[e.src], labels[e.dst]
		if a == "" && b == "" {
			continue
		}
		line := a + " → " + b
		if e.label != "" {
			line += "（" + e.label + "）"
		}
		links = append(links, line)
	}
	if len(links) > 0 {
		w.heading(3, i18n.T("连线"))
		w.bullets(links)
	}
}
