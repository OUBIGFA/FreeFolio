package app

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The file registry lives only as long as this session. Its resolver accepts
// individual user-selected paths and never walks their parent directories.
//
// The browser comes when a loose file opens and goes with its last tab: only
// "返回空间" leads back to the spaces, which stay suspended meanwhile.
type fileSession struct {
	marks     space.State
	locations *space.Space
	previous  *document
	split     *splitGroup
	// order lists the tabs, left to right, by their names in locations.
	order []string
	// readOnly remembers each tab's lock while another tab is shown, since
	// only the shown tab keeps an editor.
	readOnly map[string]bool
	// middle is the tab a middle press began on; releasing over it closes it.
	middle string
	// revealed is the tab last scrolled into the tab strip's view.
	revealed string
	// endWidth caches the right-side toolbar group width from the previous frame
	// so narrow windows can bound the left-side word count without hiding tabs.
	endWidth float32
	// openWidth is the open button's width from the previous frame, taken
	// from the room the tabs share.
	openWidth float32
	// closing is set once the last tab asked the window to close. Close is
	// posted, so frames still draw until it lands; they draw nothing rather
	// than flash the empty browser the user did not ask to keep.
	closing bool
}

func (a *App) openLooseFile(full string) bool {
	if a.doc != nil && a.doc.fullPath() == full && a.files != nil {
		return true
	}
	f := a.files
	if f == nil {
		f = &fileSession{locations: &space.Space{ID: space.ExternalID, Name: i18n.T("文件浏览")}, previous: a.doc, split: a.split, readOnly: map[string]bool{}}
	}
	if a.files != nil && a.split != nil {
		for i, pane := range a.split.panes {
			if pane.doc != nil && pane.doc.fullPath() == full {
				a.focusPane(i, true)
				a.palette.open = false
				return true
			}
		}
	}
	name := f.locations.AddExternal(full)
	text, err := readFile(f.locations, name)
	if err != nil {
		if !slices.Contains(f.order, name) {
			delete(f.locations.ExternalFiles, name)
		}
		a.notify("无法打开文件；%v", err)
		return false
	}
	if a.files == nil {
		a.suspendSplit()
		a.files = f
	}
	shown := a.fileTabShown()
	if !slices.Contains(f.order, name) {
		at := len(f.order)
		if i := slices.Index(f.order, shown); i >= 0 && a.prefs.Files.TabAfterCurrent {
			at = i + 1
		}
		f.order = slices.Insert(f.order, at, name)
	}
	f.closing = false
	if shown != "" {
		f.readOnly[shown] = a.doc.ed.ReadOnly
		a.noteFilePlace(a.doc)
	}
	old := a.doc
	a.doc = openDocument(f.locations, name, text, false)
	a.syncSplit()
	a.palette.open = false
	a.retireClosedDocuments(old)
	a.restoreFilePlace(a.doc)
	readOnly, seen := f.readOnly[name]
	if !seen {
		readOnly = a.prefs.Files.ReadOnly
	}
	a.doc.ed.SetReadOnly(readOnly)
	a.doc.ed.RequestFocus = true
	return true
}

// fileTabShown is the name of the tab the browser shows, "" for none.
func (a *App) fileTabShown() string {
	if f := a.files; f != nil && a.doc != nil && a.doc.space == f.locations {
		return a.doc.rel
	}
	return ""
}

// showFileTab saves the shown tab and shows the tab of rel.
func (a *App) showFileTab(rel string) bool {
	f := a.files
	if f == nil || a.fileTabShown() == rel {
		return f != nil
	}
	return a.flushCurrent() && a.openLooseFile(f.locations.ExternalFiles[rel])
}

// stepFileTab shows the tab by after the shown one, wrapping around.
func (a *App) stepFileTab(by int) {
	f := a.files
	if f == nil || len(f.order) < 2 {
		return
	}
	i := max(0, slices.Index(f.order, a.fileTabShown()))
	a.showFileTab(f.order[(i+by+len(f.order))%len(f.order)])
}

func (a *App) leaveFileMode() bool {
	if a.files == nil {
		return true
	}
	if !a.flush() {
		return false
	}
	var closed []*document
	if a.split != nil {
		for _, p := range a.split.panes {
			if p.doc != nil {
				a.noteFilePlace(p.doc)
				closed = append(closed, p.doc)
			}
		}
	}
	a.noteFilePlace(a.doc)
	old := a.doc
	a.doc, a.split = a.files.previous, a.files.split
	a.files = nil
	a.palette.open = false
	a.retireClosedDocuments(append(closed, old)...)
	return true
}

// Reopening a tray-resident window should not resurrect an empty file browser.
// Live tabs keep their existing behavior; the user's home preference still wins.
func (a *App) prepareWindowReopen() {
	if a.prefs.StartAtHome {
		a.showSpaceHome()
		return
	}
	if a.files != nil && len(a.files.order) == 0 {
		a.leaveFileMode()
	}
}

// closeFileTab closes the tab of rel, showing its right neighbor, else its
// left, when it was shown. The last tab takes the window with it unless the
// user keeps the empty browser; it never returns to the spaces.
func (a *App) closeFileTab(rel string) {
	f := a.files
	i := -1
	if f != nil {
		i = slices.Index(f.order, rel)
	}
	if i < 0 {
		return
	}
	if a.split != nil {
		for i, p := range a.split.panes {
			if p.doc != nil && p.doc.rel == rel && p.doc.space == f.locations {
				a.focusPane(i, false)
				if !a.flushCurrent() {
					return
				}
				a.noteFilePlace(p.doc)
				a.closePane(i)
				if a.split != nil && slices.Contains(a.split.panes, p) {
					return
				}
				break
			}
		}
	}
	shown := a.fileTabShown() == rel
	if shown && !a.flushCurrent() {
		return
	}
	if shown {
		a.noteFilePlace(a.doc)
	}
	delete(f.locations.ExternalFiles, rel)
	delete(f.readOnly, rel)
	f.order = slices.Delete(f.order, i, i+1)
	if !shown {
		return
	}
	old := a.doc
	a.doc = nil
	defer a.retireClosedDocuments(old)
	if len(f.order) > 0 {
		a.openLooseFile(f.locations.ExternalFiles[f.order[min(i, len(f.order)-1)]])
		return
	}
	if !a.prefs.Files.KeepOpen && a.closeMain != nil {
		f.closing = true
		a.closeMain()
	}
}

// closeOtherFileTabs keeps the tab of rel and closes those drop picks by
// their index, rel's being keep. It shows rel first, so no tab that closes
// has to open another on the way.
func (a *App) closeOtherFileTabs(rel string, drop func(i, keep int) bool) {
	f := a.files
	if f == nil || !a.showFileTab(rel) {
		return
	}
	keep := slices.Index(f.order, rel)
	var gone []string
	for i, name := range f.order {
		if i != keep && drop(i, keep) {
			gone = append(gone, name)
		}
	}
	for _, name := range gone {
		a.closeFileTab(name)
	}
}

func (a *App) chooseSaveAs() {
	d, win := a.doc, a.win
	if d == nil || d.loadErr != nil || d.isPDF() || win == nil {
		return
	}
	options := mygo.SaveDialogOptions{Parent: win, Title: i18n.T("另存为"), DefaultPath: d.fullPath(), Filters: []mygo.FileFilter{{Name: "Markdown", Extensions: []string{"md", "markdown", "mdx"}}, {Name: "Legacy", Extensions: []string{"arcmd"}}}}
	go func() {
		dest, err := mygo.Dialog.Save(options)
		win.Update(func() {
			if err != nil {
				a.notify("另存为失败；%v", err)
				return
			}
			if dest != "" && a.doc == d {
				a.saveAs(dest)
			}
		})
	}()
}

// The destination receives the committed model. A conflict on the original
// can thus be rescued to a different file without overwriting the original.
func (a *App) saveAs(dest string) bool {
	d := a.doc
	if d == nil || d.loadErr != nil || d.isPDF() {
		return false
	}
	if filepath.Ext(dest) == "" {
		dest += ".md"
	}
	if !vault.IsText(dest) {
		a.notify("请选择 Markdown 或兼容文档格式")
		return false
	}
	d.waitAsyncSave()
	srcInfo, _ := os.Stat(d.fullPath())
	dstInfo, statErr := os.Stat(dest)
	if srcInfo != nil && dstInfo != nil && os.SameFile(srcInfo, dstInfo) {
		return a.flushCurrent()
	}
	for _, other := range a.residentDocuments() {
		if other != d && dstInfo != nil {
			info, _ := os.Stat(other.fullPath())
			if info != nil && os.SameFile(info, dstInfo) {
				a.notify("文件已打开，请先关闭目标文件")
				return false
			}
		}
	}
	committed := d.ed.Committed()
	text := d.diskText
	err := committed.Validate()
	if err == nil {
		switch {
		case strings.EqualFold(filepath.Ext(dest), ".arcmd"):
			text, err = committed.Encode()
		case d.dirty() || strings.EqualFold(filepath.Ext(d.rel), ".arcmd"):
			text = editor.ExportMarkdown(committed)
		}
	}
	if err == nil {
		switch {
		case errors.Is(statErr, os.ErrNotExist):
			err = fsutil.CreateFileAtomic(dest, []byte(text), 0644)
		case statErr != nil:
			err = statErr
		default:
			err = fsutil.WriteFileAtomic(dest, []byte(text), dstInfo.Mode().Perm())
		}
	}
	if err != nil {
		a.notify("另存为失败；%v", err)
		return false
	}
	// Restore the original's on-disk model before navigation flushes editors.
	if d.dirty() || d.conflict || d.missing {
		if original, e := readFile(d.space, d.rel); e == nil {
			a.replaceDoc(original)
		} else {
			a.replaceReferences(d, nil)
		}
	}
	f, from := a.files, d.rel
	if !a.openSystemFile(dest) {
		return false
	}
	if f != nil && a.files == f && d.space == f.locations && a.doc.rel != from {
		// The copy takes the original's tab, as an editor's save as does.
		to := a.doc.rel
		f.order = slices.DeleteFunc(f.order, func(name string) bool { return name == to })
		if i := slices.Index(f.order, from); i >= 0 {
			f.order[i] = to
			delete(f.locations.ExternalFiles, from)
			delete(f.readOnly, from)
		} else {
			f.order = append(f.order, to)
		}
	}
	return true
}

// fileBarHeight is the least height of the file browser's bar.
const fileBarHeight = 44

// fileView is the file browser: one bar of tabs over the page. It shows no
// paths, and "返回空间" is its only way back to the spaces.
func (a *App) fileView(c *ui.Context) {
	f := a.files
	w, _ := c.Size()
	a.docTheme = c.Theme()
	a.paperDark = c.Theme().Dark
	a.look = lookOf(space.NewTheme(0), a.paperDark)
	t := spaceTheme(c.Theme(), a.look)
	c.SetTheme(t)
	a.syncWatches()
	if a.autosaveEnabled() {
		for _, d := range a.visibleDocuments() {
			d.autosaveWith(c, a.asyncPost())
		}
	}
	for _, n := range a.notices {
		c.Toast(n)
	}
	a.notices = nil
	if !a.settingsOpen && !a.oneBotEditShortcuts(c) {
		switch {
		case c.Shortcut(ui.Cmd, ui.KeyO):
			a.chooseExternalFiles()
		case a.pressed(c, "settings"):
			a.openSettings(tabFiles)
		case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS):
			a.chooseSaveAs()
		case a.pressed(c, "save"):
			a.flushCurrent()
		case c.Shortcut(ui.Cmd, ui.KeyW):
			a.closeFileTab(a.fileTabShown())
		case c.Shortcut(ui.Ctrl, ui.KeyTab):
			a.stepFileTab(1)
		case c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab):
			a.stepFileTab(-1)
		case a.pressed(c, "split-add"):
			a.addSplitPane()
		case a.pressed(c, "split-close"):
			if a.split != nil {
				a.closePane(a.split.active)
			}
		case a.pressed(c, "split-next"):
			a.cyclePane()
		case a.canZoomPage(a.doc) && a.pressed(c, "zoom-in"):
			a.stepPageZoom(1)
		case a.canZoomPage(a.doc) && a.pressed(c, "zoom-out"):
			a.stepPageZoom(-1)
		case a.canZoomPage(a.doc) && a.pressed(c, "zoom-reset"):
			a.resetPageZoom()
		case a.pressed(c, "source"):
			a.toggleSource()
		case a.pressed(c, "full-width"):
			a.toggleFullWidth()
		case a.pressed(c, "outline"):
			a.toggleOutline()
		case a.pressed(c, "export-pdf"):
			a.exportPDF()
		case a.pressed(c, "ask"):
			a.toggleQA()
		}
	}
	a.winW = w
	a.qa.resizing = false
	ui.Column(c.Key("file-mode")).Fill().Background(t.Background).Children(func() {
		if f.closing && len(f.order) == 0 {
			return
		}
		if a.fileBar(c, f); a.files != f {
			return
		}
		// The bar keeps the whole width; OneBot slides in flat beside the
		// page, parted from it by a line, as the page lies flat under the bar.
		body := ui.Row(c.Key("file-body")).Grow(1).MinHeight(0).AlignItems(ui.Stretch)
		body.Children(func() {
			a.oneBotResizer(c, 0)
			target := float32(0)
			if a.qa.open {
				target = a.oneBotWidth(0)
			}
			motion := slide
			if a.qa.dragging || a.qa.resizing {
				motion = 0
			}
			a.qa.shown = body.Animate("qa-width", target, motion)
			a.qa.resizing = a.qa.resizing || a.qa.dragging || a.qa.shown != target
			if a.split != nil {
				ui.Column(c.Key("file-split")).Grow(1).MinWidth(0).MinHeight(0).Children(func() { a.splitView(c) })
			} else if d := a.doc; d != nil {
				ui.Themed(c.Key("file-paper"), documentTheme(a.paperDark)).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() { a.documentContent(c, d, w-a.qa.shown) })
			} else {
				ui.Column(c.Key("file-empty-pane")).Grow(1).MinWidth(0).Children(func() { a.fileEmpty(c) })
			}
			a.qaPanel(c, 0)
		})
	})
	a.paletteView(c)
	a.unsavedPromptView(c)
	a.databasePeekModal(c)
	a.settingsView(c)
	a.chatGPTWelcomeView(c)
	a.pdfExportView(c)
	a.mainWindowButtons(c)
}

// fileBar holds the tabs and, at its end, the page's tools, the menu and
// the way back. Its free stretch drags the window; files dropped on it open.
func (a *App) fileBar(c *ui.Context, f *fileSession) {
	bar := a.titleBar(c)
	row := ui.Row(c.Key("file-toolbar")).Height(max(bar.Height, fileBarHeight)).Shrink(0).Gap(6).AlignItems(ui.Center).DragWindow()
	for _, file := range row.DroppedFiles() {
		a.openSystemFile(file)
	}
	row.Children(func() {
		start := ui.Row(c.Key("file-toolbar-start")).Shrink(0).Gap(6).AlignItems(ui.Center).PaddingStart(bar.Left + 10).Children(func() {
			logo := ui.Image(c, brandLogo()).Size(18, 18).Shrink(0)
			if d := a.doc; hasWordCount(d) {
				logo.Margin(0, 2, 0, 0)
				maxW := float32(0)
				if rowW := row.LayoutBounds().W; rowW > 0 && f.endWidth > 0 {
					// Keep room for at least a tab and the trailing "+" open button on narrow windows.
					maxW = max(64, rowW-(bar.Left+36)-f.endWidth-108)
				}
				d.ed.WordButtonFit(c, maxW, 30)
			} else {
				logo.Margin(0, 4, 0, 0)
			}
		})
		end := ui.Row(c.Key("file-toolbar-end")).Shrink(0).Gap(6).AlignItems(ui.Center).PaddingEnd(bar.Right + 8).MarginStart(ui.Auto).Children(func() {
			if a.split != nil || a.doc != nil && a.doc.loadErr == nil {
				a.addSplitButton(c, a.addSplitPane)
			}
			if d := a.doc; d != nil && d.loadErr == nil {
				a.documentCorner(c, d)
			}
			ask := iconButton(c, "ask", iconAsk, i18n.T("OneBot"), a.withKey(i18n.T("向打开的文件提问"), "ask"), false).Shrink(0)
			if a.qa.open {
				ask.Background(navigationFill(c.Theme()))
			}
			if ask.Clicked() {
				a.toggleQA()
			}
			a.fileMenu(c, f)
			if shellAction(c, "return", iconBack, i18n.T("返回空间")).Clicked() {
				a.leaveFileMode()
			}
		})
		if w := end.LayoutBounds().W; w > 0 {
			f.endWidth = w
		}
		if a.files != f {
			return
		}
		// Center the tabs while their trailing open button also clears the
		// page tools. Narrow windows keep the button outside the scrolling strip.
		b, l, r := row.LayoutBounds(), start.LayoutBounds(), end.LayoutBounds()
		from, to := l.X+l.W-b.X+12, r.X-b.X-12
		area := ui.Row(c.Key("file-tab-area")).Absolute().Top(0).Bottom(0).Gap(6).AlignItems(ui.Center).Clip()
		var strip, tabs, open ui.Element
		tabMax := a.fileTabMaxWidth(c, f, to-from-6-f.openWidth)
		area.Children(func() {
			strip = ui.ScrollHorizontal(c.Key("file-tabs")).Shrink(1).MinWidth(0).Role(ui.RoleTabList).Label(i18n.T("已打开的文件"))
			strip.Children(func() {
				tabs = ui.Row(c.Key("file-tab-row")).Shrink(0).Gap(2).AlignItems(ui.Center).Children(func() {
					for i, rel := range slices.Clone(f.order) {
						if a.files != f {
							return
						}
						a.fileTab(c, f, i, rel, tabMax)
					}
				})
			})
			open = iconButton(c, "open", iconPlus, i18n.T("打开文件"), i18n.T("打开文件")+" ("+combo(ui.Cmd, ui.KeyO).Label()+")", false).Shrink(0)
			if open.Clicked() {
				a.chooseExternalFiles()
			}
		})
		if w := open.LayoutBounds().W; w > 0 {
			f.openWidth = w
		}
		width := tabs.LayoutBounds().W
		groupWidth := width + 6 + open.LayoutBounds().W
		centered := (b.W - width) / 2
		if width > 0 && centered >= from && centered+groupWidth <= to {
			area.Left(centered).Width(groupWidth)
		} else {
			area.Left(from).Width(max(0, to-from))
		}
		// Measured bounds arrive after building the view. Recheck placement
		// and reveal the active tab when a resize changes its visible space.
		for _, element := range []ui.Element{row, start, end, tabs, strip, open} {
			before := element.LayoutBounds()
			isStrip := element == strip
			element.Draw(func(_ *ui.Painter, now ui.Rect) {
				if now != before {
					if isStrip && now.W != before.W {
						f.revealed = ""
					}
					c.AnimationFrame()
				}
			})
		}
	})
}

// fileTabLabel names rel's tab by its file name, and its folder too when
// another open file has the same name.
func fileTabLabel(f *fileSession, rel string) string {
	full := f.locations.ExternalFiles[rel]
	name := filepath.Base(full)
	for _, other := range f.order {
		if other != rel && filepath.Base(f.locations.ExternalFiles[other]) == name {
			// The folder tells two files of one name apart without a path.
			return name + " · " + filepath.Base(filepath.Dir(full))
		}
	}
	return name
}

// Tab widths: the least a tab takes, and the most each takes when the
// tabs do not all fit, so that the strip scrolls rather than squeezing
// every name.
const (
	fileTabMinWidth = 72
	fileTabCapWidth = 240
)

// fileTabMaxWidth is the widest a tab may grow within avail, the strip's
// room. Tabs that all fit show their names whole; otherwise the room is
// shared out, short names keeping their width and long ones splitting the
// rest, but never below fileTabCapWidth.
func (a *App) fileTabMaxWidth(c *ui.Context, f *fileSession, avail float32) float32 {
	if avail <= 0 || len(f.order) == 0 {
		return fileTabCapWidth
	}
	// The label at the shown tab's weight, padding, border and a pixel to
	// spare, the gap to the spacer, and the gap to the close button and it.
	extra := float32(10 + 4 + 2 + 1 + 4)
	if !a.prefs.Files.HideTabClose {
		extra += 4 + 20
	}
	widths := make([]float32, len(f.order))
	for i, rel := range f.order {
		w, _ := c.MeasureText(0, ui.Span{Text: fileTabLabel(f, rel), Size: 13, Weight: 500})
		widths[i] = max(fileTabMinWidth, float32(math.Ceil(float64(w)))+extra)
	}
	avail -= float32(2 * (len(widths) - 1))
	slices.Sort(widths)
	for i, w := range widths {
		// What is left, shared by this tab and the wider ones after it.
		share := avail / float32(len(widths)-i)
		if w > share {
			return max(fileTabCapWidth, share)
		}
		avail -= w
	}
	return widths[len(widths)-1]
}

// fileTab is the tab of rel: a click shows it; a double click, if allowed,
// a middle click or its close button closes it.
func (a *App) fileTab(c *ui.Context, f *fileSession, i int, rel string, maxWidth float32) {
	t := c.Theme()
	full := f.locations.ExternalFiles[rel]
	label := fileTabLabel(f, rel)
	shown := a.fileTabShown() == rel
	tab := ui.ButtonBase(c.Key("tab-"+rel)).Role(ui.RoleTab).AccessSelected(shown).Label(label).
		Height(30).MinWidth(fileTabMinWidth).MaxWidth(maxWidth).Shrink(0).Radius(controlRadius).Padding(0, 4, 0, 10).Gap(4).Justify(ui.Start)
	ink := t.TextMuted
	switch {
	case shown:
		tab.Background(t.Surface).Border(1, t.Border)
		ink = t.Text
		if f.revealed != rel {
			f.revealed = rel
			tab.ScrollIntoView()
		}
	case tab.Hovered():
		tab.Background(t.Text.Alpha(0.06))
	}
	if button, ok := tab.PressedButton(); ok && button == 2 {
		f.middle = rel
	} else if f.middle == rel {
		f.middle = ""
		if tab.Hovered() {
			a.closeFileTab(rel)
			return
		}
	}
	tab.ContextMenu(func(m *ui.Menu) {
		if m.Item(i18n.T("关闭标签页")).Chosen() {
			a.closeFileTab(rel)
		}
		if m.Item(i18n.T("关闭其他标签页")).Disabled(len(f.order) < 2).Chosen() {
			a.closeOtherFileTabs(rel, func(int, int) bool { return true })
		}
		if m.Item(i18n.T("关闭右侧标签页")).Disabled(i == len(f.order)-1).Chosen() {
			a.closeOtherFileTabs(rel, func(at, keep int) bool { return at > keep })
		}
		m.Separator()
		if m.Item(i18n.T("复制文件路径")).Chosen() {
			c.WriteClipboard(full)
		}
		if m.Item(i18n.T("在文件管理器中显示")).Chosen() {
			a.revealIn(f.locations, rel)
		}
	})
	closing := false
	tab.Children(func() {
		weight := 400
		if shown {
			weight = 600
		}
		ui.Text(c, label).FontSize(13).FontWeight(weight).TextColor(ink).SingleLine().Shrink(1).MinWidth(0)
		ui.Spacer(c)
		if a.prefs.Files.HideTabClose {
			return
		}
		x := ui.ButtonBase(c.Key("close")).Size(20, 20).Shrink(0).Radius(5).Center().Cursor(ui.CursorPointer).
			Label(i18n.T("关闭标签页 ") + label).Tooltip(i18n.T("关闭标签页"))
		if x.Hovered() {
			x.Background(t.Text.Alpha(0.1))
		}
		closing = x.Clicked()
		x.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(ink) })
	})
	switch {
	case closing, !a.prefs.Files.NoDoubleClickClose && tab.DoubleClicked():
		a.closeFileTab(rel)
	case tab.Clicked():
		a.showFileTab(rel)
	}
}

// fileMenu holds the shown file's actions and the settings.
func (a *App) fileMenu(c *ui.Context, f *fileSession) {
	b := iconButton(c, "file-actions", iconEllipsis, i18n.T("更多"), i18n.T("更多"), false).Role(ui.RoleMenuButton)
	b.PopoverMenuButton(func(m *ui.Menu) {
		d := a.doc
		noDoc := d == nil || d.loadErr != nil
		noText := noDoc || d.isPDF() || d.isMDZ() || d.isDatabase()
		if !noDoc && !d.isPDF() && !d.isMDZ() && !d.isDatabase() {
			m.Submenu(i18n.T("更多格式"), d.ed.FormattingMenu).Disabled(d.ed.ReadOnly || d.source)
			if m.Item(i18n.T("锁定文档（只读）")).Checked(d.ed.ReadOnly).Disabled(d.ed.Locked).Chosen() {
				d.ed.SetReadOnly(!d.ed.ReadOnly)
			}
		}
		if m.Item(i18n.T("另存为")).Disabled(noText).Chosen() {
			a.chooseSaveAs()
		}
		if m.Item(i18n.T("Markdown 源码")).Checked(d != nil && d.source).Disabled(noText).Chosen() {
			a.toggleSource()
		}
		m.Submenu(i18n.T("页面宽度"), a.pageWidthMenu).Disabled(!canAdjustPageWidth(d))
		m.Submenu(i18n.T("页面缩放"), a.pageZoomMenu).Disabled(!a.canZoomPage(d))
		if m.Item(i18n.T("导出 MDZip (.mdz)")).Disabled(noDoc || d.isPDF() || d.isMDZ() || d.isDatabase() || d.isWhiteboard()).Chosen() {
			a.exportMDZ()
		}
		if m.Item(i18n.T("清理不存在的文件")).Chosen() {
			a.cleanMissingFileTabs()
		}
		if m.Item(i18n.T("目录")).Checked(a.outlineOpen()).Disabled(noDoc || d.source).Chosen() {
			a.toggleOutline()
		}
		if m.Item(i18n.T("导出 PDF")).Disabled(noText).Chosen() {
			a.exportPDF()
		}
		m.Separator()
		if m.Item(i18n.T("复制文件路径")).Disabled(d == nil).Chosen() {
			c.WriteClipboard(d.fullPath())
		}
		if m.Item(i18n.T("在文件管理器中显示")).Disabled(d == nil).Chosen() {
			a.revealIn(f.locations, d.rel)
		}
		m.Separator()
		if m.Item(i18n.T("文件浏览设置")).Chosen() {
			a.openSettings(tabFiles)
		}
	})
}

// fileEmpty is the browser kept open after its last tab closed.
func (a *App) fileEmpty(c *ui.Context) {
	t := c.Theme()
	area := ui.Column(c.Key("file-empty")).Grow(1).MinHeight(0).Center().Gap(12)
	for _, file := range area.DroppedFiles() {
		a.openSystemFile(file)
	}
	area.Children(func() {
		ui.Image(c, brandLogo()).Size(44, 44)
		ui.Text(c, i18n.T("没有打开的文件")).FontSize(17).FontWeight(600)
		ui.Text(c, i18n.T("打开或拖入 Markdown 文件来查看和编辑")).FontSize(13).TextColor(t.TextMuted)
		if ui.PrimaryButton(c.Key("empty-open"), i18n.T("打开文件")).Margin(6, 0, 0, 0).Clicked() {
			a.chooseExternalFiles()
		}
	})
}

// cleanMissingFileTabs drops only stale references. Dirty editors remain open
// so the user can recover their text with Save As.
func (a *App) cleanMissingFileTabs() {
	f := a.files
	if f == nil {
		return
	}
	removed, skipped := 0, 0
	for _, rel := range slices.Clone(f.order) {
		if !vault.Files(f.locations.ExternalFiles).Missing(rel) {
			continue
		}
		if d := a.residentDocument(f.locations.ID, rel); d != nil {
			d.pollAsyncSave()
			if d.saving || d.dirty() {
				skipped++
				continue
			}
			a.replaceReferences(d, nil)
		}
		delete(f.locations.ExternalFiles, rel)
		delete(f.readOnly, rel)
		f.order = slices.DeleteFunc(f.order, func(name string) bool { return name == rel })
		removed++
	}
	if a.doc == nil && len(f.order) > 0 {
		a.showFileTab(f.order[0])
	}
	a.notify("已清理 %d 个失效引用，跳过 %d 个未保存文件", removed, skipped)
}
