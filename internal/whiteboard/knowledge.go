package whiteboard

import (
	"slices"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/wiki"
)

type SummaryLine struct{ ElementID, Text string }

// Search follows dim0's node palette: title, body and ID, across the whole
// board. Bound labels resolve to their owning card; results are never capped.
func (s *Scene) Search(query string) []SummaryLine {
	query = strings.ToLower(strings.TrimSpace(query))
	var out []SummaryLine
	for _, e := range s.Elements {
		if e.IsDeleted || e.Kind == KindText && e.ContainerID != "" {
			continue
		}
		text := strings.TrimSpace(s.ElementText(e) + "\n" + e.Name + "\n" + e.Link)
		if n := AINodeOf(e); n != nil {
			text = strings.TrimSpace(n.Title + "\n" + n.Instruction + "\n" + n.Note + "\n" + text)
			if result := n.Output(); result != nil && result.Kind == "text" {
				text += "\n" + result.Text
			}
		}
		if text == "" {
			text = string(e.Kind) + " " + e.ID
		}
		if query == "" || strings.Contains(strings.ToLower(text+" "+e.ID), query) {
			out = append(out, SummaryLine{e.ID, text})
		}
	}
	return out
}

func (s *Scene) MarkdownSummary() []SummaryLine {
	var out []SummaryLine
	for _, e := range s.Elements {
		if e.IsDeleted {
			continue
		}
		text := ""
		if e.Kind == KindText {
			text = e.OriginalText
			if text == "" {
				text = e.Text
			}
		} else if e.IsFrame() {
			text = e.Name
		} else if n := AINodeOf(e); n != nil {
			// A node's instruction and text result are searchable content;
			// media paths and settings are not.
			parts := []string{n.Title, n.Instruction}
			if out := n.Output(); out != nil && out.Kind == "text" {
				parts = append(parts, out.Text)
			}
			if n.Note != "" {
				parts = append(parts, "[["+n.Note+"]]")
			}
			text = strings.TrimSpace(strings.Join(slices.DeleteFunc(parts, func(p string) bool { return strings.TrimSpace(p) == "" }), "\n"))
		}
		if e.Link != "" {
			if text != "" {
				text += "\n"
			}
			if strings.HasPrefix(e.Link, "[[") || strings.Contains(e.Link, "://") {
				text += e.Link
			} else {
				text += "[[" + e.Link + "]]"
			}
		}
		if strings.TrimSpace(text) != "" {
			out = append(out, SummaryLine{e.ID, text})
		}
	}
	return out
}
func (s *Scene) WikiLinks() []wiki.Link {
	var out []wiki.Link
	for _, l := range s.MarkdownSummary() {
		out = append(out, wiki.Links(l.Text)...)
	}
	return out
}
func (s *Scene) RewriteWikiLinks(from, to string, m MeasureFunc) bool {
	return s.MapText(func(text string) string { v, _ := wiki.Rename(text, from, to); return v }, m)
}
func (s *Scene) MapText(fn func(string) string, m MeasureFunc) bool {
	changed := false
	for _, e := range s.Elements {
		if e.IsDeleted {
			continue
		}
		dirty := false
		if e.Kind == KindText {
			old := e.OriginalText
			if old == "" {
				old = e.Text
			}
			if next := fn(old); next != old {
				e.OriginalText = next
				e.Text = next
				s.LayoutText(e.ID, m)
				dirty = true
			}
		}
		if e.Name != "" {
			if next := fn(e.Name); next != e.Name {
				e.Name = next
				dirty = true
			}
		}
		if e.Link != "" {
			value := e.Link
			if !strings.HasPrefix(value, "[[") && !strings.Contains(value, "://") {
				value = "[[" + value + "]]"
			}
			if next := fn(value); next != value {
				e.Link = next
				dirty = true
			}
		}
		if dirty {
			Touch(e)
			changed = true
		}
	}
	return changed
}
