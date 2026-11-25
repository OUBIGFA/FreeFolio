package app

import (
	"math"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo/ui"
)

// Design tokens. Colors come from the window's theme and the active
// space's look; nothing else in the package names a color.

const (
	controlRadius = 6
	panelRadius   = 10
)

var (
	white = ui.RGB(255, 255, 255)
	black = ui.RGB(0, 0, 0)
)

func splitFocus(t *ui.Theme) ui.Color { return t.Accent }
func splitEmpty(t *ui.Theme) ui.Color { return glass(t) }
func splitDropColors(t *ui.Theme) (ui.Color, ui.Color) {
	return t.Accent.Alpha(0.18), t.Accent.Alpha(0.7)
}

// spaceEditorSurface is the face of the space's pickers: the window's
// own background, ink and hairline.
func spaceEditorSurface(dark bool) (ui.LinearGradient, ui.Color, ui.Color) {
	t := ui.LightTheme()
	if dark {
		t = ui.DarkTheme()
	}
	return ui.LinearGradient{Angle: 180, From: t.Background, To: t.Background}, t.Text, t.Border
}

// spaceTheme is the window's chrome: MyGo's own light or dark theme, as
// the system appearance is, with the space's accent. The document's card
// uses its own paper theme (documentTheme).
func spaceTheme(base *ui.Theme, l spaceLook) *ui.Theme {
	t := mixThemes(ui.LightTheme(), ui.DarkTheme(), l.darkness)
	t.Dark = l.Dark
	t.Font, t.FontSize, t.Spacing, t.ScrollbarWidth = base.Font, base.FontSize, base.Spacing, base.ScrollbarWidth
	t.Text, t.TextMuted = l.text, l.muted
	t.Background = l.opaqueTop()
	t.Surface = mixColor(white, ui.DarkTheme().Surface, l.darkness)
	acc := l.accent
	t.Accent = acc
	t.AccentText = white
	t.AccentHover = acc.Mix(black, 0.1)
	t.AccentPressed = acc.Mix(black, 0.2)
	t.Focus = acc.Alpha(0.55)
	t.Selection = acc.Alpha(0.28)
	t.Radius = controlRadius
	return &t
}

// mixThemes is the theme f of the way from x to y, color by color.
func mixThemes(x, y *ui.Theme, f float32) ui.Theme {
	if f <= 0 {
		return *x
	}
	if f >= 1 {
		return *y
	}
	out := *x
	for _, c := range []struct{ out, x, y *ui.Color }{
		{&out.Background, &x.Background, &y.Background}, {&out.Surface, &x.Surface, &y.Surface},
		{&out.SurfaceHover, &x.SurfaceHover, &y.SurfaceHover}, {&out.SurfacePressed, &x.SurfacePressed, &y.SurfacePressed},
		{&out.Border, &x.Border, &y.Border}, {&out.Text, &x.Text, &y.Text}, {&out.TextMuted, &x.TextMuted, &y.TextMuted},
		{&out.Accent, &x.Accent, &y.Accent}, {&out.AccentHover, &x.AccentHover, &y.AccentHover},
		{&out.AccentPressed, &x.AccentPressed, &y.AccentPressed}, {&out.AccentText, &x.AccentText, &y.AccentText},
		{&out.Danger, &x.Danger, &y.Danger}, {&out.Warning, &x.Warning, &y.Warning}, {&out.Success, &x.Success, &y.Success},
		{&out.Selection, &x.Selection, &y.Selection}, {&out.Focus, &x.Focus, &y.Focus},
		{&out.Inverse, &x.Inverse, &y.Inverse}, {&out.InverseText, &x.InverseText, &y.InverseText},
		{&out.Scrollbar, &x.Scrollbar, &y.Scrollbar},
	} {
		*c.out = mixColor(*c.x, *c.y, f)
	}
	return out
}

// darknessOf is how dark a theme is, from its background: 0 for a light
// one, 1 for a dark one, and between while two spaces' looks are mixed.
func darknessOf(t *ui.Theme) float32 {
	b := t.Background
	l := (float32(b.R) + float32(b.G) + float32(b.B)) / (3 * 255)
	return min(max((0.9-l)/0.7, 0), 1)
}

// pick is light on a light theme and dark on a dark one, mixed by how
// dark the theme is, so that chrome mixed between two spaces' looks
// changes gradually.
func pick(t *ui.Theme, light, dark ui.Color) ui.Color {
	return mixColor(light, dark, darknessOf(t))
}

// glass is the fill of what is chosen on the chrome: the document shown,
// as Arc's current tab (Cra's selectedFill).
func glass(t *ui.Theme) ui.Color {
	return pick(t, white.Alpha(0.74), white.Alpha(0.13))
}

// separator is the hairline between the list's pinned and the rest.
func separator(t *ui.Theme) ui.Color {
	return pick(t, black.Alpha(0.09), white.Alpha(0.12))
}

// hoverFill is the fill of a row under the pointer on the chrome.
func hoverFill(t *ui.Theme) ui.Color {
	return pick(t, black.Alpha(0.045), white.Alpha(0.07))
}

// closedIconAlpha and closedInk dim a pinned document that is closed, as
// Zen dims an unloaded pinned tab, so the open ones stand out beside them.
const closedIconAlpha = 0.5

func closedInk(t *ui.Theme) ui.Color { return t.Text.Alpha(0.55) }

// closeMarkInk is the ink of a row's close button: Arc's row glyph tone,
// a touch brighter on dark chrome.
func closeMarkInk(t *ui.Theme) ui.Color {
	return pick(t, t.Text.Alpha(0.55), white.Alpha(0.6))
}

// docPalette are the colors of documents' icons (docicon.go): quiet hues
// spread around the wheel, each dark enough to read on its own tint.
var docPalette = []ui.Color{
	ui.RGB(0x3B, 0x82, 0xF6), // blue
	ui.RGB(0x63, 0x66, 0xF1), // indigo
	ui.RGB(0x8B, 0x5C, 0xF6), // violet
	ui.RGB(0xD9, 0x46, 0xEF), // fuchsia
	ui.RGB(0xEC, 0x48, 0x99), // pink
	ui.RGB(0xF4, 0x3F, 0x5E), // rose
	ui.RGB(0xEA, 0x58, 0x0C), // orange
	ui.RGB(0xCA, 0x8A, 0x04), // amber
	ui.RGB(0x65, 0xA3, 0x0D), // lime
	ui.RGB(0x16, 0xA3, 0x4A), // green
	ui.RGB(0x0D, 0x94, 0x88), // teal
	ui.RGB(0x08, 0x91, 0xB2), // cyan
}

// spaceLook is the window's chrome while a space is shown: MyGo's own
// light or dark surface, as the system appearance is, and the space's
// color only as the accent of what is chosen on it.
type spaceLook struct {
	space.Look
	systemDark bool
	// stops are the chrome's fill from top to bottom; both are the same
	// native surface, kept as a list so two spaces mix while paging.
	stops []ui.Color
	// accent colors icons and what is chosen on the chrome; text and muted
	// color its text.
	accent, text, muted ui.Color
	// darkness is 1 for dark chrome and 0 for light chrome.
	darkness float32
}

// chromeFill is the window's chrome behind the paper.
func chromeFill(dark bool) ui.Color {
	if dark {
		return ui.Hex("#141416")
	}
	return ui.Hex("#F4F4F5")
}

// spaceAccent turns a space's color into an accent that reads on the
// native chrome: its hue and some of its chroma kept at one OKLCH
// lightness, so every space's accent has the same contrast. Near-grey
// colors take MyGo's blue instead.
func spaceAccent(hex string, dark bool) ui.Color {
	l := float32(0.46)
	if dark {
		l = 0.66
	}
	chroma, hue := oklchOf(hex)
	if chroma < 0.035 {
		return ui.Oklch(l, 0.16, 263) // MyGo's blue at the same lightness
	}
	return ui.Oklch(l, min(max(chroma, 0.09), 0.14), hue)
}

// oklchOf is the chroma and hue, in degrees, of an sRGB hex color.
func oklchOf(hex string) (chroma, hue float32) {
	r, g, b := space.Components(hex)
	lin := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	r, g, b = lin(r), lin(g), lin(b)
	lc := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	mc := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	sc := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	A := 1.9779984951*lc - 2.4285922050*mc + 0.4505937099*sc
	B := 0.0259040371*lc + 0.7827717662*mc - 0.8086757660*sc
	h := math.Atan2(B, A) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return float32(math.Hypot(A, B)), float32(h)
}

func lookOf(t space.Theme, systemDark bool) spaceLook {
	l := spaceLook{Look: t.Look(systemDark), systemDark: systemDark}
	l.Dark, l.Solid, l.Noise = systemDark, false, 0
	native := ui.LightTheme()
	if systemDark {
		native, l.darkness = ui.DarkTheme(), 1
	}
	fill := chromeFill(systemDark)
	l.stops = []ui.Color{fill, fill}
	// MyGo's muted ink, a touch darker on light chrome to keep 4.5:1.
	l.text, l.muted = native.Text, native.TextMuted
	if !systemDark {
		l.muted = native.TextMuted.Mix(native.Text, 0.3)
	}
	l.accent = spaceAccent(space.ThemeOf(&space.Space{Theme: &t}).Colors[0], systemDark)
	return l
}

// opaqueTop is the chrome's face, as the floating sidebar shows it.
func (l spaceLook) opaqueTop() ui.Color { return l.stops[0] }

// paint fills r with the chrome.
func (l spaceLook) paint(p *ui.Painter, r ui.Rect, radius float32, _ bool) {
	p.Fill(r, l.stops[0], radius)
}

// docIconColors are the letter's color and the tint behind it of the
// document titled title: the color itself on a light tint, lighter on a
// deeper tint in the dark.
func docIconColors(t *ui.Theme, title string) (ink, fill ui.Color) {
	c := docPalette[docColorIndex(title)]
	return pick(t, c.Mix(black, 0.08), c.Mix(white, 0.35)), pick(t, c.Alpha(0.16), c.Alpha(0.28))
}

// previewLook are the colors of the card a folder or a document shows
// under the pointer (preview.go).
type previewLook struct {
	fill, title, muted, row, hairline, outline, rim, shadow ui.Color
}

// previewColors are the card's colors in the space's look: the face of the
// floating sidebar, lightened on a light theme and darkened on a dark one,
// with the theme's ink and hairlines, so the card sits in the space as its
// sidebar does.
func previewColors(t *ui.Theme, l spaceLook) previewLook {
	face := l.opaqueTop()
	if t.Dark {
		return previewLook{
			fill: face.Mix(black, 0.25), title: t.Text, muted: t.TextMuted,
			row: white.Alpha(0.08), hairline: white.Alpha(0.1),
			outline: black.Alpha(0.4), rim: white.Alpha(0.12), shadow: black.Alpha(0.35),
		}
	}
	return previewLook{
		fill: face.Mix(white, 0.72), title: t.Text, muted: t.TextMuted,
		row: black.Alpha(0.05), hairline: black.Alpha(0.08),
		outline: black.Alpha(0.12), rim: white.Alpha(0.7), shadow: black.Alpha(0.16),
	}
}

// favoriteFill is the fill of a favorite's tile, as Cra measures Arc's:
// at rest a little darker than the sidebar (lighter in the dark), darker
// still under the pointer, and the open favorite light.
func favoriteFill(t *ui.Theme, open, hovered bool) ui.Color {
	switch {
	case open:
		return pick(t, white.Alpha(0.38), white.Alpha(0.13))
	case hovered:
		return pick(t, black.Alpha(0.09), white.Alpha(0.15))
	}
	return pick(t, black.Alpha(0.045), white.Alpha(0.08))
}

// favoriteRim separates the selected tile from the surrounding sidebar.
func favoriteRim(t *ui.Theme) ui.Color {
	return pick(t, black.Alpha(0.17), white.Alpha(0.14))
}

// dragCardLook are the colors of a sidebar row lifted on its floating card
// while dragged or settling back into place (treedrag.go).
type dragCardLook struct {
	fill, rim, shadow, badgeFill, badgeText ui.Color
}

func dragCardColors(t *ui.Theme, l spaceLook) dragCardLook {
	face := l.opaqueTop()
	if t.Dark {
		return dragCardLook{
			fill:      face.Mix(white, 0.12).Alpha(0.96),
			rim:       white.Alpha(0.16),
			shadow:    black.Alpha(0.42),
			badgeFill: white.Alpha(0.15),
			badgeText: t.Text,
		}
	}
	return dragCardLook{
		fill:      face.Mix(white, 0.86).Alpha(0.97),
		rim:       black.Alpha(0.10),
		shadow:    black.Alpha(0.16),
		badgeFill: black.Alpha(0.08),
		badgeText: t.Text,
	}
}

// dashedSlotColors are the fill and dashed outline of an insertion slot or
// new-document row in the sidebar, scaled by strength in [0, 1].
func dashedSlotColors(t *ui.Theme, strength float32) (fill, border ui.Color) {
	s := min(max(strength, 0), 1)
	fill = pick(t, black.Alpha(0.035*s), white.Alpha(0.055*s))
	border = pick(t, black.Alpha(0.14+0.18*s), white.Alpha(0.15+0.20*s))
	return fill, border
}

func scaleAlpha(c ui.Color, f float32) ui.Color {
	return c.Alpha(float32(c.A) / 255 * min(max(f, 0), 1))
}

// field is the fill of the address field.
func field(t *ui.Theme) ui.Color {
	return pick(t, white.Alpha(0.45), white.Alpha(0.08))
}

// librarySearch is the fill of the library's search field: a faint shade
// of the chrome's ink.
func librarySearch(t *ui.Theme) ui.Color {
	return pick(t, black.Alpha(0.05), white.Alpha(0.07))
}

// cardShadow is the shadow under the content card.
func cardShadow(t *ui.Theme) ui.Color {
	return pick(t, black.Alpha(0.10), black.Alpha(0.35))
}

// documentTheme is the writing surface: white paper, or, while the system
// appearance is dark, dark paper, as a web page that follows
// prefers-color-scheme does in Arc whatever the space's own look.
func documentTheme(dark bool) *ui.Theme {
	if dark {
		return darkPaperTheme()
	}
	// The paper is a Feishu-style document theme.
	t := *ui.LightTheme()
	t.Font = "system-ui"
	t.Background = ui.Hex("#FFFFFF")
	t.Surface = ui.Hex("#F5F6F7")
	t.Text = ui.Hex("#1F2329")
	t.TextMuted = ui.Hex("#646A73")
	t.Border = ui.Hex("#DEE0E3")
	t.Accent = ui.Hex("#336DF4")
	t.Selection = ui.Hex("#1456F02E")
	t.Focus = t.Accent.Alpha(0.55)
	return &t
}

// darkPaperTheme is the paper of Feishu's dark document theme: a near-black
// sheet, softened white text and a lighter blue.
func darkPaperTheme() *ui.Theme {
	t := *ui.DarkTheme()
	t.Font = "system-ui"
	t.Background = ui.Hex("#1F2023")
	t.Surface = ui.Hex("#2A2B2F")
	t.Text = ui.Hex("#E3E4E7")
	t.TextMuted = ui.Hex("#A2A5AB")
	t.Border = ui.Hex("#393B40")
	t.Accent = ui.Hex("#4C88FF")
	t.Selection = ui.Hex("#4C88FF4D")
	t.Focus = t.Accent.Alpha(0.55)
	return &t
}

// pageStyle are the page's colors beyond the theme, from the same Feishu
// document theme: blue list markers, gray code blocks with line numbers,
// and callouts tinted with a matching border. Dark paper has its own.
func pageStyle(dark bool) editor.Page {
	if dark {
		p := darkPageStyle()
		p.ImageShadow = ui.Hex("#00000026")
		return p
	}
	return editor.Page{
		ImageShadow: ui.Hex("#00000014"),
		MathBorder:  ui.Hex("#DEE0E3"),
		MathShadow:  ui.Hex("#0000001F"),
		Marker:      ui.Hex("#1456F0"),
		Caption:     ui.Hex("#646A73"),
		Placeholder: ui.Hex("#BBBFC4"),
		TaskBorder:  ui.Hex("#00000038"),
		TaskDone:    ui.Hex("#646A73B3"),
		QuoteBar:    ui.Hex("#BBBFC4"),
		Rule:        ui.Hex("#1F232926"),
		CodeFill:    ui.Hex("#F5F6F7"),
		CodeBorder:  ui.Hex("#DEE0E3"),
		CodeText:    ui.Hex("#2B2F36"),
		CodeHover:   ui.Hex("#336DF4"),
		InlineFill:  ui.Hex("#F2F3F5"),
		InlineText:  ui.Hex("#2B2F36"),
		TableLine:   ui.Hex("#DEE0E3"),
		TableHeader: ui.Hex("#F2F3F5"),
		Hover:       ui.Hex("#1F232914"),
		TurnActive:  ui.Hex("#E0E9FF"),
		// Table bars and the selected line, from the same theme's tokens.
		BarFill:      ui.Hex("#EFF0F1"),
		BarHover:     ui.Hex("#DEE0E3"),
		CellSelected: ui.Hex("#1456F01A"),
		Danger:       ui.Hex("#F54A45"),
		DangerCell:   ui.Hex("#FEF0F0"),
		OnMarker:     ui.Hex("#FFFFFF"),
		// Cell fills: the theme's block backgrounds, light then medium.
		CellFills: map[string]ui.Color{
			"light-gray": ui.Hex("#F5F6F7E6"), "light-red": ui.Hex("#FEF1F1"), "light-orange": ui.Hex("#FFF5EB"),
			"light-yellow": ui.Hex("#FEFFF0"), "light-green": ui.Hex("#F0FBEF"), "light-blue": ui.Hex("#F0F4FF"),
			"light-purple": ui.Hex("#F6F1FE"), "medium-gray": ui.Hex("#F2F3F5"), "gray": ui.Hex("#EFF0F1"),
			"red": ui.Hex("#FDE2E2"), "orange": ui.Hex("#FEEAD2"), "yellow": ui.Hex("#FFFFCC"), "green": ui.Hex("#D9F5D6"),
			"blue": ui.Hex("#E1EAFF"), "purple": ui.Hex("#ECE2FE"),
		},
		FindMatch:   ui.Hex("#FFF67ACC"),
		FindCurrent: ui.Hex("#FFA53D"),
		// Text colors and highlights, by document.TextColors and
		// document.TextHighlights name.
		TextColors: map[string]ui.Color{
			"gray": ui.Hex("#8F959E"), "red": ui.Hex("#D83931"), "orange": ui.Hex("#DE7802"), "yellow": ui.Hex("#DC9B04"),
			"green": ui.Hex("#2EA121"), "blue": ui.Hex("#245BDB"), "purple": ui.Hex("#6425D0"),
		},
		Highlights: map[string]ui.Color{
			"light-gray": ui.Hex("#F2F3F5"), "light-red": ui.Hex("#FBBFBC"), "light-orange": ui.Hex("#FED4A4CC"),
			"light-yellow": ui.Hex("#FFF67ACC"), "light-green": ui.Hex("#B7EDB1CC"), "light-blue": ui.Hex("#BACEFDB3"),
			"light-purple": ui.Hex("#CDB2FAB3"), "medium-gray": ui.Hex("#DEE0E3CC"), "gray": ui.Hex("#BBBFC4"),
			"red": ui.Hex("#F76964"), "orange": ui.Hex("#FFA53D"), "yellow": ui.Hex("#FFE928"), "green": ui.Hex("#62D256"),
			"blue": ui.Hex("#4E83FD8C"), "purple": ui.Hex("#935AF68C"),
		},
		Popover: ui.Hex("#FFFFFF"),
		// The slash menu tints its rows by block family.
		Tints: map[string]ui.Color{
			"blue": ui.Hex("#336DF4"), "indigo": ui.Hex("#5B65F5"), "green": ui.Hex("#35BD4B"),
			"orange": ui.Hex("#FF811A"), "teal": ui.Hex("#2DBEAB"), "yellow": ui.Hex("#FFC60A"), "wathet": ui.Hex("#1295CA"),
		},
		Tokens: editor.CodeTokens{
			Comment:  ui.Hex("#8D9096"),
			Keyword:  ui.Hex("#B2565C"),
			String:   ui.Hex("#89713A"),
			Number:   ui.Hex("#9759B3"),
			Type:     ui.Hex("#3878B1"),
			Function: ui.Hex("#5F7E43"),
		},
		// Keyed as document.CalloutColors: the GitHub alert tones in
		// Feishu's light callout fills, borders and text colors.
		Callouts: map[string]editor.Callout{
			"":       {Fill: ui.Hex("#F0F4FF"), Border: ui.Hex("#BACEFD"), Icon: ui.Hex("#245BDB")},
			"green":  {Fill: ui.Hex("#F0FBEF"), Border: ui.Hex("#B7EDB1"), Icon: ui.Hex("#2EA121")},
			"purple": {Fill: ui.Hex("#F6F1FE"), Border: ui.Hex("#CDB2FA"), Icon: ui.Hex("#6425D0")},
			"yellow": {Fill: ui.Hex("#FFF5EB"), Border: ui.Hex("#FED4A4"), Icon: ui.Hex("#DE7802")},
			"red":    {Fill: ui.Hex("#FEF1F1"), Border: ui.Hex("#FBBFBC"), Icon: ui.Hex("#D83931")},
		},
	}
}

// searchMark marks the query in the command bar's text hits: the find
// box's yellow, and in a dark theme a dimmer one that keeps light text
// readable.
func searchMark(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Warning.Alpha(0.3)
	}
	return pageStyle(false).FindMatch
}

// darkPageStyle is pageStyle for dark paper. Fills are deep tints of the
// light theme's hues and text colors are lifted, so that what a document
// keeps by name (text colors, highlights, cell fills, callout tones) reads
// as the same color on either paper.
func darkPageStyle() editor.Page {
	return editor.Page{
		MathBorder:   ui.Hex("#393B40"),
		MathShadow:   ui.Hex("#00000059"),
		Marker:       ui.Hex("#4C88FF"),
		Caption:      ui.Hex("#A2A5AB"),
		Placeholder:  ui.Hex("#61646B"),
		TaskBorder:   ui.Hex("#FFFFFF4D"),
		TaskDone:     ui.Hex("#A2A5ABB3"),
		QuoteBar:     ui.Hex("#5A5D63"),
		Rule:         ui.Hex("#FFFFFF26"),
		CodeFill:     ui.Hex("#27282C"),
		CodeBorder:   ui.Hex("#36383D"),
		CodeText:     ui.Hex("#D5D7DB"),
		CodeHover:    ui.Hex("#4C88FF"),
		InlineFill:   ui.Hex("#303136"),
		InlineText:   ui.Hex("#D5D7DB"),
		TableLine:    ui.Hex("#393B40"),
		TableHeader:  ui.Hex("#28292D"),
		Hover:        ui.Hex("#FFFFFF14"),
		TurnActive:   ui.Hex("#22355C"),
		BarFill:      ui.Hex("#2E3034"),
		BarHover:     ui.Hex("#3E4046"),
		CellSelected: ui.Hex("#4C88FF2E"),
		Danger:       ui.Hex("#F76964"),
		DangerCell:   ui.Hex("#4A2627"),
		OnMarker:     ui.Hex("#FFFFFF"),
		CellFills: map[string]ui.Color{
			"light-gray": ui.Hex("#FFFFFF0D"), "light-red": ui.Hex("#3B2527"), "light-orange": ui.Hex("#3B2D21"),
			"light-yellow": ui.Hex("#37341F"), "light-green": ui.Hex("#22342A"), "light-blue": ui.Hex("#222D44"),
			"light-purple": ui.Hex("#2E2640"), "medium-gray": ui.Hex("#FFFFFF14"), "gray": ui.Hex("#FFFFFF1F"),
			"red": ui.Hex("#572B2C"), "orange": ui.Hex("#573D22"), "yellow": ui.Hex("#545023"), "green": ui.Hex("#264D2D"),
			"blue": ui.Hex("#253D6B"), "purple": ui.Hex("#3D2E63"),
		},
		FindMatch:   ui.Hex("#FFE92859"),
		FindCurrent: ui.Hex("#C9761E"),
		TextColors: map[string]ui.Color{
			"gray": ui.Hex("#8F959E"), "red": ui.Hex("#F76964"), "orange": ui.Hex("#FF9A2E"), "yellow": ui.Hex("#FFC60A"),
			"green": ui.Hex("#51C443"), "blue": ui.Hex("#5B93FF"), "purple": ui.Hex("#A57CF5"),
		},
		Highlights: map[string]ui.Color{
			"light-gray": ui.Hex("#FFFFFF1A"), "light-red": ui.Hex("#F7696440"), "light-orange": ui.Hex("#FFA53D40"),
			"light-yellow": ui.Hex("#FFE92840"), "light-green": ui.Hex("#62D25640"), "light-blue": ui.Hex("#4E83FD47"),
			"light-purple": ui.Hex("#935AF647"), "medium-gray": ui.Hex("#FFFFFF2E"), "gray": ui.Hex("#FFFFFF47"),
			"red": ui.Hex("#F7696480"), "orange": ui.Hex("#FFA53D80"), "yellow": ui.Hex("#FFE92873"), "green": ui.Hex("#62D25680"),
			"blue": ui.Hex("#4E83FD80"), "purple": ui.Hex("#935AF680"),
		},
		Popover: ui.Hex("#2A2B2F"),
		Tints: map[string]ui.Color{
			"blue": ui.Hex("#4C88FF"), "indigo": ui.Hex("#7880F7"), "green": ui.Hex("#45C95A"),
			"orange": ui.Hex("#FF9233"), "teal": ui.Hex("#35C9B5"), "yellow": ui.Hex("#FFC60A"), "wathet": ui.Hex("#2AA8DA"),
		},
		Tokens: editor.CodeTokens{
			Comment:  ui.Hex("#7F848E"),
			Keyword:  ui.Hex("#E8838A"),
			String:   ui.Hex("#D6B66E"),
			Number:   ui.Hex("#C495E6"),
			Type:     ui.Hex("#72A9DB"),
			Function: ui.Hex("#9CC56F"),
		},
		Callouts: map[string]editor.Callout{
			"":       {Fill: ui.Hex("#222D44"), Border: ui.Hex("#2F4880"), Icon: ui.Hex("#5B93FF")},
			"green":  {Fill: ui.Hex("#22342A"), Border: ui.Hex("#2F5C38"), Icon: ui.Hex("#51C443")},
			"purple": {Fill: ui.Hex("#2E2640"), Border: ui.Hex("#4B3A76"), Icon: ui.Hex("#A57CF5")},
			"yellow": {Fill: ui.Hex("#3B2D21"), Border: ui.Hex("#694824"), Icon: ui.Hex("#FF9A2E")},
			"red":    {Fill: ui.Hex("#3B2527"), Border: ui.Hex("#6B3435"), Icon: ui.Hex("#F76964")},
		},
	}
}

// bannerLook are the colors of the update banners above the spaces
// (updateview.go), after Cra's BannerPalette.
type bannerLook struct {
	// fill and hover grade from the card's top-left to its bottom-right.
	fill, hover                [2]ui.Color
	border, divider            ui.Color
	title, secondary, tertiary ui.Color
	badgeFill, badgeInk        ui.Color
	readyFill, readyInk        ui.Color
	icon, hoverIcon            ui.Color
	tileHover                  ui.Color
}

// bannerColors builds light and dark palettes for the sidebar update card
// from the active space's accent color, capping light-theme brightness so
// semantic badges and text retain crisp contrast on translucent cards.
func bannerColors(l spaceLook) bannerLook {
	h, s, v := space.HSB(0.55, 0.45, 0.75) // a purple, for a space without one
	if space.ValidHex(l.Look.Accent) {
		h, s, v = space.HSB(space.Components(l.Look.Accent))
	}
	hsb := func(s, v float64, a float32) ui.Color {
		r, g, b := space.FromHSB(h, min(max(s, 0), 1), min(max(v, 0), 1))
		return ui.RGBA(uint8(math.Round(r*255)), uint8(math.Round(g*255)), uint8(math.Round(b*255)), a)
	}
	lin := func(x float64) float64 {
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	iconLum := func(v float64) float64 {
		r, g, b := space.FromHSB(h, min(s*0.89, 1), min(v*0.913, 1))
		return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
	}
	dark := l.Dark
	if !dark && iconLum(v) > 0.21 {
		lo, hi := 0.0, v
		for range 20 {
			m := (lo + hi) / 2
			if iconLum(m) > 0.21 {
				hi = m
			} else {
				lo = m
			}
		}
		v = lo
	}
	if dark {
		accent := hsb(s*0.55, 0.95, 1)
		readyInk := ui.RGB(0x34, 0xD3, 0x99)
		return bannerLook{
			fill:      [2]ui.Color{white.Alpha(0.11), white.Alpha(0.06)},
			hover:     [2]ui.Color{white.Alpha(0.15), white.Alpha(0.1)},
			border:    white.Alpha(0.14),
			divider:   white.Alpha(0.1),
			title:     l.text,
			secondary: l.text.Alpha(0.72),
			tertiary:  l.text.Alpha(0.45),
			badgeFill: accent.Alpha(0.18),
			badgeInk:  accent,
			readyFill: readyInk.Alpha(0.18),
			readyInk:  readyInk,
			icon:      accent,
			hoverIcon: hsb(s*0.4, 1, 1),
			tileHover: white.Alpha(0.08),
		}
	}
	ink := hsb(s*1.2, 0.216, 1)
	accent := hsb(s*0.9, min(1, max(v, 0.55)*1.04), 1)
	readyInk := ui.RGB(0x05, 0x96, 0x69)
	return bannerLook{
		fill:      [2]ui.Color{white.Alpha(0.94), white.Alpha(0.68)},
		hover:     [2]ui.Color{white.Alpha(0.98), white.Alpha(0.78)},
		border:    ink.Alpha(0.1),
		divider:   ink.Alpha(0.08),
		title:     ink,
		secondary: ink.Alpha(0.72),
		tertiary:  ink.Alpha(0.42),
		badgeFill: accent.Alpha(0.14),
		badgeInk:  accent,
		readyFill: readyInk.Alpha(0.12),
		readyInk:  readyInk,
		icon:      hsb(s*0.89, v*0.913, 1),
		hoverIcon: hsb(s*1.28, v*0.78, 1),
		tileHover: black.Alpha(0.05),
	}
}

// toastLook are the colors of the update toast, as Cra's ShinyToast: a
// fill from the space's colors, top to bottom, with a darker outline.
type toastLook struct{ top, bottom, outline, text ui.Color }

func toastColors(l spaceLook) toastLook {
	var r, g, b float64
	n := 0
	for _, c := range l.Look.Colors {
		if space.ValidHex(c) {
			cr, cg, cb := space.Components(c)
			r, g, b, n = r+cr, g+cg, b+cb, n+1
		}
	}
	h, sat := 0.0, 0.0
	if n > 0 {
		h, sat, _ = space.HSB(r/float64(n), g/float64(n), b/float64(n))
	}
	hsb := func(s, v float64) ui.Color {
		r, g, b := space.FromHSB(h, s, v)
		return ui.RGB(uint8(math.Round(r*255)), uint8(math.Round(g*255)), uint8(math.Round(b*255)))
	}
	if l.Dark {
		s := min(0.76, sat*1.9)
		return toastLook{hsb(s, 0.40), hsb(s, 0.31), hsb(s, 0.24), white}
	}
	s := min(0.19, sat*0.3)
	return toastLook{hsb(s, 0.89), hsb(min(0.25, s*1.3), 0.85), hsb(s, 0.61), black}
}

// tourLook are the colors of the first-run tour (tour.go): the scrim over
// the window and the ring round what a step points at, and, for the
// pictures on its card, the space's own tint with a small mock of the
// window drawn in its ink and accent. alt and alt2 are the accent's hue
// turned, standing for other spaces' colors.
type tourLook struct {
	scrim      ui.Color
	scrimAlpha float32
	ring       ui.Color
	// ink draws the mock's bars and glyphs, panel its glass surfaces, and
	// paper and paperInk its sheet of writing.
	ink, panel        ui.Color
	paper, paperInk   ui.Color
	accent, alt, alt2 ui.Color
	shadow            ui.Color
}

func tourColors(l spaceLook) tourLook {
	acc := l.accent
	h, s, v := space.HSB(float64(acc.R)/255, float64(acc.G)/255, float64(acc.B)/255)
	// The other spaces' colors keep some color and light of their own
	// where the accent is nearly grey or nearly black.
	turned := func(by float64) ui.Color {
		r, g, b := space.FromHSB(h+by, min(max(s, 0.5), 0.75), min(max(v, 0.62), 0.9))
		return ui.RGB(uint8(math.Round(r*255)), uint8(math.Round(g*255)), uint8(math.Round(b*255)))
	}
	tl := tourLook{
		scrim: black, scrimAlpha: 0.42, ring: white,
		ink: l.text, panel: white.Alpha(0.78),
		paper: ui.Hex("#FFFFFF"), paperInk: ui.Hex("#1F2329"),
		accent: acc, alt: turned(140), alt2: turned(-95),
		shadow: black.Alpha(0.16),
	}
	if l.Dark {
		tl.scrimAlpha = 0.56
		tl.panel = white.Alpha(0.13)
		tl.paper, tl.paperInk = ui.Hex("#1F2023"), ui.Hex("#E3E4E7")
		tl.shadow = black.Alpha(0.4)
	}
	return tl
}

// qaCitationLook holds the color tokens for Q&A inline source chips,
// supporting claim highlights, the gliding preview card and the collapsible
// sources list, ported from Kokonut UI's ai-citations component (MIT).
type qaCitationLook struct {
	chipFill, chipHoverFill, chipActiveFill ui.Color
	chipInk, chipActiveInk                  ui.Color
	monoFill, monoInk, stackRing            ui.Color
	claimFill, claimLine                    ui.Color
	cardFill, cardBorder, cardShadow        ui.Color
	cardTitle, cardMuted                    ui.Color
	toggleInk, toggleHover, chevInk         ui.Color
	rowHover, rowTitle, rowMeta             ui.Color
	arrowInk, arrowHover                    ui.Color
}

func qaCitationColors(t *ui.Theme) qaCitationLook {
	return qaCitationLook{
		chipFill:       pick(t, black.Alpha(0.055), white.Alpha(0.10)),
		chipHoverFill:  pick(t, black.Alpha(0.10), white.Alpha(0.15)),
		chipActiveFill: pick(t, ui.Hex("#18181B"), white),
		chipInk:        pick(t, ui.Hex("#52525B"), ui.Hex("#D4D4D8")),
		chipActiveInk:  pick(t, white, ui.Hex("#18181B")),
		monoFill:       pick(t, ui.Hex("#E4E4E7"), ui.Hex("#3F3F46")),
		monoInk:        pick(t, ui.Hex("#52525B"), ui.Hex("#E4E4E7")),
		stackRing:      pick(t, white.Alpha(0.92), ui.Hex("#393B42")),
		claimFill:      pick(t, ui.Hex("#E0F2FE").Alpha(0.85), ui.Hex("#38BDF8").Alpha(0.16)),
		claimLine:      pick(t, ui.Hex("#38BDF8").Alpha(0.72), ui.Hex("#38BDF8").Alpha(0.52)),
		cardFill:       pick(t, white, ui.Hex("#18181B")),
		cardBorder:     pick(t, black.Alpha(0.10), white.Alpha(0.10)),
		cardShadow:     pick(t, black.Alpha(0.18), black.Alpha(0.42)),
		cardTitle:      pick(t, ui.Hex("#18181B"), ui.Hex("#F4F4F5")),
		cardMuted:      pick(t, ui.Hex("#71717A"), ui.Hex("#A1A1AA")),
		toggleInk:      pick(t, ui.Hex("#52525B"), ui.Hex("#D4D4D8")),
		toggleHover:    pick(t, black.Alpha(0.055), white.Alpha(0.10)),
		chevInk:        pick(t, ui.Hex("#71717A"), ui.Hex("#A1A1AA")),
		rowHover:       pick(t, black.Alpha(0.05), white.Alpha(0.06)),
		rowTitle:       pick(t, ui.Hex("#27272A"), ui.Hex("#E4E4E7")),
		rowMeta:        pick(t, ui.Hex("#71717A"), ui.Hex("#A1A1AA")),
		arrowInk:       pick(t, ui.Hex("#A1A1AA"), ui.Hex("#71717A")),
		arrowHover:     pick(t, ui.Hex("#52525B"), ui.Hex("#E4E4E7")),
	}
}

// versionDiffHighlight returns the intra-line emphasis background behind
// changed grapheme clusters on green (added) or red (removed) diff rows.
func versionDiffHighlight(dark, added bool) ui.Color {
	if dark {
		if added {
			return ui.Hex("#4ADE80").Alpha(0.28)
		}
		return ui.Hex("#F87171").Alpha(0.28)
	}
	if added {
		return ui.Hex("#16A34A").Alpha(0.24)
	}
	return ui.Hex("#DC2626").Alpha(0.22)
}

// Resize handles. Every handle that resizes part of the window (split
// panes, sidebar, OneBot, mind map) shows the same way, through
// resizeGrip: no dividing line, only a short rounded bar in the handle's
// middle that fades in while the handle is lit. Change it here to change
// them all.
const (
	resizeGripLength    = 36
	resizeGripThickness = 4
	resizeGripFade      = 120 * time.Millisecond
)

// resizeGripColor is the bar at rest lit; resizeGripSnapped marks a drag
// that snapped to an even split.
func resizeGripColor(t *ui.Theme) ui.Color   { return t.Text.Alpha(0.45) }
func resizeGripSnapped(t *ui.Theme) ui.Color { return t.Accent }

// resizeGrip draws handle h's bar: lit while hovered, held or focused,
// in resizeGripSnapped when the drag snaps. vertical lays the bar across
// a handle between panes stacked one over the other.
func resizeGrip(c *ui.Context, h ui.Element, vertical, lit, snapped bool) {
	t := c.Theme()
	alpha := h.Animate("grip", b2f(lit), resizeGripFade)
	if alpha <= 0 && !snapped {
		return
	}
	h.Draw(func(p *ui.Painter, r ui.Rect) {
		w, l := float32(resizeGripThickness), float32(resizeGripLength)
		if vertical {
			w, l = l, w
		}
		col := resizeGripColor(t).Alpha(alpha)
		if snapped {
			col = resizeGripSnapped(t)
		}
		p.Fill(ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-l)/2, W: w, H: l}, col, resizeGripThickness/2)
	})
}

// imageStageBg is the calm backdrop behind the image viewer and annotator canvas.
func imageStageBg(t *ui.Theme) ui.Color {
	return pick(t, t.Background.Mix(black, 0.03), t.Background.Mix(black, 0.28))
}
