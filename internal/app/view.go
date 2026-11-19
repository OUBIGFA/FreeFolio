package app

import (
	"path"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/OUBIGFA/FreeMD/internal/space"
	"github.com/OUBIGFA/FreeMD/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	// Both docking positions use the same document tree and measures:
	// sidebarWidth wide unless the user drags it between sidebarMin and
	// sidebarMax.
	sidebarWidth = 260
	sidebarMin   = 220
	sidebarMax   = 480
	// slide is how long the sidebar takes to fold and unfold.
	slide = 240 * time.Millisecond
	// peekDelay lets the pointer cross the floating panel's small outer gap.
	peekDelay = 160 * time.Millisecond
)

// Content returns the window's content: the view of the app.
func (a *App) Content() *ui.Content { return ui.View(a.View) }

// View builds the window from the state, every frame.
func (a *App) View(c *ui.Context) {
	a.now, a.reduceMotion = c.Now(), c.Preferences().ReduceMotion
	if a.files != nil {
		a.fileView(c)
		return
	}
	winW, _ := c.Size()
	a.winW = winW
	if winW > 0 {
		a.narrowTick(winW)
	}
	a.splitResizing = false
	a.qa.resizing = false
	a.carried, a.carrying = a.carrying, nil
	a.syncOpen()
	a.overClosed, a.overClose = a.overClose, place{}
	clear(a.tour.marks)
	a.shortcuts(c)
	// Finish swipe and spring state before deriving the chrome and paper,
	// so even the landing frame uses one consistent space and position.
	a.pagerTick(c)
	a.tourTheme()
	sp := a.state.ActiveSpace()
	base := c.Theme()
	a.docTheme = base
	th := space.ThemeOf(sp)
	if f := &a.spaceForm; f.open && (f.id == "" || sp != nil && f.id == sp.ID) {
		th = f.theme // the space being made or edited shows its look at once
	}
	a.look = lookOf(th, base.Dark)
	a.paperDark = a.look.Dark
	if !a.spaceForm.open && a.pager.moving() {
		// Paging, the window's tint is the two spaces either side mixed.
		pos := a.pagerShown(c.Now())
		last := float64(len(a.state.Spaces) - 1)
		pos = min(max(pos, 0), last)
		lo := int(pos)
		hi := min(lo+1, int(last))
		x := lookOf(space.ThemeOf(a.state.Spaces[lo]), base.Dark)
		y := lookOf(space.ThemeOf(a.state.Spaces[hi]), base.Dark)
		a.look = mixLooks(x, y, float32(pos-float64(lo)))
	}
	t := spaceTheme(base, a.look)
	c.SetTheme(t)
	a.syncTree()
	a.syncWatches()
	post := a.asyncPost()
	for _, d := range a.residentDocuments() {
		a.withDocument(d, func() { d.autosaveWith(c, post); a.followTitle(); a.syncExcerpts(a.doc) })
	}
	for _, n := range a.notices {
		c.Toast(n)
	}
	a.notices = a.notices[:0]
	for _, u := range a.undos {
		c.AddToast(u)
	}
	a.undos = a.undos[:0]
	a.updateTick(c)
	a.updateToastTick()

	frame := ui.Row(c).Fill().AlignItems(ui.Stretch)
	if !a.prefs.SidebarLeft {
		frame.Reverse()
	}
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	look := a.look
	frame.Draw(func(p *ui.Painter, r ui.Rect) { look.paint(p, r, 0, vibrant) })
	var docked float32
	frame.Children(func() {
		if a.focus {
			a.focusView(c)
			return
		}
		// The docked sidebar slides as it folds: its column narrows while
		// what is in it keeps its width, cut by the column.
		dock := ui.Column(c.Key("dock")).Shrink(0).Clip()
		target := a.sidebarW()
		if a.folded() {
			target = 0
		}
		inner := a.sidebarW()
		if a.library.open {
			// The library takes over the sidebar, widened for its rail,
			// shown even when the sidebar is folded away.
			inner = a.libraryWidth(winW)
			target = inner
		}
		// Dragged, the edge follows the pointer at once.
		motion := slide
		if a.sidebarResizing {
			motion = 0
		}
		docked = dock.Animate("width", target, motion)
		dock.Width(docked).Hidden(docked < 0.5).Children(func() {
			if docked < 0.5 {
				return
			}
			if a.library.open {
				a.libraryView(c, inner)
				return
			}
			side := ui.Column(c.Key("side")).Width(a.sidebarW()).Grow(1)
			// Back from the library, the spaces fade in again, as Arc's.
			pose := ui.Pose{}
			if a.library.frames == 0 {
				pose.Fade = 1
			}
			side.AnimatePose(pose, ui.Animation{Duration: librarySlideIn, Ease: ui.EaseOut})
			if a.library.frames++; a.library.frames == 1 {
				c.AnimationFrame()
			}
			side.Children(func() { a.sidebar(c, false) })
			a.sidebarResizer(c, docked)
		})
		a.main(c, docked)
	})
	if a.folded() && docked < 0.5 && !a.library.open && !a.focus {
		a.peekSidebar(c)
	} else {
		a.peek = false
		a.peekLeave = time.Time{}
	}
	a.dragOverlayView(c)
	a.previewView(c)
	a.paletteView(c)
	a.updateToast(c)
	a.settingsView(c)
	a.replaceView(c)
	a.tagRenameView(c)
	a.versionsView(c)
	a.updateWindowView(c)
	a.pdfExportView(c)
	a.tourView(c)
	if a.captureSplits() {
		a.saveState()
	}
}

// sidebarW is the docked sidebar's width as the user left it, within
// bounds; the window's own width caps it so the page keeps room.
func (a *App) sidebarW() float32 {
	w := a.prefs.SidebarWidth
	if w == 0 {
		w = sidebarWidth
	}
	hi := float32(sidebarMax)
	if a.winW > 0 {
		hi = min(hi, max(sidebarMin, a.winW-400))
	}
	return min(max(w, sidebarMin), hi)
}

// sidebarResizer is the docked sidebar's inner edge: dragging it sets the
// width, a double click goes back to the standard one or, from it, to the
// widest, and the arrow keys move it by 10.
func (a *App) sidebarResizer(c *ui.Context, docked float32) {
	if a.folded() || a.library.open || docked < sidebarMin-1 {
		a.sidebarResizing = false
		return
	}
	t := c.Theme()
	label := i18n.T("调整侧边栏宽度")
	hint := i18n.T("拖动调整宽度；双击恢复默认，默认宽度时双击展开到最宽")
	h := ui.Box(c.Key("sidebar-resizer")).Absolute().Top(0).Bottom(0).Width(8).Cursor(ui.CursorResizeEW).
		Focusable().Role(ui.RoleSplitter).Label(label).Description(hint)
	if a.prefs.SidebarLeft {
		h.Right(0)
	} else {
		h.Left(0)
	}
	a.resizerTip(c, h, hint)
	set := func(w float32) {
		a.prefs.SidebarWidth = w
		a.prefs.SidebarWidth = a.sidebarW()
		if a.prefs.SidebarWidth == sidebarWidth {
			a.prefs.SidebarWidth = 0
		}
	}
	dx, _, held := h.Dragged()
	if held {
		if !a.sidebarResizing {
			a.sidebarResizing, a.sidebarDrag = true, a.sidebarW()
		}
		// The sidebar on the right widens as its left edge goes left.
		if !a.prefs.SidebarLeft {
			dx = -dx
		}
		a.sidebarDrag += dx
		set(a.sidebarDrag)
	} else if a.sidebarResizing {
		a.sidebarResizing = false
		a.savePrefs()
	}
	step := float32(10)
	if !a.prefs.SidebarLeft {
		step = -step
	}
	switch {
	case h.DoubleClicked():
		// Away from the standard width it goes back there; at it, out to
		// the widest the window allows. The press of the double click
		// also starts a drag, whose width is moved along so the frames it
		// still holds keep the new width instead of the old one.
		if a.sidebarW() != sidebarWidth {
			set(sidebarWidth)
		} else {
			set(sidebarMax)
		}
		a.sidebarDrag = a.sidebarW()
		a.savePrefs()
	case h.Shortcut(0, ui.KeyLeft):
		set(a.sidebarW() - step)
		a.savePrefs()
	case h.Shortcut(0, ui.KeyRight):
		set(a.sidebarW() + step)
		a.savePrefs()
	}
	if held || h.Hovered() || h.FocusVisible() {
		h.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + r.W/2 - 1, Y: r.Y + 12, W: 2, H: r.H - 24}, t.Accent.Alpha(0.5), 1)
		})
	}
}

// resizerTip shows the resizer's hint beside the line, toward the document,
// at the pointer's height. The line spans the window, so Element.Tooltip
// would find no room above or below it and pin the tip to the window's top,
// over the toolbar's controls next to the line.
func (a *App) resizerTip(c *ui.Context, h ui.Element, hint string) {
	t := c.Theme()
	r := h.Bounds()
	_, y, inside := c.Root().PointerPosition()
	if !inside || r.H <= 0 {
		y = r.Y + r.H/2
	}
	// Keep the tip off the toolbar band and the window's bottom edge.
	y = min(max(y, r.Y+64), r.Y+r.H-48)
	ui.TooltipBase(c, h, func(tip ui.Element) {
		fill, text := t.Inverse, t.InverseText
		if fill == ui.Transparent {
			fill = t.Text
		}
		if text == ui.Transparent {
			text = t.Background
		}
		tip.MaxWidth(t.Space(80)).Padding(t.Space(1.25), t.Space(2)).Radius(t.Space(1.25)).
			Background(fill).TextColor(text).FontSize(t.FontSize-1).Shadow(0, 2, 8, 0, black.Alpha(0.2))
		tip.Children(func() { ui.Text(c, hint) })
		if a.prefs.SidebarLeft {
			tip.AttachTo(h, ui.AnchorRight, ui.AnchorLeft).MarginStart(6)
		} else {
			tip.AttachTo(h, ui.AnchorLeft, ui.AnchorRight).MarginEnd(6)
		}
		tip.Top(y - (r.Y + r.H/2))
	})
}

// spaceKeys are the digits that, with ⌘, go to the first nine spaces.
var spaceKeys = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// shortcuts handles the window's keys.
func (a *App) shortcuts(c *ui.Context) {
	if a.settingsOpen || a.recording != "" || a.tour.open {
		return // the settings take the next key, the tour its own
	}
	switch {
	case c.Shortcut(ui.Cmd, ui.KeyO):
		a.chooseExternalFiles()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS):
		a.chooseSaveAs()
	case a.pressed(c, "palette"):
		a.openPalette()
	case a.pressed(c, "split-add"):
		a.addSplitPane()
	case a.pressed(c, "split-close"):
		if a.split != nil {
			a.closePane(a.split.active)
		}
	case a.pressed(c, "split-next"):
		a.cyclePane()
	case a.pressed(c, "new-doc"):
		a.newDoc(a.currentDir())
	case a.pressed(c, "new-folder"):
		a.newFolder(a.currentDir())
	case a.pressed(c, "daily"):
		a.openDaily()
	case a.pressed(c, "replace-all"):
		a.openReplaceAll()
	case a.pressed(c, "sidebar"):
		a.toggleSidebar()
	case a.pressed(c, "focus"):
		a.toggleFocus()
	case a.pressed(c, "source"):
		a.toggleSource()
	case a.pressed(c, "back"):
		a.goHistory(-1)
	case a.pressed(c, "forward"):
		a.goHistory(1)
	case a.pressed(c, "prev-space"), c.Shortcut(0, ui.KeyBack):
		a.stepSpace(-1)
	case a.pressed(c, "next-space"), c.Shortcut(0, ui.KeyForward):
		a.stepSpace(1)
	case a.pressed(c, "reload"):
		a.reload()
	case a.pressed(c, "save"):
		a.flush()
	case a.pressed(c, "settings"):
		a.openSettings(tabGeneral)
	case a.pressed(c, "overview"):
		a.toggleOverview()
	case a.pressed(c, "library"):
		a.toggleLibrary()
	case a.pressed(c, "outline"):
		a.toggleOutline()
	case a.pressed(c, "bookmark"):
		a.toggleBookmarkAtCaret()
	case a.pressed(c, "bookmarks"):
		a.openBookmarks()
	case a.pressed(c, "prev-bookmark"):
		a.stepBookmark(-1)
	case a.pressed(c, "next-bookmark"):
		a.stepBookmark(1)
	case a.pressed(c, "history"):
		a.openVersions()
	case a.pressed(c, "ask"):
		a.toggleQA()
	case a.pressed(c, "export-pdf"):
		a.exportPDF()
	}
	for i, k := range spaceKeys {
		if i < splitLimit && c.Shortcut(ui.Ctrl|ui.Shift, k) {
			a.focusPane(i, true)
		}
		if c.Shortcut(ui.Cmd, k) || c.Shortcut(ui.Ctrl, k) {
			a.switchSpaceAt(i)
		}
	}
}

// currentDir is the folder new documents go in: the open document's.
func (a *App) currentDir() string {
	if a.doc == nil || a.doc.space != a.state.ActiveSpace() {
		return ""
	}
	if dir := path.Dir(a.doc.rel); dir != "." {
		return dir
	}
	return ""
}

// peekSidebar measures pointer distance from the selected docking edge.
func (a *App) peekSidebar(c *ui.Context) {
	t := c.Theme()
	x, _, inside := c.Root().PointerPosition()
	w, _ := c.Size()
	if !a.prefs.SidebarLeft {
		x = w - x
	}
	// Test the final panel area, so moving into it during the slide does
	// not close it before the animated panel reaches the pointer.
	over := inside && (x < 8 || a.peek && x < a.sidebarW()+12)
	switch {
	case a.palette.open || a.settingsOpen:
		a.peek = false
		a.peekLeave = time.Time{}
	case over || a.renaming != "":
		a.peek = true
		a.peekLeave = time.Time{}
	case a.peek:
		if a.peekLeave.IsZero() {
			a.peekLeave = c.Now().Add(peekDelay)
		}
		if wait := a.peekLeave.Sub(c.Now()); wait > 0 {
			c.After(wait)
		} else {
			a.peek = false
			a.peekLeave = time.Time{}
		}
	}
	look := a.look
	ui.Overlay(c, func() {
		_, h := c.Size()
		panel := ui.Column(c.Key("peek")).Absolute().Top(6).Width(a.sidebarW()).Height(h-12).
			Radius(12).Background(a.look.opaqueTop()).Border(1, t.Border.Alpha(0.5)).
			Draw(func(p *ui.Painter, r ui.Rect) { look.paint(p, r, 12, false) }).
			Shadow(0, 10, 32, 0, black.Alpha(0.28)).Clip()
		target := float32(0)
		if a.peek {
			target = 1
		}
		v := panel.Animate("in", target, 150*time.Millisecond)
		offset := (a.sidebarW() + 24) * (1 - v)
		if a.prefs.SidebarLeft {
			panel.Left(6)
			offset = -offset
		} else {
			panel.Right(6)
		}
		panel.Translate(offset, 0).Hidden(v < 0.01 && !a.peek)
		panel.Children(func() { a.sidebar(c, true) })
	})
}

// main is the toolbar over the card holding the open document.
func (a *App) main(c *ui.Context, docked float32) {
	t := c.Theme()
	main := ui.Row(c.Key("main")).Grow(1).MinWidth(0).AlignItems(ui.Stretch)
	target := float32(0)
	if a.qa.open && a.state.ActiveSpace() != nil && (!a.spaceForm.open || a.spaceForm.id != "") {
		target = qaWidth
	}
	// Resolve the panel before the document: both must use this frame's
	// width, and offscreen paragraphs can keep their shaping while it moves.
	a.qa.shown = main.Animate("qa-width", target, slide)
	a.qa.resizing = a.qa.shown != target
	left := float32(8)
	if docked > 0.5 && a.prefs.SidebarLeft {
		left = 0
	}
	// The question panel, when open, stands right of the document and its
	// toolbar, which then leaves the window buttons' room to it.
	panel := a.qa.shown
	right := float32(8)
	if panel > 0.5 || docked > 0.5 && !a.prefs.SidebarLeft {
		right = 0
	}
	main.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Padding(0, right, 8, left).Children(func() {
			if a.split != nil && !(a.spaceForm.open && a.spaceForm.id == "") {
				a.splitView(c)
				return
			}
			a.toolbar(c, docked)
			card := ui.Themed(c.Key("card"), documentTheme(a.paperDark)).Grow(1).MinHeight(0).Radius(panelRadius).Border(1, t.Border).Clip()
			w, _ := c.Size()
			a.splitDropTarget(c, card)
			a.tourMark("bookmarks", card)
			card.Children(func() { a.documentContent(c, a.doc, w-docked-right-left-2-panel) })
		})
		if !a.spaceForm.open || a.spaceForm.id != "" {
			a.qaPanel(c, docked)
		}
	})
}

func (a *App) documentContent(c *ui.Context, d *document, cardWidth float32) {

	if a.spaceForm.open && a.spaceForm.id == "" {
		a.draftState(c)
		return
	}
	if d == nil {
		a.emptyState(c)
		return
	}
	if d.loadErr != nil {
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Text(c, i18n.T("文档无法打开")).FontSize(20)
			ui.Text(c, d.loadErr.Error()).TextColor(c.Theme().Danger).MaxWidth(460)
			if ui.Button(c, i18n.T("重新载入")).Clicked() {
				a.activateDocument(d)
				a.reload()
			}
		})
		return
	}
	applyModeFade := func(el ui.Element) {
		if d.modeSwitchPending {
			d.modeSwitchPending = false
			if !a.reduceMotion {
				d.modeAt = c.Now()
			}
		}
		if !d.modeAt.IsZero() && !a.reduceMotion {
			const dur = 90 * time.Millisecond
			if elapsed := c.Now().Sub(d.modeAt); elapsed >= 0 && elapsed < dur {
				p := float32(elapsed) / float32(dur)
				k := 1 - (1-p)*(1-p)
				el.Opacity(0.84 + 0.16*k)
				c.After(0)
			} else {
				d.modeAt = time.Time{}
			}
		}
	}
	if d.source {
		box := ui.Column(c.Key("source-page")).Grow(1).Padding(24).Gap(12)
		applyModeFade(box)
		box.Children(func() {
			ui.Row(c).Gap(12).Children(func() {
				ui.Text(c, i18n.T("Markdown 导出预览")).Grow(1)
				if ui.Button(c, i18n.T("导出 Markdown 文件")).Clicked() {
					a.activateDocument(d)
					a.exportMarkdown()
				}
			})
			ta := ui.TextArea(c.Key("export-preview"), &d.preview).Grow(1).ReadOnly(true).
				TrackScroll(&d.previewScroll).Label(i18n.T("Markdown 导出预览"))
			if d.previewCaretPending {
				d.previewCaretPending = false
				ta.SetTextSelection(d.previewCaret, d.previewCaret)
			} else if d.previewEnteredY < 0 {
				d.previewEnteredY = d.previewScroll.Y
			}
		})
		return
	}
	// One column always, so that the bar coming and going keeps the
	// editor's place in the tree, and with it its scroll and focus.
	page := ui.Column(c.Key("page")).Grow(1).MinHeight(0)
	applyModeFade(page)
	if d == a.doc && a.navDir != 0 && !a.reduceMotion {
		if a.navAt.IsZero() {
			a.navAt = c.Now()
		}
		elapsed := c.Now().Sub(a.navAt)
		const dur = 90 * time.Millisecond
		if elapsed >= 0 && elapsed < dur {
			p := float32(elapsed) / float32(dur)
			k := 1 - (1-p)*(1-p)
			page.Opacity(0.86+0.14*k).Translate(float32(a.navDir)*6*(1-k), 0)
			c.After(0)
		} else {
			a.navDir = 0
			a.navAt = time.Time{}
		}
	}
	page.Children(func() {
		a.conflictBarFor(c, d)
		editor.View(c.Key(d.viewKey()), d.ed, a.editorOptionsFor(c, cardWidth, false, d))
	})
}

// toolbar is the row above the card: the history's arrows, the address
// of the document, as Arc's, the editor's word count and format menu,
// and the more menu. It drags the window.
func (a *App) toolbar(c *ui.Context, docked float32) {
	bar := c.TitleBar()
	// The sidebar reserves the left controls; Windows and Linux can keep
	// their window buttons on the right, over this toolbar instead.
	left, right := bar.Left, bar.Right
	if a.prefs.SidebarLeft {
		left = max(0, left-docked)
	} else {
		right = max(0, right-docked)
	}
	if a.qa.shown > 0.5 {
		right = 0 // the question panel's header holds the window buttons' room
	}
	toolbar := ui.Row(c.Key("toolbar")).Height(max(bar.Height, 52)).Padding(0, right+8, 0, left).Gap(2).AlignItems(ui.Center).DragWindow()
	if a.qa.shown > 0.5 {
		// A narrow document's controls must not paint or receive clicks over
		// the question panel while it takes room beside the document.
		toolbar.Clip()
	}
	// The toggle that opens the folded sidebar sits on the sidebar's edge.
	showSidebar := func() {
		if a.folded() && iconButton(c, "show-sidebar", a.sidebarIcon(), i18n.T("展开侧边栏"), a.withKey(i18n.T("展开侧边栏"), "sidebar"), false).Clicked() {
			a.toggleSidebar()
		}
	}
	toolbar.Children(func() {
		ui.Row(c.Key("brand")).PaddingX(8).Shrink(0).Children(func() { brand(c, "", true) })
		if a.prefs.SidebarLeft {
			showSidebar()
		}
		// The history's arrows live beside the sidebar's search field; with
		// the sidebar folded away they come back here to stay reachable.
		if docked < 0.5 {
			a.historyButtons(c)
		}
		ui.Box(c).Width(8)
		a.address(c)
		ui.Box(c).Width(8)
		// A narrow pane, as beside the library, keeps its room for the path.
		if a.doc != nil && a.doc.loadErr == nil && !a.doc.source && toolbar.LayoutBounds().W >= 560 {
			a.doc.ed.Corner(c)
		}
		ask := iconButton(c, "ask", iconAsk, i18n.T("问答"), a.withKey(i18n.T("向当前空间提问"), "ask"), a.state.ActiveSpace() == nil)
		if a.qa.open {
			ask.Background(glass(c.Theme()))
		}
		if ask.Clicked() {
			a.toggleQA()
		}
		a.moreMenu(c)
		if !a.prefs.SidebarLeft {
			showSidebar()
		}
	})
}

// historyButtons are the back, forward and reload buttons.
func (a *App) historyButtons(c *ui.Context) {
	if iconButton(c, "back", iconBack, i18n.T("后退"), a.withKey(i18n.T("后退"), "back"), !a.canGo(-1)).Clicked() {
		a.goHistory(-1)
	}
	if iconButton(c, "forward", iconForward, i18n.T("前进"), a.withKey(i18n.T("前进"), "forward"), !a.canGo(1)).Clicked() {
		a.goHistory(1)
	}
	if iconButton(c, "reload", iconReload, i18n.T("重新载入"), a.withKey(i18n.T("从磁盘重新载入"), "reload"), a.doc == nil).Clicked() {
		a.reload()
	}
}

// address shows where the document is, centered in the toolbar: its name
// over the space and its folders. It only shows; the toolbar around it
// drags the window, and the sidebar's field opens the command bar.
func (a *App) address(c *ui.Context) {
	t := c.Theme()
	f := ui.Row(c.Key("address")).Grow(1).MinWidth(0).Height(42).PaddingX(12).Gap(8).
		AlignItems(ui.Center).Justify(ui.Center).Label(i18n.T("文档路径"))
	f.Children(func() {
		if a.spaceForm.open && a.spaceForm.id == "" {
			ui.Text(c, i18n.T("新空间")).TextColor(t.TextMuted).NoWrap()
			return
		}
		if a.doc == nil {
			if sp := a.state.ActiveSpace(); sp != nil {
				ui.Text(c, sp.Name).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			}
			return
		}
		ui.Column(c).MinWidth(0).Shrink(1).AlignItems(ui.Center).Children(func() {
			ui.Text(c, path.Base(a.doc.rel)).FontSize(15).FontWeight(600).SingleLine().Tooltip(a.doc.fullPath())
			breadcrumb := a.doc.space.Name
			if dir := path.Dir(a.doc.rel); dir != "." {
				breadcrumb += " / " + strings.ReplaceAll(dir, "/", " / ")
			}
			ui.Text(c, breadcrumb).FontSize(12).TextColor(t.TextMuted).SingleLine()
		})
		status, color := "", t.TextMuted
		switch {
		case a.doc.saveErr != nil:
			status, color = i18n.T("保存失败"), t.Danger
		case a.doc.dirty():
			status = i18n.T("已修改")
		}
		if status != "" {
			ui.Text(c, status).FontSize(12).TextColor(color).NoWrap().Tooltip(errText(a.doc.saveErr))
		}
	})
}

// iconButton is a quiet button showing only an icon, named by label for
// assistive technology, with tip as its tooltip.
func iconButton(c *ui.Context, key string, icon *ui.SVG, label, tip string, disabled bool) ui.Element {
	return quietButton(c, key, func(b ui.Element) ui.Element {
		return b.Size(30, 30).Padding(0).Center().Label(label).Tooltip(tip).Disabled(disabled).
			Children(func() { ui.Icon(c, icon).FontSize(16) })
	})
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return i18n.T(err.Error())
}

// followLink opens a link of the document: another document of the space,
// or a web address in the browser.
func (a *App) followLink(url string) {
	if a.doc != nil {
		if a.doc.space.IsExternal() {
			file, _, _ := strings.Cut(url, "#")
			if vault.IsDocument(file) {
				if full, err := a.doc.localImage(file); err == nil {
					a.openSystemFile(full)
					return
				}
			}
		}
		if rel, ok := a.doc.resolveLink(url); ok {
			a.openIn(a.doc.space, rel)
			return
		}
	}
	if ui.SafeToOpen(url) {
		mygo.Shell.OpenExternal(url)
	}
}

// emptyState shows when no document is open.
func (a *App) emptyState(c *ui.Context) {
	t := c.Theme()
	sp := a.state.ActiveSpace()
	ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
		if sp != nil {
			a.spaceGlyph(c, sp.Icon, sp.Symbol, sp.Image, sp.Name, 40, t.Accent)
			ui.Text(c, sp.Name).FontSize(20).Bold()
		}
		switch {
		case a.treeErr != nil:
			ui.Text(c, i18n.T("读取空间文件夹失败。")+a.treeErr.Error()).TextColor(t.Danger).MaxWidth(420).TextAlign(ui.Center)
		case sp.IsExternal() && len(a.docs) == 0:
			ui.Text(c, i18n.T("打开的外部文档会显示在这里，文件保留在原位置。")).TextColor(t.TextMuted).MaxWidth(360).TextAlign(ui.Center)
		case len(a.docs) == 0:
			ui.Text(c, i18n.T("此空间暂无文档。")).TextColor(t.TextMuted)
		default:
			ui.Text(c, i18n.T("选择一篇文档，或按 ⌘K 搜索。")).TextColor(t.TextMuted)
		}
		ui.Row(c).Gap(8).Children(func() {
			if sp.IsExternal() {
				if ui.PrimaryButton(c.Key("empty-open"), i18n.T("打开文件…")).Clicked() {
					a.chooseExternalFiles()
				}
			} else if ui.PrimaryButton(c.Key("empty-new"), i18n.T("新建文档")).Tooltip("⌘N").Clicked() {
				a.newDoc("")
			}
			if ui.Button(c.Key("empty-palette"), i18n.T("命令栏")).Tooltip("⌘K").Clicked() {
				a.openPalette()
			}
		})
	})
}
