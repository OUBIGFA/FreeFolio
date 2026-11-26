package editor

import (
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// boardChrome holds centralized surface, ink, and interaction tokens for
// floating whiteboard panels, toolbars, and swatches.
type boardChrome struct {
	dark                             bool
	surface, surfaceMuted, border    ui.Color
	divider, shadow, hover           ui.Color
	activeBg, activeInk, text, muted ui.Color
	danger, dangerSoft               ui.Color
}

func boardChromeFor(t *ui.Theme) boardChrome {
	if t == nil {
		t = ui.LightTheme()
	}
	if t.Dark {
		return boardChrome{
			dark:         true,
			surface:      ui.Hex("#222429"),
			surfaceMuted: ui.Hex("#2A2D34"),
			border:       t.Border,
			divider:      t.Border.Alpha(0.75),
			shadow:       ui.RGBA(0, 0, 0, 0.34),
			hover:        t.SurfaceHover,
			activeBg:     t.Accent.Alpha(0.20),
			activeInk:    t.Accent,
			text:         t.Text,
			muted:        t.TextMuted,
			danger:       t.Danger,
			dangerSoft:   t.Danger.Alpha(0.14),
		}
	}
	return boardChrome{
		dark:         false,
		surface:      ui.Hex("#FFFFFF"),
		surfaceMuted: ui.Hex("#F5F6F8"),
		border:       t.Border,
		divider:      t.Border.Alpha(0.65),
		shadow:       ui.RGBA(0, 0, 0, 0.08),
		hover:        t.SurfaceHover,
		activeBg:     t.Accent.Alpha(0.14),
		activeInk:    t.Accent,
		text:         t.Text,
		muted:        t.TextMuted,
		danger:       t.Danger,
		dangerSoft:   t.Danger.Alpha(0.10),
	}
}

func boardSurface(el ui.Element, st boardChrome) ui.Element {
	return el.Background(st.surface).Border(1, st.border).Radius(12).Shadow(0, 4, 16, 0, st.shadow)
}

func boardToolbarDivider(c *ui.Context, key string, st boardChrome) {
	ui.Box(c.Key(key)).Width(1).Height(18).Margin(0, 3).Background(st.divider).AlignSelf(ui.Center)
}

func boardPanelHeader(c *ui.Context, st boardChrome, title, iconName, iconKind string, dark bool, onClose func()) {
	ui.Row(c).AlignItems(ui.Center).Gap(8).Margin(0, 0, 2, 0).Children(func() {
		if iconName != "" {
			aiItemBadge(c, iconName, iconKind, dark)
		}
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(st.text).Grow(1).MinWidth(0).SingleLine()
		if onClose != nil {
			if boardIconButton(c, "close", "close", i18n.T("关闭"), false).Size(26, 26).Clicked() {
				onClose()
			}
		}
	})
}

func boardSectionLabel(c *ui.Context, st boardChrome, text string) {
	ui.Text(c, text).FontSize(11.5).FontWeight(600).TextColor(st.muted)
}

func boardFlatButton(c *ui.Context, id, label string, active bool, st boardChrome) ui.Element {
	e := ui.ButtonBase(c.Key(id)).Height(28).PaddingX(9).Center().Radius(6).Focusable().Label(label)
	textColor := st.text
	if active {
		e.Background(st.activeBg)
		textColor = st.activeInk
	} else if e.Hovered() {
		e.Background(st.hover)
	}
	e.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(textColor).NoWrap()
	})
	return e
}

func boardSwatch(c *ui.Context, key, color, label string, active, dark bool, st boardChrome) ui.Element {
	el := ui.ButtonBase(c.Key(key)).Size(22, 22).Radius(6).Background(boardColor(color, dark)).Border(1, st.border).Focusable().Label(label)
	if color == "transparent" {
		el.Draw(func(p *ui.Painter, r ui.Rect) {
			p.StrokePolyline([][2]float32{{r.X + 4, r.Y + r.H - 4}, {r.X + r.W - 4, r.Y + 4}}, 1.5, st.danger)
		})
	}
	if active {
		el.Border(2, st.activeInk)
	}
	return el
}

type aiNodeColors struct {
	accent, accentSoft         ui.Color
	cardBg, headerBg, footerBg ui.Color
	mediaBg, divider, border   ui.Color
	ink, secondary, muted      ui.Color
	danger, dangerSoft, shadow ui.Color
}

func aiNodePalette(kind string, dark bool) aiNodeColors {
	var accent ui.Color
	if dark {
		switch kind {
		case "image":
			accent = ui.Hex("#34D399")
		case "video":
			accent = ui.Hex("#FB923C")
		default:
			accent = ui.Hex("#60A5FA")
		}
		return aiNodeColors{
			accent:     accent,
			accentSoft: accent.Alpha(0.16),
			cardBg:     ui.Hex("#1D1F24"),
			headerBg:   ui.Hex("#23262D"),
			footerBg:   ui.Hex("#1D1F24"),
			mediaBg:    ui.Hex("#15171B"),
			divider:    ui.Hex("#2C303A"),
			border:     ui.Hex("#363B47"),
			ink:        ui.Hex("#E6E8EC"),
			secondary:  ui.Hex("#C4C9D4"),
			muted:      ui.Hex("#939AA7"),
			danger:     ui.Hex("#F85149"),
			dangerSoft: ui.Hex("#F85149").Alpha(0.16),
			shadow:     ui.RGBA(0, 0, 0, 0.30),
		}
	}
	switch kind {
	case "image":
		accent = ui.Hex("#15803D")
	case "video":
		accent = ui.Hex("#C2410C")
	default:
		accent = ui.Hex("#2563EB")
	}
	return aiNodeColors{
		accent:     accent,
		accentSoft: accent.Alpha(0.11),
		cardBg:     ui.Hex("#FFFFFF"),
		headerBg:   ui.Hex("#F8F9FB"),
		footerBg:   ui.Hex("#FFFFFF"),
		mediaBg:    ui.Hex("#F3F4F6"),
		divider:    ui.Hex("#E6E9EF"),
		border:     ui.Hex("#D5D9E0"),
		ink:        ui.Hex("#1F2329"),
		secondary:  ui.Hex("#3F4550"),
		muted:      ui.Hex("#646A73"),
		danger:     ui.Hex("#CF222E"),
		dangerSoft: ui.Hex("#CF222E").Alpha(0.10),
		shadow:     ui.RGBA(0, 0, 0, 0.07),
	}
}
