package document

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
)

var ErrEditConflict = errors.New("the original content changed; review against the latest document")

// EditTarget binds an asynchronous request to stable leaf IDs and the original
// containing structures. Absolute offsets are resolved again at application.
type EditTarget struct {
	First  string   `json:"first"`
	Last   string   `json:"last"`
	Start  int      `json:"start"`
	End    int      `json:"end"`
	Blocks []*Block `json:"blocks"`
	Text   string   `json:"text"`
}

type EditOperation struct {
	ExpectedSource   *string       `json:"expectedSource,omitempty"`
	ExpectedMetadata *PageMetadata `json:"expectedMetadata,omitempty"`
	Metadata         *MetadataEdit `json:"metadata,omitempty"`
	Kind             string        `json:"kind"`
	Target           EditTarget    `json:"target"`
	Text             string        `json:"text,omitempty"`
	Fragment         *Document     `json:"fragment,omitempty"`
	// Blocks replaces whole selected top-level structures, including media.
	Blocks []*Block `json:"blocks,omitempty"`
}

type PageMetadata struct {
	Icon        string  `json:"icon"`
	Cover       string  `json:"cover"`
	CoverY      float64 `json:"coverY"`
	Goal        int     `json:"goal"`
	Frontmatter string  `json:"frontmatter"`
}
type MetadataEdit struct {
	Icon        *string  `json:"icon,omitempty"`
	Cover       *string  `json:"cover,omitempty"`
	CoverY      *float64 `json:"coverY,omitempty"`
	Goal        *int     `json:"goal,omitempty"`
	Frontmatter *string  `json:"frontmatter,omitempty"`
}

func PageMetadataOf(d *Document) PageMetadata {
	return PageMetadata{d.Icon, d.Cover, d.CoverY, d.Goal, d.Frontmatter}
}
func setPageMetadata(d *Document, m PageMetadata) {
	d.Icon, d.Cover, d.CoverY, d.Goal, d.Frontmatter = m.Icon, m.Cover, m.CoverY, m.Goal, m.Frontmatter
}

type EditReceipt struct {
	Before *Document `json:"before"`
	After  *Document `json:"after"`
}

func (e *Engine) CaptureEdit(a, z int) (EditTarget, error) {
	if a < 0 || z < a || z > e.TextLength() {
		return EditTarget{}, errors.New("invalid edit range")
	}
	first, last := e.LeafAt(a), e.LeafAt(z)
	if z > a && last.Start == z {
		last = e.LeafAt(z - 1)
	}
	target := EditTarget{First: first.Block.ID, Last: last.Block.ID, Start: a - first.Start, End: z - last.Start, Text: e.TextRange(a, z)}
	if ByteOffset(first.Block.Text(), target.Start) < len(first.Block.Text()) && Len(first.Block.Text()[:ByteOffset(first.Block.Text(), target.Start)]) != target.Start {
		return EditTarget{}, errors.New("selection splits a Unicode character")
	}
	if Len(last.Block.Text()[:ByteOffset(last.Block.Text(), target.End)]) != target.End {
		return EditTarget{}, errors.New("selection splits a Unicode character")
	}
	include := false
	for _, b := range e.Doc.Blocks {
		if b.ID == first.Owner {
			include = true
		}
		if include {
			target.Blocks = append(target.Blocks, b.Clone())
		}
		if b.ID == last.Owner {
			break
		}
	}
	return target, nil
}

func (e *Engine) ResolveEdit(t EditTarget) (int, int, error) {
	first, ok := e.LeafByID(t.First)
	if !ok {
		return 0, 0, ErrEditConflict
	}
	last, ok := e.LeafByID(t.Last)
	if !ok {
		return 0, 0, ErrEditConflict
	}
	if t.Start < 0 || t.Start > first.End-first.Start || t.End < 0 || t.End > last.End-last.Start {
		return 0, 0, ErrEditConflict
	}
	a, z := first.Start+t.Start, last.Start+t.End
	if z < a {
		return 0, 0, ErrEditConflict
	}
	var current []*Block
	include := false
	for _, b := range e.Doc.Blocks {
		if b.ID == first.Owner {
			include = true
		}
		if include {
			current = append(current, b)
		}
		if b.ID == last.Owner {
			break
		}
	}
	if !reflect.DeepEqual(current, t.Blocks) || e.TextRange(a, z) != t.Text {
		return 0, 0, ErrEditConflict
	}
	return a, z, nil
}

// ApplyAI validates every precondition before any mutation. Work happens on an
// isolated model, then commits as one undo step. Failed operations cannot leave
// a partially edited document or history entry.
func (e *Engine) ApplyAI(operations []EditOperation) (EditReceipt, error) {
	if e.ReadOnly {
		return EditReceipt{}, errors.New("document is read-only")
	}
	if len(operations) == 0 {
		return EditReceipt{}, errors.New("no edits to apply")
	}
	type located struct {
		op   EditOperation
		a, z int
	}
	items := make([]located, 0, len(operations))
	metadataSeen := false
	for _, op := range operations {
		if op.Kind == "update_source" && (op.ExpectedSource == nil || *op.ExpectedSource != e.Doc.ImportSource) {
			return EditReceipt{}, ErrEditConflict
		}
		if op.Kind == "update_metadata" && (op.ExpectedMetadata == nil || *op.ExpectedMetadata != PageMetadataOf(e.Doc)) {
			return EditReceipt{}, ErrEditConflict
		}
		a, z, err := e.ResolveEdit(op.Target)
		if err != nil {
			return EditReceipt{}, err
		}
		if op.Kind == "update_metadata" {
			if metadataSeen {
				return EditReceipt{}, errors.New("duplicate metadata change")
			}
			metadataSeen = true
			a, z = -1, -1
		}
		items = append(items, located{op, a, z})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].a > items[j].a })
	for i := 1; i < len(items); i++ {
		if items[i].op.Kind == "update_metadata" {
			continue
		}
		if items[i].z > items[i-1].a || items[i].a == items[i-1].a {
			return EditReceipt{}, errors.New("overlapping edits must be reviewed together")
		}
	}
	work := NewEngine(e.Doc)
	work.SetSelection(e.Anchor, e.Caret)
	for _, item := range items {
		op := item.op
		a, z := item.a, item.z
		switch op.Kind {
		case "update_source":
			work.Doc.ImportSource = op.Text
		case "update_metadata":
			m := op.Metadata
			if m == nil {
				return EditReceipt{}, errors.New("missing metadata changes")
			}
			if m.Icon != nil {
				work.Doc.Icon = *m.Icon
			}
			if m.Cover != nil {
				work.Doc.Cover = *m.Cover
			}
			if m.CoverY != nil {
				work.Doc.CoverY = *m.CoverY
			}
			if m.Goal != nil {
				work.Doc.Goal = *m.Goal
			}
			if m.Frontmatter != nil {
				work.Doc.Frontmatter = *m.Frontmatter
			}
		case "replace_text", "delete":
			text := op.Text
			if op.Kind == "delete" {
				text = ""
			}
			first, last := work.LeafAt(a), work.LeafAt(z)
			if z > a && last.Start == z {
				last = work.LeafAt(z - 1)
			}
			if first.Block.ID == last.Block.ID && (first.Block.Kind == "code" || first.Block.Kind == "raw" || first.Block.Kind == "math" || first.Block.Kind == "cell" || !strings.Contains(text, "\n")) {
				old := first.Block.Text()
				start, end := a-first.Start, z-first.Start
				selected := SliceRuns(first.Block.Runs, start, end)
				middle := rewriteStyledRuns(selected, text, styleAtRange(first.Block.Runs, 0, start, end))
				runs := Compact(append(append(SliceRuns(first.Block.Runs, 0, start), middle...), SliceRuns(first.Block.Runs, end, Len(old))...))
				comments := ShiftComments(first.Block.Comments, start, end, Len(text), Len(old)-end+start+Len(text))
				work.ReplaceBlockRunsAndComments(first.Block.ID, runs, comments, a+Len(text), a+Len(text), Style{}, false)
			} else {
				for _, leaf := range work.Doc.Leaves() {
					if leaf.End < a || leaf.Start > z {
						continue
					}
					if leaf.Block.Kind == "cell" || leaf.Block.Kind == "image" || leaf.Block.Kind == "raw" {
						return EditReceipt{}, errors.New("mixed structural content requires an explicit block edit")
					}
				}
				work.ReplaceLiteral(a, z, text)
			}
		case "replace_fragment", "paste_fragment", "insert_before", "insert_after":
			if op.Fragment == nil {
				return EditReceipt{}, errors.New("replacement fragment is missing")
			}
			if err := op.Fragment.Validate(); err != nil {
				return EditReceipt{}, err
			}
			if op.Kind == "replace_fragment" && wholeEditBlocks(work, op.Target, a, z) && sameEditShape(op.Target.Blocks, op.Fragment.Blocks) {
				for i, source := range op.Target.Blocks {
					parent, at := work.find(source.ID)
					if parent == nil {
						return EditReceipt{}, ErrEditConflict
					}
					(*parent)[at] = preserveEditBlock(source, op.Fragment.Blocks[i])
				}
				work.InvalidateIndex()
				work.SetSelection(a, min(z, work.TextLength()))
				break
			}
			if op.Kind == "insert_before" {
				z = a
			}
			if op.Kind == "insert_after" {
				a = z
			}
			work.SetSelection(a, z)
			work.Paste(op.Fragment)
		case "replace_blocks", "replace_snapshot":
			if len(op.Target.Blocks) == 0 {
				return EditReceipt{}, errors.New("block replacement has no target")
			}
			// Whole-block replacement cannot expand a partial selection silently.
			first, last := work.LeafAt(a), work.LeafAt(z)
			if z > a && last.Start == z {
				last = work.LeafAt(z - 1)
			}
			if a != first.Start || z != last.End {
				return EditReceipt{}, errors.New("block replacement requires whole blocks")
			}
			for _, leaf := range work.Doc.Leaves() {
				if leaf.Owner == op.Target.Blocks[0].ID && leaf.Start < a || leaf.Owner == op.Target.Blocks[len(op.Target.Blocks)-1].ID && leaf.End > z {
					return EditReceipt{}, errors.New("block replacement cannot extend outside the selected structure")
				}
			}
			start := -1
			for i, b := range work.Doc.Blocks {
				if b.ID == op.Target.Blocks[0].ID {
					start = i
					break
				}
			}
			if start < 0 {
				return EditReceipt{}, ErrEditConflict
			}
			blocks := cloneBlocks(op.Blocks)
			for i, b := range blocks {
				if b == nil {
					return EditReceipt{}, errors.New("replacement contains a null block")
				}
				if op.Kind == "replace_blocks" {
					assignEditIDs(b)
					if i < len(op.Target.Blocks) && b.Kind == op.Target.Blocks[i].Kind {
						b.ID = op.Target.Blocks[i].ID
					}
				}
			}
			work.Doc.Blocks = append(append(append([]*Block(nil), work.Doc.Blocks[:start]...), blocks...), work.Doc.Blocks[start+len(op.Target.Blocks):]...)
			if len(work.Doc.Blocks) == 0 {
				work.Doc.Blocks = []*Block{NewBlock("paragraph", "")}
			}
			work.InvalidateIndex()
		default:
			return EditReceipt{}, fmt.Errorf("unsupported edit operation %q", op.Kind)
		}
		if err := work.Doc.Validate(); err != nil {
			return EditReceipt{}, err
		}
	}
	receipt := EditReceipt{Before: e.Doc.Clone(), After: work.Doc.Clone()}
	e.Edit(func() { e.Doc = work.Doc; e.InvalidateIndex(); e.SetSelection(work.Anchor, work.Caret) })
	return receipt, nil
}

func wholeEditBlocks(e *Engine, t EditTarget, a, z int) bool {
	if len(t.Blocks) == 0 {
		return false
	}
	lo, hi := -1, -1
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Owner == t.Blocks[0].ID && lo < 0 {
			lo = leaf.Start
		}
		if leaf.Owner == t.Blocks[len(t.Blocks)-1].ID {
			hi = leaf.End
		}
	}
	return lo == a && hi == z
}
func sameEditShape(a, b []*Block) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] == nil || b[i] == nil || a[i].Kind != b[i].Kind || !sameEditShape(a[i].Children, b[i].Children) {
			return false
		}
	}
	return true
}

// Markdown omits native visual metadata. When its block structure is unchanged,
// carry that metadata through and preserve marks on unchanged text. Explicit
// native block operations remain available for deliberate formatting changes.
func preserveEditBlock(source, answer *Block) *Block {
	out := source.Clone()
	if source.Kind == "heading" {
		out.Level = answer.Level
	}
	if source.Kind == "task" {
		out.Checked = answer.Checked
	}
	if len(source.Children) > 0 {
		for i := range source.Children {
			out.Children[i] = preserveEditBlock(source.Children[i], answer.Children[i])
		}
		return out
	}
	out.Runs = rewriteStyledRuns(source.Runs, answer.Text(), styleAtRange(source.Runs, 0, 0, 0))
	out.Comments = shiftCommentsByDiff(source.Comments, source.Text(), answer.Text())
	// Preserve answer-authored emphasis and links without erasing richer source
	// styles that Markdown could not express.
	position := 0
	for _, run := range answer.Runs {
		end := position + Len(run.Text)
		if run.Style != (Style{}) {
			middle := SliceRuns(out.Runs, position, end)
			for i := range middle {
				m := &middle[i].Style
				v := run.Style
				m.Bold = m.Bold || v.Bold
				m.Italic = m.Italic || v.Italic
				m.Strike = m.Strike || v.Strike
				m.Underline = m.Underline || v.Underline
				m.Code = m.Code || v.Code
				if v.Link != "" {
					m.Link = v.Link
				}
				if v.Color != "" {
					m.Color = v.Color
				}
				if v.Highlight != "" {
					m.Highlight = v.Highlight
				}
			}
			out.Runs = Compact(append(append(SliceRuns(out.Runs, 0, position), middle...), SliceRuns(out.Runs, end, Len(answer.Text()))...))
		}
		position = end
	}
	if source.Kind == "image" {
		out.URL = answer.URL
	}
	if source.Kind == "code" && answer.Language != "" {
		out.Language = answer.Language
	}
	return out
}

func cloneBlocks(blocks []*Block) []*Block {
	out := make([]*Block, len(blocks))
	for i, b := range blocks {
		if b != nil {
			out[i] = b.Clone()
		}
	}
	return out
}
func assignEditIDs(b *Block) {
	if b == nil {
		return
	}
	b.ID = NewID()
	for _, child := range b.Children {
		assignEditIDs(child)
	}
}

// RebaseEdit accepts only unchanged original leaf content inside an otherwise
// changed containing block. Used after accepting independent edits from this
// same proposal; never used to bypass a user's concurrent content changes.
func (e *Engine) RebaseEdit(t EditTarget) (EditTarget, error) {
	first, ok := e.LeafByID(t.First)
	if !ok {
		return t, ErrEditConflict
	}
	last, ok := e.LeafByID(t.Last)
	if !ok {
		return t, ErrEditConflict
	}
	a, z := first.Start+t.Start, last.Start+t.End
	if a < first.Start || z > last.End || z < a || e.TextRange(a, z) != t.Text {
		return t, ErrEditConflict
	}
	return e.CaptureEdit(a, z)
}

// RevertAI reverses only changed top-level blocks. Unrelated later edits remain
// intact; edits overlapping a changed block require manual conflict resolution.
func (e *Engine) RevertAI(r EditReceipt) error {
	if e.ReadOnly {
		return errors.New("document is read-only")
	}
	if r.Before == nil || r.After == nil {
		return errors.New("missing edit receipt")
	}
	before, after := map[string]*Block{}, map[string]*Block{}
	for _, b := range r.Before.Blocks {
		before[b.ID] = b
	}
	for _, b := range r.After.Blocks {
		after[b.ID] = b
	}
	changed := map[string]bool{}
	for id, b := range before {
		if !reflect.DeepEqual(b, after[id]) {
			changed[id] = true
		}
	}
	for id, b := range after {
		if !reflect.DeepEqual(b, before[id]) {
			changed[id] = true
		}
	}
	current := map[string]*Block{}
	for _, b := range e.Doc.Blocks {
		current[b.ID] = b
	}
	// A reopened Markdown document has new IDs. Bind only unique unchanged
	// after-structures, never a fuzzy text occurrence. Existing IDs remain strict.
	ids := map[string]string{}
	bind := func(expected *Block) error {
		if expected == nil || current[expected.ID] != nil || ids[expected.ID] != "" {
			return nil
		}
		var found *Block
		matches := 0
		for _, block := range e.Doc.Blocks {
			if sameEditBlock(expected, block) {
				found = block
				matches++
			}
		}
		if matches != 1 {
			return ErrEditConflict
		}
		mapEditIDs(expected, found, ids)
		return nil
	}
	for id := range changed {
		if err := bind(after[id]); err != nil {
			return err
		}
	}
	// Restoring a deleted block also needs a surviving neighbor. Markdown
	// reopening regenerates those IDs even though the neighbor was not edited.
	for i, b := range r.Before.Blocks {
		if !changed[b.ID] || after[b.ID] != nil {
			continue
		}
		var anchor *Block
		for j := i + 1; j < len(r.Before.Blocks); j++ {
			if after[r.Before.Blocks[j].ID] != nil {
				anchor = after[r.Before.Blocks[j].ID]
				break
			}
		}
		if anchor == nil {
			for j := i - 1; j >= 0; j-- {
				if after[r.Before.Blocks[j].ID] != nil {
					anchor = after[r.Before.Blocks[j].ID]
					break
				}
			}
		}
		if err := bind(anchor); err != nil {
			return err
		}
	}
	if len(ids) > 0 {
		r.Before = r.Before.Clone()
		r.After = r.After.Clone()
		for _, d := range []*Document{r.Before, r.After} {
			var remap func(*Block)
			remap = func(b *Block) {
				if id := ids[b.ID]; id != "" {
					b.ID = id
				}
				for _, child := range b.Children {
					remap(child)
				}
			}
			for _, b := range d.Blocks {
				remap(b)
			}
		}
		return e.RevertAI(r)
	}
	for id := range changed {
		if !reflect.DeepEqual(current[id], after[id]) {
			return ErrEditConflict
		}
	}
	work := e.Doc.Clone()
	if r.Before.ImportSource != r.After.ImportSource {
		if e.Doc.ImportSource != r.After.ImportSource {
			return ErrEditConflict
		}
		work.ImportSource = r.Before.ImportSource
	}
	if PageMetadataOf(r.Before) != PageMetadataOf(r.After) {
		if PageMetadataOf(e.Doc) != PageMetadataOf(r.After) {
			return ErrEditConflict
		}
		setPageMetadata(work, PageMetadataOf(r.Before))
	}
	var kept []*Block
	for _, b := range work.Blocks {
		if !changed[b.ID] {
			kept = append(kept, b)
		} else if original := before[b.ID]; original != nil {
			kept = append(kept, original.Clone())
		}
	}
	for i, b := range r.Before.Blocks {
		if !changed[b.ID] || after[b.ID] != nil {
			continue
		}
		at := len(kept)
		anchored := false
		for j := i + 1; j < len(r.Before.Blocks); j++ {
			found := -1
			for k, cur := range kept {
				if cur.ID == r.Before.Blocks[j].ID {
					found = k
					break
				}
			}
			if found >= 0 {
				at = found
				anchored = true
				break
			}
		}
		if !anchored {
			for j := i - 1; j >= 0; j-- {
				for k, cur := range kept {
					if cur.ID == r.Before.Blocks[j].ID {
						at = k + 1
						anchored = true
						break
					}
				}
				if anchored {
					break
				}
			}
		}
		kept = slices.Insert(kept, at, b.Clone())
	}
	work.Blocks = kept
	if len(work.Blocks) == 0 {
		return ErrEditConflict
	}
	if err := work.Validate(); err != nil {
		return err
	}
	e.Edit(func() {
		e.Doc = work
		e.InvalidateIndex()
		e.SetSelection(min(e.Caret, e.TextLength()), min(e.Caret, e.TextLength()))
	})
	return nil
}

func sameEditBlock(a, b *Block) bool {
	a, b = a.Clone(), b.Clone()
	var clear func(*Block)
	clear = func(b *Block) {
		b.ID = ""
		for _, child := range b.Children {
			clear(child)
		}
	}
	clear(a)
	clear(b)
	return reflect.DeepEqual(a, b)
}
func mapEditIDs(a, b *Block, ids map[string]string) {
	ids[a.ID] = b.ID
	for i, child := range a.Children {
		mapEditIDs(child, b.Children[i], ids)
	}
}

// rewriteStyledRuns uses a bounded rune LCS so unchanged text keeps its exact
// style. Large rewrites retain common ends and inherit style for new content.
func rewriteStyledRuns(runs []Run, after string, inherit Style) []Run {
	before := blockText(runs)
	a, b := []rune(before), []rune(after)
	if len(a)*len(b) > 1_000_000 {
		prefix, suffix := commonPrefixSuffixU16(before, after)
		return Compact(append(append(SliceRuns(runs, 0, prefix), Run{Text: after[ByteOffset(after, prefix):ByteOffset(after, Len(after)-suffix)], Style: inherit}), SliceRuns(runs, Len(before)-suffix, Len(before))...))
	}
	w := len(b) + 1
	dp := make([]uint32, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i*w+j] = 1 + dp[(i+1)*w+j+1]
			} else {
				dp[i*w+j] = max(dp[(i+1)*w+j], dp[i*w+j+1])
			}
		}
	}
	i, j, offset := 0, 0, 0
	var out []Run
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			n := Len(string(a[i]))
			out = append(out, SliceRuns(runs, offset, offset+n)...)
			offset += n
			i++
			j++
		case j < len(b) && (i == len(a) || dp[i*w+j+1] >= dp[(i+1)*w+j]):
			out = append(out, Run{Text: string(b[j]), Style: inherit})
			j++
		default:
			offset += Len(string(a[i]))
			i++
		}
	}
	return Compact(out)
}
