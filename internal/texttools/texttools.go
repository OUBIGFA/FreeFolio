// Package texttools holds the changes and checks the Tools menu of a
// source file offers: line operations for any text, and formatting or
// syntax checks for the formats FreeFolio can parse.
package texttools

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"go/format"
	"go/scanner"
	"io"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// SyntaxError is a parse error at a place in the text. Line and Column
// count from 1; zero means unknown.
type SyntaxError struct {
	Line, Column int
	Err          error
}

func (e *SyntaxError) Error() string {
	switch {
	case e.Column > 0:
		return fmt.Sprintf("line %d, column %d: %v", e.Line, e.Column, e.Err)
	case e.Line > 0:
		return fmt.Sprintf("line %d: %v", e.Line, e.Err)
	}
	return e.Err.Error()
}

func (e *SyntaxError) Unwrap() error { return e.Err }

// splitLines takes text apart into its lines, reporting whether it ends
// with a line break, which joinLines puts back.
func splitLines(text string) ([]string, bool) {
	end := strings.HasSuffix(text, "\n")
	if end {
		text = text[:len(text)-1]
	}
	return strings.Split(text, "\n"), end
}

func joinLines(lines []string, end bool) string {
	text := strings.Join(lines, "\n")
	if end {
		text += "\n"
	}
	return text
}

// SortLines sorts the lines of text, in byte order or its reverse; equal
// lines keep their order.
func SortLines(text string, descending bool) string {
	lines, end := splitLines(text)
	slices.SortStableFunc(lines, func(a, b string) int {
		if descending {
			return strings.Compare(b, a)
		}
		return strings.Compare(a, b)
	})
	return joinLines(lines, end)
}

// UniqueLines removes the lines of text equal to one before them.
func UniqueLines(text string) string {
	lines, end := splitLines(text)
	seen := make(map[string]bool, len(lines))
	out := lines[:0]
	for _, l := range lines {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return joinLines(out, end)
}

// RemoveBlankLines removes the lines of text holding only white space.
func RemoveBlankLines(text string) string {
	lines, end := splitLines(text)
	out := lines[:0]
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return joinLines(out, end)
}

// TrimTrailingSpace removes the spaces and tabs ending each line of text.
func TrimTrailingSpace(text string) string {
	lines, end := splitLines(text)
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return joinLines(lines, end)
}

// position is the line and column, from 1, of the byte offset off of text.
func position(text string, off int) (int, int) {
	off = max(0, min(off, len(text)))
	head := text[:off]
	line := strings.Count(head, "\n") + 1
	if i := strings.LastIndexByte(head, '\n'); i >= 0 {
		head = head[i+1:]
	}
	return line, utf8.RuneCountInString(head) + 1
}

func jsonError(text string, err error) error {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		line, col := position(text, int(se.Offset)-1)
		return &SyntaxError{Line: line, Column: col, Err: errors.New(se.Error())}
	}
	return err
}

// FormatJSON indents a JSON value by two spaces, keeping its keys' order
// and its numbers as written.
func FormatJSON(text string) (string, error) {
	var buf bytes.Buffer
	// Indent keeps the white space ending its input; one line break ends
	// the result instead.
	if err := json.Indent(&buf, []byte(strings.TrimRight(text, " \t\r\n")), "", "  "); err != nil {
		return "", jsonError(text, err)
	}
	return buf.String() + "\n", nil
}

// CompactJSON writes a JSON value on one line, without spaces.
func CompactJSON(text string) (string, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(text)); err != nil {
		return "", jsonError(text, err)
	}
	if strings.HasSuffix(text, "\n") {
		buf.WriteByte('\n')
	}
	return buf.String(), nil
}

// CheckJSON reports whether text is one JSON value.
func CheckJSON(text string) error {
	_, err := CompactJSON(text)
	return err
}

// CheckJSONLines reports whether each line of text not blank is a JSON
// value, as JSON Lines want, and counts those lines.
func CheckJSONLines(text string) (int, error) {
	lines, _ := splitLines(text)
	n := 0
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if err := json.Unmarshal([]byte(l), new(json.RawMessage)); err != nil {
			col := 0
			var se *json.SyntaxError
			if errors.As(err, &se) {
				_, col = position(l, int(se.Offset)-1)
			}
			return n, &SyntaxError{Line: i + 1, Column: col, Err: err}
		}
		n++
	}
	return n, nil
}

// CheckXML reports whether text is well-formed XML.
func CheckXML(text string) error {
	d := xml.NewDecoder(strings.NewReader(text))
	// The text is UTF-8 already, whatever encoding its declaration names.
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	root := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			var se *xml.SyntaxError
			if errors.As(err, &se) {
				return &SyntaxError{Line: se.Line, Err: errors.New(se.Msg)}
			}
			return err
		}
		if _, ok := tok.(xml.StartElement); ok {
			root = true
		}
	}
	if !root {
		return errors.New("no root element")
	}
	return nil
}

var yamlLine = regexp.MustCompile(`^yaml: line (\d+): (.*)$`)

// CheckYAML reports whether text is YAML, of one document or more.
func CheckYAML(text string) error {
	d := yaml.NewDecoder(strings.NewReader(text))
	for {
		var n yaml.Node
		err := d.Decode(&n)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if m := yamlLine.FindStringSubmatch(err.Error()); m != nil {
				line, _ := strconv.Atoi(m[1])
				return &SyntaxError{Line: line, Err: errors.New(m[2])}
			}
			return err
		}
	}
}

// FormatGo formats Go source as gofmt does.
func FormatGo(text string) (string, error) {
	out, err := format.Source([]byte(text))
	if err != nil {
		var list scanner.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			return "", &SyntaxError{Line: list[0].Pos.Line, Column: list[0].Pos.Column, Err: errors.New(list[0].Msg)}
		}
		return "", err
	}
	return string(out), nil
}

// Separator is the field separator of the delimited text of the file
// name: a tab for .tsv and .tab, else whichever of comma, semicolon, tab
// and bar the first line holds most, a comma when none.
func Separator(name, text string) rune {
	switch strings.ToLower(path.Ext(name)) {
	case ".tsv", ".tab":
		return '\t'
	}
	first := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		first = text[:i]
	}
	best, most := ',', 0
	for _, r := range []rune{',', ';', '\t', '|'} {
		if n := strings.Count(first, string(r)); n > most {
			best, most = r, n
		}
	}
	return best
}

// Record is a row of delimited text and the line it starts on.
type Record struct {
	Line   int
	Fields []string
}

// ParseDelimited reads text as rows of fields separated by sep, quoted as
// CSV quotes them; blank lines are skipped.
func ParseDelimited(text string, sep rune) ([]Record, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sep
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	var out []Record
	for {
		fields, err := r.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				return nil, &SyntaxError{Line: pe.Line, Column: pe.Column, Err: pe.Err}
			}
			return nil, err
		}
		line, _ := r.FieldPos(0)
		out = append(out, Record{Line: line, Fields: fields})
	}
}

// RaggedRecord is the first record of rows with a field count other than
// the first's, or nil when they all agree.
func RaggedRecord(rows []Record) *Record {
	for i := range rows {
		if len(rows[i].Fields) != len(rows[0].Fields) {
			return &rows[i]
		}
	}
	return nil
}
