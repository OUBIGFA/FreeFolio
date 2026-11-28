package app

import (
	"image"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/database"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/editor/dbview"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// narrowWidth is the window width below which the sidebar folds away by
// itself, as Arc's does, and comes back when the window widens.
const narrowWidth = 900

// narrowTick notes whether the window is narrow; crossing the width
// forgets the sidebar shown while narrow.
func (a *App) narrowTick(w float32) {
	if narrow := w < narrowWidth; narrow != a.narrow {
		a.narrow, a.narrowShown = narrow, false
	}
}

// folded reports whether the sidebar is folded away: by ⌘\, or by a
// narrow window unless shown there.
func (a *App) folded() bool {
	return a.sidebarHidden || a.narrow && !a.narrowShown
}

func (a *App) toggleSidebar() {
	a.finishSpaceEdit()
	if a.narrow {
		if a.folded() {
			a.sidebarHidden, a.narrowShown = false, true
		} else {
			a.narrowShown = false
		}
	} else {
		a.sidebarHidden = !a.sidebarHidden
	}
	a.peek = false
	a.peekLeave = time.Time{}
}

// toggleFocus enters or leaves focus mode, as Zen's: the document alone,
// nothing of the window around it, the other paragraphs faded.
func (a *App) toggleFocus() {
	a.finishSpaceEdit()
	a.focus = !a.focus
	if a.doc != nil {
		if a.doc.isPDF() {
			a.doc.pdf.RequestFocus = true
		} else {
			a.doc.ed.RequestFocus = true
		}
	}
}

// toggleTypewriter keeps the line being written in the middle of the page,
// or lets it go.
func (a *App) toggleTypewriter() {
	a.prefs.Typewriter = !a.prefs.Typewriter
	a.savePrefs()
}

// setOutline opens or collapses the outline right of the page, in every
// document.
func (a *App) setOutline(open bool) {
	a.prefs.OutlineClosed = !open
	a.savePrefs()
}

// toggleOutline opens or collapses the outline of the document in front.
func (a *App) toggleOutline() {
	if a.doc.isPDF() {
		// A PDF's outline is its sidebar of thumbnails and contents.
		a.doc.pdf.ToggleOutline()
		return
	}
	if a.doc.isMDZ() {
		m := a.doc.mdz
		if m.hasActivePage() {
			if len(m.pages) > 1 && m.sidebarOpen && !a.focus {
				m.sidebarOpen = false
				m.pendingOutline = true
				a.setOutline(true)
				return
			}
			if ed := m.activeEditor(); ed != nil {
				ed.ToggleOutline()
			}
		}
		return
	}
	if a.doc != nil && a.doc.loadErr == nil && !a.doc.source && !a.doc.isDatabase() {
		a.doc.ed.ToggleOutline()
	}
}

// outlineOpen reports whether the document in front shows its outline.
func (a *App) outlineOpen() bool {
	if a.doc.isPDF() {
		return a.doc.pdf.SidebarOpen
	}
	if a.doc != nil && a.doc.isMDZ() {
		if ed := a.doc.mdz.activeEditor(); ed != nil {
			return ed.OutlineOpen()
		}
		return false
	}
	return a.doc != nil && a.doc.ed.OutlineOpen()
}

// toggleDim fades the paragraphs but the caret's in focus mode, or not.
func (a *App) toggleDim() {
	a.prefs.NoDim = !a.prefs.NoDim
	a.savePrefs()
}

// focusView is focus mode: the card over the whole window, a strip at its
// top to drag the window by and to leave.
func (a *App) focusView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c.Key("focus")).Grow(1).MinWidth(0).Padding(0, 12, 12, 12).Children(func() {
		bar := a.titleBar(c)
		top := ui.Row(c.Key("focus-bar")).Height(max(bar.Height, 36)).Padding(0, bar.Right, 0, bar.Left).AlignItems(ui.Center).DragWindow()
		_, py, inside := c.Root().PointerPosition()
		show := inside && py < 60
		top.Children(func() {
			ui.Spacer(c)
			if a.doc != nil {
				words := ui.Text(c, a.doc.title()).FontSize(12).TextColor(t.TextMuted).SingleLine()
				words.Opacity(words.Animate("shown", b2f(show), 150*time.Millisecond))
			}
			ui.Spacer(c)
			if a.zoom.showPageZoomButton(a.doc) {
				zb := a.zoom.pageZoomButton(c, a.doc)
				zb.Opacity(zb.Animate("shown", b2f(show || zb.Focused()), 150*time.Millisecond))
			}
			exit := iconButton(c, "focus-exit", iconMinimize, i18n.T("退出专注模式"), a.withKey(i18n.T("退出专注模式"), "focus"), false)
			exit.Opacity(exit.Animate("shown", b2f(show || exit.Focused()), 150*time.Millisecond))
			if exit.Clicked() {
				a.toggleFocus()
			}
		})
		card := ui.Themed(c.Key("card"), documentTheme(a.paperDark)).Grow(1).MinHeight(0).Radius(10).
			Clip()
		card.Children(func() {
			if a.doc == nil || a.doc.loadErr != nil {
				ui.Column(c).Grow(1).Center().Gap(16).Children(func() {
					ui.Text(c, i18n.T("专注模式下没有可编辑的文档")).TextColor(c.Theme().TextMuted)
					if ui.Button(c.Key("focus-leave"), i18n.T("退出专注模式")).Clicked() {
						a.toggleFocus()
					}
				})
				return
			}
			if a.doc.source {
				w, _ := c.Size()
				a.sourceMode.sourceView(c, a.doc, w-18)
				return
			}
			w, _ := c.Size()
			ui.Column(c.Key("page")).Grow(1).MinHeight(0).Children(func() {
				a.conflictBar(c)
				if a.doc.isMDZ() {
					a.mdzFiles.mdzView(c, a.doc, w-18, true)
				} else if a.doc.isDatabase() {
					a.databases.databaseWorkspace(c, a.doc, w-18)
				} else {
					editor.View(c.Key(a.doc.viewKey()), a.doc.ed, a.editorOptions(c, w-18, true))
				}
			})
		})
	})
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// editorOptions are the open document's editor's, width wide; focus mode
// fades the other paragraphs.
func (a *App) editorOptions(c *ui.Context, width float32, focus bool) editor.Options {
	return a.editorOptionsFor(c, width, focus, a.doc)
}

func (a *App) editorOptionsFor(c *ui.Context, width float32, focus bool, d *document) editor.Options {
	run := func(fn func()) {
		switch {
		case a.activateDocument(d):
			fn()
		case a.databases.dbPeek.open && a.databases.dbPeek.doc == d:
			a.withDocument(d, fn)
		case a.inWindow(d):
			// A document in its own window acts as the document.
			a.withDocument(d, fn)
		}
	}
	hasDB := false
	if d != nil {
		store, _, _, _ := a.databases.owningDatabaseFor(d)
		hasDB = store != nil
	}
	options := editor.Options{
		Zoom:            a.zoom.pageZoom(),
		OnZoom:          a.zoom.setPageZoom,
		HideFrontmatter: hasDB,
		LoadDatabase: func(target string) (*database.Store, dbview.Options, uint64, error) {
			return a.databases.loadDatabaseFor(d, target)
		},
		OpenDatabase: func(target, viewID string, split bool) { a.databases.openDatabaseFrom(d, target, viewID, split) },
		ListDatabases: func() []database.SpaceDBInfo {
			if d.space == nil || d.space.IsExternal() || d.space.Root == "" {
				return nil
			}
			dbs := a.databases.databaseCatalogFor(d.space)
			for i := range dbs {
				if res := a.residentDocument(d.space.ID, dbs[i].RelPath); res != nil && res.loadErr == nil && res.isDatabase() {
					liveDB := res.dbView.Store.Database()
					if liveDB != nil {
						dbs[i].Title = liveDB.Title
						dbs[i].Icon = liveDB.Icon
						views := make([]database.SpaceDBViewInfo, 0, len(liveDB.Views))
						for _, v := range liveDB.Views {
							views = append(views, database.SpaceDBViewInfo{
								ID:   v.ID,
								Name: v.Name,
								Type: v.Type,
							})
						}
						dbs[i].Views = views
					}
				}
			}
			return dbs
		},
		CreateDatabase: func(title string) (string, string, string, error) {
			return a.databases.createEmbeddedDatabaseFor(d, title)
		},
		DatabaseSnapshot: func(target, viewID string) string { return a.databases.databaseSnapshotFor(d, target, viewID) },

		CreateWhiteboard: func() { a.insertWhiteboard(d) },
		OpenWhiteboard:   func(url string) { a.openBoardEmbed(d, url) },
		BoardOptions:     a.boardOptions(c, d),
		EditWithAI:       func(instruction string) { run(func() { a.qa.editSelectionWithOneBot(instruction) }) },
		AddToAI:          func() { run(func() { a.qa.addSelectionToOneBot(d) }) },
		FontSize:         a.prefs.ReadingSize(),
		LineHeight:       a.prefs.ReadingLine(),
		FontFamily:       a.fonts.bodyFont(),
		Column:           a.prefs.Column(),
		NoAutoPair:       a.prefs.NoAutoPair,
		AutoSpace:        a.prefs.AutoSpace,
		CodeNoWrap:       a.prefs.CodeNoWrap,
		NoWideTables:     a.prefs.NoWideTables,
		EmojiSuggest:     suggestEmoji,
		Resizing:         a.splitResizing || a.qa.resizing,
		Width:            width,
		Theme:            c.Theme(),
		Page:             pageStyle(a.paperDark),
		Attach:           func() { run(a.pickImages) },
		DropFiles:        func(paths []string, at int) { run(func() { a.editorFileDrop(d, paths, at) }) },
		PasteImage:       func(data []byte) { run(func() { a.pasteImage(data) }) },
		LoadImage:        func(url string) editor.ImageResource { return a.imageResourceFor(d, url) },
		OpenVideo:        openVideo,
		ReplaceImage:     func(id, url string) { a.pickReplacementImage(d, id, url) },
		RevealImage:      func(url string) { a.revealImage(d, url) },
		CropImage:        func(id, url string, r image.Rectangle) { a.cropImage(d, id, url, r) },
		OpenLink:         func(url string) { a.navigateInPane(d, func() { a.followLink(url) }) },
		OpenWiki:         func(name string) { a.navigateInPane(d, func() { a.links.openWiki(name) }) },
		OpenTag:          func(tag string) { run(func() { a.links.openTag(tag) }) },
		PreviewWiki:      func(name string) editor.WikiPreview { return a.links.previewWikiFor(d, name) },
		WikiSuggest:      func(typed string) []editor.WikiItem { return a.links.wikiSuggestFor(d, typed) },
		Footer:           func(c *ui.Context) { a.links.pageFooterFor(c, d) },
		Typewriter:       a.prefs.Typewriter,
		DrawAnnotations:  a.prefs.DrawAnnotations,
		Dim:              focus && !a.prefs.NoDim,
		PickCover:        func(done func(string)) { run(func() { a.pickCover(done) }) },
		Bindings:         a.editorBindings(),
		Outline:          !a.prefs.OutlineClosed,
		SetOutline:       a.setOutline,
		Bookmarks:        a.bookmarks.marksOf(d),
		ToggleBookmark: func(id string) {
			a.bookmarks.toggleBookmark(d, id)
		},
		LabelBookmark: func(id, label string) { a.bookmarks.labelBookmark(d.space.ID, d.rel, id, label) },
		BookmarkKey:   a.cachedBookmarkShortKey(),
		Reserved:      a.bookmarks.reservedKeys(),
	}
	if a.files != nil && d.space == a.files.locations {
		// Space search, backlinks and tags have no meaning for a loose file.
		options.Footer = nil
		options.OpenWiki = nil
		options.PreviewWiki = nil
		options.WikiSuggest = nil
		options.OpenTag = nil
	}
	if d != nil {
		options.DocTitle = d.title()
	}
	return options
}
