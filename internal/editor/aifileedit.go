package editor

import (
	"fmt"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/history"
)

// MarkdownSourceSpan maps the same source lines used by file tools back to
// native blocks, including frontmatter and preserved Markdown formatting.
type MarkdownSourceSpan struct {
	Block           string
	Index, From, To int
}

func MarkdownSourceSpans(doc *document.Document) (string, []MarkdownSourceSpan) {
	source := ExportMarkdown(doc)
	parsed := parseMarkdownSource(source)
	body, _, _, _, _, _, _ := splitFrontmatter(source)
	prefix := len(source) - len(body)
	var spans []MarkdownSourceSpan
	next := 0
	for _, ch := range parsed.chunks {
		if ch.from >= len(parsed.doc.Blocks) {
			continue
		}
		index := ch.from
		for j := next; j < len(doc.Blocks); j++ {
			if sameBlockSemantics(doc.Blocks[j], parsed.doc.Blocks[ch.from]) {
				index = j
				next = j + ch.to - ch.from
				break
			}
		}
		id := ""
		if index < len(doc.Blocks) {
			id = doc.Blocks[index].ID
		}
		from := strings.Count(source[:prefix+ch.start], "\n") + 1
		to := strings.Count(source[:prefix+ch.end], "\n") + 1
		spans = append(spans, MarkdownSourceSpan{id, index, from, to})
	}
	if len(spans) == 0 && len(doc.Blocks) > 0 {
		spans = append(spans, MarkdownSourceSpan{doc.Blocks[0].ID, 0, 1, strings.Count(strings.TrimSuffix(source, "\n"), "\n") + 1})
	}
	return source, spans
}

// MarkdownFileEdits translates a completed file-tool result into the existing
// native undo transactions. This is host integration, not a model-facing edit
// language. Unchanged blocks retain identity, fold state and source formatting.
func MarkdownFileEdits(original *document.Document, after string) ([]document.EditOperation, error) {
	next := ImportMarkdown(after)
	if err := next.Validate(); err != nil {
		return nil, err
	}
	keys := func(blocks []*document.Block) []string {
		out := make([]string, len(blocks))
		for i, b := range blocks {
			out[i] = fmt.Sprintf("%x", blockSemanticHash(b))
		}
		return out
	}
	diff := history.Diff(keys(original.Blocks), keys(next.Blocks))
	type group struct{ a, z, b, y int }
	var groups []group
	i, j := 0, 0
	for at := 0; at < len(diff); {
		if diff[at].Op == history.Same {
			if !sameBlockSemantics(original.Blocks[i], next.Blocks[j]) {
				return nil, fmt.Errorf("cannot map unchanged Markdown block")
			}
			next.Blocks[j] = original.Blocks[i].Clone()
			i++
			j++
			at++
			continue
		}
		g := group{a: i, b: j}
		for at < len(diff) && diff[at].Op != history.Same {
			if diff[at].Op == history.Removed {
				i++
			} else {
				j++
			}
			at++
		}
		g.z, g.y = i, j
		// Preserve IDs/folding for changed blocks that keep the same structure.
		for k := 0; k < min(g.z-g.a, g.y-g.b); k++ {
			carryFileEditIdentity(original.Blocks[g.a+k], next.Blocks[g.b+k])
		}
		groups = append(groups, g)
	}
	// Pure insertions borrow an unchanged neighbor as a stable location. Merge
	// overlapping anchors so all accepted operations remain non-overlapping.
	for n := range groups {
		g := &groups[n]
		if g.a == g.z {
			if g.z < len(original.Blocks) {
				g.z++
				g.y++
			} else {
				g.a--
				g.b--
			}
		}
	}
	var merged []group
	for _, g := range groups {
		if len(merged) > 0 && g.a < merged[len(merged)-1].z {
			p := &merged[len(merged)-1]
			p.z = max(p.z, g.z)
			p.y = max(p.y, g.y)
		} else {
			merged = append(merged, g)
		}
	}
	e := document.NewEngine(original)
	leaves := original.Leaves()
	var ops []document.EditOperation
	for _, g := range merged {
		lo, hi := -1, -1
		for _, leaf := range leaves {
			if leaf.Owner == original.Blocks[g.a].ID && lo < 0 {
				lo = leaf.Start
			}
			if leaf.Owner == original.Blocks[g.z-1].ID {
				hi = leaf.End
			}
		}
		if lo < 0 || hi < lo {
			return nil, fmt.Errorf("cannot locate Markdown edit in native document")
		}
		target, err := e.CaptureEdit(lo, hi)
		if err != nil {
			return nil, err
		}
		ops = append(ops, document.EditOperation{Kind: "replace_snapshot", Target: target, Blocks: next.Blocks[g.b:g.y]})
	}
	beforeMeta, afterMeta := document.PageMetadataOf(original), document.PageMetadataOf(next)
	if beforeMeta != afterMeta {
		target, err := e.CaptureEdit(0, e.TextLength())
		if err != nil {
			return nil, err
		}
		m := &document.MetadataEdit{}
		if beforeMeta.Icon != afterMeta.Icon {
			m.Icon = &afterMeta.Icon
		}
		if beforeMeta.Cover != afterMeta.Cover {
			m.Cover = &afterMeta.Cover
		}
		if beforeMeta.CoverY != afterMeta.CoverY {
			m.CoverY = &afterMeta.CoverY
		}
		if beforeMeta.Goal != afterMeta.Goal {
			m.Goal = &afterMeta.Goal
		}
		if beforeMeta.Frontmatter != afterMeta.Frontmatter {
			m.Frontmatter = &afterMeta.Frontmatter
		}
		ops = append(ops, document.EditOperation{Kind: "update_metadata", Target: target, ExpectedMetadata: &beforeMeta, Metadata: m})
	}
	// Markdown source-only edits (spacing, marker style, comments) still count
	// as edits. Preserve the exact supplied source when the rich model agrees.
	if len(ops) == 0 && after != ExportMarkdown(original) {
		target, err := e.CaptureEdit(0, e.TextLength())
		if err != nil {
			return nil, err
		}
		source := original.ImportSource
		ops = append(ops, document.EditOperation{Kind: "update_source", Target: target, ExpectedSource: &source, Text: after})
	}
	return ops, nil
}

func carryFileEditIdentity(before, after *document.Block) {
	if before.Kind != after.Kind {
		return
	}
	after.ID = before.ID
	if before.Kind != "toggle" {
		after.Folded = before.Folded
	}
	for i := 0; i < min(len(before.Children), len(after.Children)); i++ {
		carryFileEditIdentity(before.Children[i], after.Children[i])
	}
}
