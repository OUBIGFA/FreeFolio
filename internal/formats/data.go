package formats

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

// ---- Jupyter notebooks ----

type notebook struct {
	Cells []struct {
		Type    string          `json:"cell_type"`
		Source  json.RawMessage `json:"source"`
		Outputs []struct {
			Type string                     `json:"output_type"`
			Text json.RawMessage            `json:"text"`
			Data map[string]json.RawMessage `json:"data"`
		} `json:"outputs"`
	} `json:"cells"`
	Metadata struct {
		Kernel struct {
			Language string `json:"language"`
		} `json:"kernelspec"`
		Language struct {
			Name string `json:"name"`
		} `json:"language_info"`
	} `json:"metadata"`
}

// joinSource reads a notebook string, stored as a string or a list of
// lines.
func joinSource(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var lines []string
	if json.Unmarshal(raw, &lines) == nil {
		return strings.Join(lines, "")
	}
	return ""
}

func convertNotebook(data []byte) (string, error) {
	var nb notebook
	if err := json.Unmarshal(data, &nb); err != nil {
		return "", fmt.Errorf("formats: not a valid notebook: %w", err)
	}
	lang := nb.Metadata.Language.Name
	if lang == "" {
		lang = nb.Metadata.Kernel.Language
	}
	if lang == "" {
		lang = "python"
	}
	var w mdWriter
	for _, c := range nb.Cells {
		if w.full() {
			break
		}
		src := joinSource(c.Source)
		switch c.Type {
		case "markdown":
			w.raw(src) // notebook Markdown is Markdown already
		case "code":
			w.code(lang, src)
			for _, o := range c.Outputs {
				text := joinSource(o.Text)
				if text == "" {
					text = joinSource(o.Data["text/plain"])
				}
				if text != "" {
					w.code("text", text)
				}
			}
		default:
			w.code("", src)
		}
	}
	return w.String(), nil
}

// ---- SQLite ----

// sqliteFile reads the database file format (sqlite.org/fileformat2.html)
// without the SQLite engine: tables only, read-only.
type sqliteFile struct {
	data     []byte
	pageSize int
	usable   int
	pages    int
	encoding int
	visited  int
}

var errSQLite = errors.New("formats: malformed SQLite database")

func openSQLite(data []byte) (*sqliteFile, error) {
	if len(data) < 100 || string(data[:16]) != "SQLite format 3\x00" {
		return nil, ErrUnsupported
	}
	ps := int(binary.BigEndian.Uint16(data[16:]))
	if ps == 1 {
		ps = 65536
	}
	if ps < 512 || ps&(ps-1) != 0 {
		return nil, errSQLite
	}
	f := &sqliteFile{data: data, pageSize: ps, usable: ps - int(data[20]), pages: len(data) / ps, encoding: int(binary.BigEndian.Uint32(data[56:]))}
	return f, nil
}

func (f *sqliteFile) page(n int) []byte {
	if n < 1 || n > f.pages {
		return nil
	}
	return f.data[(n-1)*f.pageSize : n*f.pageSize]
}

func sqliteVarint(b []byte) (int64, int) {
	var v uint64
	for i := 0; i < 9 && i < len(b); i++ {
		if i == 8 {
			return int64(v<<8 | uint64(b[i])), 9
		}
		v = v<<7 | uint64(b[i]&0x7F)
		if b[i]&0x80 == 0 {
			return int64(v), i + 1
		}
	}
	return int64(v), max(len(b), 1)
}

// payload reads a table-leaf cell's record, following overflow pages.
func (f *sqliteFile) payload(pg []byte, off int) ([]byte, error) {
	if off >= len(pg) {
		return nil, errSQLite
	}
	size, n := sqliteVarint(pg[off:])
	off += n
	_, n = sqliteVarint(pg[min(off, len(pg)):]) // rowid
	off += n
	if size < 0 || size > maxZipEntry || off > len(pg) {
		return nil, errSQLite
	}
	u := f.usable
	maxLocal := u - 35
	local := int(size)
	if local > maxLocal {
		minLocal := (u-12)*32/255 - 23
		local = minLocal + (int(size)-minLocal)%(u-4)
		if local > maxLocal {
			local = minLocal
		}
	}
	if off+local > len(pg) {
		return nil, errSQLite
	}
	out := append([]byte(nil), pg[off:off+local]...)
	if local == int(size) {
		return out, nil
	}
	if off+local+4 > len(pg) {
		return nil, errSQLite
	}
	next := int(binary.BigEndian.Uint32(pg[off+local:]))
	for guard := 0; len(out) < int(size) && next != 0; guard++ {
		p := f.page(next)
		if p == nil || guard > f.pages {
			return nil, errSQLite
		}
		next = int(binary.BigEndian.Uint32(p))
		out = append(out, p[4:min(u, 4+int(size)-len(out))]...)
	}
	return out, nil
}

// rows walks the table B-tree rooted at root, calling fn with each record's
// values until it returns false.
func (f *sqliteFile) rows(root int, fn func([]any) bool) error {
	var walk func(n, depth int) (bool, error)
	walk = func(n, depth int) (bool, error) {
		f.visited++
		if depth > 32 || f.visited > 4*f.pages+16 {
			return false, errSQLite
		}
		pg := f.page(n)
		if pg == nil {
			return false, errSQLite
		}
		hdr := 0
		if n == 1 {
			hdr = 100
		}
		if hdr+8 > len(pg) {
			return false, errSQLite
		}
		kind := pg[hdr]
		cells := int(binary.BigEndian.Uint16(pg[hdr+3:]))
		ptrs := hdr + 8
		if kind == 0x05 {
			ptrs = hdr + 12
		}
		for i := range cells {
			p := ptrs + 2*i
			if p+2 > len(pg) {
				return false, errSQLite
			}
			off := int(binary.BigEndian.Uint16(pg[p:]))
			switch kind {
			case 0x05: // interior table page: left child, then key
				if off+4 > len(pg) {
					return false, errSQLite
				}
				if ok, err := walk(int(binary.BigEndian.Uint32(pg[off:])), depth+1); !ok || err != nil {
					return ok, err
				}
			case 0x0D: // leaf table page
				rec, err := f.payload(pg, off)
				if err != nil {
					return false, err
				}
				if !fn(f.record(rec)) {
					return false, nil
				}
			default:
				return false, errSQLite
			}
		}
		if kind == 0x05 {
			return walk(int(binary.BigEndian.Uint32(pg[hdr+8:])), depth+1)
		}
		return true, nil
	}
	_, err := walk(root, 0)
	return err
}

// record decodes a record into nil, int64, float64, string or []byte.
func (f *sqliteFile) record(b []byte) []any {
	hlen, n := sqliteVarint(b)
	if hlen < int64(n) || hlen > int64(len(b)) {
		return nil
	}
	var types []int64
	for p := n; p < int(hlen); {
		t, k := sqliteVarint(b[p:])
		types = append(types, t)
		p += k
	}
	body := b[hlen:]
	var out []any
	for _, t := range types {
		size := 0
		switch {
		case t >= 1 && t <= 4:
			size = int(t)
		case t == 5:
			size = 6
		case t == 6 || t == 7:
			size = 8
		case t >= 12:
			size = int((t - 12) / 2)
		}
		if size > len(body) {
			out = append(out, nil)
			body = nil
			continue
		}
		v := body[:size]
		body = body[size:]
		switch {
		case t == 0:
			out = append(out, nil)
		case t >= 1 && t <= 6:
			var x int64
			for _, c := range v {
				x = x<<8 | int64(c)
			}
			if shift := 64 - 8*uint(size); size < 8 {
				x = x << shift >> shift
			}
			out = append(out, x)
		case t == 7:
			out = append(out, math.Float64frombits(binary.BigEndian.Uint64(v)))
		case t == 8:
			out = append(out, int64(0))
		case t == 9:
			out = append(out, int64(1))
		case t >= 12 && t%2 == 0:
			out = append(out, append([]byte(nil), v...))
		case t >= 13:
			switch f.encoding {
			case 2:
				out = append(out, utf16LE(v))
			case 3:
				u := make([]byte, len(v))
				for i := 0; i+1 < len(v); i += 2 {
					u[i], u[i+1] = v[i+1], v[i]
				}
				out = append(out, utf16LE(u))
			default:
				out = append(out, strings.ToValidUTF8(string(v), "\uFFFD"))
			}
		default:
			out = append(out, nil)
		}
	}
	return out
}

func sqlValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return x
	case []byte:
		return fmt.Sprintf("BLOB (%s)", humanSize(len(x)))
	}
	return ""
}

// sqlColumns reads the column names of a CREATE TABLE statement.
func sqlColumns(create string) []string {
	open := strings.IndexByte(create, '(')
	close := strings.LastIndexByte(create, ')')
	if open < 0 || close <= open {
		return nil
	}
	body := create[open+1 : close]
	var parts []string
	depth, start := 0, 0
	for i, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, body[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, body[start:])
	var cols []string
	for _, p := range parts {
		f := strings.Fields(strings.TrimSpace(p))
		if len(f) == 0 {
			continue
		}
		switch strings.ToUpper(f[0]) {
		case "CONSTRAINT", "PRIMARY", "UNIQUE", "CHECK", "FOREIGN":
			continue
		}
		cols = append(cols, strings.Trim(f[0], "\"`[]'"))
	}
	return cols
}

func convertSQLite(data []byte) (string, error) {
	db, err := openSQLite(data)
	if err != nil {
		return "", err
	}
	type table struct {
		name, sql string
		root      int
	}
	var tables []table
	var views []string
	err = db.rows(1, func(v []any) bool {
		if len(v) < 5 {
			return true
		}
		kind, _ := v[0].(string)
		name, _ := v[1].(string)
		root, _ := v[3].(int64)
		sql, _ := v[4].(string)
		switch {
		case kind == "table" && !strings.HasPrefix(name, "sqlite_") && root > 0:
			if strings.Contains(strings.ToUpper(sql), "WITHOUT ROWID") || strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "CREATE VIRTUAL") {
				views = append(views, name)
				return true
			}
			tables = append(tables, table{name, sql, int(root)})
		case kind == "view":
			views = append(views, name)
		}
		return true
	})
	if err != nil {
		return "", err
	}
	var w mdWriter
	w.raw("> " + i18n.T("只读预览：每张表最多显示 2000 行。"))
	for _, t := range tables {
		if w.full() {
			break
		}
		w.heading(2, t.name)
		w.code("sql", t.sql)
		cols := sqlColumns(t.sql)
		rows := [][]string{cols}
		count, more := 0, false
		db.visited = 0
		err := db.rows(t.root, func(v []any) bool {
			if count >= maxRows {
				more = true
				return false
			}
			count++
			row := make([]string, max(len(cols), len(v)))
			for i, x := range v {
				row[i] = sqlValue(x)
			}
			rows = append(rows, row)
			return true
		})
		if err != nil {
			w.para(i18n.T("（无法读取此表）"))
			continue
		}
		if count == 0 {
			w.para(i18n.T("（空表）"))
			continue
		}
		if len(cols) == 0 {
			rows[0] = make([]string, len(rows[1]))
			for i := range rows[0] {
				rows[0][i] = fmt.Sprintf("#%d", i+1)
			}
		}
		w.table(rows)
		if more {
			w.para(i18n.T("… 更多行未显示"))
		}
	}
	if len(views) > 0 {
		w.heading(2, i18n.T("视图与其他表"))
		w.bullets(views)
	}
	return w.String(), nil
}

// ---- Archives ----

type archiveEntry struct {
	name string
	size int64
	mod  time.Time
	dir  bool
}

func writeArchive(w *mdWriter, entries []archiveEntry) {
	var files, dirs int
	var total int64
	rows := [][]string{{i18n.T("名称"), i18n.T("大小"), i18n.T("修改时间")}}
	for _, e := range entries {
		if e.dir {
			dirs++
			continue
		}
		files++
		total += e.size
		mod := ""
		if !e.mod.IsZero() && e.mod.Year() > 1980 {
			mod = e.mod.Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{e.name, humanSize(int(min(e.size, math.MaxInt32))), mod})
	}
	w.raw("> " + fmt.Sprintf(i18n.T("压缩包内有 %d 个文件、%d 个文件夹，解压后共 %s。"), files, dirs, humanSize(int(min(total, math.MaxInt32)))))
	if files > 0 {
		w.table(rows)
	}
}

func convertZip(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	var entries []archiveEntry
	for _, f := range z.r.File {
		name := f.Name
		if f.NonUTF8 {
			name = decodeText([]byte(name))
		}
		entries = append(entries, archiveEntry{name, int64(f.UncompressedSize64), f.Modified, f.FileInfo().IsDir()})
	}
	var w mdWriter
	writeArchive(&w, entries)
	return w.String(), nil
}

func convertTarLike(data []byte) (string, error) {
	var r io.Reader = bytes.NewReader(data)
	inner := ""
	switch {
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		zr, err := gzip.NewReader(r)
		if err != nil {
			return "", err
		}
		defer zr.Close()
		inner = zr.Name
		r = zr
	case bytes.HasPrefix(data, []byte("BZh")):
		r = bzip2.NewReader(r)
	}
	r = io.LimitReader(r, 1<<30)
	// A tar header is a block of 512 bytes with "ustar" at 257.
	br := &peekReader{r: r}
	head := br.peek(512)
	var w mdWriter
	if len(head) < 512 || !bytes.HasPrefix(head[257:], []byte("ustar")) && !validTarChecksum(head) {
		// A single compressed file.
		n, _ := io.Copy(io.Discard, br)
		if inner == "" {
			inner = i18n.T("（压缩的单个文件）")
		}
		writeArchive(&w, []archiveEntry{{name: inner, size: n}})
		return w.String(), nil
	}
	tr := tar.NewReader(br)
	var entries []archiveEntry
	for len(entries) < 100000 {
		h, err := tr.Next()
		if err != nil {
			break
		}
		entries = append(entries, archiveEntry{path.Clean(h.Name), h.Size, h.ModTime, h.Typeflag == tar.TypeDir})
	}
	writeArchive(&w, entries)
	return w.String(), nil
}

func validTarChecksum(h []byte) bool {
	want, err := strconv.ParseInt(strings.Trim(string(h[148:156]), " \x00"), 8, 64)
	if err != nil {
		return false
	}
	var sum int64
	for i, c := range h[:512] {
		if i >= 148 && i < 156 {
			c = ' '
		}
		sum += int64(c)
	}
	return sum == want
}

// peekReader lets the first bytes be inspected and then read again.
type peekReader struct {
	r   io.Reader
	buf []byte
}

func (p *peekReader) peek(n int) []byte {
	for len(p.buf) < n {
		chunk := make([]byte, n-len(p.buf))
		k, err := p.r.Read(chunk)
		p.buf = append(p.buf, chunk[:k]...)
		if err != nil {
			break
		}
	}
	return p.buf
}

func (p *peekReader) Read(b []byte) (int, error) {
	if len(p.buf) > 0 {
		n := copy(b, p.buf)
		p.buf = p.buf[n:]
		return n, nil
	}
	return p.r.Read(b)
}
