package app

// The page width control, ported from ArcMD 8e9fecf: narrow, standard and
// wide text columns, and a full width that spans the page with only its
// margins. Full width toggles back to the width chosen before it.

import (
	"fmt"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// pageWidthChoices are prefs.Pages with their names, narrow to full.
var pageWidthChoices = []struct{ value, label string }{
	{"narrow", "窄"},
	{"", "标准"},
	{"wide", "宽"},
	{"full", "全宽"},
}

func pageWidthLabel(page string) string {
	for _, opt := range pageWidthChoices {
		if opt.value == page {
			return opt.label
		}
	}
	return "标准"
}

// canAdjustPageWidth reports whether d lays its text out in the column:
// a rich text page, not a whiteboard, PDF or Markdown source.
func canAdjustPageWidth(d *document) bool {
	return d != nil && d.loadErr == nil && !d.source && d.board == nil && d.boardOverlay == nil && !d.isPDF()
}

func (a *App) setPageWidth(page string) {
	if a.prefs.Page == page {
		return
	}
	if page == "full" {
		a.pageWidthBeforeFull = a.prefs.Page
	}
	a.prefs.Page = page
	a.savePrefs()
}

// toggleFullWidth switches between full width and the width before it.
func (a *App) toggleFullWidth() {
	if !canAdjustPageWidth(a.doc) {
		return
	}
	if a.prefs.FullWidth() {
		a.setPageWidth(a.pageWidthBeforeFull)
		return
	}
	a.setPageWidth("full")
}

func (a *App) pageWidthMenu(m *ui.Menu) {
	for _, opt := range pageWidthChoices {
		item := m.Item(i18n.T(opt.label))
		if opt.value == "full" {
			item = a.menuKey(item, "full-width")
		}
		if item.Checked(a.prefs.Page == opt.value).Chosen() {
			a.setPageWidth(opt.value)
		}
	}
}

// pageWidthButton opens the page width menu from a document's corner,
// marked while the column is wider than standard.
func (a *App) pageWidthButton(c *ui.Context, d *document) {
	tip := a.withKey(fmt.Sprintf(i18n.T("页面宽度：%s"), i18n.T(pageWidthLabel(a.prefs.Page))), "full-width")
	b := iconButton(c, "page-width", iconPageWidth, i18n.T("页面宽度"), tip, false).Role(ui.RoleMenuButton)
	if a.prefs.Page == "wide" || a.prefs.Page == "full" {
		if !a.inWindow(d) && a.split == nil {
			b.Background(glass(c.Theme()))
		} else {
			b.Background(c.Theme().Accent.Alpha(.14))
		}
	}
	b.PopoverMenuButton(a.pageWidthMenu)
}
