// Package fileedit ports OpenCode's file edit and patch algorithms to Go.
// Copyright (c) 2025 opencode. MIT; see third_party/opencode/LICENSE.
// Source revision and function mapping: third_party/opencode/README.md.
package fileedit

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type match struct{ start, end int }

// Edit has OpenCode edit.ts semantics: exact, then typography, then trailing
// whitespace matching. Offsets use runes so its one-to-one Unicode mappings
// remain correct in Go's UTF-8 strings. Unmatched bytes and line endings survive.
func Edit(source, oldString, newString string, replaceAll bool) (string, int, error) {
	if !utf8.ValidString(source) || !utf8.ValidString(oldString) || !utf8.ValidString(newString) {
		return "", 0, errors.New("file edits require valid UTF-8")
	}
	if oldString == newString {
		return "", 0, errors.New("No changes to apply: oldString and newString are identical.")
	}
	if oldString == "" {
		return "", 0, errors.New("oldString must not be empty. Use write to create or overwrite a file.")
	}
	endingsOnly := strings.ReplaceAll(oldString, "\r", "") == strings.ReplaceAll(newString, "\r", "")
	var matches []match
	replace := func(match) (string, error) { return newString, nil }
	original := []rune(source)
	if endingsOnly {
		matches = findOccurrences(original, []rune(oldString))
	} else {
		view := []rune(strings.ReplaceAll(source, "\r\n", "\n"))
		search := strings.ReplaceAll(oldString, "\r\n", "\n")
		matches = findOccurrences(view, []rune(search))
		if len(matches) == 0 {
			matches = findOccurrences([]rune(normalize(string(view))), []rune(normalize(search)))
		}
		if len(matches) == 0 {
			matches = findLineOccurrences(string(view), search)
		}
		var removed []int
		for i := 0; i+1 < len(original); i++ {
			if original[i] == '\r' && original[i+1] == '\n' {
				removed = append(removed, i-len(removed))
			}
		}
		for i := range matches {
			matches[i] = match{matches[i].start + sort.SearchInts(removed, matches[i].start), matches[i].end + sort.SearchInts(removed, matches[i].end)}
		}
		replacement := strings.ReplaceAll(newString, "\r\n", "\n")
		fileEnding := lineEnding(source, "\n")
		replace = func(m match) (string, error) {
			region := string(original[m.start:m.end])
			return withRegionEndings(region, replacement, lineEnding(region, fileEnding))
		}
	}
	if len(matches) == 0 {
		hint := missHint(source, oldString, newString)
		if hint != "" {
			hint = "\n\n" + hint
		}
		return "", 0, fmt.Errorf("Could not find oldString. It must match exactly, including whitespace and indentation.%s", hint)
	}
	if len(matches) > 1 && !replaceAll {
		return "", 0, fmt.Errorf("Found %d matches for oldString, but expected exactly one. Add more surrounding context to make oldString unique, or set replaceAll to true to replace every occurrence.", len(matches))
	}
	result := original
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		text, err := replace(m)
		if err != nil {
			return "", 0, err
		}
		result = append(append(append([]rune(nil), result[:m.start]...), []rune(text)...), result[m.end:]...)
	}
	if string(result) == source {
		return "", 0, errors.New("No changes to apply: the edit leaves the file unchanged. Line endings follow the file; to change them, write \\r explicitly in oldString or newString.")
	}
	return string(result), len(matches), nil
}

func normalize(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case strings.ContainsRune("‘’‚‛", r):
			return '\''
		case strings.ContainsRune("“”„‟", r):
			return '"'
		case strings.ContainsRune("‐‑‒–—―−", r):
			return '-'
		case r == '\u00a0' || r >= '\u2002' && r <= '\u200a' || r == '\u202f' || r == '\u205f' || r == '\u3000':
			return ' '
		}
		return r
	}, value)
}

// JS trim includes BOM and excludes U+0085, unlike unicode.IsSpace.
func jsSpace(r rune) bool     { return r == '\ufeff' || r != '\u0085' && unicode.IsSpace(r) }
func trimEnd(s string) string { return strings.TrimRightFunc(s, jsSpace) }
func trim(s string) string    { return strings.TrimFunc(s, jsSpace) }

func findOccurrences(content, search []rune) []match {
	var result []match
	if len(search) == 0 {
		return result
	}
	text, needle := string(content), string(search)
	byteOffset, runeOffset := 0, 0
	for byteOffset < len(text) {
		found := strings.Index(text[byteOffset:], needle)
		if found < 0 {
			break
		}
		start := runeOffset + utf8.RuneCountInString(text[byteOffset:byteOffset+found])
		result = append(result, match{start, start + len(search)})
		byteOffset += found + len(needle)
		runeOffset = start + len(search)
	}
	return result
}

func findLineOccurrences(content, search string) []match {
	trailing := strings.HasSuffix(search, "\n")
	expected := strings.Split(search, "\n")
	if trailing {
		expected = expected[:len(expected)-1]
	}
	type line struct {
		text                   string
		start, end, contentEnd int
		newline                bool
	}
	var lines []line
	start := 0
	for _, s := range strings.SplitAfter(content, "\n") {
		if s == "" {
			continue
		}
		text := strings.TrimSuffix(s, "\n")
		end := start + utf8.RuneCountInString(s)
		lines = append(lines, line{text, start, end, start + utf8.RuneCountInString(strings.TrimSuffix(text, "\r")), strings.HasSuffix(s, "\n")})
		start = end
	}
	var result []match
	for i := 0; i+len(expected) <= len(lines); i++ {
		same := true
		for j, s := range expected {
			if normalize(trimEnd(lines[i+j].text)) != normalize(trimEnd(s)) {
				same = false
				break
			}
		}
		if !same {
			continue
		}
		last := lines[i+len(expected)-1]
		if trailing && !last.newline {
			continue
		}
		end := last.contentEnd
		if trailing {
			end = last.end
		}
		candidate := match{lines[i].start, end}
		if len(result) > 0 && result[len(result)-1].end > candidate.start {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func lineEnding(text, fallback string) string {
	crlf := strings.Count(text, "\r\n")
	lf := strings.Count(text, "\n") - crlf
	if crlf == lf {
		return fallback
	}
	if crlf > lf {
		return "\r\n"
	}
	return "\n"
}

func withRegionEndings(region, replacement, fallback string) (string, error) {
	before := strings.Split(strings.ReplaceAll(region, "\r\n", "\n"), "\n")
	var endings []string
	for i, r := range region {
		if r == '\n' {
			end := "\n"
			if i > 0 && region[i-1] == '\r' {
				end = "\r\n"
			}
			endings = append(endings, end)
		}
	}
	after := strings.Split(replacement, "\n")
	keys := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, s := range lines {
			out[i] = normalize(trimEnd(s))
		}
		return out
	}
	source, err := alignLines(keys(before), keys(after))
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for i, line := range after {
		out.WriteString(line)
		if i == len(after)-1 {
			break
		}
		ending := fallback
		if source[i] >= 0 && source[i] < len(endings) {
			ending = endings[source[i]]
		}
		out.WriteString(ending)
	}
	return out.String(), nil
}

func alignLines(before, after []string) ([]int, error) {
	width := len(after) + 1
	// A desktop host must not allocate an unbounded quadratic table. Report
	// the limit so the model can issue smaller edits; do not change matching.
	if int64(len(before)+1)*int64(width) > 16_000_000 {
		return nil, errors.New("edit has too many lines; split it into smaller exact edits")
	}
	table := make([]uint32, (len(before)+1)*width)
	for i := len(before) - 1; i >= 0; i-- {
		for j := len(after) - 1; j >= 0; j-- {
			if before[i] == after[j] {
				table[i*width+j] = table[(i+1)*width+j+1] + 1
			} else {
				table[i*width+j] = max(table[(i+1)*width+j], table[i*width+j+1])
			}
		}
	}
	result := make([]int, len(after))
	for i := range result {
		result[i] = -1
	}
	var gapBefore, gapAfter []int
	flush := func() {
		for k, j := range gapAfter {
			if k < len(gapBefore) {
				result[j] = gapBefore[k]
			}
		}
		gapBefore, gapAfter = nil, nil
	}
	i, j := 0, 0
	for i < len(before) && j < len(after) {
		if before[i] == after[j] {
			flush()
			result[j] = i
			i++
			j++
			continue
		}
		if table[(i+1)*width+j] >= table[i*width+j+1] {
			gapBefore = append(gapBefore, i)
			i++
		} else {
			gapAfter = append(gapAfter, j)
			j++
		}
	}
	for ; i < len(before); i++ {
		gapBefore = append(gapBefore, i)
	}
	for ; j < len(after); j++ {
		gapAfter = append(gapAfter, j)
	}
	flush()
	return result, nil
}

func missHint(source, oldString, newString string) string {
	view := strings.ReplaceAll(source, "\r\n", "\n")
	replacement := strings.ReplaceAll(newString, "\r\n", "\n")
	if utf8.RuneCountInString(trim(replacement)) >= 20 && strings.Contains(normalize(view), normalize(replacement)) {
		return "newString is already in the file, so this edit may already be applied."
	}
	key := func(s string) string { return strings.Join(strings.FieldsFunc(normalize(s), jsSpace), " ") }
	distinct := func(s string) bool {
		for _, r := range s {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				return true
			}
		}
		return false
	}
	lines, search := strings.Split(view, "\n"), strings.Split(strings.ReplaceAll(oldString, "\r\n", "\n"), "\n")
	positions := map[string][]int{}
	for i, s := range lines {
		k := key(s)
		if distinct(k) {
			positions[k] = append(positions[k], i)
		}
	}
	votes := map[int]int{}
	var order []int
	wanted := 0
	for offset, s := range search {
		k := key(s)
		if !distinct(k) {
			continue
		}
		wanted++
		for _, i := range positions[k] {
			start := i - offset
			if votes[start] == 0 {
				order = append(order, start)
			}
			votes[start]++
		}
	}
	start, count := 0, 0
	for _, i := range order {
		if votes[i] > count {
			start, count = i, votes[i]
		}
	}
	if count == 0 || count*2 < wanted {
		return ""
	}
	var matched []int
	for off, s := range search {
		i := start + off
		if k := key(s); k != "" && i >= 0 && i < len(lines) && key(lines[i]) == k {
			matched = append(matched, i)
		}
	}
	if len(matched) == 0 {
		return ""
	}
	from, to := matched[0], min(matched[len(matched)-1]+1, matched[0]+12)
	out := []string{"The closest lines in the file:"}
	for i := from; i < to; i++ {
		s := []rune(lines[i])
		if len(s) > 200 {
			s = append(s[:200], '…')
		}
		out = append(out, fmt.Sprintf("%d: %s", i+1, string(s)))
	}
	return strings.Join(out, "\n")
}
