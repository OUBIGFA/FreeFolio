package formats

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

// rel is a relationship of an OPC package part.
type rel struct {
	Target   string
	Type     string
	External bool
}

// rels reads a part's relationships by ID, internal targets resolved
// against the part's folder. part "" reads the package's own.
func (z *zipArchive) rels(part string) map[string]rel {
	dir, base := path.Split(part)
	out := map[string]rel{}
	data, err := z.read(dir + "_rels/" + base + ".rels")
	if err != nil {
		return out
	}
	d := newXMLDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		r := rel{Target: xmlAttr(se, "Target"), Type: xmlAttr(se, "Type")}
		if strings.EqualFold(xmlAttr(se, "TargetMode"), "External") {
			r.External = true
		} else if strings.HasPrefix(r.Target, "/") {
			r.Target = strings.TrimPrefix(path.Clean(r.Target), "/")
		} else {
			r.Target = strings.TrimPrefix(path.Clean("/"+path.Join(dir, r.Target)), "/")
		}
		out[xmlAttr(se, "Id")] = r
	}
	return out
}

// mainPart is the package's main document, or fallback.
func (z *zipArchive) mainPart(fallback string) string {
	for _, r := range z.rels("") {
		if strings.HasSuffix(r.Type, "/officeDocument") && !r.External && z.has(r.Target) {
			return r.Target
		}
	}
	return fallback
}

// relOfType is the first internal relationship of part whose type ends
// with suffix, or fallback.
func (z *zipArchive) relOfType(part, suffix, fallback string) string {
	for _, r := range z.rels(part) {
		if strings.HasSuffix(r.Type, suffix) && !r.External {
			return r.Target
		}
	}
	return fallback
}

// relID is a relationship attribute (r:id, r:embed), told from same-named
// attributes of other namespaces.
func relID(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local && strings.Contains(a.Name.Space, "relationships") {
			return a.Value
		}
	}
	return ""
}

// isOn reads a toggle property such as <w:b/> or <w:b w:val="0"/>.
func isOn(se xml.StartElement) bool {
	switch strings.ToLower(xmlAttr(se, "val")) {
	case "0", "false", "off", "none":
		return false
	}
	return true
}

// span is a run of text sharing its emphasis.
type span struct {
	text                 string
	bold, italic, strike bool
	link                 string
}

// spansMarkdown renders spans as inline Markdown, a line break as a hard
// break.
func spansMarkdown(spans []span) string {
	// Merge neighbors that look the same.
	var merged []span
	for _, s := range spans {
		if n := len(merged); n > 0 {
			p := &merged[n-1]
			if p.bold == s.bold && p.italic == s.italic && p.strike == s.strike && p.link == s.link {
				p.text += s.text
				continue
			}
		}
		merged = append(merged, s)
	}
	var b strings.Builder
	for _, s := range merged {
		lines := strings.Split(s.text, "\n")
		for i, line := range lines {
			if i > 0 {
				b.WriteString("\\\n")
			}
			b.WriteString(emphasize(line, s))
		}
	}
	return strings.TrimSpace(b.String())
}

func emphasize(text string, s span) string {
	core := strings.TrimSpace(text)
	if core == "" {
		return text
	}
	lead := text[:strings.Index(text, core)]
	trail := text[len(lead)+len(core):]
	out := escapeInlineMid(core)
	if s.strike {
		out = "~~" + out + "~~"
	}
	if s.italic {
		out = "*" + out + "*"
	}
	if s.bold {
		out = "**" + out + "**"
	}
	if s.link != "" {
		out = "[" + out + "](" + linkTarget(s.link) + ")"
	}
	return lead + out + trail
}

// escapeInlineMid escapes text that may sit mid-line: no block markers.
func escapeInlineMid(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '*', '_', '[', ']', '<', '>', '|', '`', '$', '~':
			b.WriteByte('\\')
		}
		if r < 32 && r != '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// linkTarget makes a URL safe inside Markdown's parentheses.
func linkTarget(u string) string {
	r := strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E")
	return r.Replace(strings.TrimSpace(u))
}

// lineStart guards the start of a rendered line against reading as a
// block marker.
func lineStart(md string) string {
	if md == "" {
		return md
	}
	switch md[0] {
	case '#', '-', '+', '=', '>':
		return "\\" + md
	}
	return escapeListNumber(md)
}

func plainOf(spans []span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.text)
	}
	return b.String()
}

// ---- Word ----

type docxStyle struct {
	name    string
	outline int // 1-based, 0 none
}

func docxStyles(z *zipArchive, main string) map[string]docxStyle {
	out := map[string]docxStyle{}
	data, err := z.read(z.relOfType(main, "/styles", "word/styles.xml"))
	if err != nil {
		return out
	}
	d := newXMLDecoder(bytes.NewReader(data))
	var id string
	var cur docxStyle
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "style":
				id, cur = xmlAttr(t, "styleId"), docxStyle{}
			case "name":
				cur.name = strings.ToLower(xmlAttr(t, "val"))
			case "outlineLvl":
				if n, err := strconv.Atoi(xmlAttr(t, "val")); err == nil && n < 9 {
					cur.outline = n + 1
				}
			}
		case xml.EndElement:
			if t.Name.Local == "style" && id != "" {
				out[id] = cur
				id = ""
			}
		}
	}
	return out
}

// headingLevel is the heading level a paragraph style gives, 0 for body.
func headingLevel(styleID string, styles map[string]docxStyle) int {
	st, ok := styles[styleID]
	name := st.name
	if !ok {
		name = strings.ToLower(styleID)
	}
	switch {
	case name == "title":
		return 1
	case strings.HasPrefix(name, "heading"):
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(name, "heading"))); err == nil && n > 0 {
			return min(n, 6)
		}
	}
	if st.outline > 0 {
		return min(st.outline, 6)
	}
	return 0
}

// docxNumbering tells for numId and level whether a list is ordered.
func docxNumbering(z *zipArchive, main string) func(numID string, level int) bool {
	data, err := z.read(z.relOfType(main, "/numbering", "word/numbering.xml"))
	if err != nil {
		return func(string, int) bool { return false }
	}
	abstract := map[string]map[int]bool{} // abstractNumId → level → ordered
	nums := map[string]string{}           // numId → abstractNumId
	d := newXMLDecoder(bytes.NewReader(data))
	var absID, numID string
	level := -1
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "abstractNum":
				absID = xmlAttr(t, "abstractNumId")
				abstract[absID] = map[int]bool{}
			case "lvl":
				level, _ = strconv.Atoi(xmlAttr(t, "ilvl"))
			case "numFmt":
				if absID != "" && level >= 0 {
					f := xmlAttr(t, "val")
					abstract[absID][level] = f != "bullet" && f != "none" && f != ""
				}
			case "num":
				numID = xmlAttr(t, "numId")
			case "abstractNumId":
				if numID != "" {
					nums[numID] = xmlAttr(t, "val")
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "abstractNum":
				absID, level = "", -1
			case "num":
				numID = ""
			}
		}
	}
	return func(id string, level int) bool { return abstract[nums[id]][level] }
}

type docxPara struct {
	spans   []span
	style   string
	list    bool
	numID   string
	level   int
	outline int
}

type tableBuild struct {
	rows [][]string
	row  []string
	cell []string
}

func convertDOCX(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	main := z.mainPart("word/document.xml")
	doc, err := z.read(main)
	if err != nil {
		return "", err
	}
	styles := docxStyles(z, main)
	ordered := docxNumbering(z, main)
	rels := z.rels(main)
	var w mdWriter
	if err := walkWordML(doc, rels, styles, ordered, &w); err != nil {
		return "", err
	}
	return w.String(), nil
}

// walkWordML writes the body of a WordprocessingML part.
func walkWordML(doc []byte, rels map[string]rel, styles map[string]docxStyle, ordered func(string, int) bool, w *mdWriter) error {
	d := newXMLDecoder(bytes.NewReader(doc))
	var stack []string
	var paras []*docxPara
	var tables []*tableBuild
	var runProps span
	var link string
	skip := 0 // depth inside a subtree to ignore
	listNumber := map[int]int{}
	parent := func(n int) string {
		if len(stack) > n {
			return stack[len(stack)-1-n]
		}
		return ""
	}
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		if w.full() {
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
			case "Fallback", "instrText", "delText", "del", "footnoteReference", "commentReference", "rPh":
				skip = 1
			case "p":
				paras = append(paras, &docxPara{})
			case "pStyle":
				if p := last(paras); p != nil {
					p.style = xmlAttr(t, "val")
				}
			case "numPr":
				if p := last(paras); p != nil {
					p.list = true
				}
			case "ilvl":
				if p := last(paras); p != nil {
					p.level, _ = strconv.Atoi(xmlAttr(t, "val"))
				}
			case "numId":
				if p := last(paras); p != nil {
					p.numID = xmlAttr(t, "val")
					if p.numID == "0" {
						p.list = false
					}
				}
			case "outlineLvl":
				if p := last(paras); p != nil && parent(1) == "pPr" {
					if n, err := strconv.Atoi(xmlAttr(t, "val")); err == nil && n < 9 {
						p.outline = n + 1
					}
				}
			case "r":
				runProps = span{}
			case "b", "i", "strike", "dstrike":
				if parent(1) == "rPr" && parent(2) == "r" {
					on := isOn(t)
					switch name {
					case "b":
						runProps.bold = on
					case "i":
						runProps.italic = on
					default:
						runProps.strike = on
					}
				}
			case "hyperlink":
				if r, ok := rels[relID(t, "id")]; ok && r.External {
					link = r.Target
				}
			case "tab":
				if parent(1) == "r" {
					addText(paras, runProps, link, "\t")
				}
			case "br", "cr":
				if parent(1) == "r" {
					addText(paras, runProps, link, "\n")
				}
			case "tbl":
				tables = append(tables, &tableBuild{})
			case "tr":
				if tb := lastTable(tables); tb != nil {
					tb.row = nil
				}
			case "tc":
				if tb := lastTable(tables); tb != nil {
					tb.cell = nil
				}
			}
		case xml.CharData:
			if skip == 0 && parent(0) == "t" && parent(1) == "r" {
				addText(paras, runProps, link, string(t))
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
			case "hyperlink":
				link = ""
			case "p":
				if len(paras) == 0 {
					continue
				}
				p := paras[len(paras)-1]
				paras = paras[:len(paras)-1]
				if tb := lastTable(tables); tb != nil && len(paras) == 0 {
					if s := strings.TrimSpace(plainOf(p.spans)); s != "" {
						tb.cell = append(tb.cell, s)
					}
					continue
				}
				writeDocxPara(w, p, styles, ordered, listNumber)
			case "tc":
				if tb := lastTable(tables); tb != nil {
					tb.row = append(tb.row, strings.Join(tb.cell, "\n"))
				}
			case "tr":
				if tb := lastTable(tables); tb != nil {
					tb.rows = append(tb.rows, tb.row)
				}
			case "tbl":
				if len(tables) == 0 {
					continue
				}
				tb := tables[len(tables)-1]
				tables = tables[:len(tables)-1]
				if outer := lastTable(tables); outer != nil {
					// A nested table flattens into the outer cell.
					for _, r := range tb.rows {
						outer.cell = append(outer.cell, strings.Join(r, " · "))
					}
					continue
				}
				clear(listNumber)
				w.table(tb.rows)
			}
		}
	}
	return nil
}

func last(ps []*docxPara) *docxPara {
	if len(ps) == 0 {
		return nil
	}
	return ps[len(ps)-1]
}

func lastTable(ts []*tableBuild) *tableBuild {
	if len(ts) == 0 {
		return nil
	}
	return ts[len(ts)-1]
}

func addText(paras []*docxPara, props span, link, text string) {
	p := last(paras)
	if p == nil {
		return
	}
	s := props
	s.text, s.link = text, link
	p.spans = append(p.spans, s)
}

func writeDocxPara(w *mdWriter, p *docxPara, styles map[string]docxStyle, ordered func(string, int) bool, numbers map[int]int) {
	plain := strings.TrimSpace(plainOf(p.spans))
	if plain == "" {
		return
	}
	level := headingLevel(p.style, styles)
	if level == 0 && p.outline > 0 {
		level = min(p.outline, 6)
	}
	if level > 0 && !p.list {
		clear(numbers)
		w.heading(level, plain)
		return
	}
	md := spansMarkdown(p.spans)
	if p.list {
		lvl := min(max(p.level, 0), 8)
		for k := range numbers {
			if k > lvl {
				delete(numbers, k)
			}
		}
		marker := "- "
		if ordered(p.numID, lvl) {
			numbers[lvl]++
			marker = strconv.Itoa(numbers[lvl]) + ". "
		}
		md = strings.ReplaceAll(md, "\\\n", " ")
		w.listItem(strings.Repeat("  ", lvl) + marker + md)
		return
	}
	clear(numbers)
	w.block(lineStart(md))
}

// listItem writes a list line, joining consecutive items into one list.
func (w *mdWriter) listItem(line string) {
	if w.full() {
		return
	}
	if w.inList {
		w.b.WriteString("\n" + line)
		return
	}
	w.block(line)
	w.inList = true
}

// ---- Excel ----

type xlsxSheet struct{ name, part string }

func convertXLSX(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	main := z.mainPart("xl/workbook.xml")
	wb, err := z.read(main)
	if err != nil {
		return "", err
	}
	rels := z.rels(main)
	var sheets []xlsxSheet
	date1904 := false
	d := newXMLDecoder(bytes.NewReader(wb))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "workbookPr":
			v := strings.ToLower(xmlAttr(se, "date1904"))
			date1904 = v == "1" || v == "true"
		case "sheet":
			if r, ok := rels[relID(se, "id")]; ok && !r.External {
				sheets = append(sheets, xlsxSheet{xmlAttr(se, "name"), r.Target})
			}
		}
	}
	if len(sheets) == 0 {
		for _, n := range z.names("xl/worksheets/", ".xml") {
			sheets = append(sheets, xlsxSheet{strings.TrimSuffix(path.Base(n), ".xml"), n})
		}
	}
	shared := xlsxShared(z, z.relOfType(main, "/sharedStrings", "xl/sharedStrings.xml"))
	dates := xlsxDateStyles(z, z.relOfType(main, "/styles", "xl/styles.xml"))
	var w mdWriter
	for _, sh := range sheets {
		if w.full() {
			break
		}
		b, err := z.read(sh.part)
		if err != nil {
			continue
		}
		rows, more := xlsxRows(b, shared, dates, date1904)
		w.heading(2, sh.name)
		if len(trimTable(rows)) == 0 {
			w.para(i18n.T("（空工作表）"))
			continue
		}
		w.table(rows)
		if more > 0 {
			w.para(fmt.Sprintf(i18n.T("… 另有 %d 行未显示"), more))
		}
	}
	return w.String(), nil
}

func xlsxShared(z *zipArchive, part string) []string {
	data, err := z.read(part)
	if err != nil {
		return nil
	}
	var out []string
	var cur strings.Builder
	in, skip := false, 0
	var stack []string
	d := newXMLDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			if skip > 0 || t.Name.Local == "rPh" || t.Name.Local == "phoneticPr" {
				skip++
				continue
			}
			if t.Name.Local == "si" {
				in = true
				cur.Reset()
			}
		case xml.CharData:
			if in && skip == 0 && len(stack) > 0 && stack[len(stack)-1] == "t" {
				cur.Write(t)
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if skip > 0 {
				skip--
				continue
			}
			if t.Name.Local == "si" {
				in = false
				out = append(out, cur.String())
			}
		}
	}
	return out
}

// xlsxDateStyles tells, by cell style index, which styles show dates.
func xlsxDateStyles(z *zipArchive, part string) []bool {
	data, err := z.read(part)
	if err != nil {
		return nil
	}
	custom := map[int]bool{}
	var out []bool
	inXfs := false
	d := newXMLDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "numFmt":
				id, _ := strconv.Atoi(xmlAttr(t, "numFmtId"))
				custom[id] = isDateFormat(xmlAttr(t, "formatCode"))
			case "cellXfs":
				inXfs = true
			case "xf":
				if inXfs {
					id, _ := strconv.Atoi(xmlAttr(t, "numFmtId"))
					date, ok := custom[id]
					if !ok {
						date = id >= 14 && id <= 22 || id >= 45 && id <= 47 || id >= 27 && id <= 36 || id >= 50 && id <= 58
					}
					out = append(out, date)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "cellXfs" {
				inXfs = false
			}
		}
	}
	return out
}

// isDateFormat reports whether an Excel number format shows a date or time.
func isDateFormat(code string) bool {
	var b strings.Builder
	quoted, bracket := false, false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '[':
			bracket = true
		case c == ']':
			bracket = false
		case bracket:
		case c == '\\' || c == '_' || c == '*':
			i++
		default:
			b.WriteByte(c)
		}
	}
	s := strings.ToLower(b.String())
	return strings.ContainsAny(s, "ydh") || strings.Contains(s, "m") && strings.Contains(s, "s")
}

// excelTime turns a serial date into text.
func excelTime(v float64, date1904 bool) string {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 2958465 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	if date1904 {
		base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	days, frac := math.Modf(v)
	t := base.AddDate(0, 0, int(days)).Add(time.Duration(math.Round(frac*86400)) * time.Second)
	switch {
	case frac == 0:
		return t.Format("2006-01-02")
	case days == 0:
		return t.Format("15:04:05")
	}
	return t.Format("2006-01-02 15:04")
}

// excelNumber shows a stored number as Excel does, to 15 digits.
func excelNumber(v string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return v
	}
	r, err := strconv.ParseFloat(strconv.FormatFloat(f, 'g', 15, 64), 64)
	if err != nil {
		return v
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

// cellColumn is the 0-based column of a reference such as "AB12".
func cellColumn(ref string) int {
	col := 0
	for _, c := range strings.ToUpper(ref) {
		if c < 'A' || c > 'Z' {
			break
		}
		col = col*26 + int(c-'A'+1)
		if col > 1<<20 {
			return -1
		}
	}
	return col - 1
}

// xlsxRows reads a worksheet, returning at most maxRows+1 rows and how many
// more there were.
func xlsxRows(data []byte, shared []string, dates []bool, date1904 bool) ([][]string, int) {
	var rows [][]string
	more := 0
	var row []string
	var ctype, cstyle, value, inline string
	col := -1
	var stack []string
	d := newXMLDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			switch t.Name.Local {
			case "row":
				row, col = nil, -1
			case "c":
				ctype, cstyle, value, inline = xmlAttr(t, "t"), xmlAttr(t, "s"), "", ""
				if ref := xmlAttr(t, "r"); ref != "" {
					col = cellColumn(ref)
				} else {
					col++
				}
			}
		case xml.CharData:
			if n := len(stack); n > 0 {
				switch {
				case stack[n-1] == "v":
					value += string(t)
				case stack[n-1] == "t" && slicesContains(stack, "is") && !slicesContains(stack, "rPh"):
					inline += string(t)
				}
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			switch t.Name.Local {
			case "c":
				if col < 0 || col >= maxColumns {
					continue
				}
				text := value
				switch ctype {
				case "s":
					if i, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && i >= 0 && i < len(shared) {
						text = shared[i]
					}
				case "inlineStr":
					text = inline
				case "b":
					text = "FALSE"
					if strings.TrimSpace(value) == "1" {
						text = "TRUE"
					}
				case "str", "e":
				default:
					if value != "" {
						if s, err := strconv.Atoi(cstyle); err == nil && s >= 0 && s < len(dates) && dates[s] {
							if f, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
								text = excelTime(f, date1904)
								break
							}
						}
						text = excelNumber(value)
					}
				}
				for len(row) <= col {
					row = append(row, "")
				}
				row[col] = text
			case "row":
				if len(rows) > maxRows {
					if !emptyRow(row) {
						more++
					}
					continue
				}
				rows = append(rows, row)
			}
		}
	}
	return rows, more
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ---- PowerPoint ----

func convertPPTX(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	main := z.mainPart("ppt/presentation.xml")
	pres, err := z.read(main)
	if err != nil {
		return "", err
	}
	rels := z.rels(main)
	var slides []string
	d := newXMLDecoder(bytes.NewReader(pres))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sldId" {
			if r, ok := rels[relID(se, "id")]; ok && !r.External {
				slides = append(slides, r.Target)
			}
		}
	}
	if len(slides) == 0 {
		slides = z.names("ppt/slides/slide", ".xml")
	}
	var w mdWriter
	for i, part := range slides {
		if w.full() {
			break
		}
		b, err := z.read(part)
		if err != nil {
			continue
		}
		s := readDrawingML(b)
		title := fmt.Sprintf(i18n.T("幻灯片 %d"), i+1)
		if s.title != "" {
			title += " · " + s.title
		}
		w.heading(2, title)
		writeShapes(&w, s)
		if notes := z.relOfType(part, "/notesSlide", ""); notes != "" {
			if nb, err := z.read(notes); err == nil {
				n := readDrawingML(nb)
				var lines []string
				for _, p := range n.body {
					if p.ph == "body" {
						lines = append(lines, p.text)
					}
				}
				if len(lines) > 0 {
					w.raw("> **" + i18n.T("备注") + "**\\\n> " + strings.Join(mapStrings(lines, escapeInlineMid), "\\\n> "))
				}
			}
		}
	}
	return w.String(), nil
}

func mapStrings(in []string, f func(string) string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = f(s)
	}
	return out
}

// drawingPara is a paragraph of a slide's shapes.
type drawingPara struct {
	text  string
	level int
	ph    string // the placeholder type of its shape
}

type drawingSlide struct {
	title  string
	body   []drawingPara
	tables [][][]string
	// order interleaves body paragraphs (-1-i) and tables (i).
	order []int
}

// readDrawingML reads the text of a PresentationML slide or notes part.
func readDrawingML(data []byte) drawingSlide {
	var s drawingSlide
	var ph string
	var para strings.Builder
	level := 0
	inPara := false
	var tables []*tableBuild
	var stack []string
	d := newXMLDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			switch t.Name.Local {
			case "sp":
				ph = ""
			case "ph":
				ph = xmlAttr(t, "type")
				if ph == "" {
					ph = "body"
				}
			case "p":
				if len(stack) > 1 && stack[len(stack)-2] == "txBody" {
					inPara, level = true, 0
					para.Reset()
				}
			case "pPr":
				if inPara {
					level, _ = strconv.Atoi(xmlAttr(t, "lvl"))
				}
			case "br":
				if inPara {
					para.WriteString(" ")
				}
			case "tbl":
				tables = append(tables, &tableBuild{})
			case "tr":
				if tb := lastTable(tables); tb != nil {
					tb.row = nil
				}
			case "tc":
				if tb := lastTable(tables); tb != nil {
					tb.cell = nil
				}
			}
		case xml.CharData:
			if inPara && len(stack) > 0 && stack[len(stack)-1] == "t" {
				para.Write(t)
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			switch t.Name.Local {
			case "p":
				if !inPara {
					continue
				}
				inPara = false
				text := oneLine(para.String())
				if text == "" {
					continue
				}
				if tb := lastTable(tables); tb != nil {
					tb.cell = append(tb.cell, text)
					continue
				}
				switch ph {
				case "title", "ctrTitle":
					if s.title == "" {
						s.title = text
					} else {
						s.title += " " + text
					}
				case "sldNum", "dt", "ftr", "sldImg", "hdr":
				default:
					s.order = append(s.order, -1-len(s.body))
					s.body = append(s.body, drawingPara{text, min(max(level, 0), 8), ph})
				}
			case "tc":
				if tb := lastTable(tables); tb != nil {
					tb.row = append(tb.row, strings.Join(tb.cell, "\n"))
				}
			case "tr":
				if tb := lastTable(tables); tb != nil {
					tb.rows = append(tb.rows, tb.row)
				}
			case "tbl":
				if len(tables) > 0 {
					tb := tables[len(tables)-1]
					tables = tables[:len(tables)-1]
					s.order = append(s.order, len(s.tables))
					s.tables = append(s.tables, tb.rows)
				}
			}
		}
	}
	return s
}

func writeShapes(w *mdWriter, s drawingSlide) {
	for _, o := range s.order {
		if o >= 0 {
			w.table(s.tables[o])
			continue
		}
		p := s.body[-1-o]
		w.listItem(strings.Repeat("  ", p.level) + "- " + escapeInlineMid(p.text))
	}
}
