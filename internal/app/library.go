package app

import (
	"path"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/pinyin"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/trashbin"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// The library is Arc's Library (Cra's LibraryView): not a window but a
// mode of the sidebar, opened by the button at its foot (⇧⌘L). The sidebar
// widens and shows a rail of pages on the left, the page beside it:
//
//   - 空间, every space as a card in its own theme, the sidebar as wide as
//     the cards need (Arc's Spaces page);
//   - 插件, the plugins, empty until the market opens;
//   - 最近删除, what was deleted recently, as Arc's archive.
//
// As Arc's, it is left by the shortcut, Escape or the back arrow, never by
// a click elsewhere, and reopens on the page last shown. Measured by Cra on
// Arc 1.170: a 113 DIP rail with a faint wash brightening to the right and
// a hairline; page buttons 97×80, 20 apart, centred; a 304 DIP list with a
// 36 DIP search field and 56 DIP rows; the back arrow 32×32, 8 in.

type libraryPage int

const (
	libSpaces libraryPage = iota
	libPlugins
	libTrash
)

var libraryPages = []struct {
	page  libraryPage
	title string
	icon  *ui.SVG
}{
	{libSpaces, "空间", iconSpaces},
	{libPlugins, "插件", iconPlugins},
	{libTrash, "最近删除", iconTrash},
}

const (
	libraryRail     = 112
	libraryList     = 304
	libraryButtonW  = 92
	libraryButtonH  = 64
	libraryRowH     = 54
	libraryHeaderH  = 36
	libraryCardW    = 244
	libraryCardGap  = 20
	libraryCardLead = 24
	libraryInset    = 36 // the cards from the window's top and bottom
	libraryPlus     = 36
	libraryPlusGap  = 24
	libraryTrail    = 28
	libraryPageRoom = 420 // what the Spaces page leaves the document
	librarySlide    = 14  // how far the library slides in from the left
	librarySlideIn  = 120 * time.Millisecond
	libraryCardRow  = 32
)

// library is the state of the library: shown or not, and its pages'.
// libraryHost is what the library needs from the window.
type libraryHost interface {
	activeDocument() *document
	activeTreeOf(id string) ([]*vault.Node, bool, error)
	cardGrip(c *ui.Context, s *space.Space, index int)
	cardOrder() ([]*space.Space, int)
	dismissForLibrary()
	documentTheme() *ui.Theme
	editSpace(s *space.Space)
	finishSpaceEdit()
	movedCardX(now time.Time) (float32, bool)
	newDoc(dir string)
	newSpaceForm()
	open(rel string)
	overlayOpen() bool
	preferences() *prefs.Prefs
	reloadPlugins()
	removeSpace(id string)
	restoreDeleted(it trashbin.Item)
	saveState()
	spaceGlyph(c *ui.Context, emoji, symbol, image, name string, size float32, color ui.Color) ui.Element
	spaces() *space.State
	switchSpace(id string)
	switchSpaceOnly(id string) bool
	titleBar(c *ui.Context) ui.TitleBar
	trashBin() *trashbin.Bin
	withKey(tip, id string) string
}

type library struct {
	host libraryHost
	open bool
	page libraryPage // the page shown, kept while it is closed
	// frames counts the frames since it opened or closed, for the slide.
	frames int
	// trees and errs are each space's folder by ID, read as it opened.
	trees map[string][]*vault.Node
	errs  map[string]error
	cards ui.ScrollState
	// move is the card the pointer drags to reorder the spaces.
	move *cardMove
	// query filters what was deleted; items is what the bin lists.
	query string
	items []trashbin.Item
	err   error
}

// openLibrary shows the library on page.
func (lb *library) openLibrary(page libraryPage) {
	lb.host.finishSpaceEdit()
	l := lb
	if !l.open {
		l.frames = 0
	}
	l.open, l.page = true, page
	lb.host.dismissForLibrary()
	lb.loadLibrary()
}

// loadLibrary reads what the page shows.
func (lb *library) loadLibrary() {
	l := lb
	switch l.page {
	case libSpaces:
		l.trees = make(map[string][]*vault.Node, len(lb.host.spaces().Spaces))
		l.errs = make(map[string]error)
		for _, s := range lb.host.spaces().Spaces {
			if nodes, ok, err := lb.host.activeTreeOf(s.ID); ok {
				l.trees[s.ID], l.errs[s.ID] = nodes, err
				continue
			}
			l.trees[s.ID], l.errs[s.ID] = readTree(s)
		}
	case libTrash:
		l.items, l.err = lb.host.trashBin().List(time.Now())
	case libPlugins:
		lb.host.reloadPlugins()
	}
}

// closeLibrary gives the sidebar back its spaces.
func (lb *library) closeLibrary() {
	if lb.open {
		lb.open, lb.frames, lb.move = false, 0, nil
	}
}

// toggleLibrary opens the library on the page last shown, or closes it.
func (lb *library) toggleLibrary() {
	if lb.open {
		lb.closeLibrary()
		return
	}
	lb.openLibrary(lb.page)
}

// toggleOverview opens the library on its spaces, or closes it there.
func (lb *library) toggleOverview() {
	if lb.open && lb.page == libSpaces {
		lb.closeLibrary()
		return
	}
	lb.openLibrary(libSpaces)
}

// openTrash shows what was deleted recently.
func (lb *library) openTrash() { lb.openLibrary(libTrash) }

// openPlugins shows the plugins.
func (lb *library) openPlugins() { lb.openLibrary(libPlugins) }

// libraryWidth is the sidebar's width with the library in it, the window
// width wide: the rail and the list, or on the Spaces page as wide as
// every card needs, leaving the document some room (the cards scroll
// sideways when they do not fit).
func (lb *library) libraryWidth(window float32) float32 {
	base := float32(libraryRail + libraryList)
	if lb.page != libSpaces {
		return base
	}
	n := float32(len(lb.host.spaces().Spaces))
	want := libraryRail + libraryCardLead + n*libraryCardW + max(n-1, 0)*libraryCardGap + libraryPlusGap + libraryPlus + libraryTrail
	return max(base, min(want, window-libraryPageRoom))
}

// libraryInk is the library's ink: black on light chrome, white on dark,
// faded per use.
func libraryInk(t *ui.Theme) ui.Color { return pick(t, black, white) }

// libraryView is the sidebar while the library is open, width wide.
func (lb *library) libraryView(c *ui.Context, width float32) {
	l := lb
	if c.Shortcut(0, ui.KeyEscape) && !lb.host.overlayOpen() {
		if m := l.move; m != nil && !m.settling {
			m.canceled = true // Escape gives up a card's drag first
		} else {
			lb.closeLibrary()
		}
	}
	row := ui.Row(c.Key("library")).Width(width).Grow(1).AlignItems(ui.Stretch)
	// It slides in from a little to the left, as Arc's.
	pose := ui.Pose{}
	if l.frames == 0 {
		pose = ui.Pose{X: librarySlide, Fade: 1}
		if lb.host.preferences().SidebarLeft {
			pose.X = -librarySlide
		}
	}
	row.AnimatePose(pose, ui.Animation{Duration: librarySlideIn, Ease: ui.EaseOut})
	if l.frames++; l.frames == 1 {
		c.AnimationFrame()
	}
	row.Children(func() {
		lb.libraryRail(c)
		ui.Column(c.Key("library-page")).Grow(1).MinWidth(0).Children(func() {
			switch l.page {
			case libSpaces:
				lb.librarySpaces(c)
			case libPlugins:
				lb.libraryPlugins(c)
			case libTrash:
				lb.libraryTrash(c)
			}
		})
	})
}

// libraryRail is the column of pages: a subtle wash ending in a hairline,
// compact page tabs, and a clear back action at the foot.
func (lb *library) libraryRail(c *ui.Context) {
	t := c.Theme()
	ink := libraryInk(t)
	bar := lb.host.titleBar(c)
	rail := ui.Column(c.Key("library-rail")).Width(libraryRail).Shrink(0).AlignItems(ui.Center).DragWindow()
	rail.Draw(func(p *ui.Painter, r ui.Rect) {
		wash := ui.Rect{X: r.X, Y: r.Y, W: r.W - 1, H: r.H}
		p.Fill(wash, ink.Alpha(0.025), 0)
		p.Fill(ui.Rect{X: r.X + r.W - 1, Y: r.Y, W: 1, H: r.H}, separator(t), 0)
	})
	rail.Children(func() {
		ui.Box(c).Height(max(bar.Height, 52)).Shrink(0)
		ui.Spacer(c)
		ui.Column(c).Gap(12).Children(func() {
			for _, pg := range libraryPages {
				lb.libraryButton(c, pg.page, i18n.T(pg.title), pg.icon)
			}
		})
		ui.Spacer(c)
		ui.Row(c).AlignSelf(ui.Stretch).Padding(0, 16, 20, 16).Children(func() {
			b := ui.Row(c.Key("library-back")).Grow(1).Height(32).Radius(8).Gap(8).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label(i18n.T("退出资料库")).Tooltip(i18n.T("退出资料库 (Esc)"))
			if b.Hovered() {
				b.Background(hoverFill(t))
			}
			b.Children(func() {
				ui.Icon(c, iconBack).FontSize(14).TextColor(t.TextMuted)
				ui.Text(c, i18n.T("返回")).FontSize(12).FontWeight(500).TextColor(t.TextMuted).NoWrap()
			})
			if b.Clicked() {
				lb.closeLibrary()
			}
		})
	})
}

// libraryButton is a page's button on the rail: its glyph and name, the
// page shown on an accent-tinted fill with its glyph in the space's accent.
func (lb *library) libraryButton(c *ui.Context, page libraryPage, title string, icon *ui.SVG) {
	t := c.Theme()
	chosen := lb.page == page
	b := ui.Column(c.Key("page:"+title)).Size(libraryButtonW, libraryButtonH).Radius(controlRadius).Gap(8).Center().
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleTab).Label(title).Selected(chosen)
	if chosen && lb.page != libTrash {
		// The keys leave the document, which would take Escape, for the
		// page shown; the trash page's search takes them there.
		b.AutoFocus()
	}
	switch {
	case chosen:
		b.Background(navigationFill(t))
	case b.Hovered():
		b.Background(hoverFill(t))
	}
	b.Children(func() {
		glyph := t.TextMuted
		textCol := t.TextMuted
		weight := 500
		if chosen {
			glyph = t.Accent
			textCol = t.Text
			weight = 600
		}
		ui.Icon(c, icon).FontSize(18).TextColor(glyph)
		ui.Text(c, title).FontSize(12).FontWeight(weight).TextColor(textCol).NoWrap()
	})
	if b.Clicked() && !chosen {
		lb.openLibrary(page)
	}
}

// librarySpaces is the Spaces page: a card for every space in its own
// theme, and + making another.
func (lb *library) librarySpaces(c *ui.Context) {
	t := c.Theme()
	row := ui.ScrollHorizontal(c.Key("library-cards")).Grow(1).MinHeight(0).TrackScroll(&lb.cards).
		FocusGroup(ui.Horizontal)
	// The cards are as tall as the row, which a scroll does not give its
	// content by itself.
	height := row.Bounds().H
	row.Children(func() {
		ui.Row(c).Height(max(height, 160)).PaddingStart(libraryCardLead).PaddingEnd(libraryTrail).
			PaddingY(libraryInset).Gap(libraryCardGap).AlignItems(ui.Stretch).Children(func() {
			// A drag the grip starts while the row builds shows from the
			// next frame: this one built the card in its place already.
			moved := lb.move
			shown, slot := lb.host.cardOrder()
			for i, s := range shown {
				if i == slot {
					cardSlot(c)
				}
				lb.spaceCard(c, s, false)
			}
			if slot == len(shown) {
				cardSlot(c)
			}
			if m := lb.move; m != moved {
				c.AnimationFrame()
			} else if m != nil {
				// The card dragged, or gliding into its slot, above the rest.
				x, moving := lb.host.movedCardX(c.Now())
				if s := lb.host.spaces().Get(m.id); s != nil && moving {
					lb.spaceCard(c, s, true).Absolute().Left(x).Top(libraryInset).
						Height(max(height, 160) - 2*libraryInset)
					if m.settling {
						c.AnimationFrame()
					}
				} else {
					// Home: it takes its slot in the row from the next frame.
					lb.move = nil
					c.AnimationFrame()
				}
			}
			plus := ui.Box(c.Key("library-new-space")).Size(libraryPlus, libraryPlus).Shrink(0).AlignSelf(ui.Center).
				Margin(0, 0, 0, libraryPlusGap-libraryCardGap).Radius(10).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label(i18n.T("新建空间")).Tooltip(i18n.T("新建空间")).
				Background(t.Surface)
			plusInk := t.TextMuted
			if plus.Hovered() {
				plus.Background(t.Accent.Alpha(0.10)).Border(1, t.Accent.Alpha(0.45))
				plusInk = t.Accent
			}
			plus.Children(func() { ui.Icon(c, iconPlus).FontSize(16).TextColor(plusInk) })
			if plus.Clicked() {
				lb.closeLibrary()
				lb.host.newSpaceForm()
			}
		})
	})
}

// spaceCard is one space's card on the Spaces page: its name, folders and
// documents, and a foot to make a document or open the space's menu.
// moved is the card the grip drags or one gliding into its slot, which the
// caller places; the others slide as the slots change.
func (lb *library) spaceCard(c *ui.Context, s *space.Space, moved bool) ui.Element {
	base := lb.host.documentTheme()
	look := lookOf(space.ThemeOf(s), base.Dark)
	t := spaceTheme(base, look)
	active := s == lb.host.spaces().ActiveSpace()
	card := ui.Themed(c.Key("card-"+s.ID), t).Width(libraryCardW).Shrink(0).Radius(12).Padding(12).Gap(8).
		Background(t.Surface).Role(ui.RoleGroup).Label(s.Name).Clip()
	if moved {
		// Lifted while the pointer holds it, settling flat into its slot.
		lift := lb.move != nil && !lb.move.settling
		pose := ui.Pose{}
		if lift {
			pose.Scale, pose.Y = 1.02, -4
		}
		card.AnimatePose(pose, ui.Animation{Duration: 160 * time.Millisecond, Ease: ui.EaseOut})
		if lift {
			card.Shadow(0, 8, 24, 0, floatShadow(t))
		}
	} else {
		card.Transition(ui.ElementTransition{Animation: &listAnim, Position: true})
	}
	if active {
		card.Border(1.5, t.Accent.Alpha(0.75))
	} else {
		card.Border(1, t.Border)
	}
	card.Children(func() {
		h := ui.Row(c.Key("head")).Height(36).PaddingX(12).Gap(12).Radius(8).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("打开空间 ") + s.Name)
		hovered := h.Hovered()
		if hovered {
			h.Background(hoverFill(t))
		}
		h.Children(func() {
			lb.host.spaceGlyph(c, s.Icon, s.Symbol, s.Image, s.Name, 15, t.Accent)
			ui.Text(c, s.Name).FontSize(14).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
			edit := iconButton(c, "edit", iconPalette, i18n.T("编辑空间"), i18n.T("编辑空间"), false).Size(26, 26)
			edit.Opacity(map[bool]float32{true: 1, false: 0}[hovered || edit.Hovered()])
			if edit.Clicked() {
				lb.closeLibrary()
				lb.host.editSpace(s)
			}
		})
		if h.Clicked() {
			lb.host.switchSpace(s.ID)
			if lb.host.spaces().ActiveSpace() == s {
				lb.closeLibrary()
			}
		}
		body := ui.Scroll(c.Key("body")).Grow(1).MinHeight(0).Gap(4)
		body.Children(func() {
			if err := lb.errs[s.ID]; err != nil {
				ui.Text(c, i18n.T("读取文件夹失败")+" "+err.Error()).TextColor(t.Danger).FontSize(12).Padding(8, 12)
				return
			}
			nodes := lb.trees[s.ID]
			if len(nodes) == 0 {
				ui.Text(c, i18n.T("暂无文档")).FontSize(12).TextColor(t.TextMuted).Padding(8, 16)
			}
			lb.cardNodes(c, s, nodes, 0)
		})
		foot := ui.Row(c.Key("foot")).Height(32).Gap(4).AlignItems(ui.Center)
		// Moving the card and its menu are secondary: they show with the
		// pointer on the card or the focus inside it.
		revealOnIntent(card, foot)
		foot.Children(func() {
			lb.host.cardGrip(c, s, slices.Index(lb.host.spaces().Spaces, s))
			ui.Spacer(c)
			iconButton(c, "menu", iconEllipsis, s.Name+i18n.T(" 的菜单"), i18n.T("更多"), false).Size(28, 28).Menu(func(m *ui.Menu) {
				if m.Item(i18n.T("编辑空间")).Chosen() {
					lb.closeLibrary()
					lb.host.editSpace(s)
				}
				if m.Item(i18n.T("新建文档")).Disabled(s.IsExternal()).Chosen() {
					lb.host.switchSpace(s.ID)
					if lb.host.spaces().ActiveSpace() == s {
						lb.closeLibrary()
						lb.host.newDoc("")
					}
				}
				if m.Item(i18n.T("在文件管理器中显示")).Disabled(s.IsExternal()).Chosen() {
					revealDir(s.Root)
				}
				m.Separator()
				if m.Item(i18n.T("从 FreeFolio 移除")).Disabled(s.IsExternal() || len(lb.host.spaces().Spaces) < 2).Chosen() {
					lb.host.removeSpace(s.ID)
					lb.loadLibrary()
				}
			})
		})
	})
	return card
}

// cardNodes builds a card's folders and documents.
func (lb *library) cardNodes(c *ui.Context, s *space.Space, nodes []*vault.Node, depth int) {
	for _, n := range nodes {
		if !n.Dir {
			lb.cardDoc(c, s, n.Path, n.Title(), depth)
			continue
		}
		t := c.Theme()
		open := s.IsExpanded(n.Path)
		row := lb.cardRow(c, "dir:"+n.Path, n.Title(), depth).Role(ui.RoleTreeItem).Expanded(open)
		row.Children(func() {
			icon := iconFolder
			if open {
				icon = iconFolderOpen
			}
			ui.Icon(c, icon).FontSize(15).TextColor(t.Accent)
			ui.Text(c, n.Title()).FontSize(13).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
		})
		if row.Clicked() {
			s.SetExpanded(n.Path, !open)
			lb.host.saveState()
		}
		if open {
			lb.cardNodes(c, s, n.Children, depth+1)
		}
	}
}

// cardDoc is a document's row in a card; the open document takes the
// space's accent tint. A click opens it in its space and closes the library.
func (lb *library) cardDoc(c *ui.Context, s *space.Space, rel, title string, depth int) {
	t := c.Theme()
	chosen := lb.host.activeDocument() != nil && lb.host.activeDocument().space == s && lb.host.activeDocument().rel == rel
	row := lb.cardRow(c, "doc:"+rel, title, depth).Role(ui.RoleTreeItem).Selected(chosen)
	if chosen {
		row.Background(t.Accent.Alpha(0.11))
	}
	row.Children(func() {
		iconCol := t.TextMuted
		if chosen {
			iconCol = t.Accent
		}
		ui.Icon(c, iconFile).FontSize(15).TextColor(iconCol)
		name := ui.Text(c, docName(title)).FontSize(13).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
		if chosen {
			name.FontWeight(500)
		}
	})
	if row.Clicked() {
		lb.host.switchSpaceOnly(s.ID)
		if lb.host.spaces().ActiveSpace() != s {
			return // the open document could not be saved
		}
		lb.host.open(rel)
		lb.closeLibrary()
	}
}

// cardRow is the frame shared by a card's rows.
func (lb *library) cardRow(c *ui.Context, key, label string, depth int) ui.Element {
	t := c.Theme()
	row := ui.Row(c.Key(key)).Height(libraryCardRow).Shrink(0).PaddingStart(float32(8 + depth*16)).PaddingEnd(12).Gap(12).
		Radius(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(label).Tooltip(label)
	if row.Hovered() {
		row.Background(hoverFill(t))
	}
	return row
}

// libraryPlugins is the Plugins page, empty until the market opens.
func (lb *library) libraryPlugins(c *ui.Context) {
	ui.Box(c).Height(46).Shrink(0)
	libraryEmpty(c, iconPlugins, i18n.T("插件市场即将开放"), i18n.T("扩展能力将以插件提供，敬请期待"))
}

// libraryEmpty is a page with nothing on it: a glyph, a title and a line
// saying why.
func libraryEmpty(c *ui.Context, icon *ui.SVG, title, detail string) {
	t := c.Theme()
	ui.Column(c.Key("library-empty")).Grow(1).PaddingX(36).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		ui.Icon(c, icon).FontSize(28).TextColor(t.TextMuted.Alpha(0.75))
		ui.Box(c).Height(12)
		ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(t.Text).TextAlign(ui.Center)
		ui.Box(c).Height(6)
		ui.Text(c, detail).FontSize(13).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(240)
		ui.Spacer(c)
		ui.Box(c).Height(32)
	})
}

// libraryTrash is the Recently Deleted page, as Arc's archive: a search
// field, then what was deleted under the day it was, newest first.
func (lb *library) libraryTrash(c *ui.Context) {
	l := lb
	t := c.Theme()
	ink := libraryInk(t)
	top := float32(12)
	if bar := lb.host.titleBar(c); !lb.host.preferences().SidebarLeft && bar.Right > 0 {
		top += bar.Height
	}
	ui.Row(c.Key("trash-search")).Height(34).Shrink(0).Margin(top, 20, 8, 20).Radius(controlRadius).PaddingX(16).Gap(12).
		AlignItems(ui.Center).Background(t.Surface).Children(func() {
		ui.Icon(c, iconSearch).FontSize(14).TextColor(t.TextMuted)
		ui.TextInput(c.Key("trash-query"), &l.query).Grow(1).MinWidth(0).Height(24).Padding(0).
			Placeholder(i18n.T("搜索最近删除")).Label(i18n.T("搜索最近删除")).TextColor(t.Text).
			Background(ui.Transparent).Border(0, ui.Transparent).FontSize(13).AutoFocus()
	})
	switch {
	case l.err != nil:
		ui.Text(c, i18n.T("读取失败")+" "+l.err.Error()).TextColor(t.Danger).FontSize(13).Margin(24, 20, 0, 20)
		return
	case len(l.items) == 0:
		libraryEmpty(c, iconTrash, i18n.T("这里什么都没有"), i18n.T("从 FreeFolio 删除的文档和文件夹会在这里保留 30 天，可以放回原处"))
		return
	}
	words := strings.Fields(strings.ToLower(l.query))
	var shown []trashbin.Item
	for _, it := range l.items {
		hay := strings.ToLower(it.Name() + " " + it.SpaceName + " " + it.Path)
		ok := true
		for _, w := range words {
			if !pinyin.ContainsToken(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			shown = append(shown, it)
		}
	}
	if len(shown) == 0 {
		ui.Text(c, i18n.T("没有符合搜索的项目")).FontSize(13).TextColor(ink.Alpha(0.55)).AlignSelf(ui.Center).Margin(36, 0, 0, 0)
		return
	}
	now := time.Now()
	ui.Scroll(c.Key("trash-list")).Grow(1).MinHeight(0).Children(func() {
		day := ""
		for _, it := range shown {
			if d := dayLabel(it.Deleted, now); d != day {
				day = d
				ui.Row(c.Key("day:"+d)).Height(libraryHeaderH).Shrink(0).Padding(0, 0, 8, 16).
					AlignItems(ui.End).Children(func() {
					ui.Text(c, d).FontSize(13).FontWeight(600).TextColor(ink.Alpha(0.6))
				})
			}
			lb.trashRow(c, it, now)
		}
	})
}

// trashRow is a deleted item's row: its icon on a faint tile, its name
// over where it was and when it went, and, under the pointer, the bin
// (forget FreeFolio's copy) and the arrow putting it back. A click puts it
// back too, as a click on an archived tab restores it in Arc.
func (lb *library) trashRow(c *ui.Context, it trashbin.Item, now time.Time) {
	t := c.Theme()
	ink := libraryInk(t)
	name := it.Name()
	title := name
	if !it.Dir {
		title = vault.Title(it.Path)
	}
	where := it.SpaceName
	if dir := path.Dir(it.Path); dir != "." {
		where += " / " + dir
	}
	ui.Row(c.Key("trash:"+it.ID)).Height(libraryRowH).Shrink(0).Padding(4, 8, 4, 8).Children(func() {
		card := ui.Row(c.Key("card")).Grow(1).MinWidth(0).Radius(10).PaddingStart(12).PaddingEnd(20).Gap(16).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(title).Tooltip(where + " / " + name)
		hovered := card.Hovered()
		if hovered {
			card.Background(hoverFill(t))
		}
		acted := false
		card.Children(func() {
			ui.Box(c).Size(24, 24).Shrink(0).Center().Children(func() {
				if it.Dir {
					ui.Icon(c, iconFolder).FontSize(15).TextColor(t.Accent)
				} else {
					ui.Icon(c, iconFile).FontSize(15).TextColor(t.TextMuted)
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, docName(title)).FontSize(14).TextColor(ink.Alpha(0.82)).SingleLine()
				ui.Text(c, where+" · "+strings.TrimSpace(ago(it.Deleted, now))+i18n.T("删除")).FontSize(12).
					TextColor(ink.Alpha(0.55)).SingleLine()
			})
			forget := lb.trashButton(c, "forget", iconTrash, i18n.T("从列表移除 ")+name, i18n.T("只移除 FreeFolio 保留的副本，系统废纸篓中的原件不受影响"), hovered)
			if forget.Clicked() {
				acted = true
				lb.host.trashBin().Forget(it)
				lb.items, lb.err = lb.host.trashBin().List(time.Now())
			}
			restore := lb.trashButton(c, "restore", iconUndo, i18n.T("恢复 ")+name, i18n.T("放回原处"), hovered).Margin(0, 0, 0, 16)
			if restore.Clicked() {
				acted = true
				lb.host.restoreDeleted(it)
			}
		})
		if card.Clicked() && !acted {
			lb.host.restoreDeleted(it)
		}
	})
}

// trashButton is a row's hover button: shown while the row is under the
// pointer or the button has the keys.
func (lb *library) trashButton(c *ui.Context, key string, icon *ui.SVG, label, tip string, rowHovered bool) ui.Element {
	t := c.Theme()
	b := ui.Box(c.Key(key)).Size(24, 24).Shrink(0).Radius(8).Center().Cursor(ui.CursorPointer).
		Focusable().Role(ui.RoleButton).Label(label).Tooltip(tip)
	if b.Hovered() {
		b.Background(hoverFill(t))
	}
	if !rowHovered && !b.Focused() {
		b.Opacity(0)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(14).TextColor(libraryInk(t).Alpha(0.6)) })
	return b
}

// dayLabel is the header of the day t was: 今天, 昨天, or its date.
func dayLabel(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	today := time.Date(y2, m2, d2, 0, 0, 0, 0, now.Location())
	day := time.Date(y1, m1, d1, 0, 0, 0, 0, t.Location())
	switch {
	case day.Equal(today):
		return i18n.T("今天")
	case day.Equal(today.AddDate(0, 0, -1)):
		return i18n.T("昨天")
	case y1 == y2:
		return t.Format(i18n.T("1月2日"))
	}
	return t.Format(i18n.T("2006年1月2日"))
}

// libraryFootButton shares the workspace toolbar's hit target and icon style.
func (lb *library) libraryFootButton(c *ui.Context) ui.Element {
	label := i18n.T("资料库")
	return iconButton(c, "library", symbolIcon("archive"), label, lb.host.withKey(label, "library"), false).Shrink(0).Checked(lb.open)
}
