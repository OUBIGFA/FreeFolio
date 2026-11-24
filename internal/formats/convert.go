package formats

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// Convert reads data, the content of the file name, into Markdown. Only
// converted formats convert; the result is meant to be shown read-only or
// saved as a new .md file, never written over the original.
func Convert(name string, data []byte) (md string, err error) {
	f, ok := Lookup(name)
	if !ok || f.Mode != Converted {
		return "", ErrUnsupported
	}
	if len(data) > MaxConvertSize {
		return "", ErrTooLarge
	}
	defer func() {
		// Readers walk untrusted binary structures: a malformed file must
		// fail its conversion, never take the application down.
		if r := recover(); r != nil {
			md, err = "", fmt.Errorf("formats: %s: malformed file (%v)", f.Name, r)
		}
	}()
	if f.Plugin != "" {
		pluginMu.RLock()
		fn := pluginConvert
		pluginMu.RUnlock()
		if fn == nil {
			return "", fmt.Errorf("formats: the plugin %s is not running", f.Plugin)
		}
		return fn(f, name, data)
	}
	conv := converterFor(f.Ext)
	if conv == nil {
		return "", ErrUnsupported
	}
	return conv(data)
}

func converterFor(e string) func([]byte) (string, error) {
	switch e {
	case ".docx", ".docm", ".dotx", ".dotm":
		return convertDOCX
	case ".xlsx", ".xlsm", ".xltx", ".xltm":
		return convertXLSX
	case ".pptx", ".pptm", ".potx", ".potm", ".ppsx":
		return convertPPTX
	case ".doc", ".dot", ".wps", ".wpt":
		return convertLegacyWord
	case ".xls", ".xlt", ".et", ".ett":
		return convertLegacyExcel
	case ".ppt", ".pps", ".pot", ".dps", ".dpt":
		return convertLegacyPowerPoint
	case ".odt", ".ott", ".ods", ".ots", ".odp", ".otp":
		return convertODF
	case ".fodt", ".fods", ".fodp":
		return convertFlatODF
	case ".rtf":
		return convertRTF
	case ".abw":
		return convertAbiWord
	case ".pages", ".numbers", ".key":
		return convertIWork
	case ".ofd":
		return convertOFD
	case ".xps", ".oxps":
		return convertXPS
	case ".epub":
		return convertEPUB
	case ".mobi", ".azw", ".azw3", ".prc":
		return convertMOBI
	case ".fb2":
		return convertFB2
	case ".eml":
		return convertEML
	case ".msg":
		return convertMSG
	case ".ics", ".ical":
		return convertICS
	case ".vcf", ".vcard":
		return convertVCF
	case ".xmind":
		return convertXMind
	case ".mm":
		return convertFreeMind
	case ".opml":
		return convertOPML
	case ".drawio", ".dio":
		return convertDrawio
	case ".ipynb":
		return convertNotebook
	case ".sqlite", ".sqlite3", ".db", ".db3":
		return convertSQLite
	case ".zip", ".jar", ".apk", ".cbz":
		return convertZip
	case ".tar", ".cbt", ".tgz", ".gz", ".bz2":
		return convertTarLike
	}
	return nil
}

// Limits keep a conversion's output readable and its memory bounded.
const (
	maxRows     = 2000 // table rows per sheet or table
	maxColumns  = 64
	maxCell     = 2000 // characters of a cell
	maxOutput   = 8 << 20
	maxZipEntry = 64 << 20
)

// errStop ends a walk once maxOutput is reached.
var errStop = errors.New("formats: output limit reached")

// mdWriter builds Markdown.
type mdWriter struct {
	b         strings.Builder
	truncated bool
	// inList is set while list items follow one another (listItem); any
	// other block ends the list.
	inList bool
}

func (w *mdWriter) full() bool {
	if w.b.Len() >= maxOutput {
		w.truncated = true
		return true
	}
	return false
}

func (w *mdWriter) block(s string) {
	if w.full() {
		return
	}
	s = strings.TrimRight(s, " \t\n")
	if s == "" {
		return
	}
	w.inList = false
	if w.b.Len() > 0 {
		w.b.WriteString("\n\n")
	}
	w.b.WriteString(s)
}

// heading writes a heading of text, level 1 to 6.
func (w *mdWriter) heading(level int, text string) {
	text = oneLine(text)
	if text == "" {
		return
	}
	level = min(max(level, 1), 6)
	w.block(strings.Repeat("#", level) + " " + escapeInline(text))
}

// para writes text as paragraphs, one per non-empty line group.
func (w *mdWriter) para(text string) {
	for _, p := range splitParagraphs(text) {
		w.block(escapeLines(p))
	}
}

// raw writes Markdown as is.
func (w *mdWriter) raw(md string) { w.block(md) }

// bullets writes a list.
func (w *mdWriter) bullets(items []string) {
	var b strings.Builder
	for _, it := range items {
		if it = oneLine(it); it != "" {
			b.WriteString("- " + escapeInline(it) + "\n")
		}
	}
	w.block(b.String())
}

// fields writes "**key**: value" lines.
func (w *mdWriter) fields(pairs [][2]string) {
	var b strings.Builder
	for _, p := range pairs {
		if v := oneLine(p[1]); v != "" {
			b.WriteString("- **" + escapeInline(p[0]) + "**: " + escapeInline(v) + "\n")
		}
	}
	w.block(b.String())
}

// code writes a fenced code block.
func (w *mdWriter) code(lang, text string) {
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return
	}
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	w.block(fence + lang + "\n" + text + "\n" + fence)
}

// table writes rows as a GFM table, the first row its header.
func (w *mdWriter) table(rows [][]string) {
	rows = trimTable(rows)
	if len(rows) == 0 {
		return
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	cols = min(cols, maxColumns)
	var b strings.Builder
	line := func(r []string) {
		b.WriteString("|")
		for i := range cols {
			cell := ""
			if i < len(r) {
				cell = cellText(r[i])
			}
			b.WriteString(" " + cell + " |")
		}
		b.WriteString("\n")
	}
	line(rows[0])
	b.WriteString("|" + strings.Repeat(" --- |", cols) + "\n")
	for i, r := range rows[1:] {
		if i >= maxRows {
			b.WriteString("\n")
			w.block(b.String())
			w.para(fmt.Sprintf("… %d more rows", len(rows)-1-maxRows))
			return
		}
		line(r)
	}
	w.block(b.String())
}

// String returns the Markdown, noting a truncation.
func (w *mdWriter) String() string {
	s := w.b.String()
	if w.truncated {
		s += "\n\n> The file is too long to show in full; the rest was left out."
	}
	if s != "" {
		s += "\n"
	}
	return s
}

// trimTable drops empty trailing rows and columns.
func trimTable(rows [][]string) [][]string {
	for len(rows) > 0 && emptyRow(rows[len(rows)-1]) {
		rows = rows[:len(rows)-1]
	}
	last := -1
	for _, r := range rows {
		for i := len(r) - 1; i > last; i-- {
			if strings.TrimSpace(r[i]) != "" {
				last = i
				break
			}
		}
	}
	if last < 0 {
		return nil
	}
	out := make([][]string, len(rows))
	for i, r := range rows {
		if len(r) > last+1 {
			r = r[:last+1]
		}
		out[i] = r
	}
	return out
}

func emptyRow(r []string) bool {
	for _, c := range r {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func cellText(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxCell {
		s = string([]rune(s)[:maxCell]) + "…"
	}
	s = escapeInlineMid(s)
	s = strings.ReplaceAll(s, "\r\n", "<br>")
	return strings.ReplaceAll(s, "\n", "<br>")
}

// oneLine collapses whitespace runs, line breaks included, to one space.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// splitParagraphs splits text at blank lines, keeping single line breaks.
func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var out []string
	for p := range strings.SplitSeq(text, "\n\n") {
		var lines []string
		for l := range strings.SplitSeq(p, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				lines = append(lines, l)
			}
		}
		if len(lines) > 0 {
			out = append(out, strings.Join(lines, "\n"))
		}
	}
	return out
}

// escapeLines escapes each line of a paragraph, joining them with hard
// breaks so the lines stay lines.
func escapeLines(p string) string {
	lines := strings.Split(p, "\n")
	for i, l := range lines {
		lines[i] = escapeInline(l)
	}
	return strings.Join(lines, "\\\n")
}

// escapeInline makes text literal in Markdown.
func escapeInline(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '*', '_', '[', ']', '<', '>', '|', '`', '$', '~':
			b.WriteByte('\\')
		}
		if r == 0 || r < 32 && r != '\n' && r != '\t' {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if out != "" {
		switch out[0] {
		case '#', '-', '+', '=':
			out = "\\" + out
		}
		out = escapeListNumber(out)
	}
	return out
}

// escapeListNumber escapes "12. " or "3) " at the start of a line, which
// Markdown would read as an ordered list; "0.3" stays as it is.
func escapeListNumber(s string) string {
	i := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	if i <= 0 || i >= len(s) || s[i] != '.' && s[i] != ')' {
		return s
	}
	if i+1 < len(s) && s[i+1] != ' ' && s[i+1] != '\t' {
		return s
	}
	return s[:i] + "\\" + s[i:]
}

// DecodeText turns bytes of unknown encoding into UTF-8: a BOM wins, then
// valid UTF-8, then GB18030, which covers GBK and GB2312.
func DecodeText(b []byte) string { return decodeText(b) }

func decodeText(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return string(b[3:])
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}), bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		out, _, err := transform.Bytes(unicode.UTF16(unicode.BigEndian, unicode.UseBOM).NewDecoder(), b)
		if err == nil {
			return string(out)
		}
	case utf8.Valid(b):
		return string(b)
	}
	out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), b)
	if err != nil {
		return strings.ToValidUTF8(string(b), "\uFFFD")
	}
	return string(out)
}

// decodeCharset decodes b from the named charset, falling back to
// decodeText.
func decodeCharset(charset string, b []byte) string {
	if enc := charsetEncoding(charset); enc != nil {
		if out, _, err := transform.Bytes(enc.NewDecoder(), b); err == nil {
			return string(out)
		}
	}
	return decodeText(b)
}

func charsetEncoding(charset string) encoding.Encoding {
	cs := strings.ToLower(strings.TrimSpace(charset))
	switch cs {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return nil
	case "gb2312", "gbk", "cp936", "gb18030", "x-gbk":
		return simplifiedchinese.GB18030
	}
	enc, err := htmlindex.Get(cs)
	if err != nil {
		return nil
	}
	return enc
}

// newXMLDecoder reads XML leniently, in any charset it declares.
func newXMLDecoder(r io.Reader) *xml.Decoder {
	d := xml.NewDecoder(r)
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		enc := charsetEncoding(charset)
		if enc == nil {
			return input, nil
		}
		return transform.NewReader(input, enc.NewDecoder()), nil
	}
	return d
}

// zipFiles opens data as a ZIP archive, indexing its entries by name.
type zipArchive struct {
	r     *zip.Reader
	files map[string]*zip.File
}

func openZip(data []byte) (*zipArchive, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("formats: not a valid package: %w", err)
	}
	z := &zipArchive{r: r, files: make(map[string]*zip.File, len(r.File))}
	for _, f := range r.File {
		z.files[strings.TrimPrefix(path.Clean("/"+f.Name), "/")] = f
	}
	return z, nil
}

// read returns the entry name, case-insensitively when it is not found as
// spelled; a missing entry is fs.ErrNotExist.
func (z *zipArchive) read(name string) ([]byte, error) {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	f := z.files[name]
	if f == nil {
		for n, g := range z.files {
			if strings.EqualFold(n, name) {
				f = g
				break
			}
		}
	}
	if f == nil {
		return nil, fmt.Errorf("formats: %s: %w", name, errMissing)
	}
	if f.UncompressedSize64 > maxZipEntry {
		return nil, ErrTooLarge
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxZipEntry+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxZipEntry {
		return nil, ErrTooLarge
	}
	return b, nil
}

// has reports whether the archive has the entry.
func (z *zipArchive) has(name string) bool {
	_, ok := z.files[strings.TrimPrefix(path.Clean("/"+name), "/")]
	return ok
}

// names lists the entries under prefix, sorted naturally (sheet2 before
// sheet10).
func (z *zipArchive) names(prefix, suffix string) []string {
	var out []string
	for n := range z.files {
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(strings.ToLower(n), suffix) {
			out = append(out, n)
		}
	}
	sortNatural(out)
	return out
}

var errMissing = errors.New("missing from the package")

// sortNatural sorts names comparing digit runs by value.
func sortNatural(names []string) {
	less := func(a, b string) bool {
		for a != "" && b != "" {
			da, db := digitRun(a), digitRun(b)
			if da > 0 && db > 0 {
				na, nb := strings.TrimLeft(a[:da], "0"), strings.TrimLeft(b[:db], "0")
				if len(na) != len(nb) {
					return len(na) < len(nb)
				}
				if na != nb {
					return na < nb
				}
				a, b = a[da:], b[db:]
				continue
			}
			if a[0] != b[0] {
				return a[0] < b[0]
			}
			a, b = a[1:], b[1:]
		}
		return len(a) < len(b)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && less(names[j], names[j-1]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
}

func digitRun(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

// xmlAttr returns the attribute local, ignoring its namespace.
func xmlAttr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}
