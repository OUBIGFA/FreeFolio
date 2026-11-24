package formats

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"path"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

// ---- EPUB ----

func convertEPUB(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	if enc, err := z.read("META-INF/encryption.xml"); err == nil && epubDRM(enc) {
		return "", ErrEncrypted
	}
	opf := ""
	if c, err := z.read("META-INF/container.xml"); err == nil {
		d := newXMLDecoder(bytes.NewReader(c))
		for opf == "" {
			tok, err := d.Token()
			if err != nil {
				break
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "rootfile" {
				opf = xmlAttr(se, "full-path")
			}
		}
	}
	if opf == "" {
		if n := z.names("", ".opf"); len(n) > 0 {
			opf = n[0]
		}
	}
	pkg, err := z.read(opf)
	if err != nil {
		return "", err
	}
	dir := path.Dir(opf)
	type item struct{ href, media string }
	items := map[string]item{}
	var spine []string
	var title string
	var creators []string
	var stack []string
	d := newXMLDecoder(bytes.NewReader(pkg))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			switch t.Name.Local {
			case "item":
				href := xmlAttr(t, "href")
				if u, err := urlUnescape(href); err == nil {
					href = u
				}
				items[xmlAttr(t, "id")] = item{path.Join(dir, href), xmlAttr(t, "media-type")}
			case "itemref":
				spine = append(spine, xmlAttr(t, "idref"))
			}
		case xml.CharData:
			if n := len(stack); n > 1 && stack[n-2] == "metadata" {
				switch stack[n-1] {
				case "title":
					if title == "" {
						title = oneLine(string(t))
					}
				case "creator":
					if s := oneLine(string(t)); s != "" {
						creators = append(creators, s)
					}
				}
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	var w mdWriter
	w.heading(1, title)
	if len(creators) > 0 {
		w.fields([][2]string{{i18n.T("作者"), strings.Join(creators, ", ")}})
	}
	for _, id := range spine {
		if w.full() {
			break
		}
		it, ok := items[id]
		if !ok || !strings.Contains(it.media, "html") && !strings.HasSuffix(strings.ToLower(it.href), "html") && !strings.HasSuffix(strings.ToLower(it.href), ".htm") {
			continue
		}
		b, err := z.read(it.href)
		if err != nil {
			continue
		}
		htmlMarkdown(&w, decodeText(b))
	}
	return w.String(), nil
}

// epubDRM reports whether encryption.xml encrypts content, not only
// obfuscates fonts.
func epubDRM(enc []byte) bool {
	d := newXMLDecoder(bytes.NewReader(enc))
	for {
		tok, err := d.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "CipherReference" {
			u := strings.ToLower(xmlAttr(se, "URI"))
			if strings.HasSuffix(u, "html") || strings.HasSuffix(u, ".htm") || strings.HasSuffix(u, ".opf") {
				return true
			}
		}
	}
}

func urlUnescape(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, ok := unhex(s[i+1]); ok {
				if v2, ok := unhex(s[i+2]); ok {
					b.WriteByte(v<<4 | v2)
					i += 2
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// ---- Mobipocket and Kindle ----

func be16(b []byte, off int) int {
	if off < 0 || off+2 > len(b) {
		return 0
	}
	return int(binary.BigEndian.Uint16(b[off:]))
}

func be32(b []byte, off int) int {
	if off < 0 || off+4 > len(b) {
		return 0
	}
	return int(binary.BigEndian.Uint32(b[off:]))
}

func convertMOBI(data []byte) (string, error) {
	if len(data) < 78+8 {
		return "", ErrUnsupported
	}
	kind := string(data[60:68])
	if kind != "BOOKMOBI" && kind != "TEXtREAd" {
		return "", ErrUnsupported
	}
	n := be16(data, 76)
	if n < 2 || 78+8*n > len(data) {
		return "", ErrUnsupported
	}
	offsets := make([]int, n+1)
	for i := range n {
		offsets[i] = be32(data, 78+8*i)
	}
	offsets[n] = len(data)
	record := func(i int) []byte {
		if i < 0 || i >= n {
			return nil
		}
		a, b := offsets[i], offsets[i+1]
		if a < 0 || b > len(data) || a > b {
			return nil
		}
		return data[a:b]
	}
	r0 := record(0)
	if len(r0) < 16 {
		return "", ErrUnsupported
	}
	compression, textRecords, encryption := be16(r0, 0), be16(r0, 8), be16(r0, 12)
	if encryption != 0 {
		return "", ErrEncrypted
	}
	if compression != 1 && compression != 2 {
		return "", ErrUnsupported // HUFF/CDIC compression
	}
	utf8Text, title, extraFlags := false, "", 0
	if len(r0) >= 24 && string(r0[16:20]) == "MOBI" {
		headerLen := be32(r0, 20)
		utf8Text = be32(r0, 28) == 65001
		if off, l := be32(r0, 0x54), be32(r0, 0x58); off > 0 && l > 0 && off+l <= len(r0) {
			title = string(r0[off : off+l])
		}
		if headerLen >= 0xE4 && len(r0) >= 0xF4 {
			extraFlags = be16(r0, 0xF2)
		}
	}
	var text []byte
	for i := 1; i <= textRecords && i < n; i++ {
		rec := trimTrailing(record(i), extraFlags)
		if compression == 2 {
			rec = palmDecompress(rec)
		}
		text = append(text, rec...)
		if len(text) > maxZipEntry {
			break
		}
	}
	var s string
	if utf8Text {
		s = strings.ToValidUTF8(string(text), "\uFFFD")
	} else {
		s = cp1252(text)
		if !utf8Text {
			if utf8Title := decodeText([]byte(title)); utf8Title != "" {
				title = utf8Title
			}
		}
	}
	var w mdWriter
	w.heading(1, decodeText([]byte(title)))
	if kind == "TEXtREAd" {
		w.para(s)
	} else {
		htmlMarkdown(&w, s)
	}
	return w.String(), nil
}

// trimTrailing removes a record's trailing entries, as the extra data
// flags tell.
func trimTrailing(rec []byte, flags int) []byte {
	for bit := 1; bit < 16; bit++ {
		if flags&(1<<bit) == 0 || len(rec) == 0 {
			continue
		}
		// A backward-encoded size: 7 bits a byte, the high bit marks its
		// first byte.
		size, shift := 0, 0
		for i := len(rec) - 1; i >= 0 && i >= len(rec)-4; i-- {
			c := int(rec[i])
			size |= (c & 0x7F) << shift
			shift += 7
			if c&0x80 != 0 {
				break
			}
		}
		if size <= 0 || size > len(rec) {
			return rec
		}
		rec = rec[:len(rec)-size]
	}
	if flags&1 != 0 && len(rec) > 0 {
		m := int(rec[len(rec)-1]&3) + 1
		if m <= len(rec) {
			rec = rec[:len(rec)-m]
		}
	}
	return rec
}

// palmDecompress expands PalmDOC's LZ77 compression.
func palmDecompress(in []byte) []byte {
	out := make([]byte, 0, 4096)
	for i := 0; i < len(in); {
		c := in[i]
		i++
		switch {
		case c >= 1 && c <= 8:
			end := min(i+int(c), len(in))
			out = append(out, in[i:end]...)
			i = end
		case c < 0x80:
			out = append(out, c)
		case c >= 0xC0:
			out = append(out, ' ', c^0x80)
		default:
			if i >= len(in) {
				return out
			}
			v := (int(c)<<8 | int(in[i])) & 0x3FFF
			i++
			dist, length := v>>3, v&7+3
			if dist <= 0 || dist > len(out) {
				continue
			}
			for k := 0; k < length; k++ {
				out = append(out, out[len(out)-dist])
			}
		}
	}
	return out
}
