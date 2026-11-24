package app

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/vault"

	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo"
)

// spaceForm holds a new space draft or one existing space property being edited.
type spaceForm struct {
	open    bool
	id      string // "" for a new space
	edit    string // "theme", "icon", or "name" for an existing space
	focused bool   // the inline name field has received focus
	name    string
	icon    string
	// symbol is the stored (Lucide) name of the space's glyph; it wins over icon.
	// image, a picture's file name in space-icons, wins over both.
	symbol string
	image  string
	root   string // "" for the default folder of a new space
	// theme is the look being edited, which the window previews for an
	// existing space until the dialog closes.
	theme space.Theme
	// page is the page of preset colors shown; knob is the key color being
	// dragged on the wheel, -1 for none.
	page int
	knob int
	// cardOpen shows the theme card beside the sidebar; iconsOpen shows the
	// icon picker.
	cardOpen, iconsOpen bool
	picker              iconPickerState
}

// newSpaceForm starts a new space in the sidebar, as Arc does: the sidebar
// shows the draft with its name field, the theme card opens beside it and
// the window takes the draft's look until it is made or given up.
func (a *App) newSpaceForm() {
	a.finishSpaceEdit()
	n := len(a.state.Spaces)
	a.spaceForm = spaceForm{open: true, icon: space.Icons[n%len(space.Icons)], theme: space.NewTheme(n), knob: -1, cardOpen: true}
	a.spaceForm.page = max(0, space.PageOf(a.spaceForm.theme.Colors))
	a.revealSidebar()
}

// editSpace opens only the theme card, leaving the sidebar in place.
func (a *App) editSpace(s *space.Space) {
	a.editSpaceProperty(s, "theme")
}

// editSpaceProperty opens the appropriate anchored editor after switching
// to its space. A failed switch must not edit the space left behind.
func (a *App) editSpaceProperty(s *space.Space, property string) {
	if s.IsExternal() && property == "name" {
		return
	}
	a.finishSpaceEdit()
	if a.state.ActiveSpace() != s {
		a.switchSpace(s.ID)
		if a.state.ActiveSpace() != s {
			return // the open document could not be saved
		}
	}
	th := space.ThemeOf(s)
	a.spaceForm = spaceForm{open: true, id: s.ID, edit: property, name: s.Name, icon: s.Icon, symbol: s.Symbol, image: s.Image, root: s.Root, theme: th, knob: -1, cardOpen: property == "theme"}
	a.spaceForm.page = max(0, space.PageOf(th.Colors))
	if property == "icon" {
		a.spaceForm.openIconPicker()
	}
	a.revealSidebar()
}

// finishSpaceEdit keeps a theme preview or nonempty inline name before
// leaving its editor. An unchosen icon and a new draft are discarded.
func (a *App) finishSpaceEdit() {
	f := &a.spaceForm
	// The theme card the tour shows is its own: nothing of it is kept.
	if f.open && f.id != "" && (f.edit == "theme" || f.edit == "name") && !a.tour.themeHeld {
		a.submitSpace()
	}
	a.cancelSpaceForm()
}

// revealSidebar docks the sidebar, where the space form is.
func (a *App) revealSidebar() {
	a.sidebarHidden = false
	a.narrowShown = true
	a.focus = false
	if a.library.open {
		a.library.open, a.library.frames = false, 0
	}
	a.peek = false
}

// submitSpace makes or updates the space the form describes.
func (a *App) submitSpace() {
	f := &a.spaceForm
	name := strings.TrimSpace(f.name)
	if name == "" {
		return
	}
	icon := strings.TrimSpace(f.icon)
	if s := a.state.Get(f.id); s != nil {
		switch f.edit {
		case "name":
			s.Name = name
		case "icon":
			s.SetIcon(icon, f.symbol, f.image)
		case "theme":
			s.SetTheme(f.theme)
		}
		f.open = false
		a.saveState()
		return
	}
	root := f.root
	if root == "" {
		root = a.defaultRoot(name)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		a.notify("创建文件夹 %s 失败。%v", root, err)
		return
	}
	// An existing folder may hold a legacy system folder.
	if err := vault.MigrateSystemDir(root); err != nil {
		a.notify("迁移空间 %s 的系统文件夹失败。%v", name, err)
	}
	s := a.state.Add(name, root)
	s.SetIcon(icon, f.symbol, f.image)
	s.SetTheme(f.theme)
	// An empty folder starts with a page showing how to write. Only whether
	// it holds any document is asked: listing a large folder whole took
	// seconds before the new space showed.
	if has, err := vault.HasDocument(root); err == nil && !has {
		if name := starterFile(); writeStarter(root, name) == nil {
			s.LastOpen = name
		}
	}
	f.open = false
	a.saveState()
	a.switchSpace(s.ID)
}

// spacesFromFolders makes a space of each folder dropped on the space bar's
// +, in place (nothing is copied): named as the folder, with a random glyph
// and a random preset color. A folder some space already uses switches to
// that space instead; dropped files are not folders and are skipped.
func (a *App) spacesFromFolders(paths []string) {
	var skipped []string
	for _, dir := range paths {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			skipped = append(skipped, filepath.Base(dir))
			continue
		}
		if sp := a.spaceAt(info); sp != nil {
			a.switchSpace(sp.ID)
			continue
		}
		name := strings.TrimSpace(filepath.Base(dir))
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = strings.TrimSpace(filepath.VolumeName(dir)) // a drive's root
		}
		if name == "" {
			name = dir
		}
		// Greyscale, the last page, is left out: a random space should be colored.
		pages := space.PresetPages[:len(space.PresetPages)-1]
		swatches := pages[rand.IntN(len(pages))].Swatches
		theme := space.Theme{Colors: swatches[rand.IntN(len(swatches))], Intensity: 0.8, Appearance: space.AppearanceAuto}
		a.finishSpaceEdit()
		a.spaceForm = spaceForm{open: true, name: name, symbol: spaceSymbols[rand.IntN(len(spaceSymbols))], root: dir, theme: theme, knob: -1}
		a.submitSpace()
		if a.spaceForm.open {
			a.cancelSpaceForm() // its failure was told; no draft stays behind
		}
	}
	if len(skipped) > 0 {
		a.notify("只能用文件夹新建空间：%s 未添加。", strings.Join(skipped, "、"))
	}
}

// spaceAt is the space whose folder is the one info describes, or nil.
func (a *App) spaceAt(info os.FileInfo) *space.Space {
	for _, sp := range a.state.Spaces {
		if sp.IsExternal() {
			continue
		}
		if si, err := os.Stat(sp.Root); err == nil && os.SameFile(si, info) {
			return sp
		}
	}
	return nil
}

// removeSpace takes a space out of FreeFolio; its folder stays on disk.
func (a *App) removeSpace(id string) {
	if id == space.ExternalID || len(a.state.Spaces) < 2 {
		return
	}
	var spaceDocs []*document
	for _, d := range a.residentDocuments() {
		if d.space != nil && d.space.ID == id {
			spaceDocs = append(spaceDocs, d)
		}
	}
	if !a.leaveDocs(spaceDocs, func() { a.removeSpace(id) }) {
		return
	}
	if a.state.ActiveSpace() != nil && a.state.ActiveSpace().ID == id {
		a.suspendSplit()
	}
	for _, d := range a.residentDocuments() {
		if d.space.ID == id {
			a.replaceReferences(d, nil)
		}
	}
	if a.state.Remove(id) == nil {
		delete(a.sessions, id)
		a.saveState()
		a.refreshTree()
		if sp := a.state.ActiveSpace(); sp != nil && sp.LastOpen != "" {
			a.open(sp.LastOpen)
		}
	}
}

// chooseFolder asks for the new space's folder, off the main thread as
// the dialog blocks.
func (a *App) chooseFolder() {
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: i18n.T("选择空间文件夹"), Directory: true})
		if err != nil || len(paths) == 0 {
			return
		}
		win.Update(func() { a.spaceForm.root = paths[0] })
	}()
}

func revealDir(dir string) { mygo.Shell.OpenPath(dir) }
