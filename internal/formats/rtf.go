package formats

import (
	"strconv"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// codePage maps a Windows code page to its encoding; nil is Windows-1252.
func codePage(cp int) encoding.Encoding {
	switch cp {
	case 936, 54936:
		return simplifiedchinese.GB18030
	case 950:
		return traditionalchinese.Big5
	case 932:
		return japanese.ShiftJIS
	case 949:
		return korean.EUCKR
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	case 1253:
		return charmap.Windows1253
	case 1254:
		return charmap.Windows1254
	case 1255:
		return charmap.Windows1255
	case 1256:
		return charmap.Windows1256
	case 1257:
		return charmap.Windows1257
	case 1258:
		return charmap.Windows1258
	case 874:
		return charmap.Windows874
	case 437:
		return charmap.CodePage437
	case 850:
		return charmap.CodePage850
	case 866:
		return charmap.CodePage866
	}
	return charmap.Windows1252
}

// charsetCodePage maps an RTF \fcharsetN to a code page.
func charsetCodePage(cs int) int {
	switch cs {
	case 128:
		return 932
	case 129:
		return 949
	case 134:
		return 936
	case 136:
		return 950
	case 161:
		return 1253
	case 162:
		return 1254
	case 177:
		return 1255
	case 178:
		return 1256
	case 186:
		return 1257
	case 204:
		return 1251
	case 222:
		return 874
	case 238:
		return 1250
	}
	return 0
}

// rtfDestinations are groups whose text is not the document's.
var rtfDestinations = map[string]bool{
	"fonttbl": true, "colortbl": true, "stylesheet": true, "info": true, "pict": true, "object": true,
	"header": true, "headerl": true, "headerr": true, "headerf": true, "footer": true, "footerl": true,
	"footerr": true, "footerf": true, "footnote": true, "annotation": true, "atnid": true, "atnauthor": true,
	"fldinst": true, "themedata": true, "colorschememapping": true, "latentstyles": true, "datastore": true,
	"xmlnstbl": true, "listtable": true, "listoverridetable": true, "rsidtbl": true, "generator": true,
	"mmathPr": true, "pgdsctbl": true, "filetbl": true, "revtbl": true, "bkmkstart": true, "bkmkend": true,
	"shppict": true, "nonshppict": true, "blipuid": true, "wgrffmtfilter": true, "listtext": true,
	"pntext": true, "pntxta": true, "pntxtb": true, "template": true, "userprops": true, "docvar": true,
}

type rtfState struct {
	skip    bool
	uc      int // characters to skip after \uN
	font    int
	bold    bool
	italic  bool
	strike  bool
	inTable bool
}

func convertRTF(data []byte) (string, error) {
	s := string(data)
	if !strings.HasPrefix(strings.TrimLeft(s, " \r\n\t"), "{\\rtf") {
		return "", ErrUnsupported
	}
	var w mdWriter
	docCP := 1252
	fontCP := map[int]int{}
	var stack []rtfState
	st := rtfState{uc: 1}
	var spans []span
	var pending []byte // bytes of \'hh escapes awaiting decoding
	var row []string
	var rows [][]string
	skipChars := 0
	fontTable := false
	fontDepth := 0
	curFont := -1

	cpOf := func() int {
		if cp, ok := fontCP[st.font]; ok && cp != 0 {
			return cp
		}
		return docCP
	}
	flushBytes := func() {
		if len(pending) == 0 {
			return
		}
		out, err := codePage(cpOf()).NewDecoder().Bytes(pending)
		if err != nil {
			out = []byte(latin1(pending))
		}
		pending = pending[:0]
		if !st.skip {
			spans = append(spans, span{text: string(out), bold: st.bold, italic: st.italic, strike: st.strike})
		}
	}
	emit := func(text string) {
		flushBytes()
		if st.skip || text == "" {
			return
		}
		spans = append(spans, span{text: text, bold: st.bold, italic: st.italic, strike: st.strike})
	}
	endPara := func() {
		flushBytes()
		md := spansMarkdown(spans)
		spans = nil
		if st.inTable {
			if len(row) == 0 {
				row = append(row, "")
			}
			if md != "" {
				if row[len(row)-1] != "" {
					row[len(row)-1] += "\n"
				}
				row[len(row)-1] += plainUnescape(md)
			}
			return
		}
		if len(rows) > 0 {
			w.table(rows)
			rows = nil
		}
		if md != "" {
			w.block(lineStart(md))
		}
	}

	for i := 0; i < len(s) && !w.full(); {
		c := s[i]
		switch c {
		case '{':
			flushBytes()
			stack = append(stack, st)
			i++
			if strings.HasPrefix(s[i:], "\\*") {
				st.skip = true
				i += 2
			}
		case '}':
			flushBytes()
			if fontTable && len(stack) == fontDepth {
				fontTable = false
			}
			if n := len(stack); n > 0 {
				inTable := st.inTable
				st = stack[n-1]
				st.inTable = st.inTable || inTable
				stack = stack[:n-1]
			}
			i++
		case '\\':
			i++
			if i >= len(s) {
				break
			}
			d := s[i]
			switch {
			case d == '\'':
				if i+2 < len(s) {
					if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
						if skipChars > 0 {
							skipChars--
						} else if !st.skip {
							pending = append(pending, byte(v))
						}
					}
				}
				i += 3
			case d == '\\' || d == '{' || d == '}':
				emit(string(d))
				i++
			case d == '~':
				emit("\u00a0")
				i++
			case d == '_':
				emit("-")
				i++
			case d == '-':
				i++
			case d == '\n' || d == '\r':
				endPara()
				i++
			case d >= 'a' && d <= 'z' || d >= 'A' && d <= 'Z':
				j := i
				for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z') {
					j++
				}
				word := s[i:j]
				k := j
				if k < len(s) && s[k] == '-' {
					k++
				}
				for k < len(s) && s[k] >= '0' && s[k] <= '9' {
					k++
				}
				param, hasParam := 0, k > j
				if hasParam {
					param, _ = strconv.Atoi(s[j:k])
				}
				if k < len(s) && s[k] == ' ' {
					k++
				}
				i = k
				if word != "u" && word != "uc" {
					flushBytes()
				}
				switch word {
				case "ansicpg":
					docCP = param
				case "fonttbl":
					fontTable, fontDepth = true, len(stack)
					st.skip = true
				case "f":
					if fontTable {
						curFont = param
					} else {
						st.font = param
					}
				case "fcharset":
					if fontTable && curFont >= 0 {
						fontCP[curFont] = charsetCodePage(param)
					}
				case "cpg":
					if fontTable && curFont >= 0 {
						fontCP[curFont] = param
					}
				case "uc":
					st.uc = max(param, 0)
				case "u":
					flushBytes()
					if param < 0 {
						param += 65536
					}
					emit(string(rune(param)))
					skipChars = st.uc
				case "par", "sect", "page":
					endPara()
				case "line":
					emit("\n")
				case "tab":
					emit(" ")
				case "emdash":
					emit("—")
				case "endash":
					emit("–")
				case "bullet":
					emit("•")
				case "lquote":
					emit("‘")
				case "rquote":
					emit("’")
				case "ldblquote":
					emit("“")
				case "rdblquote":
					emit("”")
				case "b":
					st.bold = !hasParam || param != 0
				case "i":
					st.italic = !hasParam || param != 0
				case "strike":
					st.strike = !hasParam || param != 0
				case "plain":
					st.bold, st.italic, st.strike = false, false, false
				case "intbl":
					st.inTable = true
				case "pard":
					st.inTable = false
				case "cell":
					endPara()
					if len(row) == 0 {
						row = append(row, "")
					}
					row = append(row, "")
				case "row":
					endPara()
					if n := len(row); n > 0 && row[n-1] == "" {
						row = row[:n-1]
					}
					rows = append(rows, row)
					row = nil
					st.inTable = false
				case "bin":
					i = min(i+max(param, 0), len(s))
				default:
					if rtfDestinations[word] {
						st.skip = true
					}
				}
			default:
				i++
			}
		case '\r', '\n':
			i++
		default:
			j := i
			for j < len(s) && s[j] != '\\' && s[j] != '{' && s[j] != '}' && s[j] != '\r' && s[j] != '\n' {
				j++
			}
			chunk := s[i:j]
			i = j
			if skipChars > 0 {
				n := min(skipChars, len(chunk))
				chunk = chunk[n:]
				skipChars -= n
			}
			if fontTable || st.skip || chunk == "" {
				continue
			}
			pending = append(pending, chunk...)
		}
		if c != '\\' {
			skipChars = 0
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	st.inTable = false
	endPara()
	if len(rows) > 0 {
		w.table(rows)
	}
	return w.String(), nil
}

// plainUnescape undoes inline escaping, for text going into a table cell,
// which escapes again.
func plainUnescape(md string) string {
	var b strings.Builder
	for i := 0; i < len(md); i++ {
		if md[i] == '\\' && i+1 < len(md) {
			if md[i+1] == '\n' {
				b.WriteByte('\n')
				i++
				continue
			}
			if strings.IndexByte("\\*_[]<>|`$~#-+=.)", md[i+1]) >= 0 {
				i++
			}
		}
		b.WriteByte(md[i])
	}
	return b.String()
}
