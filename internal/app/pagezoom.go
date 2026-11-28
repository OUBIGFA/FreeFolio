package app

import (
	"fmt"
	"math"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/egoist/mygo/ui"
)

// pageZoomHost is what page zoom needs from the window.
type pageZoomHost interface {
	inWindow(d *document) bool
	menuKey(item *ui.MenuItem, id string) *ui.MenuItem
	modalOrOverlayBlocksEsc() bool
	preferences() *prefs.Prefs
	recordPeekOpen() bool
	savePrefs()
	splitShown() bool
	withKey(tip, id string) string
}

// pageZoomFeature scales the page from 50% to 200%: shortcuts, the menu and
// the title-bar button.
type pageZoomFeature struct {
	host pageZoomHost
}

var pageZoomPresets = []float32{0.75, 1.00, 1.25, 1.50, 2.00}

func (pz *pageZoomFeature) pageZoom() float32 {
	if pz.host.preferences() == nil {
		return prefs.DefaultPageZoom
	}
	return pz.host.preferences().Zoom()
}

func pageZoomPercent(z float32) int {
	return int(math.Round(float64(prefs.NormalizePageZoom(z)) * 100))
}

func (pz *pageZoomFeature) canZoomPage(d *document) bool {
	if !canAdjustPageWidth(d) {
		return false
	}
	if d.isMDZ() {
		if ed := d.mdz.activeEditor(); ed != nil && ed.HasZoomModal() {
			return false
		}
		return true
	}
	if d.ed != nil && d.ed.HasZoomModal() {
		return false
	}
	return true
}

func (pz *pageZoomFeature) overlayBlocksZoom() bool {
	return pz.host.modalOrOverlayBlocksEsc() || pz.host.recordPeekOpen()
}

func (pz *pageZoomFeature) setPageZoom(zoom float32) {
	if pz.host.preferences() == nil {
		return
	}
	next := prefs.NormalizePageZoom(zoom)
	if next == pz.host.preferences().Zoom() {
		return
	}
	if next == prefs.DefaultPageZoom {
		pz.host.preferences().PageZoom = 0
	} else {
		pz.host.preferences().PageZoom = next
	}
	pz.host.savePrefs()
}

func (pz *pageZoomFeature) stepPageZoom(delta int) {
	pz.setPageZoom(prefs.StepPageZoom(pz.pageZoom(), delta))
}

func (pz *pageZoomFeature) resetPageZoom() {
	pz.setPageZoom(prefs.DefaultPageZoom)
}

func (pz *pageZoomFeature) pageZoomMenu(m *ui.Menu) {
	z := pz.pageZoom()
	if pz.host.menuKey(m.Item(i18n.T("放大页面")), "zoom-in").Disabled(z >= prefs.MaxPageZoom).Chosen() {
		pz.stepPageZoom(1)
	}
	if pz.host.menuKey(m.Item(i18n.T("缩小页面")), "zoom-out").Disabled(z <= prefs.MinPageZoom).Chosen() {
		pz.stepPageZoom(-1)
	}
	if pz.host.menuKey(m.Item(i18n.T("重置为 100%")), "zoom-reset").Disabled(z == prefs.DefaultPageZoom).Chosen() {
		pz.resetPageZoom()
	}
	m.Separator()
	curPct := pageZoomPercent(z)
	for _, preset := range pageZoomPresets {
		pct := pageZoomPercent(preset)
		val := preset
		if m.Item(fmt.Sprintf("%d%%", pct)).Radio(curPct == pct).Chosen() {
			pz.setPageZoom(val)
		}
	}
}

func (pz *pageZoomFeature) showPageZoomButton(d *document) bool {
	return pz.canZoomPage(d) && pz.pageZoom() != prefs.DefaultPageZoom
}

// pageZoomButton draws a subtle percentage pill in the top-right corner when
// page zoom differs from 100%, staying hidden at 100% to keep the bar calm.
func (pz *pageZoomFeature) pageZoomButton(c *ui.Context, d *document) ui.Element {
	pct := pageZoomPercent(pz.pageZoom())
	t := c.Theme()
	tip := pz.host.withKey(fmt.Sprintf(i18n.T("页面缩放：%d%%"), pct), "zoom-reset")
	b := ui.ButtonBase(c.Key("page-zoom")).Height(28).PaddingX(8).Radius(7).Center().
		Cursor(ui.CursorPointer).Role(ui.RoleMenuButton).
		Label(i18n.T("页面缩放")).Tooltip(tip)
	if b.Hovered() {
		b.Background(hoverFill(t))
	} else if !pz.host.inWindow(d) && !pz.host.splitShown() {
		b.Background(glass(t))
	} else {
		b.Background(t.Accent.Alpha(.14))
	}
	b.Children(func() {
		ui.Text(c, fmt.Sprintf("%d%%", pct)).FontSize(12).FontWeight(500).TextColor(t.Text).NoWrap()
	})
	b.PopoverMenuButton(pz.pageZoomMenu)
	return b
}
