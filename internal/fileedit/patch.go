// Copyright (c) 2025 opencode. MIT; see third_party/opencode/LICENSE.
// Native port of packages/util/src/patch.ts at the recorded revision.
package fileedit

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Hunk and Chunk retain OpenCode's patch grammar, including EOF anchors/moves.
type Hunk struct {
	Type, Path, Contents, MovePath string
	Chunks                         []Chunk
}
type Chunk struct {
	OldLines, NewLines []string
	ChangeContext      string
	EndOfFile          bool
}

func invalidHunk(line string, number int, reason string) error {
	if reason != "" {
		return fmt.Errorf("Invalid hunk at line %d: %s", number, reason)
	}
	return fmt.Errorf("Invalid hunk at line %d: '%s' is not a valid hunk header. Valid hunk headers: '*** Add File: {path}', '*** Delete File: {path}', '*** Update File: {path}'", number, line)
}

// Parse accepts the same patch envelope, heredocs and hunk forms as OpenCode.
func Parse(patchText string) ([]Hunk, error) {
	lines := strings.Split(stripHeredoc(trim(patchText)), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	end := len(lines) - 1
	if trim(lines[0]) != "*** Begin Patch" {
		return nil, errors.New("The first line of the patch must be '*** Begin Patch'")
	}
	if end == 0 || trim(lines[end]) != "*** End Patch" {
		return nil, errors.New("The last line of the patch must be '*** End Patch'")
	}
	var hunks []Hunk
	for i := 1; i < end; {
		header := trim(lines[i])
		if i == 1 && strings.HasPrefix(header, "*** Environment ID:") && trim(strings.TrimPrefix(header, "*** Environment ID:")) != "" {
			i++
			continue
		}
		switch {
		case strings.HasPrefix(header, "*** Add File: "):
			h := Hunk{Type: "add", Path: trim(strings.TrimPrefix(header, "*** Add File: "))}
			i++
			var content []string
			for i < end && !isBoundary(trim(lines[i])) {
				if !strings.HasPrefix(lines[i], "+") {
					return nil, invalidHunk(trim(lines[i]), i+1, fmt.Sprintf("Invalid Add File line for '%s': expected a line starting with '+', got '%s'", h.Path, trim(lines[i])))
				}
				content = append(content, lines[i][1:])
				i++
			}
			h.Contents = strings.Join(content, "\n")
			hunks = append(hunks, h)
		case strings.HasPrefix(header, "*** Delete File: "):
			h := Hunk{Type: "delete", Path: trim(strings.TrimPrefix(header, "*** Delete File: "))}
			if i+1 < end && !isBoundary(trim(lines[i+1])) {
				next := trim(lines[i+1])
				reason := ""
				if !strings.HasPrefix(next, "*** ") {
					reason = fmt.Sprintf("Unexpected line after Delete File '%s': '%s'. Delete hunks do not contain body lines", h.Path, next)
				}
				return nil, invalidHunk(next, i+2, reason)
			}
			hunks = append(hunks, h)
			i++
		case strings.HasPrefix(header, "*** Update File: "):
			h := Hunk{Type: "update", Path: trim(strings.TrimPrefix(header, "*** Update File: "))}
			hunk := i
			i++
			for i < end && trimEnd(lines[i]) == "*** End of File" {
				i++
			}
			move := trimEnd(lines[i])
			if move == "*** Move to:" || strings.HasPrefix(move, "*** Move to: ") {
				h.MovePath = trim(strings.TrimPrefix(move, "*** Move to:"))
				if h.MovePath == "" {
					return nil, invalidHunk(trim(lines[i]), i+1, fmt.Sprintf("Move destination for '%s' must not be empty", h.Path))
				}
				i++
			}
			chunks, next, err := parseUpdate(lines, i, end, h.Path, hunk)
			if err != nil {
				return nil, err
			}
			h.Chunks = chunks
			hunks = append(hunks, h)
			i = next
		default:
			return nil, invalidHunk(header, i+1, "")
		}
	}
	return hunks, nil
}

func parseUpdate(lines []string, start, end int, path string, hunk int) ([]Chunk, int, error) {
	var chunks []Chunk
	i, afterEOF := start, false
	bad := func(line string, number int, reason string) ([]Chunk, int, error) {
		return nil, 0, invalidHunk(line, number, reason)
	}
	unexpected := func(line string) string {
		return fmt.Sprintf("Unexpected line found in update hunk: '%s'. Every line should start with ' ' (context line), '+' (added line), or '-' (removed line)", line)
	}
	for i < end {
		line := lines[i]
		update := trimEnd(line)
		if afterEOF {
			if update == "" {
				i++
				continue
			}
			if update == "@@" || strings.HasPrefix(update, "@@ ") {
				afterEOF = false
			} else if isBoundary(update) {
				break
			} else {
				return bad(line, i+1, fmt.Sprintf("Expected update hunk to start with a @@ context marker, got: '%s'", line))
			}
		}
		if update == "*** End of File" {
			if len(chunks) > 0 {
				ch := &chunks[len(chunks)-1]
				if len(ch.OldLines) == 0 && len(ch.NewLines) == 0 {
					return bad(update, i+1, "Update hunk does not contain any lines")
				}
				ch.EndOfFile = true
				afterEOF = true
			}
			i++
			continue
		}
		if isBoundary(update) {
			break
		}
		if update == "@@" || strings.HasPrefix(update, "@@ ") {
			if len(chunks) > 0 {
				ch := chunks[len(chunks)-1]
				if len(ch.OldLines) == 0 && len(ch.NewLines) == 0 {
					return bad(line, i+1, unexpected(line))
				}
			}
			ch := Chunk{}
			if update != "@@" {
				ch.ChangeContext = update[3:]
			}
			chunks = append(chunks, ch)
			i++
			continue
		}
		if len(chunks) == 0 {
			chunks = append(chunks, Chunk{})
		}
		ch := &chunks[len(chunks)-1]
		switch {
		case line == "":
			ch.OldLines = append(ch.OldLines, "")
			ch.NewLines = append(ch.NewLines, "")
		case strings.HasPrefix(line, " "):
			ch.OldLines = append(ch.OldLines, line[1:])
			ch.NewLines = append(ch.NewLines, line[1:])
		case strings.HasPrefix(line, "-"):
			ch.OldLines = append(ch.OldLines, line[1:])
		case strings.HasPrefix(line, "+"):
			ch.NewLines = append(ch.NewLines, line[1:])
		default:
			reason := unexpected(line)
			if len(ch.OldLines) > 0 || len(ch.NewLines) > 0 {
				reason = fmt.Sprintf("Expected update hunk to start with a @@ context marker, got: '%s'", line)
			}
			return bad(line, i+1, reason)
		}
		i++
	}
	if len(chunks) == 0 {
		return bad(trim(lines[hunk]), hunk+1, fmt.Sprintf("Update file hunk for path '%s' is empty", path))
	}
	last := chunks[len(chunks)-1]
	if len(last.OldLines) == 0 && len(last.NewLines) == 0 {
		line := trim(lines[i])
		reason := unexpected(line)
		if line == "*** End Patch" {
			reason = "Update hunk does not contain any lines"
		}
		return bad(line, i+1, reason)
	}
	return chunks, i, nil
}

func isBoundary(line string) bool {
	return line == "*** End Patch" || strings.HasPrefix(line, "*** Add File: ") || strings.HasPrefix(line, "*** Delete File: ") || strings.HasPrefix(line, "*** Update File: ")
}

// Derive computes every chunk against the original before applying them in
// reverse order, exactly as upstream. Patch updates ensure a terminal newline.
func Derive(path string, chunks []Chunk, original string) (string, error) {
	bom := strings.HasPrefix(original, "\ufeff")
	lines := strings.Split(strings.TrimPrefix(original, "\ufeff"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	type replacement struct {
		start, remove int
		insert        []string
	}
	var replacements []replacement
	lineIndex := 0
	for _, ch := range chunks {
		if ch.ChangeContext != "" {
			context := seek(lines, []string{ch.ChangeContext}, lineIndex, false)
			if context < 0 {
				return "", fmt.Errorf("Failed to find context '%s' in %s", ch.ChangeContext, path)
			}
			lineIndex = context + 1
		}
		if len(ch.OldLines) == 0 {
			replacements = append(replacements, replacement{len(lines), 0, ch.NewLines})
			continue
		}
		oldLines, newLines := ch.OldLines, ch.NewLines
		found := seek(lines, oldLines, lineIndex, ch.EndOfFile)
		if found < 0 && oldLines[len(oldLines)-1] == "" {
			oldLines = oldLines[:len(oldLines)-1]
			if len(newLines) > 0 && newLines[len(newLines)-1] == "" {
				newLines = newLines[:len(newLines)-1]
			}
			found = seek(lines, oldLines, lineIndex, ch.EndOfFile)
		}
		if found < 0 {
			blank := true
			for _, line := range ch.OldLines {
				if line != "" {
					blank = false
				}
			}
			if blank {
				expected := "an expected blank line"
				if len(ch.OldLines) != 1 {
					expected = fmt.Sprintf("%d consecutive blank lines", len(ch.OldLines))
				}
				return "", fmt.Errorf("Failed to find %s in %s", expected, path)
			}
			return "", fmt.Errorf("Failed to find expected lines in %s:\n%s", path, strings.Join(ch.OldLines, "\n"))
		}
		replacements = append(replacements, replacement{found, len(oldLines), newLines})
		lineIndex = found + len(oldLines)
	}
	sort.SliceStable(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		lines = append(append(append([]string(nil), lines[:r.start]...), r.insert...), lines[r.start+r.remove:]...)
	}
	if len(lines) == 0 || lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	text := strings.Join(lines, "\n")
	if strings.HasPrefix(text, "\ufeff") {
		bom = true
		text = strings.TrimPrefix(text, "\ufeff")
	}
	if bom {
		text = "\ufeff" + text
	}
	return text, nil
}

func seek(lines, pattern []string, start int, eof bool) int {
	if len(pattern) == 0 {
		return -1
	}
	comparisons := []func(string) string{func(s string) string { return s }, trimEnd, trim, func(s string) string { return normalize(trim(s)) }}
	for _, key := range comparisons {
		from, to := start, len(lines)-len(pattern)
		if eof {
			from = to
			if from < start {
				return -1
			}
		}
		for offset := from; offset <= to; offset++ {
			same := true
			for i, s := range pattern {
				if key(lines[offset+i]) != key(s) {
					same = false
					break
				}
			}
			if same {
				return offset
			}
		}
	}
	return -1
}

var heredocStart = regexp.MustCompile(`^(?:cat\s+)?<<(['"]?)(\w+)(['"]?)\s*$`)

func stripHeredoc(input string) string {
	first := strings.IndexByte(input, '\n')
	last := strings.LastIndexByte(input, '\n')
	if first < 0 || last <= first {
		return input
	}
	m := heredocStart.FindStringSubmatch(input[:first])
	if m == nil || m[1] != m[3] || trim(input[last+1:]) != m[2] {
		return input
	}
	return input[first+1 : last]
}
