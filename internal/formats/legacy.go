package formats

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/richardlehane/mscfb"
	"golang.org/x/text/encoding/charmap"
)

// ErrEncrypted reports a password-protected file.
var ErrEncrypted = errors.New("formats: the file is encrypted; remove its password in the program that made it")

var (
	zipMagic = []byte("PK\x03\x04")
	oleMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
)

// oleStreams reads the top-level streams of a compound file by name, each
// bounded by maxZipEntry.
func oleStreams(data []byte, want ...string) (map[string][]byte, error) {
	if !bytes.HasPrefix(data, oleMagic) {
		return nil, ErrUnsupported
	}
	r, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("formats: not a valid compound file: %w", err)
	}
	out := map[string][]byte{}
	for entry, err := r.Next(); err == nil; entry, err = r.Next() {
		if entry.Name == "EncryptedPackage" || entry.Name == "EncryptionInfo" {
			return nil, ErrEncrypted
		}
		if len(entry.Path) != 0 || entry.Size <= 0 {
			continue
		}
		for _, w := range want {
			if entry.Name == w {
				if entry.Size > maxZipEntry {
					return nil, ErrTooLarge
				}
				b, err := io.ReadAll(io.LimitReader(entry, entry.Size))
				if err != nil {
					return nil, err
				}
				out[w] = b
			}
		}
	}
	return out, nil
}

func u16(b []byte, off int) int {
	if off < 0 || off+2 > len(b) {
		return 0
	}
	return int(binary.LittleEndian.Uint16(b[off:]))
}

func u32(b []byte, off int) int {
	if off < 0 || off+4 > len(b) {
		return 0
	}
	return int(binary.LittleEndian.Uint32(b[off:]))
}

// utf16LE decodes little-endian UTF-16.
func utf16LE(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// cp1252 decodes Windows-1252, what Word's compressed text is in.
func cp1252(b []byte) string {
	out, err := charmap.Windows1252.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// ---- Word 97-2003 and WPS Writer ----

func convertLegacyWord(data []byte) (string, error) {
	if bytes.HasPrefix(data, zipMagic) {
		return convertDOCX(data)
	}
	if bytes.HasPrefix(bytes.TrimLeft(data[:min(len(data), 16)], " \r\n\t"), []byte("{\\rtf")) {
		return convertRTF(data)
	}
	st, err := oleStreams(data, "WordDocument", "0Table", "1Table")
	if err != nil {
		return "", err
	}
	wd := st["WordDocument"]
	if len(wd) < 0x1AA || u16(wd, 0) != 0xA5EC {
		return "", ErrUnsupported
	}
	flags := u16(wd, 0x0A)
	if flags&0x0100 != 0 { // fEncrypted
		return "", ErrEncrypted
	}
	table := st["0Table"]
	if flags&0x0200 != 0 { // fWhichTblStm
		table = st["1Table"]
	}
	ccpText := u32(wd, 76)
	fcClx, lcbClx := u32(wd, 418), u32(wd, 422)
	text, err := wordPieces(wd, table, fcClx, lcbClx, ccpText)
	if err != nil {
		return "", err
	}
	return wordTextMarkdown(text), nil
}

// wordPieces reads the main text through the piece table of the Clx.
func wordPieces(wd, table []byte, fc, lcb, ccp int) (string, error) {
	if fc < 0 || lcb <= 0 || fc+lcb > len(table) {
		return "", ErrUnsupported
	}
	clx := table[fc : fc+lcb]
	i := 0
	for i < len(clx) && clx[i] == 0x01 { // Prc: skip property modifiers
		i += 3 + u16(clx, i+1)
	}
	if i+5 > len(clx) || clx[i] != 0x02 {
		return "", ErrUnsupported
	}
	plcLen := u32(clx, i+1)
	plc := clx[i+5:]
	if plcLen > len(plc) || plcLen < 4 {
		return "", ErrUnsupported
	}
	plc = plc[:plcLen]
	n := (plcLen - 4) / 12
	var b strings.Builder
	total := 0
	for k := 0; k < n && total < ccp; k++ {
		cpStart, cpEnd := u32(plc, 4*k), u32(plc, 4*(k+1))
		count := cpEnd - cpStart
		if count <= 0 || count > len(wd) {
			continue
		}
		count = min(count, ccp-total)
		pcd := 4*(n+1) + 8*k
		fcRaw := u32(plc, pcd+2)
		if fcRaw&0x40000000 != 0 {
			off := (fcRaw &^ 0x40000000) / 2
			if off < 0 || off+count > len(wd) {
				continue
			}
			b.WriteString(cp1252(wd[off : off+count]))
		} else {
			if fcRaw < 0 || fcRaw+2*count > len(wd) {
				continue
			}
			b.WriteString(utf16LE(wd[fcRaw : fcRaw+2*count]))
		}
		total += count
		if b.Len() > maxOutput {
			break
		}
	}
	return b.String(), nil
}

// wordTextMarkdown turns Word's text stream, with its control characters,
// into paragraphs and tables.
func wordTextMarkdown(text string) string {
	var w mdWriter
	var para strings.Builder
	var row, cells []string
	var rows [][]string
	field := 0 // depth of field codes being skipped
	flushPara := func() string {
		s := strings.TrimSpace(para.String())
		para.Reset()
		return s
	}
	flushTable := func() {
		if len(rows) > 0 {
			w.table(rows)
			rows = nil
		}
	}
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case 0x13: // field begin: skip its code up to the separator
			field++
			continue
		case 0x14:
			if field > 0 {
				field--
			}
			continue
		case 0x15:
			if field > 0 {
				field--
			}
			continue
		}
		if field > 0 {
			continue
		}
		switch r {
		case '\r':
			if len(cells) > 0 || len(row) > 0 {
				cells = append(cells, flushPara())
				continue
			}
			flushTable()
			w.para(flushPara())
		case 0x07: // cell end; two in a row end the row
			cells = append(cells, flushPara())
			cell := strings.TrimSpace(strings.Join(cells, "\n"))
			cells = nil
			if i+1 < len(runes) && runes[i+1] == 0x07 || cell == "" && len(row) > 0 && i > 0 && runes[i-1] == 0x07 {
				if cell != "" {
					row = append(row, cell)
				}
				if i+1 < len(runes) && runes[i+1] == 0x07 {
					i++
				}
				rows = append(rows, row)
				row = nil
				continue
			}
			row = append(row, cell)
		case 0x0B:
			para.WriteByte('\n')
		case 0x0C, 0x0E:
			flushTable()
			w.para(flushPara())
		case '\t':
			para.WriteByte(' ')
		case 0x1E:
			para.WriteByte('-')
		case 0x1F, 0x01, 0x02, 0x03, 0x04, 0x05, 0x08:
		case 0xA0:
			para.WriteByte(' ')
		default:
			if r >= 0x20 {
				para.WriteRune(r)
			}
		}
		if w.full() {
			break
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	flushTable()
	w.para(flushPara())
	return w.String()
}

// ---- Excel 97-2003 and WPS Spreadsheets ----

type biffSheet struct {
	name        string
	offset      int
	visibleKind int // 0 worksheet
}

func convertLegacyExcel(data []byte) (string, error) {
	if bytes.HasPrefix(data, zipMagic) {
		return convertXLSX(data)
	}
	st, err := oleStreams(data, "Workbook", "Book")
	if err != nil {
		return "", err
	}
	wb := st["Workbook"]
	if wb == nil {
		wb = st["Book"]
	}
	if len(wb) < 8 {
		return "", ErrUnsupported
	}
	var sheets []biffSheet
	var sst []string
	// The globals substream, up to its EOF.
	pos := 0
	for pos+4 <= len(wb) {
		typ, size := u16(wb, pos), u16(wb, pos+2)
		body := pos + 4
		if body+size > len(wb) {
			break
		}
		switch typ {
		case 0x002F: // FILEPASS
			return "", ErrEncrypted
		case 0x0085: // BOUNDSHEET8
			if size >= 8 {
				name, _ := biffShortString(wb[body+6 : body+size])
				sheets = append(sheets, biffSheet{name: name, offset: u32(wb, body), visibleKind: int(wb[body+5])})
			}
		case 0x00FC: // SST and its CONTINUE records
			chunks := [][]byte{wb[body : body+size]}
			next := body + size
			for next+4 <= len(wb) && u16(wb, next) == 0x003C {
				n := u16(wb, next+2)
				if next+4+n > len(wb) {
					break
				}
				chunks = append(chunks, wb[next+4:next+4+n])
				next += 4 + n
			}
			sst = biffSST(chunks)
		}
		pos = body + size
		if typ == 0x000A { // EOF of the globals
			break
		}
	}
	var w mdWriter
	for _, sh := range sheets {
		if w.full() {
			break
		}
		if sh.visibleKind != 0 || sh.offset <= 0 || sh.offset >= len(wb) {
			continue // chart and macro sheets hold no cells
		}
		rows, more := biffRows(wb, sh.offset, sst)
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

// biffShortString reads a ShortXLUnicodeString: a byte count and a flags
// byte, then the characters.
func biffShortString(b []byte) (string, int) {
	if len(b) < 2 {
		return "", len(b)
	}
	n, wide := int(b[0]), b[1]&1 != 0
	if wide {
		end := min(2+2*n, len(b))
		return utf16LE(b[2:end]), end
	}
	end := min(2+n, len(b))
	return latin1(b[2:end]), end
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// biffSST reads the shared strings. A string's characters may continue in
// the next chunk, which then starts with a fresh flags byte.
func biffSST(chunks [][]byte) []string {
	if len(chunks) == 0 || len(chunks[0]) < 8 {
		return nil
	}
	unique := u32(chunks[0], 4)
	ci, pos := 0, 8
	cur := chunks[0]
	nextChunk := func() bool {
		ci++
		if ci >= len(chunks) {
			return false
		}
		cur, pos = chunks[ci], 0
		return true
	}
	need := func(n int) bool {
		for pos+n > len(cur) {
			if pos < len(cur) || !nextChunk() {
				return pos+n <= len(cur)
			}
		}
		return true
	}
	skip := func(n int) {
		for n > 0 {
			if pos >= len(cur) && !nextChunk() {
				return
			}
			step := min(n, len(cur)-pos)
			pos += step
			n -= step
		}
	}
	out := make([]string, 0, min(unique, 1<<20))
	for len(out) < unique && len(out) < 1<<20 {
		if !need(3) {
			break
		}
		count := u16(cur, pos)
		flags := cur[pos+2]
		pos += 3
		runs, ext := 0, 0
		if flags&0x08 != 0 {
			if !need(2) {
				break
			}
			runs = u16(cur, pos)
			pos += 2
		}
		if flags&0x04 != 0 {
			if !need(4) {
				break
			}
			ext = u32(cur, pos)
			pos += 4
		}
		wide := flags&1 != 0
		var sb strings.Builder
		for left := count; left > 0; {
			if pos >= len(cur) {
				if !nextChunk() || len(cur) == 0 {
					left = 0
					break
				}
				wide = cur[0]&1 != 0
				pos = 1
			}
			size := 1
			if wide {
				size = 2
			}
			n := min(left, (len(cur)-pos)/size)
			if n == 0 {
				pos = len(cur)
				continue
			}
			if wide {
				sb.WriteString(utf16LE(cur[pos : pos+2*n]))
			} else {
				sb.WriteString(latin1(cur[pos : pos+n]))
			}
			pos += n * size
			left -= n
		}
		skip(4*runs + ext)
		out = append(out, sb.String())
	}
	return out
}

// rkNumber decodes an RK value.
func rkNumber(rk uint32) float64 {
	var v float64
	if rk&2 != 0 {
		v = float64(int32(rk) >> 2)
	} else {
		v = math.Float64frombits(uint64(rk&0xFFFFFFFC) << 32)
	}
	if rk&1 != 0 {
		v /= 100
	}
	return v
}

func formatNumber(v float64) string {
	return excelNumber(strconv.FormatFloat(v, 'g', -1, 64))
}

// biffRows reads a worksheet substream from its BOF at offset.
func biffRows(wb []byte, offset int, sst []string) ([][]string, int) {
	cells := map[[2]int]string{}
	maxRow, maxCol := -1, -1
	more := 0
	set := func(r, c int, v string) {
		if c < 0 || c >= maxColumns || r < 0 {
			return
		}
		if r > maxRows {
			if v != "" && r > maxRow {
				more = max(more, r-maxRows)
			}
			return
		}
		cells[[2]int{r, c}] = v
		maxRow, maxCol = max(maxRow, r), max(maxCol, c)
	}
	pos := offset
	var formulaAt [2]int
	pendingString := false
	for pos+4 <= len(wb) {
		typ, size := u16(wb, pos), u16(wb, pos+2)
		body := pos + 4
		if body+size > len(wb) {
			break
		}
		b := wb[body : body+size]
		pos = body + size
		switch typ {
		case 0x000A: // EOF
			pos = len(wb)
		case 0x00FD: // LABELSST
			if size >= 10 {
				if i := u32(b, 6); i >= 0 && i < len(sst) {
					set(u16(b, 0), u16(b, 2), sst[i])
				}
			}
		case 0x0204: // LABEL
			if size >= 9 {
				n, wide := u16(b, 6), b[8]&1 != 0
				if wide {
					set(u16(b, 0), u16(b, 2), utf16LE(b[9:min(9+2*n, len(b))]))
				} else {
					set(u16(b, 0), u16(b, 2), latin1(b[9:min(9+n, len(b))]))
				}
			}
		case 0x0203: // NUMBER
			if size >= 14 {
				set(u16(b, 0), u16(b, 2), formatNumber(math.Float64frombits(binary.LittleEndian.Uint64(b[6:]))))
			}
		case 0x027E: // RK
			if size >= 10 {
				set(u16(b, 0), u16(b, 2), formatNumber(rkNumber(binary.LittleEndian.Uint32(b[6:]))))
			}
		case 0x00BD: // MULRK
			if size >= 6 {
				row, first := u16(b, 0), u16(b, 2)
				for k := 0; 4+6*k+6 <= size-2; k++ {
					set(row, first+k, formatNumber(rkNumber(binary.LittleEndian.Uint32(b[4+6*k+2:]))))
				}
			}
		case 0x0205: // BOOLERR
			if size >= 8 {
				v := "#ERR"
				if b[7] == 0 {
					v = "FALSE"
					if b[6] != 0 {
						v = "TRUE"
					}
				}
				set(u16(b, 0), u16(b, 2), v)
			}
		case 0x0006: // FORMULA, its cached value
			if size >= 14 {
				row, col := u16(b, 0), u16(b, 2)
				if b[12] == 0xFF && b[13] == 0xFF {
					switch b[6] {
					case 0: // a string, in the next STRING record
						formulaAt, pendingString = [2]int{row, col}, true
					case 1:
						v := "FALSE"
						if b[8] != 0 {
							v = "TRUE"
						}
						set(row, col, v)
					case 2:
						set(row, col, "#ERR")
					}
				} else {
					set(row, col, formatNumber(math.Float64frombits(binary.LittleEndian.Uint64(b[6:]))))
				}
			}
		case 0x0207: // STRING
			if pendingString && size >= 3 {
				n, wide := u16(b, 0), b[2]&1 != 0
				v := ""
				if wide {
					v = utf16LE(b[3:min(3+2*n, len(b))])
				} else {
					v = latin1(b[3:min(3+n, len(b))])
				}
				set(formulaAt[0], formulaAt[1], v)
			}
			pendingString = false
		}
	}
	if maxRow < 0 {
		return nil, 0
	}
	rows := make([][]string, maxRow+1)
	for r := range rows {
		rows[r] = make([]string, maxCol+1)
	}
	for k, v := range cells {
		rows[k[0]][k[1]] = v
	}
	return rows, more
}

// ---- PowerPoint 97-2003 and WPS Presentation ----

type pptSlide struct {
	title string
	body  []string
	notes []string
}

func convertLegacyPowerPoint(data []byte) (string, error) {
	if bytes.HasPrefix(data, zipMagic) {
		return convertPPTX(data)
	}
	st, err := oleStreams(data, "PowerPoint Document")
	if err != nil {
		return "", err
	}
	doc := st["PowerPoint Document"]
	if len(doc) < 8 {
		return "", ErrUnsupported
	}
	var slides []*pptSlide
	var listSlides []*pptSlide // from SlideListWithText, when slides hold no text
	var cur *pptSlide
	inNotes, inMaster, inList := false, false, false
	textType := -1
	var walk func(b []byte, depth int)
	walk = func(b []byte, depth int) {
		for pos := 0; pos+8 <= len(b); {
			verInst, typ, size := u16(b, pos), u16(b, pos+2), u32(b, pos+4)
			body := pos + 8
			if size < 0 || body+size > len(b) {
				return
			}
			rec := b[body : body+size]
			pos = body + size
			if verInst&0x0F == 0x0F && depth < 32 { // a container
				switch typ {
				case 0x03EE: // Slide
					cur = &pptSlide{}
					slides = append(slides, cur)
					walk(rec, depth+1)
					cur = nil
				case 0x03F0: // Notes
					inNotes = true
					walk(rec, depth+1)
					inNotes = false
				case 0x03F8, 0x0FD9: // MainMaster, Handout
					inMaster = true
					walk(rec, depth+1)
					inMaster = false
				case 0x0FF0: // SlideListWithText
					if verInst>>4 == 0 { // the slides' list, not masters' or notes'
						inList = true
						walk(rec, depth+1)
						inList = false
					}
				default:
					walk(rec, depth+1)
				}
				continue
			}
			switch typ {
			case 0x03F3: // SlidePersistAtom
				if inList {
					listSlides = append(listSlides, &pptSlide{})
				}
			case 0x0F9F: // TextHeaderAtom
				textType = u32(rec, 0)
			case 0x0FA0, 0x0FA8: // TextCharsAtom, TextBytesAtom
				if inMaster || inNotes && cur == nil && len(slides) == 0 {
					continue
				}
				text := ""
				if typ == 0x0FA0 {
					text = utf16LE(rec)
				} else {
					text = latin1(rec)
				}
				text = strings.NewReplacer("\r", "\n", "\v", "\n").Replace(text)
				var target *pptSlide
				switch {
				case inList && len(listSlides) > 0:
					target = listSlides[len(listSlides)-1]
				case inNotes && len(slides) > 0:
					// Notes follow their slides in the stream.
					slides[len(slides)-1].notes = append(slides[len(slides)-1].notes, splitLines(text)...)
					continue
				case cur != nil:
					target = cur
				}
				if target == nil {
					continue
				}
				if (textType == 0 || textType == 6) && target.title == "" {
					target.title = oneLine(text)
				} else {
					target.body = append(target.body, splitLines(text)...)
				}
				textType = -1
			}
		}
	}
	walk(doc, 0)
	hasText := false
	for _, s := range slides {
		hasText = hasText || s.title != "" || len(s.body) > 0
	}
	if !hasText && len(listSlides) > 0 {
		notes := make([][]string, len(slides))
		for i, s := range slides {
			notes[i] = s.notes
		}
		slides = listSlides
		for i := range slides {
			if i < len(notes) {
				slides[i].notes = notes[i]
			}
		}
	}
	var w mdWriter
	for i, s := range slides {
		if w.full() {
			break
		}
		title := fmt.Sprintf(i18n.T("幻灯片 %d"), i+1)
		if s.title != "" {
			title += " · " + s.title
		}
		w.heading(2, title)
		for _, line := range s.body {
			w.listItem("- " + escapeInlineMid(line))
		}
		if len(s.notes) > 0 {
			w.raw("> **" + i18n.T("备注") + "**\\\n> " + strings.Join(mapStrings(s.notes, escapeInlineMid), "\\\n> "))
		}
	}
	return w.String(), nil
}

func splitLines(text string) []string {
	var out []string
	for l := range strings.SplitSeq(text, "\n") {
		if l = oneLine(l); l != "" && l != "*" {
			out = append(out, l)
		}
	}
	return out
}
