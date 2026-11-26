package editor

import (
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

type boardSearchState struct {
	query, cached string
	rows          []whiteboard.SummaryLine
	scene         *whiteboard.Scene
	revision      uint64
	index         int
	focus, reveal bool
}

func (b *Board) searchPanel(c *ui.Context) {
	s := &b.search
	field := ui.TextInput(c.Key("board-search-input"), &s.query).Placeholder(i18n.T("搜索白板内容")).Label(i18n.T("搜索白板内容")).WidthPercent(100)
	if s.focus {
		field.Focus()
		s.focus = false
	}
	if s.scene != b.Scene || s.revision != b.revision || s.cached != s.query {
		s.rows = b.Scene.Search(s.query)
		s.scene = b.Scene
		s.revision = b.revision
		s.cached = s.query
		s.index = 0
		s.reveal = true
	}
	if field.Shortcut(0, ui.KeyDown) {
		s.index = min(len(s.rows)-1, s.index+1)
		s.reveal = true
	}
	if field.Shortcut(0, ui.KeyUp) {
		s.index = max(0, s.index-1)
		s.reveal = true
	}
	if field.Shortcut(0, ui.KeyEscape) {
		b.panel = ""
		b.RequestFocus = true
		return
	}
	if field.Submitted() && s.index >= 0 && s.index < len(s.rows) {
		b.RevealElement(s.rows[s.index].ElementID)
	}
	if len(s.rows) == 0 {
		ui.Text(c, i18n.T("没有匹配的白板内容")).FontSize(13)
		return
	}
	ui.Scroll(c.Key("board-search-results")).Height(min(380, max(120, b.lastRect.H-230))).WidthPercent(100).Children(func() {
		for i, row := range s.rows {
			title := strings.Join(strings.Fields(row.Text), " ")
			if runes := []rune(title); len(runes) > 100 {
				title = string(runes[:100]) + "…"
			}
			button := ui.ButtonBase(c.Key("board-search-result-"+row.ElementID)).WidthPercent(100).Padding(7, 10).Radius(6).Focusable().Label(title)
			if i == s.index {
				button.Background(c.Theme().Accent.Alpha(.14))
				if s.reveal {
					button.ScrollIntoView()
				}
			} else if button.Hovered() {
				button.Background(c.Theme().SurfaceHover)
			}
			button.Children(func() { ui.Text(c, title).FontSize(13).SingleLine() })
			if button.Clicked() {
				s.index = i
				b.RevealElement(row.ElementID)
			}
		}
	})
	s.reveal = false
}

func (b *Board) outlinePanel(c *ui.Context, o BoardOptions) {
	ui.Text(c, i18n.T("每行一个主题，用缩进表示子主题")).FontSize(13)
	ui.TextArea(c.Key("board-outline-input"), &b.outline).Height(230).WidthPercent(100).Label(i18n.T("文字大纲"))
	if ui.Button(c, i18n.T("插入思维导图")).Disabled(strings.TrimSpace(b.outline) == "" || b.ReadOnly).Clicked() {
		s, err := whiteboard.OutlineScene(b.outline, boardMeasure)
		if err != nil {
			b.err = err
			return
		}
		if b.pasteScene(s, o) {
			b.panel = ""
			b.RequestFocus = true
		}
	}
}

func (b *Board) autoLayout(direction string, o BoardOptions) {
	if b.ReadOnly {
		return
	}
	b.edit(o, func() { b.err = b.Scene.AutoLayout(b.SelectedIDs(), direction) })
	b.RequestFocus = true
}

func (b *Board) layoutMenu(m *ui.Menu, o BoardOptions) {
	for _, item := range []struct{ direction, label string }{{"LR", "自动排列：从左到右"}, {"RL", "自动排列：从右到左"}, {"TB", "自动排列：从上到下"}, {"BT", "自动排列：从下到上"}} {
		if m.Item(i18n.T(item.label)).Chosen() {
			b.autoLayout(item.direction, o)
		}
	}
}
