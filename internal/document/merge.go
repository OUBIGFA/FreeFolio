package document

// Block alignment and three-way merging are adapted from Quiet.md's merge.rs
// (MIT, copyright 2026 s1dashu). See third_party/manifests/upstreams.json.
// IDs are local editor identities; only persisted content establishes ancestry.

import (
	"encoding/json"
	"slices"
)

// MergeConflict locates an overlapping edit in the merged local-first document.
// An empty Field means Blocks at Path; Count may be zero for a local deletion.
type MergeConflict struct {
	Field string
	Path  []int
	Count int
}

// ChangedBlocks identifies rendered blocks absent from the comparison version.
// Counts, rather than a set, also identify an extra copy of an existing block.
func ChangedBlocks(before, after *Document) map[string]bool {
	counts := map[string]int{}
	for _, b := range before.Blocks {
		counts[mergeKey(b, false)]++
	}
	out := map[string]bool{}
	var mark func(*Block)
	mark = func(b *Block) {
		out[b.ID] = true
		for _, child := range b.Children {
			mark(child)
		}
	}
	for _, b := range after.Blocks {
		key := mergeKey(b, false)
		if counts[key] > 0 {
			counts[key]--
		} else {
			mark(b)
		}
	}
	return out
}

// MergeDocuments combines disjoint edits without merging competing text inside
// one paragraph. Conflicts retain ours; the caller must retain theirs as well.
func MergeDocuments(base, ours, theirs *Document) (*Document, []MergeConflict) {
	m := ours.Clone()
	var conflicts []MergeConflict
	m.Icon = mergeField("icon", base.Icon, ours.Icon, theirs.Icon, &conflicts)
	m.Cover = mergeField("cover", base.Cover, ours.Cover, theirs.Cover, &conflicts)
	m.CoverY = mergeField("coverY", base.CoverY, ours.CoverY, theirs.CoverY, &conflicts)
	m.Goal = mergeField("goal", base.Goal, ours.Goal, theirs.Goal, &conflicts)
	m.Frontmatter = mergeField("frontmatter", base.Frontmatter, ours.Frontmatter, theirs.Frontmatter, &conflicts)
	m.Blocks = mergeBlocks(base.Blocks, ours.Blocks, theirs.Blocks, nil, &conflicts)
	if len(m.Blocks) == 0 {
		m.Blocks = []*Block{NewBlock("paragraph", "")}
	}
	return m, conflicts
}

func mergeField[T comparable](name string, base, ours, theirs T, conflicts *[]MergeConflict) T {
	if ours == base {
		return theirs
	}
	if theirs != base && ours != theirs {
		*conflicts = append(*conflicts, MergeConflict{Field: name})
	}
	return ours
}

func mergeKey(b *Block, head bool) string {
	c := b.Clone()
	var clearIDs func(*Block)
	clearIDs = func(n *Block) {
		n.ID = ""
		for _, ch := range n.Children {
			clearIDs(ch)
		}
	}
	clearIDs(c)
	if head {
		c.Children = nil
	}
	raw, _ := json.Marshal(c)
	return string(raw)
}

func mergeKeys(blocks []*Block) []string {
	out := make([]string, len(blocks))
	for i, b := range blocks {
		out[i] = mergeKey(b, false)
	}
	return out
}

// Align exact blocks first. Pair equal-shaped replacement gaps by kind, then
// similar paragraphs in unequal gaps. Bounded LCS avoids quadratic allocation
// for large unrelated documents; common ends still anchor those safely.
func alignMerge(base, side []*Block, bk, sk []string) []int {
	out := make([]int, len(base))
	for i := range out {
		out[i] = -1
	}
	p := 0
	for p < len(bk) && p < len(sk) && bk[p] == sk[p] {
		out[p] = p
		p++
	}
	bend, send := len(bk), len(sk)
	for bend > p && send > p && bk[bend-1] == sk[send-1] {
		bend--
		send--
		out[bend] = send
	}
	type pair struct{ b, s int }
	var anchors []pair
	n, m := bend-p, send-p
	if n > 0 && m > 0 && int64(n)*int64(m) <= 2250000 {
		w := m + 1
		dp := make([]uint32, (n+1)*w)
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if bk[p+i] == sk[p+j] {
					dp[i*w+j] = dp[(i+1)*w+j+1] + 1
				} else {
					dp[i*w+j] = max(dp[(i+1)*w+j], dp[i*w+j+1])
				}
			}
		}
		for i, j := 0, 0; i < n && j < m; {
			if bk[p+i] == sk[p+j] {
				anchors = append(anchors, pair{p + i, p + j})
				i++
				j++
			} else if dp[(i+1)*w+j] >= dp[i*w+j+1] {
				i++
			} else {
				j++
			}
		}
	}
	anchors = append(anchors, pair{bend, send})
	b0, s0 := p, p
	for _, a := range anchors {
		if a.b-b0 == a.s-s0 {
			for i := 0; i < a.b-b0; i++ {
				if base[b0+i].Kind == side[s0+i].Kind {
					out[b0+i] = s0 + i
				}
			}
		} else {
			next := s0
			for i := b0; i < a.b; i++ {
				best, score := -1, 0.49
				for j := next; j < a.s; j++ {
					if base[i].Kind == side[j].Kind {
						v := mergeSimilarity(base[i].Text(), side[j].Text())
						if v > score {
							best, score = j, v
						}
					}
				}
				if best >= 0 {
					out[i] = best
					next = best + 1
				}
			}
		}
		if a.b < bend {
			out[a.b] = a.s
		}
		b0, s0 = a.b+1, a.s+1
	}
	return out
}

func mergeSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	ar, br := []rune(a), []rune(b)
	if len(ar) < 2 || len(br) < 2 {
		return 0
	}
	pairs := map[[2]rune]int{}
	for i := 1; i < len(ar); i++ {
		pairs[[2]rune{ar[i-1], ar[i]}]++
	}
	hits := 0
	for i := 1; i < len(br); i++ {
		p := [2]rune{br[i-1], br[i]}
		if pairs[p] > 0 {
			hits++
			pairs[p]--
		}
	}
	return float64(2*hits) / float64(len(ar)+len(br)-2)
}

func mergeBlocks(base, ours, theirs []*Block, parent []int, conflicts *[]MergeConflict) []*Block {
	bk, ok, tk := mergeKeys(base), mergeKeys(ours), mergeKeys(theirs)
	oa, ta := alignMerge(base, ours, bk, ok), alignMerge(base, theirs, bk, tk)
	var out []*Block
	appendClones := func(bs []*Block) {
		for _, b := range bs {
			out = append(out, b.Clone())
		}
	}
	b0, o0, t0 := 0, 0, 0
	for i := 0; i <= len(base); i++ {
		o, t := len(ours), len(theirs)
		if i < len(base) {
			o, t = oa[i], ta[i]
			if o < 0 || t < 0 {
				continue
			}
		}
		switch {
		case slices.Equal(ok[o0:o], bk[b0:i]):
			appendClones(theirs[t0:t])
		case slices.Equal(tk[t0:t], bk[b0:i]) || slices.Equal(ok[o0:o], tk[t0:t]):
			appendClones(ours[o0:o])
		case b0 == i:
			appendClones(ours[o0:o])
			appendClones(theirs[t0:t])
		default:
			*conflicts = append(*conflicts, MergeConflict{Path: append(slices.Clone(parent), len(out)), Count: o - o0})
			appendClones(ours[o0:o])
		}
		if i == len(base) {
			break
		}
		at := append(slices.Clone(parent), len(out))
		var r *Block
		switch {
		case tk[t] == bk[i] || ok[o] == tk[t]:
			r = ours[o].Clone()
		case ok[o] == bk[i]:
			r = theirs[t].Clone()
		default:
			bh, oh, th := mergeKey(base[i], true), mergeKey(ours[o], true), mergeKey(theirs[t], true)
			r = ours[o].Clone()
			if oh == bh {
				r = theirs[t].Clone()
			}
			if oh != bh && th != bh && oh != th {
				*conflicts = append(*conflicts, MergeConflict{Path: at, Count: 1})
			}
			// A structural kind change conflicts with children changed on the
			// other side. Never create children inside a non-container block.
			if r.Kind != base[i].Kind && len(base[i].Children) > 0 {
				*conflicts = append(*conflicts, MergeConflict{Path: at, Count: 1})
				r = ours[o].Clone()
			} else {
				r.Children = mergeBlocks(base[i].Children, ours[o].Children, theirs[t].Children, at, conflicts)
			}
		}
		r.ID = ours[o].ID
		out = append(out, r)
		b0, o0, t0 = i+1, o+1, t+1
	}
	return out
}
