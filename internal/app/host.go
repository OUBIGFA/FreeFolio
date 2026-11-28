package app

import (
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/chatgpt"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/trashbin"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Feature panels (versionsPanel, gitUIState, cloudState, …) keep their own
// state and methods and reach the window only through a narrow host
// interface that App implements. These accessors expose the App fields the
// hosts read; panels never write App fields directly.

// wirePanels gives every feature panel its host. Every App constructor
// calls it, so a panel never runs without one.
func (a *App) wirePanels() {
	a.versions.host = a
	a.resources.host = a
	a.gitSync.host = a
	a.cloud.host = a
	a.updates.host = a
	a.chatgpt.host = a
	a.fonts.host = a
	a.qa.host = a
	a.mdzFiles.host = a
	a.library.host = a
	a.pdfPages.host = a
	a.bookmarks.host = a
	a.boardAI.host = a
}

func (a *App) optionalResources() *resourceState { return &a.resources }

// providerFactory makes model services; tests replace it.
func (a *App) providerFactory() func(agent.Settings) agent.Provider { return a.qa.provider }

func (a *App) palettePanel() *palette { return &a.palette }

// fileMarks are the file browser's bookmarks while it is shown.
func (a *App) fileMarks() *space.State {
	if a.files == nil {
		return nil
	}
	return &a.files.marks
}

// reservedCombos are the keys of the bookmark commands.
func (a *App) reservedCombos() []editor.Combo {
	a.ensureKeyCache()
	return a.cachedReserved
}

// dismissForLibrary closes what the library replaces: the space form, a
// rename and the command bar.
func (a *App) dismissForLibrary() {
	a.spaceForm = spaceForm{}
	a.renaming = ""
	a.palette.open = false
}

// activeTreeOf is the active space's folder tree when id is that space.
func (a *App) activeTreeOf(id string) ([]*vault.Node, bool, error) {
	if id != a.treeFor {
		return nil, false, nil
	}
	return a.tree, true, a.treeErr
}

// overlayOpen reports whether the command bar or settings cover the window.
func (a *App) overlayOpen() bool { return a.palette.open || a.settingsOpen }

func (a *App) documentTheme() *ui.Theme { return a.docTheme }

func (a *App) splitShown() bool { return a.split != nil }

// forgetPageTree drops a space's cached folder tree so it is read again.
func (a *App) forgetPageTree(id string) { delete(a.pageTrees, id) }

func (a *App) chatGPT() *chatGPTState    { return &a.chatgpt }
func (a *App) activeTree() []*vault.Node { return a.tree }
func (a *App) docNodes() []*vault.Node   { return a.docs }
func (a *App) trashBin() *trashbin.Bin   { return &a.bin }
func (a *App) windowWidth() float32      { return a.winW }
func (a *App) consumeDragGrab()          { a.dragGrab.consumed = true }

// filePicker is the test hook that replaces the system file dialog.
func (a *App) filePicker() func(done func(paths []string)) { return a.pickFiles }

// cachedTree is a background space's last read folder tree, if any.
func (a *App) cachedTree(id string) []*vault.Node {
	if cached, ok := a.pageTrees[id]; ok && cached.err == nil {
		return cached.nodes
	}
	return nil
}

// fileLocations is the file browser's opened files while it is shown.
func (a *App) fileLocations() *space.Space {
	if a.files == nil {
		return nil
	}
	return a.files.locations
}

// boardOnScreen reports whether a whiteboard shows in the document area.
// Squeezing a media-heavy canvas every animation frame is costly, so side
// panels switch width at once while one does.
func (a *App) boardOnScreen() bool {
	board := func(d *document) bool { return d != nil && (d.board != nil || d.boardLoading || d.boardOverlay != nil) }
	if a.split != nil {
		for _, p := range a.split.panes {
			if board(p.doc) {
				return true
			}
		}
		return false
	}
	return board(a.doc)
}

// oneBotSync reports whether OneBot work runs synchronously, as in tests.
func (a *App) oneBotSync() bool { return a.qa.sync }

// resetModelFetch stops listing models so the next fetch starts over.
func (a *App) resetModelFetch() {
	a.qa.cancelModelFetch()
	a.qa.modelsFor = ""
}

// stopOneBotWorkFor stops every running turn bound to the connection id,
// in any space, and its running document edit.
func (a *App) stopOneBotWorkFor(id string) {
	for _, s := range a.qa.sessions {
		if strings.HasPrefix(s.source, id+"|") && s.cancel != nil {
			s.cancel()
		}
	}
	if task := a.qa.editTask; task != nil && task.Running && task.connectionID == id {
		a.qa.stopOneBotEdit()
	}
}

func (a *App) currentLook() spaceLook { return a.look }
func (a *App) focusMode() bool        { return a.focus }
func (a *App) libraryOpen() bool      { return a.library.open }

// savePrefsAfterLaunch saves prefs.json now, or once a deferred launch has
// shown the window; an unreadable prefs file is left untouched.
func (a *App) savePrefsAfterLaunch() {
	if a.prefsBroken {
		return
	}
	if a.cfg.DeferStartup {
		a.launch.savePrefs = true
	} else {
		a.savePrefs()
	}
}

// documentSaved tells the sync features a document was written: Git first,
// then cloud sync, each scheduling its own save-triggered run.
func (a *App) documentSaved(d *document) {
	a.gitSync.gitSaved(d)
	a.cloud.cloudSaved(d)
}

// gitBusy reports whether a Git operation is running.
func (a *App) gitBusy() bool { return a.gitSync.busy }

// gitSyncsSpace reports whether Git sync is on for the space id.
func (a *App) gitSyncsSpace(id string) bool {
	c := a.gitSync.config.Spaces[id]
	return c != nil && c.Enabled
}

// trashBinDir is where deleted files keep their recovery copies.
func (a *App) trashBinDir() string { return a.bin.Dir }

// Cross-feature questions panels ask each other through App.

// cloudSyncsSpace reports whether cloud sync is on for the space id.
func (a *App) cloudSyncsSpace(id string) bool {
	return a.cloud.config.Enabled && a.cloud.config.Spaces[id] != ""
}

// cloudBusy reports whether a cloud sync is running.
func (a *App) cloudBusy() bool { return a.cloud.busy }

// chatGPTTokenSource signs requests in as the ChatGPT connection id.
func (a *App) chatGPTTokenSource(id string) *chatgpt.TokenSource {
	a.chatgpt.ensureChatGPT()
	return a.chatgpt.client.Source(id)
}

func (a *App) appConfig() Config         { return a.cfg }
func (a *App) preferences() *prefs.Prefs { return a.prefs }
func (a *App) window() *mygo.Window      { return a.win }
func (a *App) clock() time.Time          { return a.now }
func (a *App) spaces() *space.State      { return a.state }
func (a *App) activeDocument() *document { return a.doc }
func (a *App) paperIsDark() bool         { return a.paperDark }
func (a *App) motionReduced() bool       { return a.reduceMotion }

// backgroundResult runs filesystem work away from the native view and applies
// its result on the window thread. Headless tests run the same work directly.
func (a *App) backgroundResult(work func() func()) {
	if a.win == nil {
		work()()
		return
	}
	win := a.win
	go func() { apply := work(); win.Update(apply) }()
}

// OneBot services other features use.

func (a *App) readAIKeys() (aiKeyring, error) { return a.qa.readAIKeys() }
func (a *App) cancelModelFetch()              { a.qa.cancelModelFetch() }
func (a *App) fetchModels()                   { a.qa.fetchModels() }
func (a *App) selectAIConnection(id string)   { a.qa.selectAIConnection(id) }

// The whiteboard AI image tool OneBot offers in conversations.

func (a *App) chatImageTool(d *document) *agent.Tool { return a.boardAI.chatImageTool(d) }
