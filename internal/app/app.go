// Package app is FreeFolio's interface: one native window, drawn by MyGo's
// package ui, in the manner of Arc and Zen — a sidebar of the active
// space's pinned documents and folders over the window's tinted material,
// a toolbar with the document's address, the document in a rounded card,
// a command bar, and the spaces at the sidebar's foot.
//
// The package holds state and draws it; files, spaces, plugins and the
// Markdown editing engine live in their own packages.
package app

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/cloudsync"
	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/formats"
	"github.com/OUBIGFA/FreeFolio/internal/history"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/plugin"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/search"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/trashbin"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
)

// Version is FreeFolio's version, which plugins' engines are checked against.
const Version = "0.1.1"

// Config says where FreeFolio keeps its files.
type Config struct {
	// ConfigDir holds spaces.json and the plugins folder.
	ConfigDir string
	// DocumentsDir is where the folders of new spaces go by default.
	DocumentsDir string
	// DeferStartup postpones non-essential startup I/O until after the
	// first window frame so cold starts paint immediately.
	DeferStartup bool
	// InstanceLocked reports that the caller already claimed MyGo's
	// single-instance lock before calling New.
	InstanceLocked bool
}

// DefaultConfig returns the user's usual places: the system's
// configuration folder and ~/Documents/FreeFolio.
func DefaultConfig() (Config, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return Config{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	return Config{
		ConfigDir:    filepath.Join(cfg, "FreeFolio"),
		DocumentsDir: filepath.Join(home, "Documents", "FreeFolio"),
	}, nil
}

// place is a document of a space, a step of the navigation history.
type place struct{ space, rel string }

// spaceSession keeps each space's last editor and sidebar scroll position
// while the window is open. The Markdown file remains authoritative.
type spaceSession struct {
	// doc is the document the space showed when it was left: its own, or
	// a favorite of another space, which it shows again on coming back.
	doc           *document
	tree          ui.ScrollState
	split         *splitGroup
	splitShown    bool
	splitRestored bool
}

type startupDocPrefetch struct {
	spaceID, rel, text string
	err                error
}

// App is FreeFolio's state.
type App struct {
	boardImages  []*boardImageWindow
	imports      noteImportState
	quick        quickNoteState
	cleanup      cleanupState
	fonts        fontState
	boardLibrary *boardLibraryState
	cloud        cloudState
	// Sidebar selection belongs to one space and retains a range anchor.
	picks      map[string]bool
	picksSpace string
	pickAnchor string

	// Image locations and attachment scans are scoped to resident documents/spaces.
	imgResolved map[imageKey]resolvedImage
	// frame counts the frames built, and pictureNames are a space's files
	// as the frame's pictures found them (findAttachment).
	frame             uint64
	pictureNames      frameNames
	selfWrites        map[string]time.Time
	attachmentRelinks map[string]*attachmentRelink
	movingDoc         *document

	// bookmarkGen counts changes to the bookmarks, which documents resolve
	// again (bookmarks.go).
	bookmarkGen uint64

	cfg Config
	win *mygo.Window
	// testCaptionWindow stands in for the main window's client controls in
	// tests (windowbuttons.go).
	testCaptionWindow captionWindow
	// tray is the notification area's icon while closing the window hides
	// it there; quitting is set from the start of a quit until it is
	// canceled, so that the window then closes for good.
	tray     *mygo.Tray
	quitting bool
	// remote fetches and keeps the documents' pictures from the web.
	remote *remoteImages
	// memTrim returns decoding's freed memory to the system (memtrim.go).
	memTrim memTrim
	// spaceIcons are the pictures chosen as space icons, decoded, by name.
	spaceIcons map[string]*ui.Bitmap
	// closeMain closes the main window, as the file browser does when its
	// last tab closes; nil in tests, which leaves the empty browser.
	closeMain func()
	store     *space.Store
	state     *space.State
	plugins   *plugin.Manager

	installed     []*plugin.Installed
	brokenPlugins []plugin.Broken
	// converters run the enabled format plugins; pluginFormats keys the
	// formats they contribute, to refresh the spaces when it changes
	// (formats.go).
	converters    *plugin.Converters
	pluginFormats string
	// pdfPages is the window editing a PDF's pages (pdfpages.go).
	pdfPages pdfPagesState

	// tree and docs are the folders and documents of treeFor, the space
	// they were read for; docs lists the documents in tree order.
	tree     []*vault.Node
	docs     []*vault.Node
	docPaths map[string]bool
	treeFor  string
	treeErr  error
	// treeLoading is set while the active space's folder is read off the
	// main thread; treeGen tells a read still wanted from one overtaken by
	// a later read or a switch. readTreesAsync reads folders so, as the
	// running app does; tests read them at once.
	treeLoading    bool
	treeGen        uint64
	readTreesAsync bool

	files *fileSession
	doc   *document
	// split owns the visible panes; doc and history follow its focused pane.
	split           *splitGroup
	paneNavigation  bool
	documentContext int
	splitResizing   bool
	// sidebarResizing is set while the sidebar's edge is dragged, from
	// the width sidebarDrag the pointer has taken it to; winW is the
	// window's width as the frame began.
	sidebarResizing bool
	sidebarDrag     float32
	winW            float32
	sessions        map[string]*spaceSession
	// openDocs are the pinned documents kept open behind the one shown,
	// and bounce the row springing from a close (opendocs.go).
	openDocs map[place]*document
	bounce   bounce
	// overClose is the row whose minus is under the pointer this frame,
	// overClosed the one the frame before; it shows no preview.
	overClose, overClosed place
	spaceScroll           ui.ScrollState
	spaceBarFor           string
	// history is the documents shown, as a browser's; at is the one
	// shown now.
	history []place
	at      int

	// sidebarHidden folds the sidebar away with ⌘\; it peeks out
	// over the content while the pointer is at the window's edge.
	sidebarHidden bool
	peek          bool
	peekLeave     time.Time
	// renaming is the path of the file or folder whose name is being
	// edited in the sidebar, and newName the name typed.
	renaming      string
	newName       string
	renameFocused bool
	// renameSpace is the ID of the space holding the item being renamed.
	renameSpace string

	palette palette
	// qa is the panel of questions about the space (qa.go).
	qa        qaState
	resources resourceState
	chatgpt   chatGPTState
	// pager is the strip of spaces the sidebar pages along (spaceswitch.go),
	// and pageTrees the folders of the spaces it shows beside the active
	// one; now and reduceMotion are this frame's clock and the system's
	// wish for less motion.
	pager        pager
	pageTrees    map[string]pageTree
	now          time.Time
	reduceMotion bool
	// fileDrop is where files dragged in from the system would land
	// (filedrop.go).
	fileDrop fileDropState
	// pageWidthBeforeFull is the page width full width returns to
	// (pagewidth.go).
	pageWidthBeforeFull string
	// listDrop is where the file or folder dragged over the sidebar's list
	// would land (treedrag.go), and favDrop the slot the favorites hold
	// open for one dragged over them (favorites.go).
	listDrop   *listDrop
	springOpen springOpen
	// spaceSpring is the space icon a drag rests on (crossspace.go);
	// carrying is the row seen dragged this frame and carried the one the
	// frame before, which outlive the row's element when its space pages
	// away.
	spaceSpring spaceSpring
	carrying    *dragged
	carried     *dragged
	// listDragging is whether something is dragged over the list, which
	// opens the empty pinned section's placeholder; dividerY is where the
	// line between the pinned and the rest was.
	listDragging bool
	dividerY     float32
	rows         []rowFrame
	rowsBefore   []rowFrame
	// rowWin is the window of the section of the list being built, and
	// spaced the rows it left out (rowwindow.go); rowFocus is the focused
	// row's place in the frame before, rowFocusNow in this one. rowsAll
	// builds every row: tests compare against it.
	rowWin      *rowWindow
	spaced      map[string]spacedRow
	rowFocus    rowFocus
	rowFocusNow rowFocus
	rowsAll     bool
	slot        slotState
	dragGrab    dragGrab
	dragSettle  dragSettle
	newDocPulse time.Time
	favDrop     *favDrop
	// preview is the card a folder or a document shows when the pointer
	// rests on it (preview.go).
	preview preview
	// overview shows every space at once, as Zen's.
	library   library
	spaceForm spaceForm
	// settingsOpen shows the settings window on settingsTab.
	settingsOpen bool
	settingsTab  string
	sampleTitle  string
	sampleEditor *editor.Editor
	// look is the window's look this frame, the two spaces either side
	// mixed while paging; docTheme the system's theme the document's card
	// keeps. paperDark is the active space's light or dark, which the
	// paper takes: whole, as the document shown is the active space's,
	// rather than flipping halfway through every space paged past.
	look      spaceLook
	docTheme  *ui.Theme
	paperDark bool
	// pickFiles, when set, asks for pictures in place of the system's
	// dialog: tests answer it.
	pickFiles func(done func(paths []string))
	// revealFile substitutes for the platform file manager in image tests.
	revealFile func(path string)
	// notices are messages for the user, shown as toasts by the next frame.
	notices []string
	// places are where loose files were read to (fileplaces.go), read
	// the first time they are needed.
	places *filePlaces
	// undos are toasts that offer to undo a file operation (undo.go).
	undos []ui.Toast
	// replace is the find and replace across documents (replace.go).
	replace replaceState
	unsaved unsavedPrompt
	// tagRename is the window renaming a #tag everywhere (tags.go).
	tagRename tagRename
	// systemLanguage is the language the system prefers of those the
	// interface is written in.
	systemLanguage string
	// windows are the documents shown in windows of their own
	// (windows.go); newWindow, when set, makes them in tests.
	windows   []*docWindow
	newWindow func(title string, view func(*ui.Context), close func() bool) windowHandle
	// watchFiles follows a space's folder (watch.go), and watches are the
	// folders followed, by space ID.
	watchFiles watchFunc
	watches    map[string]folderWatch
	// search is the command bar's full-text search (search.go).
	search searchState
	// prefs are the preferences beyond the spaces (prefs.json); prefsBroken
	// keeps a file this FreeFolio cannot read from being overwritten.
	prefStore   prefs.Store
	prefs       *prefs.Prefs
	prefsBroken bool
	// recording is the command whose new key the settings wait for.
	recording   string
	recordFocus bool
	// focus is focus mode; narrow whether the window is narrow enough to
	// fold the sidebar away, narrowShown whether it is shown anyway
	// (focus.go).
	focus, narrow, narrowShown bool
	// versions is the history window (versions.go); bin keeps what was
	// deleted recently, which the library lists (trash.go).
	versions versionsState
	bin      trashbin.Bin
	// printFile, when set, takes the PDF to print in place of the system's
	// viewer: tests answer it.
	pdfExport *pdfExport
	printFile func(path string)
	// trashFile, when set, moves a file to the trash in place of the
	// system: tests answer it.
	trashFile func(path string) error
	// updates are the software updates (update.go).
	updates updateState
	// tour introduces the sidebar on the first run (tour.go).
	tour tour
	// launch is what waits for the window's first frame (launch.go).
	launch          launch
	startupPrefetch *startupDocPrefetch

	runAsync             func(func())
	navDir               int
	navAt                time.Time
	keyGen               uint64
	keysCacheGen         uint64
	keysCachePrefs       *prefs.Prefs
	keysCacheLen         int
	keysCacheValid       bool
	cachedEditorBindings map[string]editor.Combo
	cachedReserved       []editor.Combo
	cachedBookmarkKey    string
}

// trashItem moves the file or folder at p to the system's trash.
func (a *App) trashItem(p string) error {
	if a.trashFile != nil {
		return a.trashFile(p)
	}
	return mygo.Shell.TrashItem(p)
}

// New loads FreeFolio's state, making the first space and its welcome
// document on the first run.
func New(cfg Config) (*App, error) {
	a := &App{
		cfg:        cfg,
		store:      space.NewStore(filepath.Join(cfg.ConfigDir, "spaces.json")),
		plugins:    plugin.NewManager(filepath.Join(cfg.ConfigDir, "plugins")),
		converters: plugin.NewConverters(plugin.NewWASMHost()),
		sessions:   make(map[string]*spaceSession),
		openDocs:   make(map[place]*document),
		prefStore:  prefs.Store{Path: filepath.Join(cfg.ConfigDir, "prefs.json")},
		remote:     newRemoteImages(filepath.Join(cfg.ConfigDir, "cache", "images")),
		bin:        trashbin.Bin{Dir: filepath.Join(cfg.ConfigDir, "trash")},
		qa:         qaState{secrets: prefs.Secrets{Dir: filepath.Join(cfg.ConfigDir, "credentials")}},
		// The spaces fade in on coming back from the library, not at start.
		library: library{frames: 1},
	}
	var (
		loadedPrefs  *prefs.Prefs
		prefsErr     error
		loadedSpaces *space.State
		spacesErr    error
		loadedCloud  cloudsync.Config
		cloudErr     error
		wg           sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		loadedPrefs, prefsErr = a.prefStore.Load()
	}()
	go func() {
		defer wg.Done()
		loadedSpaces, spacesErr = a.store.Load()
	}()
	go func() {
		defer wg.Done()
		loadedCloud, cloudErr = cloudsync.LoadConfig(cfg.ConfigDir)
	}()
	wg.Wait()
	if prefsErr == nil {
		a.prefs = loadedPrefs
	} else {
		a.prefs, a.prefsBroken = &prefs.Prefs{Version: prefs.CurrentVersion}, true
		a.notify("读取偏好设置失败，本次使用默认设置且不会覆盖原文件；%v", prefsErr)
	}
	a.systemLanguage = "zh-Hans"
	a.applyLanguage()
	a.applyLoadedCloudSync(loadedCloud, cloudErr)
	if spacesErr != nil {
		return nil, spacesErr
	}
	st := loadedSpaces
	a.state = st
	// Older installations kept loose paths in a built-in collection. Drop
	// its references only; the referenced files always belong to the user.
	if st.DetachExternal() && len(st.Spaces) > 0 {
		if err := a.store.Save(st); err != nil {
			return nil, err
		}
	}
	first := len(st.Spaces) == 0
	a.imports.offer = first
	if first {
		if err := a.firstRun(); err != nil {
			return nil, err
		}
	}
	a.startUpdates()
	if cfg.DeferStartup {
		a.launch.migrateSpaces = true
		a.launch.loadPlugins = true
		a.launch.deferSearch = true
	} else {
		// Spaces made before the rebrand keep their versions and templates in
		// the legacy system folder; it moves once, before anything reads it.
		for _, sp := range st.Spaces {
			if err := vault.MigrateSystemDir(sp.Root); err != nil {
				a.notify("迁移空间 %s 的系统文件夹失败；%v", sp.Name, err)
			}
		}
		a.reloadPlugins()
		if p := a.searchCachePath(); p != "" {
			if cached, err := search.LoadCache(p); err == nil {
				a.search.index = cached
			}
			a.search.cacheLoaded = true
		}
	}
	var prefetchCh chan startupDocPrefetch
	if sp := a.state.ActiveSpace(); sp != nil && sp.LastOpen != "" && (first || !a.prefs.StartAtHome) && (sp.Split == nil || !sp.Split.Shown) {
		lastRel := sp.LastOpen
		spCopy := *sp
		prefetchCh = make(chan startupDocPrefetch, 1)
		go func() {
			text, readErr := readFile(&spCopy, lastRel)
			prefetchCh <- startupDocPrefetch{spaceID: spCopy.ID, rel: lastRel, text: text, err: readErr}
		}()
	}
	a.syncTree()
	a.restoreSplits()
	if prefetchCh != nil {
		p := <-prefetchCh
		a.startupPrefetch = &p
	}
	// A launch picks up reading where it stopped: the active space's last
	// document, scrolled to where it was read to, unless the preference
	// starts at the space's home page. The first run opens its welcome page.
	if sp := a.state.ActiveSpace(); sp != nil && sp.LastOpen != "" && (first || !a.prefs.StartAtHome) {
		at := sp.LastAt
		if a.resumeSpace(); a.split == nil && a.doc != nil && a.doc.space == sp && a.doc.rel == sp.LastOpen {
			a.doc.ed.ScrollToReadingBlock(at)
		}
	} else {
		a.showSpaceHome()
	}
	a.startupPrefetch = nil
	// The tour shows until it was seen once. Preferences that cannot be
	// saved could not remember that, and would show it at every launch.
	if !a.prefs.TourSeen && !a.prefsBroken {
		a.startTour()
	}
	formats.SetPluginConverter(a.convertWithPlugin)
	return a, nil
}

// firstRun makes the Personal space with a welcome document.
func (a *App) firstRun() error {
	root := filepath.Join(a.cfg.DocumentsDir, "Personal")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	sp := a.state.Add("Personal", root)
	if docs, _ := vault.List(root); len(docs) == 0 {
		if err := vault.Write(root, "Welcome.md", welcome); err != nil {
			return err
		}
		if err := vault.Write(root, "Journal/Ideas.md", ideas); err != nil {
			return err
		}
		sp.LastOpen = "Welcome.md"
		a.state.ToggleFavorite(sp.ID, "Welcome.md")
		sp.Expanded = []string{"Journal"}
	}
	return a.store.Save(a.state)
}

// showSpaceHome leaves the file browser and the open document for the
// active space's home page, its empty state, saving first; it reports
// false, staying put, when a document cannot be saved. The space keeps
// its last document and split for when the user goes back to them.
func (a *App) showSpaceHome() bool {
	if !a.leaveFileMode() || !a.leaveDocs(a.leavingCurrentDoc(), func() { a.showSpaceHome() }) {
		return false
	}
	a.finishSpaceEdit()
	a.settingsOpen = false
	if a.library.open {
		a.library.open, a.library.frames = false, 0
	}
	a.suspendSplit()
	// The document left behind closes unless something still holds it (a
	// pin, a split, another window), so its editor and pixels do not linger.
	old := a.doc
	a.doc = nil
	a.retireClosedDocuments(old)
	return true
}

// closeShownDoc closes the shown document for the space's home page, as a
// file tab closes: a pinned one closes and stays pinned, and its space
// forgets it as the last document, so coming back shows the home page too.
// In a split it closes the active pane instead, which has its own document.
func (a *App) closeShownDoc() {
	d := a.doc
	if d == nil || a.files != nil {
		return
	}
	if a.split != nil {
		a.closePane(a.split.active)
		return
	}
	if !a.leaveDocs([]*document{d}, a.closeShownDoc) {
		return
	}
	sp, p := d.space, place{d.space.ID, d.rel}
	if a.openDocs[p] != nil {
		delete(a.openDocs, p)
		a.bounce = bounce{p, a.now}
	}
	if !a.showSpaceHome() {
		return
	}
	if sp.LastOpen == d.rel {
		sp.LastOpen, sp.LastAt = "", 0
		a.saveState()
	}
}

// Run opens the window and runs the app until it quits.
func (a *App) Run() error {
	if !a.cfg.InstanceLocked {
		mygo.App.SetName("FreeFolio")
		mygo.App.SetPath(mygo.PathUserData, a.cfg.ConfigDir)
		if !mygo.App.RequestSingleInstanceLock() {
			return nil
		}
	} else {
		mygo.App.SetName("FreeFolio")
		mygo.App.SetPath(mygo.PathUserData, a.cfg.ConfigDir)
	}
	// An installed app shares one taskbar button with its pinned
	// shortcuts; a development run keeps the shell's own ID, apart.
	if mygo.App.IsPackaged() {
		a.applyAppUserModelID()
	}
	var pending []string
	if !mygo.App.IsPackaged() {
		wd, _ := os.Getwd()
		pending = documentArguments(os.Args[1:], wd)
	}
	open := func(file string) {
		if a.openSystemFile(file) {
			a.win.Show()
			a.win.Focus()
			if a.files == nil {
				_ = mygo.App.AddRecentDocument(file)
			}
		}
	}
	mygo.App.OnOpenFile(func(file string) {
		if a.win == nil {
			pending = append(pending, file)
			return
		}
		a.win.Update(func() { open(file) })
	})
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		files := documentArguments(args, workingDir)
		if !mygo.App.IsPackaged() {
			// MyGo forwards os.Args[1:], so the first argument is a file.
			for _, file := range files {
				if a.win == nil {
					pending = append(pending, file)
				} else {
					a.win.Update(func() { open(file) })
				}
			}
		}
		if a.win != nil {
			// Started again without a document, as from the pinned
			// taskbar button, the window comes back as it was left, or
			// at the space's home page if the preference says so.
			if len(files) == 0 {
				a.win.Update(a.prepareWindowReopen)
			}
			a.win.Show()
			a.win.Focus()
		}
	})
	mygo.App.SetLanguages(i18n.Languages...)
	a.systemLanguage = i18n.Pick(mygo.App.PreferredLanguages())
	a.applyLanguage()
	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			a.appMenu(),
			{Role: mygo.RoleFileMenu},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleFormatMenu},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindow, Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleMinimize},
				{Role: mygo.RoleZoom},
				mygo.Separator(),
				{Label: i18n.T("下一个空间"), Accelerator: "CmdOrCtrl+Alt+Right", Click: func(*mygo.MenuItem, *mygo.Window) {
					a.win.Update(func() { a.stepSpace(1) })
				}},
				{Label: i18n.T("上一个空间"), Accelerator: "CmdOrCtrl+Alt+Left", Click: func(*mygo.MenuItem, *mygo.Window) {
					a.win.Update(func() { a.stepSpace(-1) })
				}},
				mygo.Separator(),
				{Role: mygo.RoleFront},
			}},
		}))
		a.watchFiles = a.watchFolder
		a.win = newAppWindow(mygo.WindowOptions{
			Title:                "FreeFolio",
			Width:                1240,
			Height:               800,
			MinWidth:             640,
			MinHeight:            420,
			StateKey:             "main",
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 18, Y: 20},
			Vibrancy:             mygo.VibrancySidebar,
		}, a.View)
		main := a.win
		// Called off the main thread, Close is posted to it: the frame that
		// asked for it finishes before the window closes and OnClose saves.
		a.closeMain = func() { go main.Close() }
		// The window's icon and the tray icon follow the first frame.
		a.launch.pending = true
		if len(pending) > 0 {
			files := pending
			pending = nil
			a.win.Update(func() {
				for _, file := range files {
					open(file)
				}
			})
		}
		// Index the documents after the first frame when DeferStartup is enabled,
		// otherwise start right away.
		a.search.async = true
		a.readTreesAsync = true
		if a.cfg.DeferStartup {
			a.launch.deferSearch = true
		} else {
			a.refreshSearch()
		}
		a.updates.svc, a.updates.async = newUpdateService(a.cfg.ConfigDir), true
		a.ensureChatGPT()
		a.chatgpt.openBrowser = mygo.Shell.OpenExternal
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
		mygo.App.OnQuitCanceled(func() { a.quitting = false })
		a.win.OnClose(func(event *mygo.CloseEvent) {
			a.finishLaunch()
			// Hidden in the tray, the window keeps its documents in memory:
			// only quitting asks about edits autosave did not write.
			hides := !a.quitting && a.tray != nil
			if a.autosaveEnabled() || !hides {
				if !a.leaveDocs(a.residentDocuments(), a.retryCloseMain) {
					// The window stays: an empty file browser shows again.
					if a.files != nil {
						a.files.closing = false
					}
					event.PreventDefault()
					return
				}
			}
			a.finishSpaceEdit()
			a.noteFilePlace(a.doc)
			if a.captureSplits() || a.noteReading() {
				a.saveState()
			}
			if !a.quitting && a.tray != nil {
				// Saved as on closing, the window only hides: the tray
				// brings it back, and a running PDF export goes on.
				event.PreventDefault()
				a.win.Hide()
				return
			}
			a.cancelPDF()
			a.cancelCloudSync()
			a.cancelChatGPTSignIn()
		})
	})
	return mygo.App.Run()
}

// syncTray shows the notification area's icon while closing the window
// keeps FreeFolio running, and removes it otherwise.
func (a *App) syncTray() {
	if a.prefs.KeepRunningOff {
		if a.tray != nil {
			a.tray.Destroy()
			a.tray = nil
		}
		return
	}
	if a.tray != nil || a.win == nil {
		return
	}
	// The tray icon brings the window back as it was left, or at the
	// space's home page, as a launch does.
	show := func() {
		a.win.Update(a.prepareWindowReopen)
		if a.win.IsMinimized() {
			a.win.Restore()
		}
		a.win.Show()
		a.win.Focus()
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "freefolio",
		Icon:    trayIcon(),
		ToolTip: "FreeFolio",
		Menu: mygo.NewMenu([]*mygo.MenuItem{
			{Label: i18n.T("打开 FreeFolio"), Click: func(*mygo.MenuItem, *mygo.Window) { show() }},
			{Label: i18n.T("快速笔记"), Click: func(*mygo.MenuItem, *mygo.Window) { a.win.Update(a.openQuickNote) }},
			mygo.Separator(),
			{Label: i18n.T("退出 FreeFolio"), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		}),
	})
	if err != nil {
		a.notify("无法创建托盘图标；%v", err)
		return
	}
	// On Windows and Linux a click opens the window and a right click the
	// menu; macOS shows the menu on any click.
	tray.OnClick(show)
	a.tray = tray
}

// appMenu is the application menu, with "检查更新…" after About on macOS
// as Sparkle's; the other systems keep their own, and reach the updates
// from the ⋯ menu and the command bar.
func (a *App) appMenu() *mygo.MenuItem {
	if runtime.GOOS != "darwin" {
		return &mygo.MenuItem{Role: mygo.RoleAppMenu}
	}
	return &mygo.MenuItem{Label: "FreeFolio", Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleAbout},
		{Label: i18n.T("检查更新"), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.win.Update(a.checkForUpdates)
		}},
		mygo.Separator(),
		{Label: i18n.T("设置"), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.win.Update(func() { a.openSettings(tabGeneral) })
		}},
		mygo.Separator(),
		{Role: mygo.RoleServices},
		mygo.Separator(),
		{Role: mygo.RoleHide},
		{Role: mygo.RoleHideOthers},
		{Role: mygo.RoleUnhide},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}}
}

// notify queues a message for the user.
func (a *App) notify(format string, args ...any) {
	a.notices = append(a.notices, fmt.Sprintf(i18n.T(format), args...))
}

// savePrefs writes prefs.json, telling the user when it fails.
func (a *App) savePrefs() {
	if a.prefsBroken {
		a.notify("偏好设置文件无法读取，修改不会保存")
		return
	}
	if err := a.prefStore.Save(a.prefs); err != nil {
		a.notify("保存偏好设置失败；%v", err)
	}
}

// saveState writes spaces.json, telling the user when it fails.
func (a *App) saveState() {
	if a.files != nil {
		return
	}
	a.captureSplits()
	if err := a.store.Save(a.state); err != nil {
		a.notify("保存空间设置失败；%v", err)
	}
}

// flush saves the open document now and reports whether it is safe to leave.
func (a *App) flush() bool {
	for _, d := range a.residentDocuments() {
		ok := false
		a.withDocument(d, func() { ok = a.flushCurrent() })
		if !ok {
			a.showSaveFailure(d)
			return false
		}
	}
	return true
}

func (a *App) flushCurrent() bool {
	if a.doc == nil || a.doc.loadErr != nil {
		return true
	}
	a.doc.ed.UnmarkText()
	if a.doc.board != nil {
		a.doc.board.FinishEditing()
	}
	if err := a.doc.save(); err != nil {
		if errors.Is(err, errConflict) || errors.Is(err, errMissing) {
			a.notify("「%s」%v，请先在文档上方选择保留哪个版本", a.doc.title(), err)
		} else {
			a.notify("保存 %s 失败；%v", a.doc.title(), err)
		}
		return false
	}
	a.followTitle()
	return true
}

func (a *App) asyncPost() func(func()) {
	if !a.search.async {
		return nil
	}
	if a.runAsync != nil {
		return a.runAsync
	}
	if a.win != nil {
		return a.win.Update
	}
	return nil
}

func (a *App) imageResourceFor(d *document, url string) editor.ImageResource {
	if d == nil || d.closed.Load() {
		return editor.ImageResource{}
	}
	if !isRemoteImage(url) && isVideoFile(url) {
		// A video file of the space plays from where it is.
		full, err := d.localImage(url)
		if err != nil {
			return editor.ImageResource{Problem: editor.ImageInvalid}
		}
		if _, err := os.Stat(full); err != nil {
			return editor.ImageResource{Problem: editor.ImageMissing, Local: true}
		}
		res := videoFileResource(full)
		res.Local = true
		return res
	}
	if isRemoteImage(url) {
		return a.remote.resource(url, a.asyncPost(), func() {
			if d.ed != nil && !d.closed.Load() {
				d.ed.InvalidateImage(url)
			}
		}, fmt.Sprintf("%p", d))
	}
	key := imageKey{doc: d, url: url}
	res, ok := a.imgResolved[key]
	if !ok {
		full, err := resolveSpaceImageWith(d.space, d.rel, url, a.findAttachment)
		res = resolvedImage{full: full, err: err}
		if a.documentContext > 0 || a.isResidentDocument(d) {
			if a.imgResolved == nil {
				a.imgResolved = map[imageKey]resolvedImage{}
			}
			a.imgResolved[key] = res
		}
	}
	if res.err != nil {
		problem := editor.ImageInvalid
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			problem = editor.ImageRemote
		}
		return editor.ImageResource{Problem: problem}
	}
	return d.imageResourceFrom(res.full, url, a.asyncPost())
}

// cachedImageFor loads d's pictures at once, as thumbnails and PDF export
// do: web pictures only as already fetched.
func (a *App) cachedImageFor(d *document) func(string) editor.ImageResource {
	return func(url string) editor.ImageResource {
		if isRemoteImage(url) {
			return a.remote.resource(url, nil, nil)
		}
		return d.imageResource(url)
	}
}

func (a *App) session(id string) *spaceSession {
	s := a.sessions[id]
	if s == nil {
		s = &spaceSession{}
		a.sessions[id] = s
	}
	return s
}

// refreshTree reads the active space's folder again, at once: what follows
// a file operation looks for its result in the tree.
func (a *App) refreshTree() {
	sp := a.state.ActiveSpace()
	if sp == nil {
		a.treeFor = ""
		return
	}
	a.treeFor = sp.ID
	a.treeGen++
	nodes, err := readTree(sp)
	a.setTree(sp, nodes, err)
}

// reloadTree reads the active space's folder again after it changed on
// disk, off the main thread when it can, the rows shown meanwhile.
func (a *App) reloadTree() {
	sp := a.state.ActiveSpace()
	if post := a.treePost(); post != nil && sp != nil && !sp.IsExternal() && a.treeFor == sp.ID {
		a.treeGen++
		a.loadTreeAsync(sp, post, true)
		return
	}
	a.refreshTree()
}

// syncTree reads the active space's folder when the space changed. A large
// folder is read off the main thread, so that entering a new space does
// not freeze the window while every folder of it is listed.
func (a *App) syncTree() {
	sp := a.state.ActiveSpace()
	if sp == nil || a.treeFor == sp.ID {
		return
	}
	a.treeFor = sp.ID
	a.treeGen++
	if post := a.treePost(); post != nil && !sp.IsExternal() {
		a.loadTreeAsync(sp, post, false)
		return
	}
	nodes, err := readTree(sp)
	a.setTree(sp, nodes, err)
}

// treePost is how a folder read off the main thread comes back to it, or
// nil when folders are read at once.
func (a *App) treePost() func(func()) {
	if !a.readTreesAsync {
		return nil
	}
	if a.runAsync != nil {
		return a.runAsync
	}
	if a.win != nil {
		return a.win.Update
	}
	return nil
}

// loadTreeAsync reads the folder of sp, the active space, on a goroutine.
// keep leaves the rows shown until it is read; otherwise the space shows
// what the strip last read of it, or nothing, while it loads. A result
// overtaken by another read, or by a switch, is dropped.
func (a *App) loadTreeAsync(sp *space.Space, post func(func()), keep bool) {
	gen, id, root := a.treeGen, sp.ID, sp.Root
	if !keep {
		a.tree, a.treeErr = nil, nil
		if pt, ok := a.pageTrees[id]; ok && pt.err == nil {
			a.tree = pt.nodes
		}
		a.collectDocs()
	}
	a.treeLoading = true
	go func() {
		nodes, err := vault.Tree(root)
		post(func() {
			cur := a.state.ActiveSpace()
			if gen != a.treeGen || cur == nil || cur.ID != id || cur.Root != root {
				return
			}
			if err == nil {
				nodes = dropTreeFolder(nodes, attachmentFolder(cur))
			}
			arrangeNodes(cur, "", nodes)
			a.setTree(cur, nodes, err)
		})
	}()
}

// setTree takes nodes, read from the folder of sp, the active space, as
// its tree.
func (a *App) setTree(sp *space.Space, nodes []*vault.Node, err error) {
	a.treeLoading = false
	a.tree, a.treeErr = nodes, err
	if a.treeErr == nil {
		// Pins of what is gone go with it.
		kept := pinnedNodes(sp, a.tree)
		if len(kept) != len(sp.Pins) {
			sp.Pins = sp.Pins[:0]
			for _, n := range kept {
				sp.Pins = append(sp.Pins, n.Path)
			}
			a.saveState()
		}
	}
	a.collectDocs()
	a.prunePicks(sp, a.tree)
}

// collectDocs lists the documents of the tree in tree order.
func (a *App) collectDocs() {
	a.docs = a.docs[:0]
	if a.docPaths == nil {
		a.docPaths = make(map[string]bool)
	} else {
		clear(a.docPaths)
	}
	var walk func([]*vault.Node)
	walk = func(nodes []*vault.Node) {
		for _, n := range nodes {
			if n.Dir {
				walk(n.Children)
			} else {
				a.docs = append(a.docs, n)
				a.docPaths[n.Path] = true
			}
		}
	}
	walk(a.tree)
}

// open shows the document at rel of the active space, adding it to the
// history.
func (a *App) open(rel string) { a.openIn(a.state.ActiveSpace(), rel) }

// openFavorite shows a favorite where it is, without leaving the active
// space, as Arc opens a favorite in any space.
func (a *App) openFavorite(f space.Favorite) {
	if sp := a.state.Get(f.Space); sp != nil && a.canEnter(sp.Root) {
		a.clearPicks("")
		a.openIn(sp, f.Path)
	}
}

// openIn shows the document at rel of the space sp, adding it to the
// history.
func (a *App) openIn(sp *space.Space, rel string) {
	if sp != nil && a.showIn(sp, rel) {
		p := place{sp.ID, rel}
		if a.at < len(a.history) && a.history[a.at] == p {
			return
		}
		if len(a.history) > 0 {
			a.history = a.history[:a.at+1]
		}
		a.history = append(a.history, p)
		a.at = len(a.history) - 1
	}
}

// show shows the document at rel of the active space, saving the one
// shown before, and reports whether it could.
func (a *App) show(rel string) bool { return a.showIn(a.state.ActiveSpace(), rel) }

// showIn shows the document at rel of the space sp: the active one, or a
// favorite's.
func (a *App) showIn(sp *space.Space, rel string) bool {
	if sp == nil {
		return false
	}
	if a.paneNavigation && a.split != nil {
		a.syncSplit()
		for i, p := range a.split.panes {
			if p.doc != nil && p.doc.space == sp && p.doc.rel == rel {
				a.focusPane(i, true)
				return true
			}
		}
	}
	if !a.paneNavigation {
		if active := a.state.ActiveSpace(); active != nil {
			if !a.session(active.ID).splitRestored {
				a.restoreSpaceSplit(active)
			}
			if group := a.session(active.ID).split; group != nil {
				a.syncSplit()
				for i, p := range group.panes {
					if p.doc != nil && p.doc.space == sp && p.doc.rel == rel {
						if a.split != group {
							return a.restoreSplit(group, i)
						}
						a.focusPane(i, false)
						return true
					}
				}
			}
		}
	}
	active := sp == a.state.ActiveSpace()
	if a.doc != nil && a.doc.space == sp && a.doc.rel == rel {
		return true
	}
	if w := a.windowOf(sp.ID, rel); w != nil {
		// Its window has it: one file has one editor.
		w.win.Focus()
		return false
	}
	restoring := a.doc == nil && sp.LastOpen == rel
	var text string
	var err error
	if p := a.startupPrefetch; p != nil && p.spaceID == sp.ID && p.rel == rel {
		a.startupPrefetch = nil
		text, err = p.text, p.err
	} else {
		text, err = readFile(sp, rel)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if sp.IsExternal() {
				a.notify("文件已被移动或删除：%s", sp.ExternalFiles[rel])
				return false
			}
			a.state.Forget(sp.ID, rel)
			a.saveState()
			a.refreshTree()
		} else {
			a.notify("打开 %s 失败；%v", vault.Title(rel), err)
		}
		return false
	}
	paneIdx := -1
	if a.paneNavigation && a.split != nil {
		paneIdx = a.split.active
	}
	if !a.leaveDocs(a.leavingCurrentDoc(), func() {
		if paneIdx >= 0 && a.split != nil && paneIdx < len(a.split.panes) {
			a.focusPane(paneIdx, false)
			a.navigateInPane(a.doc, func() { a.openIn(sp, rel) })
			return
		}
		a.openIn(sp, rel)
	}) {
		return false
	}
	if a.split != nil && a.doc != nil && !a.paneNavigation {
		a.suspendSplit()
	}
	prev := a.doc
	a.syncOpen() // the document left, if pinned, stays open
	var cached *document
	if restoring && active {
		cached = a.session(sp.ID).doc
	} else if a.doc == nil && !active {
		// A favorite of another space the active space had open when it
		// was left, as it was.
		if d := a.session(a.state.ActiveSpace().ID).doc; d != nil && d.space == sp {
			cached = d
		}
	}
	if resident := a.residentDocument(sp.ID, rel); resident != nil {
		if !resident.dirty() && resident.diskText != text {
			replacement := a.openDocumentForView(sp, rel, text, resident.source)
			replacement.ed.Scroll = resident.ed.Scroll
			a.replaceReferences(resident, replacement)
			resident = replacement
		}
		a.doc = resident
	} else if kept := a.keptDoc(sp, rel, text); kept != nil {
		a.doc = kept // a pinned document kept open, as it was left
	} else if cached != nil && cached.rel == rel && cached.diskText == text {
		a.doc = cached
	} else {
		preferSource := cached != nil && cached.rel == rel && cached.source
		a.doc = a.openDocumentForView(sp, rel, text, preferSource)
	}
	var oldSession *document
	if activeSp := a.state.ActiveSpace(); activeSp != nil && a.documentContext == 0 {
		if s := a.sessions[activeSp.ID]; s != nil {
			oldSession = s.doc
			s.doc = nil
		}
	}
	a.retireClosedDocuments(prev, oldSession)
	if a.paneNavigation {
		a.doc.requestFocus()
	}
	if a.search.async {
		// The backlinks below the document read the index.
		a.refreshSearch()
	}
	if !active {
		return true // a favorite of another space leaves that space as it was
	}
	if !restoring {
		a.reveal(rel)
	}
	if sp.LastOpen != rel {
		sp.LastOpen, sp.LastAt = rel, 0
		a.saveState()
	}
	return true
}

// noteReading records where the active space's last document was read
// to, for the next launch to pick up there, and reports whether that
// changed.
func (a *App) noteReading() bool {
	sp := a.state.ActiveSpace()
	if sp == nil || a.doc == nil || a.doc.space != sp || a.doc.rel != sp.LastOpen || a.doc.loadErr != nil {
		return false
	}
	at := a.doc.ed.ReadingBlock()
	if at == sp.LastAt {
		return false
	}
	sp.LastAt = at
	return true
}

// reveal opens the folders holding rel in the sidebar.
func (a *App) reveal(rel string) {
	sp := a.state.ActiveSpace()
	changed := false
	for dir := path.Dir(rel); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if !sp.IsExpanded(dir) {
			sp.SetExpanded(dir, true)
			changed = true
		}
	}
	if changed {
		a.saveState()
	}
}

// canGo reports whether the history has a document step steps away.
func (a *App) canGo(step int) bool {
	i := a.at + step
	return i >= 0 && i < len(a.history)
}

// goHistory goes back (-1) or forward (+1) in the history, switching space
// where the document is in another.
func (a *App) goHistory(step int) {
	if a.split != nil {
		a.paneNavigation = true
		defer func() { a.paneNavigation = false; a.syncSplit() }()
	}
	if !a.leaveDocs(a.leavingCurrentDoc(), func() { a.goHistory(step) }) {
		return
	}
	for a.canGo(step) {
		a.at += step
		p := a.history[a.at]
		if a.state.Get(p.space) == nil {
			continue
		}
		sp := a.state.Get(p.space)
		if a.state.IsFavorite(p.space, p.rel) {
			// A favorite opens where the window is, as from its tile.
			if a.showIn(sp, p.rel) {
				a.navDir, a.navAt = step, time.Time{}
				return
			}
			continue
		}
		if a.split != nil {
			if a.showIn(sp, p.rel) {
				a.navDir, a.navAt = step, time.Time{}
				return
			}
			continue
		}
		a.switchSpaceOnly(p.space)
		if a.show(p.rel) {
			a.navDir, a.navAt = step, time.Time{}
			return
		}
	}
}

// reload reads the open document from its file again, after saving it.
func (a *App) reload() {
	if a.doc == nil {
		return
	}
	if a.doc.conflict || a.doc.missing {
		// Reloading is what the user asks for: the file wins.
		a.useDisk()
		return
	}
	if !a.leaveDocs([]*document{a.doc}, a.reload) {
		return
	}
	rel, source := a.doc.rel, a.doc.source
	text, err := readFile(a.doc.space, rel)
	if err != nil {
		a.notify("重新载入 %s 失败；%v", vault.Title(rel), err)
		return
	}
	a.replaceReferences(a.doc, openDocument(a.doc.space, rel, text, source))
	a.refreshTree()
}

// toggleSource switches the open document between live preview and source.
func (a *App) toggleSource() {
	if a.doc == nil {
		return
	}
	a.doc.toggleMode()
}

// newDoc makes a document in the folder dir of the active space and
// opens it.
func (a *App) newDoc(dir string) {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return
	}
	if sp.IsExternal() {
		a.notify("请在普通空间中新建文档")
		return
	}
	// An empty title to type, its placeholder showing, and the starter
	// below; the file is named for the title once it has one.
	doc := model.New()
	title := model.NewBlock("heading", "")
	title.Level = 1
	doc.Blocks = []*model.Block{title, model.NewBlock("paragraph", "")}
	content := editor.ExportMarkdown(doc)
	rel, err := vault.CreateFile(sp.Root, dir, untitled, ".md", content)
	if err != nil {
		a.notify("新建文档失败；%v", err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
	}
	a.refreshTree()
	a.open(rel)
	if d := a.doc; d != nil && d.rel == rel && d.loadErr == nil {
		d.ensureBodyParagraph()
		d.ed.SetSelection(0, 0)
		d.ed.RequestFocus = true
	}
}

// untitled is a new document's file name until its title names it.
const untitled = "未命名"

// isUntitled reports whether a document's name is one newDoc gives:
// 未命名, or 未命名 numbered when that was taken.
func isUntitled(name string) bool {
	rest, ok := strings.CutPrefix(name, untitled)
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	n, ok := strings.CutPrefix(rest, " ")
	if !ok || n == "" {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// followTitle names the open document's file for its title once it is
// saved, while the file is still untitled or has the name this did give
// it, so that a new page is named by writing its title and a page renamed
// by hand keeps its name. A title taken by another file is numbered.
func (a *App) followTitle() {
	d := a.doc
	if d == nil || d.space.IsExternal() || d.loadErr != nil || d.conflict || d.missing || !vault.IsText(d.rel) || d.dirty() {
		return
	}
	if _, composing := d.ed.MarkedRange(); composing {
		return // renaming now would end the input method's composition
	}
	name := vault.Title(d.rel)
	if !isUntitled(name) && name != d.named {
		return
	}
	title := docTitle(d.ed.Doc)
	if title == "" || title == name || title == d.namedFor && name == d.named {
		return // nothing to do, or done for this title already
	}
	sp := d.space
	for i := 1; i < 100; i++ {
		want := title
		if i > 1 {
			want = fmt.Sprintf("%s %d", title, i)
		}
		sources := a.renameSources(sp, d.rel)
		to, err := renameFile(sp, d.rel, want)
		if errors.Is(err, vault.ErrExists) {
			continue
		}
		if err != nil {
			// A name the file system refuses: keep the file's, and do not
			// try this title again.
			d.named, d.namedFor = name, title
			return
		}
		focus := d.ed.Focused()
		from := d.rel
		d.named, d.namedFor = vault.Title(to), title
		if to != from {
			a.moved(sp, from, to)
			if !isUntitled(name) {
				a.relink(name, vault.Title(to), sources)
			}
		}
		d.ed.RequestFocus = focus
		return
	}
}

// docTitle is the text of a document's leading level 1 heading, on one
// line and at most 60 characters, as a file name can hold it.
func docTitle(doc *model.Document) string {
	if len(doc.Blocks) == 0 || doc.Blocks[0].Kind != "heading" || doc.Blocks[0].Level != 1 {
		return ""
	}
	title := strings.Join(strings.Fields(doc.Blocks[0].Text()), " ")
	if r := []rune(title); len(r) > 60 {
		title = strings.TrimSpace(string(r[:60]))
	}
	return strings.Trim(title, ". ")
}

// newFolder makes a folder in dir and starts naming it.
func (a *App) newFolder(dir string) {
	sp := a.state.ActiveSpace()
	if sp == nil || sp.IsExternal() {
		return
	}
	rel, err := vault.Mkdir(sp.Root, dir, i18n.T("新建文件夹"))
	if err != nil {
		a.notify("新建文件夹失败；%v", err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
		a.saveState()
	}
	a.refreshTree()
	a.startRename(rel)
}

// startRename edits the name of the file or folder at rel of the active
// space in the sidebar.
func (a *App) startRename(rel string) {
	if sp := a.state.ActiveSpace(); sp != nil {
		a.renameIn(sp, rel)
	}
}

// renameIn edits the name of the file or folder at rel of the space sp: in
// its row, or under its tile for a favorite, which may be another space's.
func (a *App) renameIn(sp *space.Space, rel string) {
	a.renaming, a.renameSpace, a.renameFocused = rel, sp.ID, false
	if sp.IsExternal() {
		rel = filepath.Base(sp.ExternalFiles[rel])
	}
	a.newName = path.Base(rel)
	if vault.IsDocument(rel) {
		a.newName = vault.Title(rel)
	}
}

// commitRename gives the file or folder being renamed the name typed.
func (a *App) commitRename() {
	rel, name := a.renaming, strings.TrimSpace(a.newName)
	a.renaming = ""
	if rel == "" || name == "" {
		return
	}
	sp := a.state.Get(a.renameSpace)
	if a.files != nil && a.renameSpace == space.ExternalID {
		sp = a.files.locations
	}
	if sp == nil {
		return
	}
	renaming := a.residentDocument(sp.ID, rel)
	if !a.flushForTask() {
		return
	}
	if renaming != nil {
		rel = renaming.rel
	}
	sources := a.renameSources(sp, rel)
	to, err := renameFile(sp, rel, name)
	if err != nil {
		a.notify("重命名 %s 失败；%v", path.Base(rel), err)
		return
	}
	if to != rel {
		a.moved(sp, rel, to)
		if vault.IsDocument(rel) {
			a.relink(vault.Title(rel), vault.Title(to), sources)
		}
	}
}

// moveInto moves the file or folder at rel into the folder dir.
func (a *App) moveInto(rel, dir string) {
	if path.Dir(rel) == dir || dir == "." && !strings.Contains(rel, "/") {
		return
	}
	sp := a.state.ActiveSpace()
	if !a.flushForTask() {
		return
	}
	to, _, err := a.moveWithResources(sp, rel, sp, dir)
	if err != nil {
		a.notify("移动 %s 失败；%v", path.Base(rel), err)
		return
	}
	if dir != "" {
		sp.SetExpanded(dir, true)
	}
	a.moved(sp, rel, to)
}

// moved follows a file or folder of the space sp that moved from one path
// to another.
func (a *App) moved(sp *space.Space, from, to string) {
	a.followPicks(sp.ID, from, to)
	if a.files != nil && sp == a.files.locations {
		if from == to {
			return
		}
		delete(sp.ExternalFiles, from)
		for i, rel := range a.files.order {
			if rel == from {
				a.files.order[i] = to
			}
		}
		if a.doc != nil && a.doc.space == sp && a.doc.rel == from {
			a.doc.rel = to
		}
		a.files.marks.Renamed(sp.ID, from, to)
		return
	}
	_ = history.Move(sp.Root, from, to)
	a.state.Renamed(sp.ID, from, to)
	if a.dragGrab.active && a.dragGrab.space == sp.ID && a.dragGrab.rel == from {
		a.dragGrab.rel = to
	}
	a.followOpen(sp.ID, from, sp.ID, to)
	a.followSplitPaths(sp.ID, from, sp.ID, to)
	a.saveState()
	a.refreshTree()
}

// switchSpaceOnly makes the space with the ID active, without opening a
// document.
func (a *App) switchSpaceOnly(id string) bool {
	if cur := a.state.ActiveSpace(); cur != nil && cur.ID == id {
		return false
	}
	next := a.state.Get(id)
	if next == nil || !a.canEnter(next.Root) || !a.flushForTask() {
		return false
	}
	a.finishSpaceEdit()
	from := a.pagerShown(a.now)
	if cur := a.state.ActiveSpace(); cur != nil {
		a.session(cur.ID).doc = a.doc
		a.session(cur.ID).splitShown = a.split != nil
		// The strip shows the space being left as it slides away: keep
		// what was read of it rather than listing a large folder again.
		if a.treeFor == cur.ID && !a.treeLoading && a.treeErr == nil {
			if a.pageTrees == nil {
				a.pageTrees = map[string]pageTree{}
			}
			a.pageTrees[cur.ID] = pageTree{a.tree, nil}
		}
	}
	a.suspendSplit()
	a.state.SetActive(id)
	a.saveState()
	a.restoreSpaceSplit(next)
	a.syncOpen()
	a.doc = nil
	a.renaming = ""
	a.syncTree()
	a.pager.swipe = 0
	a.pagerSettle(a.spaceIndex(id), from)
	delete(a.pageTrees, id) // the active space reads its own
	return true
}

// switchSpace makes the space with the ID active and opens what it showed
// when it was left: a favorite of another space it had open, or else its
// last document. Each space keeps its own, as Arc's spaces keep their
// tabs, so a favorite opened in one does not follow into the next.
func (a *App) switchSpace(id string) {
	if a.files != nil && !a.leaveFileMode() {
		return
	}
	if !a.switchSpaceOnly(id) || a.doc != nil {
		return
	}
	a.resumeSpace()
}

// resumeSpace opens what the active space showed when it was left: its
// split, a favorite of another space it had open, or else its last
// document.
func (a *App) resumeSpace() {
	defer func() {
		if sp := a.state.ActiveSpace(); sp != nil {
			if s := a.sessions[sp.ID]; s != nil {
				old := s.doc
				s.doc = nil
				a.retireClosedDocuments(old)
			}
		}
	}()
	sp := a.state.ActiveSpace()
	id := sp.ID
	if session := a.session(id); session.splitShown && session.split != nil {
		a.restoreSplit(session.split, session.split.active)
		return
	}
	if d := a.session(id).doc; d != nil && d.space != sp && a.state.IsFavorite(d.space.ID, d.rel) {
		if a.showIn(d.space, d.rel) {
			return
		}
	}
	if sp.LastOpen != "" {
		a.open(sp.LastOpen)
	}
}

// switchSpaceAt switches to the space at index i, for Cmd+1…9.
func (a *App) switchSpaceAt(i int) {
	if i >= 0 && i < len(a.state.Spaces) {
		a.switchSpace(a.state.Spaces[i].ID)
	}
}

// toggleFavorite adds the document at rel of the space sp to the
// favorites, out of the space's pins, or removes it.
func (a *App) toggleFavorite(sp *space.Space, rel string) {
	if sp != nil {
		a.state.ToggleFavorite(sp.ID, rel)
		if a.state.IsFavorite(sp.ID, rel) {
			sp.Unpin(rel)
		}
		a.saveState()
	}
}

// togglePin pins the document or folder at rel at the end of the space's
// pinned section, out of the favorites, or unpins it.
func (a *App) togglePin(sp *space.Space, rel string) {
	if sp == nil {
		return
	}
	if sp.IsPinned(rel) {
		sp.Unpin(rel)
		a.saveState()
		return
	}
	a.pinIn(sp, rel, len(pinnedNodes(sp, a.tree)))
}

// newFolderWith makes a folder beside the document at rel, moves the
// document into it, and starts naming the folder, as Cra's "New Folder with
// Tab".
func (a *App) newFolderWith(rel string) {
	sp := a.state.ActiveSpace()
	if sp == nil || sp.IsExternal() {
		return
	}
	dir := parentDir(rel)
	folder, err := vault.Mkdir(sp.Root, dir, i18n.T("新建文件夹"))
	if err != nil {
		a.notify("新建文件夹失败；%v", err)
		return
	}
	a.refreshTree()
	if sp.IsPinned(rel) {
		sp.Unpin(rel)
	}
	a.placeEntry(rel, folder, 0)
	sp.SetExpanded(folder, true)
	a.saveState()
	a.startRename(folder)
}

// toggleFolder opens or closes a folder in the sidebar.
func (a *App) toggleFolder(rel string) {
	sp := a.state.ActiveSpace()
	sp.SetExpanded(rel, !sp.IsExpanded(rel))
	a.saveState()
}

// trash moves the file or folder at rel of the active space to the
// system's trash, which never deletes it for good.
func (a *App) trash(rel string) {
	sp := a.state.ActiveSpace()
	if sp.IsExternal() {
		a.forgetExternal(sp, rel)
		return
	}
	p, err := resolveFile(sp, rel)
	if err != nil {
		return
	}
	if !a.flushForTask() {
		return
	}
	kept, ok := a.keepDeleted(sp.ID, sp.Name, sp.Root, rel)
	if err := a.trashItem(p); err != nil {
		if ok {
			a.bin.Forget(kept)
		}
		a.notify("将 %s 移到废纸篓失败；%v", path.Base(rel), err)
		return
	}
	for _, d := range a.residentDocuments() {
		if d.space == sp && within(d.rel, rel) {
			a.replaceReferences(d, nil)
		}
	}
	a.state.Forget(sp.ID, rel)
	a.saveState()
	a.refreshTree()
	if ok {
		a.offerUndo(i18n.T("已移到废纸篓"), path.Base(rel), func() { a.restoreDeleted(kept) })
	}
}

// revealInFinder shows the file or folder at rel of the active space in
// the file manager.
func (a *App) revealInFinder(rel string) { a.revealIn(a.state.ActiveSpace(), rel) }

// revealIn shows the file or folder at rel of the space sp in the file
// manager.
func (a *App) revealIn(sp *space.Space, rel string) {
	if p, err := resolveFile(sp, rel); err == nil {
		mygo.Shell.ShowItemInFolder(p)
	}
}

// reloadPlugins scans the plugins folder again.
func (a *App) reloadPlugins() {
	a.launch.loadPlugins = false
	var err error
	a.installed, a.brokenPlugins, err = a.plugins.Scan()
	if err != nil {
		a.notify("读取插件文件夹失败；%v", err)
	}
	a.syncFormatPlugins()
}

func (a *App) ensurePluginsLoaded() {
	if a.launch.loadPlugins {
		a.reloadPlugins()
	}
}

// defaultRoot returns the folder a new space named name gets by default.
func (a *App) defaultRoot(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		name = i18n.T("空间")
	}
	return filepath.Join(a.cfg.DocumentsDir, name)
}

const welcome = `# Welcome to FreeFolio

Open a Markdown file directly or organize a folder as a workspace. Documents save directly as standard Markdown (.md) files, so any editor, Git or sync tool works on the same folder. Use the formatting bar or type ` + "`#`" + ` and a space for a heading, ` + "`-`" + ` and a space for a list, ` + "`**bold**`" + ` for **bold**.

- **Spaces** organize local folders. Switch spaces using the icons in the sidebar.
- **The command bar** finds documents and runs commands. Select Search in the sidebar or the document name above to open it.
- Choose **New Document** to start writing. When the sidebar is folded, move the pointer to its edge of the window to show it again.
- [ ] Drag a document onto a folder to move it
- [ ] Right-click a name and choose Rename to rename it

## Plugins

AI and other powers will come from the plugin marketplace. FreeFolio's core stays small.
`

const ideas = `# Ideas

Click a folder in the sidebar to expand or collapse it.
`

func writeNative(root, rel, markdown string) error {
	if strings.EqualFold(path.Ext(rel), ".arcmd") {
		text, err := editor.ImportMarkdown(markdown).Encode()
		if err != nil {
			return err
		}
		return vault.Write(root, rel, text)
	}
	return vault.Write(root, rel, markdown)
}

func (a *App) exportMarkdown() {
	if a.doc == nil || a.doc.loadErr != nil || a.doc.isPDF() {
		return
	}
	if a.doc.board != nil {
		a.exportBoardMarkdown(a.doc)
		return
	}
	a.doc.ed.UnmarkText()
	root, name, err := fileLocation(a.doc.space, a.doc.rel)
	if err != nil {
		a.notify("导出失败；%v", err)
		return
	}
	rel, err := vault.CreateFile(root, path.Dir(name), a.doc.title()+" Export", ".md", a.doc.text())
	if err != nil {
		a.notify("导出失败；%v", err)
		return
	}
	a.refreshTree()
	a.notify("已导出 %s", rel)
}

// applyLanguage shows the interface in the language the preferences pick,
// or the system's.
func (a *App) applyLanguage() {
	tag := a.prefs.Language
	if tag == "" {
		tag = a.systemLanguage
	}
	i18n.SetLanguage(tag)
	if a.files != nil {
		a.files.locations.Name = i18n.T("文件浏览")
	}
	if a.win != nil {
		mygo.App.SetLocale(i18n.Language())
	}
}

func (a *App) invalidateResolvedImages() { a.imgResolved, a.pictureNames = nil, frameNames{} }

func (a *App) isResidentDocument(d *document) bool {
	if d == nil {
		return false
	}
	if d == a.movingDoc {
		return true
	}
	for _, r := range a.residentDocuments() {
		if r == d {
			return true
		}
	}
	return false
}

func (a *App) retireClosedDocuments(candidates ...*document) {
	if a.documentContext > 0 {
		return
	}
	resident := make(map[*document]bool, 8)
	for _, d := range a.residentDocuments() {
		resident[d] = true
	}
	if a.movingDoc != nil {
		resident[a.movingDoc] = true
	}
	if a.versions.open && a.versions.for_ != nil {
		resident[a.versions.for_] = true
	}
	for _, d := range candidates {
		if d != nil && !resident[d] {
			if child := d.boardOverlay; child != nil && !resident[child] {
				child.close()
				d.boardOverlay = nil
			}
			d.close()
		}
	}
	if len(a.imgResolved) > 0 {
		removed := 0
		for k := range a.imgResolved {
			if k.doc == nil || !resident[k.doc] {
				if k.doc != nil {
					k.doc.close()
				}
				delete(a.imgResolved, k)
				removed++
			}
		}
		if len(a.imgResolved) == 0 {
			a.imgResolved = nil
		} else if removed > 0 {
			compact := make(map[imageKey]resolvedImage, len(a.imgResolved))
			for k, v := range a.imgResolved {
				compact[k] = v
			}
			a.imgResolved = compact
		}
	}
	if a.search.hitsKey.doc != nil && !resident[a.search.hitsKey.doc] {
		a.search.hitsKey = hitsKey{}
		a.search.hits = nil
	}
	if len(a.search.liveDocs) > 0 {
		for d := range a.search.liveDocs {
			if !resident[d] {
				delete(a.search.liveDocs, d)
				a.search.overlayReady = false
			}
		}
	}
}

// Preview loaders keep local thumbnails small and reuse only cached network data.
func (a *App) previewImageFor(d *document) func(string) editor.ImageResource {
	return func(url string) editor.ImageResource {
		if isRemoteImage(url) {
			return a.remote.resource(url, nil, nil)
		}
		return d.thumbImageResource(url)
	}
}
