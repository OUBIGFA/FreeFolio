package search

import (
	"strings"
	"testing"
	"unicode"
)

// The searches below read the text where it is. These are the plain
// definitions they must agree with, over the whole text as runes.

func findPlain(text string, q []rune) (count, first int) {
	runes := []rune(text)
	first = -1
	for i := 0; i+len(q) <= len(runes); {
		ok := true
		for j, r := range q {
			if unicode.ToLower(runes[i+j]) != r {
				ok = false
				break
			}
		}
		if ok {
			if first < 0 {
				first = i
			}
			count++
			i += len(q)
			continue
		}
		i++
	}
	return count, first
}

func snipPlain(text string, at, n int) (string, [2]int) {
	runes := []rune(text)
	start := max(0, at-before)
	for i := at - 1; i >= start; i-- {
		if runes[i] == '\n' {
			start = i + 1
			break
		}
	}
	end := min(len(runes), at+n+after)
	head := strings.TrimLeft(collapse(runes[start:at]), " ")
	if start > 0 && runes[start-1] != '\n' {
		head = "…" + head
	}
	match := collapse(runes[at : at+n])
	tail := strings.TrimRight(collapse(runes[at+n:end]), " ")
	if end < len(runes) {
		tail += "…"
	}
	return head + match + tail, [2]int{len(head), len(head) + len(match)}
}

func anchorPlain(text string, runes [][]rune, terms []string, firstHits []int) (termIdx, runePos, bestDistinct int) {
	for i, t := range terms {
		if Worth(t) {
			termIdx = i
			break
		}
	}
	runePos = firstHits[termIdx]
	bestDistinct = 1
	docRunes := []rune(text)
	lineStart := 0
	for lineStart <= len(docRunes) {
		lineEnd := lineStart
		for lineEnd < len(docRunes) && docRunes[lineEnd] != '\n' {
			lineEnd++
		}
		line := docRunes[lineStart:lineEnd]
		distinct := 0
		earliestIdx := -1
		earliestPos := len(line) + 1
		for i, q := range runes {
			if pos := findRunesFirst(line, q); pos >= 0 {
				distinct++
				if earliestIdx < 0 || pos < earliestPos || (pos == earliestPos && Worth(terms[i]) && !Worth(terms[earliestIdx])) {
					earliestIdx = i
					earliestPos = pos
				}
			}
		}
		if distinct > bestDistinct && earliestIdx >= 0 {
			bestDistinct = distinct
			termIdx = earliestIdx
			runePos = lineStart + earliestPos
			if bestDistinct == len(runes) {
				break
			}
		}
		lineStart = lineEnd + 1
	}
	return termIdx, runePos, bestDistinct
}

func TestLowerIsUnicodeToLower(t *testing.T) {
	for r := rune(-1); r <= unicode.MaxRune+1; r++ {
		if got, want := lower(r), unicode.ToLower(r); got != want {
			t.Fatalf("lower(%U) = %U, want %U", r, got, want)
		}
	}
}

func TestSearchAgreesWithPlainDefinitions(t *testing.T) {
	long := strings.Repeat("前言 intro 段落。\n", 40) + "Go 语言 and 周报 together\n" + strings.Repeat("尾声 outro。\n", 40)
	texts := []string{
		"",
		"a",
		"aaaa",
		"Hello World, hello world. HELLO",
		"周报：本周完成了 Go 重构\n第二行 周报 go\n\n最后 GO",
		"İstanbul ıSTANBUL Straße ÉCOLE école",
		"broken \xff\xfe utf8 \xe4\xb8 tail 周",
		"line one\nline two\r\n\tindented   spaces\n",
		"😀 emoji 😀😀 mixed 字",
		long,
	}
	queries := []string{"a", "aa", "hello", "周报", "go", "周", "é", "i", "\ufffd", "tail 周", "😀", "outro。\n尾声", "zz", "尾声"}
	for _, text := range texts {
		for _, query := range queries {
			q := []rune(strings.Map(unicode.ToLower, query))
			count, first := find(text, q)
			wantCount, wantFirst := findPlain(text, q)
			if count != wantCount || first != wantFirst {
				t.Fatalf("find(%q, %q) = %d, %d; want %d, %d", text, query, count, first, wantCount, wantFirst)
			}
			if count == 0 {
				continue
			}
			got, gotAt := snip(text, first, len(q))
			want, wantAt := snipPlain(text, first, len(q))
			if got != want || gotAt != wantAt {
				t.Fatalf("snip(%q, %d, %d) = %q %v; want %q %v", text, first, len(q), got, gotAt, want, wantAt)
			}
		}
		for _, pair := range [][]string{{"周报", "go"}, {"hello", "world"}, {"a", "周"}, {"go", "语言", "周报"}, {"intro", "outro"}} {
			var runes [][]rune
			var firsts []int
			found := true
			for _, term := range pair {
				q := []rune(term)
				_, first := find(text, q)
				found = found && first >= 0
				runes, firsts = append(runes, q), append(firsts, first)
			}
			if !found {
				continue
			}
			a, b, c := bestMultiSnippetAnchor(text, runes, pair, firsts)
			x, y, z := anchorPlain(text, runes, pair, firsts)
			if a != x || b != y || c != z {
				t.Fatalf("anchor(%q, %q) = %d %d %d; want %d %d %d", text, pair, a, b, c, x, y, z)
			}
		}
	}
}

// BenchmarkSearchMulti is a query of two terms over 2,000 documents of
// mixed Chinese and English, as each key typed into the command bar runs.
func BenchmarkSearchMulti(b *testing.B) {
	ix := &Index{entries: map[key]*entry{}}
	body := strings.Repeat("这是一段中文内容，用来模拟真实笔记。FreeFolio keeps Markdown files in place, with inline code and links.\n", 14)
	for i := 0; i < 2000; i++ {
		k := key{"s", strings.Repeat("d", 1+i%7) + string(rune('a'+i%26)) + strings.Repeat("x", i%11) + ".md"}
		k.path = k.path + "-" + strings.Repeat("i", i%3) + string(rune('0'+i%10)) + string(rune('0'+i/10%10)) + string(rune('0'+i/100%10)) + string(rune('0'+i/1000))
		text := body
		if i%10 == 0 {
			text += "周报 summary of the week\n"
		}
		ix.entries[k] = &entry{space: k.space, path: k.path, text: text}
		ix.order = append(ix.order, k)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if hits := ix.SearchMulti("周报 week", nil, 9); len(hits) != 9 {
			b.Fatal(len(hits))
		}
	}
}
