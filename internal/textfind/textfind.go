// Package textfind finds and replaces text as FreeFolio's find boxes do:
// literally or by a regular expression, ignoring case or not, whole words
// only or not. Matches are found left to right, never overlap and are
// never empty.
package textfind

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Options are how a query matches.
type Options struct {
	// CaseSensitive matches letters only in their own case.
	CaseSensitive bool
	// WholeWord rejects a match that continues a word on either side.
	WholeWord bool
	// Regex reads the query as a regular expression (RE2 syntax, ^ and $
	// at every line) and lets the replacement use $1 and ${name}.
	Regex bool
}

// Matcher finds one query. It keeps scratch state between calls, so one
// Matcher must not be used by two goroutines at once.
type Matcher struct {
	opts Options
	// pattern and failure drive a linear literal search (KMP), folding one
	// rune at a time; starts is a pattern-sized ring of match starts.
	pattern         []rune
	failure, starts []int
	re              *regexp.Regexp
	err             error
}

// New prepares query for matching with o.
func New(query string, o Options) *Matcher {
	m := &Matcher{opts: o}
	if o.Regex {
		if query == "" {
			return m
		}
		flags := "(?m)"
		if !o.CaseSensitive {
			flags = "(?im)"
		}
		m.re, m.err = regexp.Compile(flags + query)
		return m
	}
	q := []rune(query)
	if !o.CaseSensitive {
		q = []rune(strings.Map(unicode.ToLower, query))
	}
	m.pattern, m.failure, m.starts = q, make([]int, len(q)), make([]int, len(q))
	for i, j := 1, 0; i < len(q); i++ {
		for j > 0 && q[i] != q[j] {
			j = m.failure[j-1]
		}
		if q[i] == q[j] {
			j++
		}
		m.failure[i] = j
	}
	return m
}

// Err is why the query cannot match, an invalid regular expression.
func (m *Matcher) Err() error { return m.err }

// Options are the matcher's options.
func (m *Matcher) Options() Options { return m.opts }

// Each calls fn with the byte range of every match in text, in order.
func (m *Matcher) Each(text string, fn func(start, end int)) {
	if m == nil {
		return
	}
	if m.opts.Regex {
		if m.re == nil {
			return
		}
		for _, loc := range m.re.FindAllStringIndex(text, -1) {
			if m.keep(text, loc[0], loc[1]) {
				fn(loc[0], loc[1])
			}
		}
		return
	}
	q := m.pattern
	if len(q) == 0 || len(text) < len(q) {
		return
	}
	index, matched := 0, 0
	for i, r := range text {
		m.starts[index%len(q)] = i
		if !m.opts.CaseSensitive {
			r = unicode.ToLower(r)
		}
		for matched > 0 && r != q[matched] {
			matched = m.failure[matched-1]
		}
		if r == q[matched] {
			matched++
		}
		if matched == len(q) {
			_, w := utf8.DecodeRuneInString(text[i:])
			start, end := m.starts[(index-len(q)+1)%len(q)], i+w
			if m.keep(text, start, end) {
				fn(start, end)
				matched = 0 // matches never overlap
			} else {
				// Not a whole word: a shorter overlap may still be one.
				matched = m.failure[matched-1]
			}
		}
		index++
	}
}

// keep reports whether the match [start, end) of text counts.
func (m *Matcher) keep(text string, start, end int) bool {
	return start < end && (!m.opts.WholeWord || wholeWord(text, start, end))
}

// Count is how many times the query matches in text.
func (m *Matcher) Count(text string) int {
	n := 0
	m.Each(text, func(int, int) { n++ })
	return n
}

// EachWith calls fn with the byte range of every match in text and what
// replaces it: with, its $1 and ${name} filled in for a regular expression.
func (m *Matcher) EachWith(text, with string, fn func(start, end int, replacement string)) {
	if m == nil {
		return
	}
	if !m.opts.Regex {
		m.Each(text, func(start, end int) { fn(start, end, with) })
		return
	}
	if m.re == nil {
		return
	}
	for _, loc := range m.re.FindAllStringSubmatchIndex(text, -1) {
		if m.keep(text, loc[0], loc[1]) {
			fn(loc[0], loc[1], string(m.re.ExpandString(nil, with, text, loc)))
		}
	}
}

// Replace replaces every match in text with with, expanding $1 and
// ${name} for a regular expression, and says how many there were.
func (m *Matcher) Replace(text, with string) (string, int) {
	var out []byte
	last, n := 0, 0
	m.EachWith(text, with, func(start, end int, replacement string) {
		out = append(append(out, text[last:start]...), replacement...)
		last, n = end, n+1
	})
	if n == 0 {
		return text, 0
	}
	return string(append(out, text[last:]...)), n
}

// Expand is what replaces the match starting at byte start of text: with
// itself for a literal query, with its groups filled in for a regular
// expression.
func (m *Matcher) Expand(text string, start int, with string) string {
	if m == nil || !m.opts.Regex || m.re == nil {
		return with
	}
	for _, loc := range m.re.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] == start && m.keep(text, loc[0], loc[1]) {
			return string(m.re.ExpandString(nil, with, text, loc))
		}
		if loc[0] > start {
			break
		}
	}
	return with
}

// isWord reports whether r belongs to a word: a letter, a digit, a
// combining mark or an underscore. Chinese and Japanese characters are
// words of their own, as those scripts put no spaces between words, so a
// whole-word search for them finds them anywhere.
func isWord(r rune) bool {
	if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) {
		return false
	}
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r)
}

// wholeWord reports whether [start, end) of text does not continue a word
// on either side: a word rune at its edge has none beside it.
func wholeWord(text string, start, end int) bool {
	if start > 0 {
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		first, _ := utf8.DecodeRuneInString(text[start:])
		if isWord(before) && isWord(first) {
			return false
		}
	}
	if end < len(text) {
		after, _ := utf8.DecodeRuneInString(text[end:])
		last, _ := utf8.DecodeLastRuneInString(text[:end])
		if isWord(after) && isWord(last) {
			return false
		}
	}
	return true
}
