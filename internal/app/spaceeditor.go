package app

import (
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo/ui"
)

// The space's color card floats beside the sidebar while a space is made
// or edited. The color only tints what is chosen on the chrome, so the
// card is MyGo's own color picker; every change goes into the form's
// theme at once, which the window previews.

const editorCardW = 292

// hexOf is a color as "#RRGGBB", its opacity dropped.
func hexOf(c ui.Color) string {
	return space.HexOf(float64(c.R)/255, float64(c.G)/255, float64(c.B)/255)
}

// themeCard is the card editing f.theme, beside anchor (the sidebar's
// header). Closing it commits an existing space; a new draft keeps its
// preview until the space is created or canceled.
func (a *App) themeCard(c *ui.Context, f *spaceForm, anchor ui.Element) {
	t := c.Theme()
	ui.Overlay(c, func() {
		pop := ui.PopoverBase(c.Key("theme-card"), anchor, &f.cardOpen, func(card ui.Element) {
			from, to := ui.AnchorTopLeft, ui.AnchorTopRight
			if f.id == "" {
				from, to = ui.AnchorLeft, ui.AnchorRight
			}
			if a.prefs.SidebarLeft {
				card.AttachTo(anchor, to, from).MarginStart(20)
			} else {
				card.AttachTo(anchor, from, to).MarginEnd(20)
			}
			card.Width(editorCardW).Radius(panelRadius).Padding(20).Gap(16).
				Background(t.Background).Border(1, t.Border).Shadow(0, 8, 24, 0, floatShadow(t)).
				Role(ui.RoleDialog).Label(i18n.T("空间主题"))
			a.tourMark("theme-card", card)
			card.Children(func() {
				ui.Text(c, i18n.T("空间颜色")).FontSize(13).FontWeight(600)
				color := ui.Hex(space.ThemeOf(&space.Space{Theme: &f.theme}).Colors[0])
				if ui.ColorPicker(c.Key("picker"), &color).Label(i18n.T("空间颜色")).Changed() {
					f.theme.Colors = []string{hexOf(color)}
					f.theme.Appearance = space.AppearanceAuto
				}
			})
		})
		if pop.Valid() && pop.Dismissed() && f.iconsOpen {
			// The press went to the icon picker, which is part of the form.
			f.cardOpen = true
		}
		if a.tour.themeHeld {
			f.cardOpen = true // the tour's, which keeps nothing of it
		} else if f.id != "" && !f.cardOpen {
			a.submitSpace()
		}
	})
}
