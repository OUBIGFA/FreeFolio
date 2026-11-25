package formats

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/pdfdoc"
)

// placed is text at a position of a fixed page.
type placed struct {
	x, y, size float64
	text       string
}

// pageLines orders a page's text top to bottom, left to right, joining
// pieces on one baseline into a line.
func pageLines(items []placed) []string {
	sort.SliceStable(items, func(i, j int) bool {
		if d := items[i].y - items[j].y; d < -tolerance(items[i], items[j]) || d > tolerance(items[i], items[j]) {
			return items[i].y < items[j].y
		}
		return items[i].x < items[j].x
	})
	var lines []string
	var cur strings.Builder
	lastY, lastEnd := 0.0, 0.0
	for i, it := range items {
		if strings.TrimSpace(it.text) == "" {
			continue
		}
		if i > 0 && cur.Len() > 0 && (it.y-lastY > tolerance(it, it) || lastY-it.y > tolerance(it, it)) {
			lines = append(lines, cur.String())
			cur.Reset()
		} else if cur.Len() > 0 && it.x-lastEnd > it.size*0.8 {
			cur.WriteString(" ")
		}
		cur.WriteString(it.text)
		lastY = it.y
		lastEnd = it.x + float64(len([]rune(it.text)))*it.size*0.55
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

func tolerance(a, b placed) float64 {
	return max(max(a.size, b.size)*0.4, 0.8)
}

func floatAttr(se xml.StartElement, name string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(xmlAttr(se, name)), 64)
	return f
}

// boundary reads an OFD "x y w h" box.
func boundary(s string) (x, y float64) {
	f := strings.Fields(s)
	if len(f) >= 2 {
		x, _ = strconv.ParseFloat(f[0], 64)
		y, _ = strconv.ParseFloat(f[1], 64)
	}
	return x, y
}

// ---- OFD (GB/T 33190) ----

func convertOFD(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	root, err := z.read("OFD.xml")
	if err != nil {
		return "", err
	}
	// OFD.xml names each document's Document.xml (DocRoot).
	var docRoots []string
	var title string
	var stack []string
	d := newXMLDecoder(bytes.NewReader(root))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
		case xml.CharData:
			if n := len(stack); n > 0 {
				switch stack[n-1] {
				case "DocRoot":
					docRoots = append(docRoots, strings.TrimPrefix(strings.TrimSpace(string(t)), "/"))
				case "Title":
					if title == "" {
						title = oneLine(string(t))
					}
				}
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(docRoots) == 0 {
		docRoots = []string{"Doc_0/Document.xml"}
	}
	var w mdWriter
	w.heading(1, title)
	pageNo := 0
	for _, dr := range docRoots {
		docXML, err := z.read(dr)
		if err != nil {
			continue
		}
		base := path.Dir(dr)
		var pages []string
		d := newXMLDecoder(bytes.NewReader(docXML))
		for {
			tok, err := d.Token()
			if err != nil {
				break
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Page" {
				if loc := xmlAttr(se, "BaseLoc"); loc != "" {
					if strings.HasPrefix(loc, "/") {
						pages = append(pages, strings.TrimPrefix(loc, "/"))
					} else {
						pages = append(pages, path.Join(base, loc))
					}
				}
			}
		}
		if len(pages) == 0 {
			pages = z.names(base+"/Pages/", "content.xml")
		}
		for _, p := range pages {
			if w.full() {
				break
			}
			pageNo++
			b, err := z.read(p)
			if err != nil {
				continue
			}
			w.heading(2, fmt.Sprintf(i18n.T("第 %d 页"), pageNo))
			lines := pageLines(ofdPageText(b))
			if len(lines) == 0 {
				w.para(i18n.T("（此页没有文字）"))
				continue
			}
			for _, l := range lines {
				w.para(l)
			}
		}
	}
	return w.String(), nil
}

func ofdPageText(page []byte) []placed {
	var out []placed
	d := newXMLDecoder(bytes.NewReader(page))
	var objX, objY, size float64
	var codeX, codeY float64
	inCode := false
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "TextObject":
				objX, objY = boundary(xmlAttr(t, "Boundary"))
				size = floatAttr(t, "Size")
				if size <= 0 {
					size = 3.5
				}
			case "TextCode":
				inCode = true
				codeX, codeY = floatAttr(t, "X"), floatAttr(t, "Y")
			}
		case xml.CharData:
			if inCode {
				out = append(out, placed{objX + codeX, objY + codeY, size, string(t)})
			}
		case xml.EndElement:
			if t.Name.Local == "TextCode" {
				inCode = false
			}
		}
	}
	return out
}

// ---- XPS and OpenXPS ----

func convertXPS(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	pages := z.names("", ".fpage")
	if len(pages) == 0 {
		return "", ErrUnsupported
	}
	var w mdWriter
	for i, p := range pages {
		if w.full() {
			break
		}
		b, err := z.read(p)
		if err != nil {
			continue
		}
		w.heading(2, fmt.Sprintf(i18n.T("第 %d 页"), i+1))
		var items []placed
		d := newXMLDecoder(bytes.NewReader(b))
		for {
			tok, err := d.Token()
			if err != nil {
				break
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Glyphs" {
				if s := xmlAttr(se, "UnicodeString"); s != "" {
					s = strings.TrimPrefix(s, "{}")
					size := floatAttr(se, "FontRenderingEmSize")
					if size <= 0 {
						size = 12
					}
					items = append(items, placed{floatAttr(se, "OriginX"), floatAttr(se, "OriginY"), size, s})
				}
			}
		}
		lines := pageLines(items)
		if len(lines) == 0 {
			w.para(i18n.T("（此页没有文字）"))
			continue
		}
		for _, l := range lines {
			w.para(l)
		}
	}
	return w.String(), nil
}

// ---- Apple iWork ----

// convertIWork reads the text of the preview PDF an iWork package keeps;
// the documents themselves are protobuf archives Apple does not document.
func convertIWork(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", fmt.Errorf("%w (%s)", err, i18n.T("文件夹形式的 iWork 文稿请先压缩后再打开"))
	}
	pdf, err := z.read("QuickLook/Preview.pdf")
	if err != nil {
		pdf, err = z.read("preview.pdf")
	}
	if err != nil {
		return "", fmt.Errorf("formats: %s", i18n.T("此 iWork 文稿没有内嵌预览，请在 Pages、Numbers 或 Keynote 中导出为 PDF 或 Office 格式"))
	}
	doc, err := pdfdoc.OpenBytes(pdf)
	if err != nil {
		return "", err
	}
	defer doc.Close()
	var w mdWriter
	w.raw("> " + i18n.T("以下内容取自文稿内嵌的预览，可能不完整"))
	for i := range min(doc.PageCount(), 500) {
		if w.full() {
			break
		}
		w.heading(2, fmt.Sprintf(i18n.T("第 %d 页"), i+1))
		w.para(doc.PageText(i))
	}
	return w.String(), nil
}
