package editor

import (
	"slices"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// The note's properties, as Obsidian shows and edits them: the keys of the
// YAML frontmatter the editor keeps as written (Document.Frontmatter),
// listed above the first block under a toggle that folds them away. A
// value edits as its type does: true and false as a check box, a list as
// chips, anything else as one line of text; a key renames, and a property
// can be added or deleted.
//
// Editing rewrites only the lines of the property changed, so comments and
// every other key keep their text. A property whose lines hold what the
// list cannot show (a nested map, a block scalar, a comment within, an
// anchor, a tag or a flow map) only shows: rewriting it would lose that,
// and the source mode edits it instead.

const (
	propsToggleH = 30 // the toggle's row
	propsRowH    = 30 // a property's row, before a list wraps
	propsGap     = 48 // between the properties and the first block
	propsKeyW    = 160
)

// reservedProperties are the keys FreeMD reads into the page itself (the
// icon, cover and word goal) and writes ahead of the others; a property of
// that name would be written twice.
var reservedProperties = []string{"icon", "cover", "coverY", "goal"}

// property is a key of the frontmatter and what it holds: one value, or
// the items of a list. start and end are the lines of the frontmatter it
// spans, end exclusive; fixed marks one that only shows.
type property struct {
	key        string
	values     []string
	list       bool
	start, end int
	fixed      bool
}

// parseProperties reads the top-level keys of YAML frontmatter, as far as
// a list of properties needs: scalars, flow and block lists, and block
// scalars or nested maps shown as their text on one line. Comments and
// lines before the first key are skipped.
func parseProperties(fm string) []property {
	var props []property
	var cur *property
	var folded []string // the indented lines under cur that are not list items
	flush := func() {
		if cur != nil && !cur.list && len(folded) > 0 {
			cur.values = []string{strings.Join(folded, " ")}
		}
		folded = folded[:0]
	}
	for i, line := range strings.Split(fm, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		indented := line != "" && (line[0] == ' ' || line[0] == '\t')
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if cur != nil && indented {
				cur.fixed = true // a comment within its lines
			}
			continue
		}
		if indented || strings.HasPrefix(line, "- ") || line == "-" {
			if cur == nil {
				continue
			}
			cur.end = i + 1
			if item, ok := strings.CutPrefix(trimmed, "-"); ok && len(folded) == 0 && (item == "" || item[0] == ' ') {
				cur.list = true
				item = strings.TrimSpace(item)
				if fixedYAMLValue(item) || strings.Contains(item, ": ") {
					cur.fixed = true
				}
				if v := unquoteYAMLScalar(item); v != "" {
					cur.values = append(cur.values, v)
				}
				continue
			}
			// A nested map, a block scalar's text or a plain scalar
			// running on: shown on one line, edited in the source.
			cur.fixed = true
			if !cur.list {
				folded = append(folded, trimmed)
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		flush()
		props = append(props, property{key: unquoteYAMLScalar(strings.TrimSpace(k)), start: i, end: i + 1})
		cur = &props[len(props)-1]
		v = strings.TrimSpace(v)
		switch {
		case v == "":
		case strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">"):
			cur.fixed = true
		case strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]"):
			cur.list = true
			for _, item := range strings.Split(v[1:len(v)-1], ",") {
				item = strings.TrimSpace(item)
				if fixedYAMLValue(item) {
					cur.fixed = true
				}
				if item = unquoteYAMLScalar(item); item != "" {
					cur.values = append(cur.values, item)
				}
			}
		default:
			cur.fixed = fixedYAMLValue(v) || strings.HasPrefix(v, "[")
			cur.values = []string{unquoteYAMLScalar(v)}
		}
	}
	flush()
	return props
}

// fixedYAMLValue reports whether the scalar v is more than text: a flow
// map, an anchor, an alias or a tag, or a quote that does not close.
func fixedYAMLValue(v string) bool {
	if v == "" {
		return false
	}
	switch v[0] {
	case '{', '&', '*', '!':
		return true
	case '"', '\'':
		return len(v) < 2 || v[len(v)-1] != v[0]
	}
	return false
}

// formatProperty is the YAML lines of a property: a list as one item a
// line, as Obsidian writes it; an empty value as the key alone.
func formatProperty(key string, values []string, list bool) []string {
	head := formatYAMLScalar(key) + ":"
	if !list {
		if len(values) == 0 || values[0] == "" {
			return []string{head}
		}
		return []string{head + " " + formatYAMLScalar(values[0])}
	}
	lines := []string{head}
	for _, v := range values {
		lines = append(lines, "  - "+formatYAMLScalar(v))
	}
	return lines
}

// spliceLines puts with in place of lines [start, end) of fm.
func spliceLines(fm string, start, end int, with []string) string {
	var lines []string
	if fm != "" {
		lines = strings.Split(fm, "\n")
	}
	start, end = min(start, len(lines)), min(end, len(lines))
	lines = slices.Concat(lines[:start], with, lines[end:])
	return strings.Join(lines, "\n")
}

// checkPropertyName returns the error a key name meets, or "": it must not
// be empty, be another property's or one FreeMD keeps for the page, or
// hold what YAML would read as more than a name.
func checkPropertyName(props []property, name string, except int) string {
	switch {
	case name == "":
		return i18n.T("属性名不能为空")
	case strings.ContainsAny(name, "\n\r\t") || strings.Contains(name, ": ") || strings.HasSuffix(name, ":") || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "#"):
		return i18n.T("属性名不能包含冒号、换行或以 - 和 # 开头")
	case slices.Contains(reservedProperties, name):
		return i18n.T("这个属性名由 FreeMD 用于页面图标、封面或字数目标")
	}
	for i, p := range props {
		if i != except && p.key == name {
			return i18n.T("已有同名属性")
		}
	}
	return ""
}

// properties are the document's, parsed once for each frontmatter. Printing
// leaves them out, as the page's controls.
func (e *Editor) properties() []property {
	if e.printing {
		return nil
	}
	if e.Doc.Frontmatter != e.propsSource {
		e.propsSource, e.props = e.Doc.Frontmatter, parseProperties(e.Doc.Frontmatter)
	}
	return e.props
}

// propsEditing is the state of the properties being edited: the text typed
// in each field (by the field's id) until it is committed, which fields had
// the focus in the last frame, a new property's name being typed, the
// error of the last name refused, and the measured height of the list.
type propsEditing struct {
	drafts  map[string]string
	focused map[string]bool
	adding  bool
	newName string
	err     string
	height  float32
}

// propsState is the editing state, its maps made.
func (e *Editor) propsState() *propsEditing {
	ps := &e.propsEdit
	if ps.drafts == nil {
		ps.drafts, ps.focused = map[string]string{}, map[string]bool{}
	}
	return ps
}

// setProperties writes fm as the document's frontmatter, one step of the
// history.
func (e *Editor) setProperties(fm string) {
	e.propsEdit.err = ""
	e.Engine.SetFrontmatter(fm)
	e.sync()
}

// setProperty writes property i with values, as a list or one value.
func (e *Editor) setProperty(i int, values []string, list bool) {
	p := e.props[i]
	e.setProperties(spliceLines(e.Doc.Frontmatter, p.start, p.end, formatProperty(p.key, values, list)))
}

// renameProperty gives property i the key name, unless the name is refused.
func (e *Editor) renameProperty(i int, name string) {
	name = strings.TrimSpace(name)
	if name == e.props[i].key {
		return
	}
	if err := checkPropertyName(e.props, name, i); err != "" {
		e.propsEdit.err = err
		return
	}
	p := e.props[i]
	lines := strings.Split(e.Doc.Frontmatter, "\n")
	_, rest, _ := strings.Cut(lines[p.start], ":")
	e.setProperties(spliceLines(e.Doc.Frontmatter, p.start, p.start+1, []string{formatYAMLScalar(name) + ":" + rest}))
}

// deleteProperty takes property i out of the frontmatter.
func (e *Editor) deleteProperty(i int) {
	p := e.props[i]
	e.setProperties(spliceLines(e.Doc.Frontmatter, p.start, p.end, nil))
}

// addProperty appends an empty property of the key name, unless the name
// is refused, and reports whether it did.
func (e *Editor) addProperty(name string) bool {
	name = strings.TrimSpace(name)
	if err := checkPropertyName(e.properties(), name, -1); err != "" {
		e.propsEdit.err = err
		return false
	}
	fm := strings.TrimRight(e.Doc.Frontmatter, "\n")
	lines := 0
	if fm != "" {
		lines = strings.Count(fm, "\n") + 1
	}
	e.setProperties(spliceLines(fm, lines, lines, formatProperty(name, nil, false)))
	return true
}

// AddProperty shows the properties, unfolded, with a field for the name
// of a new one, as Obsidian's "Add file property" does.
func (e *Editor) AddProperty() {
	if e.ReadOnly {
		return
	}
	e.propsFolded = false
	e.propsEdit.adding, e.propsEdit.newName, e.propsEdit.err = true, "", ""
	e.ready = false
}

// propsKey changes whenever the properties' height may, so a layout that
// would otherwise only reshape the blocks lays the header out again.
func (e *Editor) propsKey() string {
	key := e.Doc.Frontmatter
	if e.propsFolded {
		key = "folded\x00" + key
	}
	if e.propsEdit.adding {
		key = "adding\x00" + key
	}
	return key + "\x00" + e.propsEdit.err + "\x00" + strconv.FormatFloat(float64(e.propsEdit.height), 'f', 1, 32)
}

// layoutProperties places the properties at y and returns where the
// blocks start. The list takes the height it measured as last shown, as a
// list of chips may wrap; until then, a row a property.
func (e *Editor) layoutProperties(o Options, y float32) float32 {
	h := &e.header
	h.propsRect = ui.Rect{}
	props := e.properties()
	adding := e.propsEdit.adding && !e.ReadOnly && !e.printing
	if len(props) == 0 && !adding {
		return y
	}
	height := float32(propsToggleH)
	if !e.propsFolded {
		height += float32(len(props)+1) * propsRowH
		if e.propsEdit.height > 0 {
			height = e.propsEdit.height
		}
	}
	h.propsRect = ui.Rect{X: e.padding, Y: y, W: max(80, o.Width-2*e.padding), H: height}
	return y + height + propsGap
}

// propsField is a one-line field editing current, the text of field id:
// what is typed stays a draft until Enter or leaving the field commits it,
// so only text the input method has committed reaches the file; Escape
// puts current back.
func (e *Editor) propsField(c *ui.Context, id, current, label string, commit func(string)) ui.Element {
	ps := e.propsState()
	draft, ok := ps.drafts[id]
	if !ok || !ps.focused[id] {
		draft = current
	}
	// Even padding, not a fixed height, centers the line in the box: a
	// height of 26 left less room than the line and pushed the text down.
	in := ui.TextInput(c.Key(id), &draft).Label(label).FontSize(13).Padding(4, 6).Grow(1).MinWidth(0)
	was, now := ps.focused[id], in.Focused()
	ps.focused[id] = now
	e.quietField(c, in, now, 4, 6)
	if now && c.Shortcut(0, ui.KeyEscape) {
		draft = current
	}
	ps.drafts[id] = draft
	if (in.Submitted() || was && !now) && draft != current {
		commit(draft)
	}
	return in
}

// quietField lets a property's field read as text, as Obsidian's do: no
// box until the pointer is over it or it has the focus, which keeps the
// field's own ring. Under the pointer it shows only a faint hairline, no
// fill: the input's own hover border, which it sets as it paints, would
// stand out, so it gets no width and the line is drawn here instead; its
// padding (py, px, as the field sets it) grows by the width taken, so the
// text does not move.
func (e *Editor) quietField(c *ui.Context, f ui.Element, focused bool, py, px float32) {
	switch {
	case focused:
	case f.Hovered():
		t := c.Theme()
		f.Background(ui.Color{}).Border(0, ui.Color{}).Padding(py+1, px+1).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Stroke(r, t.Border, t.Radius, 1)
		})
	default:
		f.Background(ui.Color{}).Border(1, ui.Color{})
	}
}

// propertiesView shows the properties where layoutProperties placed them,
// in the surface's coordinates.
func (e *Editor) propertiesView(c *ui.Context) {
	r := e.header.propsRect
	if r.H == 0 {
		return
	}
	pg := e.page
	editable := !e.ReadOnly
	col := ui.Column(c.Key("page-properties")).Absolute().Left(r.X).Top(r.Y).Width(r.W)
	col.Children(func() {
		icon := iconChevron
		if e.propsFolded {
			icon = iconChevronRight
		}
		toggle := ui.ButtonBase(c.Key("properties-toggle")).Height(propsToggleH).PaddingX(4).Gap(6).Radius(6).
			AlignSelf(ui.Start).AlignItems(ui.Center).Role(ui.RoleButton).Label(i18n.T("笔记属性")).Expanded(!e.propsFolded).
			KeepFocus().Cursor(ui.CursorPointer)
		if toggle.Hovered() {
			toggle.Background(pg.Hover)
		}
		toggle.Children(func() {
			ui.Icon(c, icon).FontSize(14).TextColor(pg.Caption)
			ui.Text(c, i18n.T("笔记属性")).FontSize(13).FontWeight(500).TextColor(pg.Caption)
		})
		if toggle.Clicked() {
			e.propsFolded = !e.propsFolded
			e.ready = false
		}
		if e.propsFolded {
			return
		}
		for i, p := range e.props {
			if !e.propertyRow(c, i, p, editable && !p.fixed) {
				return // the frontmatter changed under the list
			}
		}
		if !editable {
			return
		}
		ps := e.propsState()
		if ps.adding {
			ui.Row(c.Key("property-new")).Height(propsRowH).Gap(8).AlignItems(ui.Center).Children(func() {
				name := ui.TextInput(c.Key("property-new-name"), &ps.newName).Label(i18n.T("新属性名")).
					Placeholder(i18n.T("属性名")).FontSize(13).Padding(4, 6).Width(propsKeyW).Shrink(0)
				if !ps.focused["new"] {
					name.Focus()
				}
				was, now := ps.focused["new"], name.Focused()
				ps.focused["new"] = now
				switch {
				case name.Submitted() || was && !now:
					if strings.TrimSpace(ps.newName) == "" || e.addProperty(ps.newName) {
						ps.adding, ps.newName = false, ""
						delete(ps.focused, "new")
					}
				case now && c.Shortcut(0, ui.KeyEscape):
					ps.adding, ps.newName, ps.err = false, "", ""
					delete(ps.focused, "new")
				}
			})
		} else {
			add := ui.ButtonBase(c.Key("property-add")).Height(26).PaddingX(4).Gap(6).Radius(6).AlignSelf(ui.Start).
				AlignItems(ui.Center).Label(i18n.T("添加属性")).KeepFocus().Cursor(ui.CursorPointer)
			if add.Hovered() {
				add.Background(pg.Hover)
			}
			add.Children(func() {
				ui.Icon(c, iconPlus).FontSize(14).TextColor(pg.Caption)
				ui.Text(c, i18n.T("添加属性")).FontSize(13).TextColor(pg.Caption)
			})
			if add.Clicked() {
				e.AddProperty()
			}
		}
		if ps.err != "" {
			ui.Text(c, ps.err).FontSize(12).TextColor(pg.Danger).PaddingStart(4).Padding(4, 0)
		}
	})
	// A list of chips may wrap: the next layout takes the height shown.
	if h := col.LayoutBounds().H; h > 0 && h != e.propsEdit.height && !e.propsFolded {
		e.propsEdit.height = h
		c.After(0)
	}
}

// propertyRow shows property i, its fields when editable, and reports
// false once an edit changed the frontmatter, whose properties are then
// parsed again before more rows show.
func (e *Editor) propertyRow(c *ui.Context, i int, p property, editable bool) bool {
	t := c.Theme()
	pg := e.page
	before := e.Doc.Frontmatter
	row := ui.Row(c.Key("property:" + p.key)).MinHeight(propsRowH).Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		if !editable {
			ui.Text(c, p.key).Width(propsKeyW).Shrink(0).PaddingStart(7).FontSize(13).TextColor(pg.Caption).SingleLine().Tooltip(p.key)
		} else {
			ui.Row(c).Width(propsKeyW).Shrink(0).Children(func() {
				e.propsField(c, "key:"+p.key, p.key, i18n.T("属性名"), func(name string) { e.renameProperty(i, name) }).
					TextColor(pg.Caption)
			})
		}
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			e.propertyValue(c, i, p, editable)
		})
		if !editable {
			if p.fixed && !e.ReadOnly {
				ui.Icon(c, iconLocked).FontSize(12).TextColor(pg.Caption).Tooltip(i18n.T("这个属性含嵌套、多行或注释内容，请在源码模式中编辑"))
			}
			return
		}
		del := ui.ButtonBase(c.Key("property-delete")).Size(24, 24).Radius(6).Center().Label(i18n.T("删除属性")).
			Tooltip(i18n.T("删除属性")).KeepFocus().Cursor(ui.CursorPointer)
		ink := pg.Caption.Alpha(0.6)
		if del.Hovered() {
			del.Background(pg.Hover)
			ink = t.Danger
		}
		del.Children(func() { ui.Icon(c, iconClose).FontSize(13).TextColor(ink) })
		if del.Clicked() {
			e.deleteProperty(i)
		}
	})
	if editable {
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(i18n.T("列表")).Checked(p.list).Chosen() && e.Doc.Frontmatter == before {
				if p.list {
					e.setProperty(i, []string{strings.Join(p.values, ", ")}, false)
				} else {
					e.setProperty(i, slices.DeleteFunc(slices.Clone(p.values), func(v string) bool { return v == "" }), true)
				}
			}
			m.Separator()
			if m.Item(i18n.T("删除属性")).Chosen() && e.Doc.Frontmatter == before {
				e.deleteProperty(i)
			}
		})
	}
	return e.Doc.Frontmatter == before
}

// propertyValue shows what property i holds: a check box for true or
// false, chips for a list, a line of text otherwise.
func (e *Editor) propertyValue(c *ui.Context, i int, p property, editable bool) {
	t := c.Theme()
	pg := e.page
	value := ""
	if len(p.values) > 0 {
		value = p.values[0]
	}
	if !editable {
		switch {
		case len(p.values) == 0:
			ui.Text(c, i18n.T("空")).PaddingStart(7).FontSize(13).TextColor(pg.Caption.Alpha(0.7))
		case p.list:
			ui.Row(c).Gap(4).Shrink(1).MinWidth(0).Clip().AlignItems(ui.Center).Children(func() {
				for j, v := range p.values {
					tagChip(c.Key(j), v, nil)
				}
			})
		default:
			ui.Text(c, value).PaddingStart(7).FontSize(13).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0).Tooltip(value)
		}
		return
	}
	switch lower := strings.ToLower(value); {
	case p.list:
		e.tagList(c, i, p)
	case lower == "true" || lower == "false":
		on := lower == "true"
		if ui.Checkbox(c.Key("value-check"), &on, "").Label(p.key).Changed() {
			e.setProperty(i, []string{strconv.FormatBool(on)}, false)
		}
	default:
		e.propsField(c, "value:"+p.key, value, p.key, func(v string) { e.setProperty(i, []string{v}, false) }).
			Placeholder(i18n.T("空"))
	}
}

// tagList edits a list property as FreeMD's own chips before an input:
// MyGo's TokenField paints its chips itself, darker than FreeMD's tags and
// with the text off center, so the property builds them here. Enter, a
// comma or leaving the input adds what was typed; a chip's × takes it out.
// Only committed text is added: an input method's composition is not in
// the draft.
func (e *Editor) tagList(c *ui.Context, i int, p property) {
	t := c.Theme()
	ps := e.propsState()
	id := "tags:" + p.key
	draft := ps.drafts[id]
	box := ui.Row(c.Key("value-list")).Wrap().Gap(4).AlignItems(ui.Center).Grow(1).MinWidth(0).Padding(4, 6).Radius(t.Radius)
	var in ui.Element
	box.Children(func() {
		for _, v := range p.values {
			tagChip(c.Key("tag:"+v), v, func() {
				e.setProperty(i, slices.DeleteFunc(slices.Clone(p.values), func(x string) bool { return x == v }), true)
			})
		}
		in = ui.TextInputBase(c.Key("tag-input"), &draft).Label(p.key).FontSize(13).Grow(1).MinWidth(60)
	})
	was, now := ps.focused[id], in.Focused()
	ps.focused[id] = now
	switch {
	case now:
		box.Draw(func(pt *ui.Painter, r ui.Rect) { pt.Stroke(r, t.Focus, t.Radius, 1) })
	case box.Hovered():
		box.Draw(func(pt *ui.Painter, r ui.Rect) { pt.Stroke(r, t.Border, t.Radius, 1) })
	}
	if now && c.Shortcut(0, ui.KeyEscape) {
		draft = ""
	}
	// What precedes a comma is a tag at once, as in TokenField; Enter or
	// leaving the input adds the rest.
	pending, rest := "", draft
	switch {
	case in.Submitted() || was && !now:
		pending, rest = draft, ""
	case strings.Contains(draft, ","):
		k := strings.LastIndex(draft, ",")
		pending, rest = draft[:k], draft[k+1:]
	}
	ps.drafts[id] = rest
	values := slices.Clone(p.values)
	for _, v := range strings.Split(pending, ",") {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(values, v) {
			values = append(values, v)
		}
	}
	if len(values) != len(p.values) {
		e.setProperty(i, values, true)
	}
	if rest != draft {
		c.Invalidate()
	}
}

// tagChip shows a tag in a light pill; remove, when given, adds the ×
// that takes it out. The line box leaves its glyphs low, so the pill's
// padding is one DIP thinner on top: Chinese then sits centered and Latin
// capitals within half a DIP of it (measured on the properties snapshot).
func tagChip(c *ui.Context, text string, remove func()) {
	t := c.Theme()
	end := float32(8)
	if remove != nil {
		end = 3
	}
	ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(1, end, 2, 8).Radius(10).Background(t.Text.Alpha(0.07)).Shrink(0).Children(func() {
		ui.Text(c, text).FontSize(13).TextColor(t.Text).NoWrap()
		if remove == nil {
			return
		}
		label := i18n.T("移除标签") + " " + text
		x := ui.ButtonBase(c.Key("remove")).Size(14, 14).Radius(7).Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
		x.Draw(func(p *ui.Painter, r ui.Rect) {
			if x.Hovered() {
				p.Fill(r, t.Text.Alpha(0.10), r.H/2)
			}
			k := r.W * 0.32
			var path ui.Path
			path.MoveTo(r.X+k, r.Y+k).LineTo(r.X+r.W-k, r.Y+r.H-k).MoveTo(r.X+r.W-k, r.Y+k).LineTo(r.X+k, r.Y+r.H-k)
			p.StrokePath(&path, 1.25, t.TextMuted)
		})
		if x.Clicked() {
			remove()
		}
	})
}
