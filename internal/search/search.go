// Package search finds text in the documents of every space, for the
// command bar. An Index is an immutable snapshot of the documents' plain
// text; Refresh makes the next one, reading again only the files whose
// size or modification time changed, so it can run on a goroutine while
// the window keeps using the last snapshot.
package search

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeMD/internal/vault"
	"github.com/OUBIGFA/FreeMD/internal/wiki"
)

// Root is a space's folder to index.
type Root struct {
	Space string // the space's ID
	Path  string // its folder
	// Files, when non-nil, limits indexing to an explicit collection.
	Files vault.Files
}

// Extract turns a document file's content into the plain text searched,
// blocks separated by newlines, and the page's icon. name is the file's
// name, telling .arcmd from Markdown. The app supplies it, as the document
// model and the Markdown importer live above this package.
type Extract func(name, content string) (Extracted, error)

// Extracted is what the index keeps of a document: its plain text and its
// page icon, an emoji or "".
type Extracted struct {
	Text, Icon string
}

type key struct{ space, path string }

// maxPDFIndexSize is the largest PDF whose text the index reads.
const maxPDFIndexSize = 32 << 20

type entry struct {
	file  string
	text  string
	icon  string
	size  int64
	mod   time.Time
	space string
	path  string
}

// Index is a snapshot of the documents' text. The zero value is empty.
type Index struct {
	entries map[key]*entry
	order   []key // spaces in the order given, then by path
}

// Refresh reads the roots' documents into a new Index, reusing what prev
// holds for files whose size and modification time are unchanged. A root
// that cannot be read leaves its documents out; files that cannot be read
// or extracted are left out too: search is a convenience, never the only
// way to a document.
func Refresh(prev *Index, roots []Root, extract Extract) *Index {
	ix, _ := RefreshChanged(prev, roots, extract)
	return ix
}

// RefreshChanged behaves like Refresh and additionally reports whether the
// resulting snapshot differs from prev (new/modified entries or changed order),
// allowing callers to skip unnecessary disk cache writes when nothing changed.
func RefreshChanged(prev *Index, roots []Root, extract Extract) (*Index, bool) {
	next := &Index{entries: map[key]*entry{}}
	changed := false
	for _, r := range roots {
		var docs []vault.Doc
		if r.Files != nil {
			docs = r.Files.List()
		} else {
			docs, _ = vault.List(r.Path)
		}
		for _, d := range docs {
			k := key{r.Space, d.Path}
			root, rel := r.Path, d.Path
			if r.Files != nil {
				full, err := r.Files.Resolve(rel)
				if err != nil {
					continue
				}
				root, rel = filepath.Dir(full), filepath.Base(full)
			}
			fullFile := filepath.Join(root, filepath.FromSlash(rel))
			info, err := os.Stat(fullFile)
			if err != nil {
				continue
			}
			if prev != nil {
				if e, ok := prev.entries[k]; ok && e.file == fullFile && e.size == info.Size() && e.mod.Equal(info.ModTime()) {
					next.entries[k] = e
					next.order = append(next.order, k)
					continue
				}
			}
			// A huge PDF stays findable by its title without loading tens of
			// megabytes during background indexing (ArcMD 14e92c1).
			if vault.IsPDF(d.Path) && info.Size() > maxPDFIndexSize {
				changed = true
				next.entries[k] = &entry{file: fullFile, size: info.Size(), mod: info.ModTime(), space: r.Space, path: d.Path}
				next.order = append(next.order, k)
				continue
			}
			content, err := vault.Read(root, rel)
			if err != nil {
				continue
			}
			ex, err := extract(d.Path, content)
			if err != nil {
				continue
			}
			changed = true
			next.entries[k] = &entry{file: fullFile, text: ex.Text, icon: ex.Icon, size: info.Size(), mod: info.ModTime(), space: r.Space, path: d.Path}
			next.order = append(next.order, k)
		}
	}
	if !changed && (prev == nil || !slices.Equal(prev.order, next.order)) {
		changed = true
	}
	return next, changed
}

// Len is how many documents the index holds.
func (ix *Index) Len() int {
	if ix == nil {
		return 0
	}
	return len(ix.entries)
}

// Hit is a document whose text holds the query.
type Hit struct {
	Space, Path string
	// Count is how many times the query appears.
	Count int
	// Snippet is the text around the first match on one line, Match the
	// byte range of the match in it.
	Snippet string
	Match   [2]int
	// Term is the matched search term used to generate Snippet/Match.
	Term string
}

// Live replaces the indexed text of one document, the one open with
// unsaved edits, for a search.
type Live struct {
	Space, Path, Text string
}

// Worth reports whether a query is long enough to search text for: two
// runes, or one that is not ASCII, as a single Chinese character is a
// word.
func Worth(query string) bool {
	q := strings.TrimSpace(query)
	n := utf8.RuneCountInString(q)
	return n >= 2 || n == 1 && q[0] >= utf8.RuneSelf
}

// WorthMulti reports whether a potentially space-separated query has at least
// one searchable term.
func WorthMulti(query string) bool {
	for _, t := range strings.Fields(strings.TrimSpace(query)) {
		if Worth(t) {
			return true
		}
	}
	return false
}

// Search finds the query literally, case-insensitively, as the editor's find box
// does, in every document. Hits come in the index's order, documents with
// more matches first, at most limit of them.
func (ix *Index) Search(query string, live *Live, limit int) []Hit {
	trimmed := strings.TrimSpace(query)
	q := []rune(strings.Map(unicode.ToLower, trimmed))
	if !Worth(query) || limit <= 0 {
		return nil
	}
	var hits []Hit
	seenLive := false
	look := func(space, path, text string) {
		count, first := find(text, q)
		if count == 0 {
			return
		}
		snippet, match := snip(text, first, len(q))
		hits = append(hits, Hit{Space: space, Path: path, Count: count, Snippet: snippet, Match: match, Term: trimmed})
	}
	if ix != nil {
		for _, k := range ix.order {
			e := ix.entries[k]
			if live != nil && live.Space == e.space && live.Path == e.path {
				seenLive = true
				look(e.space, e.path, live.Text)
				continue
			}
			look(e.space, e.path, e.text)
		}
	}
	if live != nil && !seenLive {
		look(live.Space, live.Path, live.Text)
	}
	slices.SortStableFunc(hits, func(a, b Hit) int { return b.Count - a.Count })
	return hits[:min(len(hits), limit)]
}

// SearchMulti finds documents containing all whitespace-separated query terms
// (case-insensitively, AND semantics). Single-term queries behave identically
// to Search. Multi-term queries prefer lines where multiple terms co-occur.
func (ix *Index) SearchMulti(query string, live *Live, limit int) []Hit {
	rawTerms := strings.Fields(strings.TrimSpace(query))
	if len(rawTerms) == 0 || limit <= 0 || !WorthMulti(query) {
		return nil
	}
	var terms []string
	var runes [][]rune
	seenTerm := make(map[string]bool, len(rawTerms))
	for _, t := range rawTerms {
		low := strings.Map(unicode.ToLower, t)
		if seenTerm[low] {
			continue
		}
		seenTerm[low] = true
		terms = append(terms, t)
		runes = append(runes, []rune(low))
	}
	if len(terms) == 1 {
		return ix.Search(terms[0], live, limit)
	}

	type ranked struct {
		hit   Hit
		score int
	}
	var results []ranked
	ix.each(live, func(space, path, text string) {
		totalCount := 0
		firstHits := make([]int, len(runes))
		for i, q := range runes {
			c, first := find(text, q)
			if c == 0 {
				return
			}
			totalCount += c
			firstHits[i] = first
		}
		anchorIdx, anchorRune, distinctLine := bestMultiSnippetAnchor(text, runes, terms, firstHits)
		snippet, match := snip(text, anchorRune, len(runes[anchorIdx]))
		score := totalCount + distinctLine*1000
		results = append(results, ranked{
			hit:   Hit{Space: space, Path: path, Count: totalCount, Snippet: snippet, Match: match, Term: terms[anchorIdx]},
			score: score,
		})
	})
	slices.SortStableFunc(results, func(a, b ranked) int { return b.score - a.score })
	n := min(len(results), limit)
	hits := make([]Hit, n)
	for i := 0; i < n; i++ {
		hits[i] = results[i].hit
	}
	return hits
}

func bestMultiSnippetAnchor(text string, runes [][]rune, terms []string, firstHits []int) (termIdx, runePos, bestDistinct int) {
	termIdx = 0
	for i, t := range terms {
		if Worth(t) {
			termIdx = i
			break
		}
	}
	runePos = firstHits[termIdx]
	bestDistinct = 1

	// Line by line, in one buffer: the whole document as runes at once
	// cost four times its size for every document a query found.
	var line []rune
	lineStart := 0
	for rest, more := text, true; more; {
		var raw string
		raw, rest, more = strings.Cut(rest, "\n")
		line = line[:0]
		for _, r := range raw {
			line = append(line, r)
		}
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
		lineStart += len(line) + 1
	}
	return termIdx, runePos, bestDistinct
}

func findRunesFirst(hay, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return -1
	}
	for i := 0; i <= len(hay)-len(needle); i++ {
		ok := true
		for j, r := range needle {
			if unicode.ToLower(hay[i+j]) != r {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// cased marks the pages of 256 runes that hold a rune with a lower case of
// its own, by the ranges unicode.ToLower itself reads.
var cased = func() (pages [unicode.MaxRune>>8 + 1]bool) {
	for _, c := range unicode.CaseRanges {
		if c.Delta[unicode.LowerCase] == 0 {
			continue
		}
		for page := c.Lo >> 8; page <= c.Hi>>8; page++ {
			pages[page] = true
		}
	}
	return pages
}()

// lower is unicode.ToLower, answering at once for ASCII and for the pages
// without case, as all of Chinese: looking each rune up in the case ranges
// was a fifth of a search.
func lower(r rune) rune {
	if r < utf8.RuneSelf {
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		return r
	}
	if r > unicode.MaxRune || !cased[r>>8] {
		return r
	}
	return unicode.ToLower(r)
}

// lowerWide decodes the rune a string begins with, one beyond ASCII, and
// returns it lowered with its width.
func lowerWide(s string) (rune, int) {
	r, w := utf8.DecodeRuneInString(s)
	if cased[r>>8] {
		r = unicode.ToLower(r)
	}
	return r, w
}

// find counts the non-overlapping matches of q (lowered) in text and
// returns the rune index of the first. It reads the text where it is: a
// copy of every document as runes, for every term of every query typed,
// was most of what a search cost. ASCII is lowered in the loop itself,
// which a call for every byte would slow by half.
func find(text string, q []rune) (count, first int) {
	first = -1
	if len(q) == 0 {
		return 0, first
	}
	for i, at := 0, 0; i < len(text); {
		c, w := text[i], 1
		r := rune(c)
		if c >= utf8.RuneSelf {
			r, w = lowerWide(text[i:])
		} else if 'A' <= c && c <= 'Z' {
			r += 'a' - 'A'
		}
		if r == q[0] {
			if end, ok := matchRest(text, i+w, q[1:]); ok {
				if first < 0 {
					first = at
				}
				count++
				i, at = end, at+len(q)
				continue
			}
		}
		i += w
		at++
	}
	return count, first
}

// matchRest reports whether q (lowered) is the text from byte i on, and
// the byte the match ends before.
func matchRest(text string, i int, q []rune) (end int, ok bool) {
	for _, want := range q {
		if i >= len(text) {
			return 0, false
		}
		r, w := utf8.DecodeRuneInString(text[i:])
		if lower(r) != want {
			return 0, false
		}
		i += w
	}
	return i, true
}

// Context around a match in a snippet, in runes.
const (
	before = 18
	after  = 60
)

// snip cuts the text around the match at rune at, n runes long, onto one
// line, whitespace runs as one space, and returns it with the match's byte
// range in it.
func snip(text string, at, n int) (string, [2]int) {
	// Only the runes around the match, with one more either side to tell
	// whether the text goes on: lo and hi are their rune indexes.
	lo, hi := max(0, at-before-1), at+n+after+1
	from, to := len(text), len(text)
	k := 0
	for i := range text {
		if k == lo {
			from = i
		}
		if k == hi {
			to = i
			break
		}
		k++
	}
	runes := []rune(text[from:to])
	at -= lo
	start := max(0, at-before)
	// Start at the line the match is on when it begins close by.
	for i := at - 1; i >= start; i-- {
		if runes[i] == '\n' {
			start = i + 1
			break
		}
	}
	end := min(len(runes), at+n+after)
	head := strings.TrimLeft(collapse(runes[start:at]), " ")
	if start > 0 && runes[start-1] != '\n' {
		head = "…" + head // cut inside a block, not at its start
	}
	match := collapse(runes[at : at+n])
	tail := strings.TrimRight(collapse(runes[at+n:end]), " ")
	if end < len(runes) {
		tail += "…"
	}
	return head + match + tail, [2]int{len(head), len(head) + len(match)}
}

// collapse turns each run of whitespace, newlines included, into a space.
func collapse(rs []rune) string {
	var b strings.Builder
	space := false
	for _, r := range rs {
		if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// each calls fn with every document's space, path and text, live's text in
// place of its indexed one, and live itself when the index lacks it.
func (ix *Index) each(live *Live, fn func(space, path, text string)) {
	seenLive := false
	if ix != nil {
		for _, k := range ix.order {
			e := ix.entries[k]
			if live != nil && live.Space == e.space && live.Path == e.path {
				seenLive = true
				fn(e.space, e.path, live.Text)
				continue
			}
			fn(e.space, e.path, e.text)
		}
	}
	if live != nil && !seenLive {
		fn(live.Space, live.Path, live.Text)
	}
}

// Icon is the page icon of the document at path of the space, as last
// indexed, or "".
func (ix *Index) Icon(space, path string) string {
	if ix == nil {
		return ""
	}
	if e, ok := ix.entries[key{space, path}]; ok {
		return e.icon
	}
	return ""
}

// TextOf returns the indexed plain text of the document at path in space, if present.
func (ix *Index) TextOf(space, path string) (string, bool) {
	if ix == nil {
		return "", false
	}
	if e, ok := ix.entries[key{space, path}]; ok {
		return e.text, true
	}
	return "", false
}

// Backlinks are the documents linking to the document named name with a
// [[name]], but for the one at self (its own space and path). Each hit's
// snippet is the line of the first link, Match the link in it.
func (ix *Index) Backlinks(name string, self Live, live *Live) []Hit {
	var hits []Hit
	ix.each(live, func(space, path, text string) {
		if space == self.Space && path == self.Path || !strings.Contains(text, "[[") {
			return
		}
		count, first, n := 0, -1, 0
		for _, l := range wiki.Links(text) {
			if wiki.Same(l.Target, name) {
				if first < 0 {
					first, n = utf8.RuneCountInString(text[:l.Start]), utf8.RuneCountInString(text[l.Start:l.End])
				}
				count++
			}
		}
		if count == 0 {
			return
		}
		snippet, match := snip(text, first, n)
		hits = append(hits, Hit{Space: space, Path: path, Count: count, Snippet: snippet, Match: match})
	})
	return hits
}

// Tagged are the documents holding the tag (case-insensitively; a tag
// holds its sub-tags, "#work" those with "#work/2026"), the snippet the
// line of the first.
func (ix *Index) Tagged(tag string, live *Live) []Hit {
	tag = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(tag), "#"))
	if tag == "" {
		return nil
	}
	var hits []Hit
	ix.each(live, func(space, path, text string) {
		if !strings.Contains(text, "#") {
			return
		}
		count, first, n := 0, -1, 0
		for _, t := range wiki.Tags(text) {
			name := strings.ToLower(t.Name)
			if name != tag && !strings.HasPrefix(name, tag+"/") {
				continue
			}
			if first < 0 {
				first, n = utf8.RuneCountInString(text[:t.Start]), utf8.RuneCountInString(text[t.Start:t.End])
			}
			count++
		}
		if count == 0 {
			return
		}
		snippet, match := snip(text, first, n)
		hits = append(hits, Hit{Space: space, Path: path, Count: count, Snippet: snippet, Match: match})
	})
	return hits
}

// TagCount is a tag and how many documents hold it.
type TagCount struct {
	Name  string
	Count int
}

// Tags are every tag of the documents, most used first, then by name,
// each spelled as first seen.
func (ix *Index) Tags(live *Live) []TagCount {
	counts := map[string]*TagCount{}
	var order []string
	ix.each(live, func(_, _, text string) {
		if !strings.Contains(text, "#") {
			return
		}
		for _, name := range wiki.TagNames(text) {
			k := strings.ToLower(name)
			if counts[k] == nil {
				counts[k] = &TagCount{Name: name}
				order = append(order, k)
			}
			counts[k].Count++
		}
	})
	out := make([]TagCount, 0, len(order))
	for _, k := range order {
		out = append(out, *counts[k])
	}
	slices.SortStableFunc(out, func(a, b TagCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

// WithLive overlays several open documents without changing the indexed
// snapshot. Search, tags and backlinks all see the same unsaved text.
func (ix *Index) WithLive(live []Live) *Index {
	if len(live) == 0 {
		return ix
	}
	next := &Index{entries: map[key]*entry{}}
	if ix != nil {
		for k, e := range ix.entries {
			next.entries[k] = e
		}
		next.order = slices.Clone(ix.order)
	}
	for _, d := range live {
		k := key{d.Space, d.Path}
		e := entry{space: d.Space, path: d.Path}
		if old := next.entries[k]; old != nil {
			e = *old
		} else {
			next.order = append(next.order, k)
		}
		e.text = d.Text
		next.entries[k] = &e
	}
	return next
}
