package editor

import (
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// The note's properties, as Obsidian shows them: the keys of the YAML
// frontmatter the editor keeps as written (Document.Frontmatter), listed
// above the first block under a toggle that folds them away. They only
// show; the frontmatter is saved as it was read.

const (
	propsToggleH = 30 // the toggle's row
	propsRowH    = 30 // a property's row
	propsGap     = 16 // between the properties and the first block
	propsKeyW    = 160
)

// property is a key of the frontmatter and what it holds: one value, or
// the items of a list.
type property struct {
	key    string
	values []string
	list   bool
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
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(line, "- ") || line == "-" {
			if cur == nil {
				continue
			}
			if item, ok := strings.CutPrefix(trimmed, "-"); ok && len(folded) == 0 && (item == "" || item[0] == ' ') {
				cur.list = true
				if v := unquoteYAMLScalar(strings.TrimSpace(item)); v != "" {
					cur.values = append(cur.values, v)
				}
				continue
			}
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
		props = append(props, property{key: strings.TrimSpace(k)})
		cur = &props[len(props)-1]
		v = strings.TrimSpace(v)
		switch {
		case v == "" || v == "|" || v == ">" || strings.HasPrefix(v, "|-") || strings.HasPrefix(v, ">-"):
		case strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]"):
			cur.list = true
			for _, item := range strings.Split(v[1:len(v)-1], ",") {
				if item = unquoteYAMLScalar(strings.TrimSpace(item)); item != "" {
					cur.values = append(cur.values, item)
				}
			}
		default:
			cur.values = []string{unquoteYAMLScalar(v)}
		}
	}
	flush()
	return props
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

// propsKey changes whenever the properties' height may, so a layout that
// would otherwise only reshape the blocks lays the header out again.
func (e *Editor) propsKey() string {
	if e.propsFolded {
		return "folded\x00" + e.Doc.Frontmatter
	}
	return e.Doc.Frontmatter
}

// layoutProperties places the properties at y and returns where the
// blocks start.
func (e *Editor) layoutProperties(o Options, y float32) float32 {
	h := &e.header
	h.propsRect = ui.Rect{}
	props := e.properties()
	if len(props) == 0 {
		return y
	}
	height := float32(propsToggleH)
	if !e.propsFolded {
		height += float32(len(props)) * propsRowH
	}
	h.propsRect = ui.Rect{X: e.padding, Y: y, W: max(80, o.Width-2*e.padding), H: height}
	return y + height + propsGap
}

// propertiesView shows the properties where layoutProperties placed them,
// in the surface's coordinates.
func (e *Editor) propertiesView(c *ui.Context) {
	r := e.header.propsRect
	if r.H == 0 {
		return
	}
	t := c.Theme()
	pg := e.page
	ui.Column(c.Key("page-properties")).Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).Children(func() {
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
			ui.Row(c.Key(i)).Height(propsRowH).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, p.key).Width(propsKeyW).Shrink(0).PaddingStart(4).FontSize(13).TextColor(pg.Caption).SingleLine().Tooltip(p.key)
				switch {
				case len(p.values) == 0:
					ui.Text(c, i18n.T("空")).FontSize(13).TextColor(pg.Caption.Alpha(0.7))
				case p.list:
					ui.Row(c).Gap(4).Shrink(1).MinWidth(0).Clip().AlignItems(ui.Center).Children(func() {
						for j, v := range p.values {
							ui.Text(c.Key(j), v).FontSize(12).TextColor(t.Text).Background(pg.Hover).Radius(10).
								Padding(2, 8).NoWrap().Shrink(0)
						}
					})
				default:
					ui.Text(c, p.values[0]).FontSize(13).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0).Tooltip(p.values[0])
				}
			})
		}
	})
}
