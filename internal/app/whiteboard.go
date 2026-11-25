package app

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/history"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/spacefs"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (d *document) loadWhiteboard(text string) {
	s, err := whiteboard.Decode(text)
	if err != nil {
		d.loadErr = err
		return
	}
	d.board = editor.NewBoard(s)
	canonical, _ := s.Encode()
	d.boardBase = sha256.Sum256([]byte(canonical))
	d.boardDigestValue = d.boardBase
	d.boardDigestKnown = true
}

func (a *App) openDocumentForView(sp *space.Space, rel, text string, preferSource bool) *document {
	if vault.IsConverted(rel) && a.win != nil {
		return a.openConvertedAsync(sp, rel, text)
	}
	if !vault.IsWhiteboard(rel) || a.win == nil {
		return openDocument(sp, rel, text, preferSource)
	}
	d := &document{space: sp, rel: rel, diskText: text, boardLoading: true, ed: editor.FromOwnedDocument(model.New())}
	d.initLazyBaseline()
	go func() {
		loaded := openDocument(sp, rel, text, false)
		a.win.Update(func() {
			if !d.closed.Load() && a.isResidentDocument(d) {
				a.replaceReferences(d, loaded)
			}
		})
	}()
	return d
}
func (d *document) contentVersion() uint64 {
	if d.board != nil {
		return d.boardGen
	}
	return d.ed.Version()
}
func (d *document) contentRevision() uint64 {
	if d.board != nil {
		return d.boardGen
	}
	return d.ed.Revision()
}

func whiteboardQAPage(name, content string) (*spacefs.Page, error) {
	s, err := whiteboard.Decode(content)
	if err != nil {
		return nil, err
	}
	return sceneQAPage(name, s), nil
}
func sceneQAPage(name string, s *whiteboard.Scene) *spacefs.Page {
	var parts []spacefs.Part
	for _, line := range s.MarkdownSummary() {
		parts = append(parts, spacefs.Part{Block: line.ElementID, Text: line.Text})
	}
	return spacefs.PageOf(name, parts)
}
func (d *document) qaPage() *spacefs.Page {
	if d.board != nil {
		return sceneQAPage(d.rel, d.board.Scene)
	}
	return pageOfDocument(d.rel, d.ed.Committed())
}

func (a *App) mapDocumentText(d *document, fn func(string) string) bool {
	if d.board == nil {
		return d.ed.Engine.MapText(fn)
	}
	before := d.board.Scene.Clone()
	if !d.board.Scene.MapText(fn, nil) {
		return false
	}
	d.board.History.Record(before, d.board.Scene)
	a.boardChanged(d)
	return true
}

func (a *App) mapDocumentProse(d *document, fn func(string) string) bool {
	if d.board != nil {
		return a.mapDocumentText(d, fn)
	}
	return d.ed.Engine.MapProse(fn)
}
func (d *document) whiteboardDirty() bool {
	if d.loadErr != nil {
		return false
	}
	if !d.boardDigestKnown || d.boardDigestRev != d.boardGen {
		return true
	}
	return d.boardDigestValue != d.boardBase
}
func (a *App) boardChanged(d *document) {
	d.boardGen++
	d.plainKnown = false
	d.boardDigestKnown = false
	a.search.overlayReady = false
	for _, parent := range a.residentDocuments() {
		if parent == d || parent.space.ID != d.space.ID {
			continue
		}
		for _, url := range parent.ed.ImageURLs() {
			if path.Clean(path.Join(path.Dir(parent.rel), unescapeImagePath(url))) == d.rel {
				delete(parent.imgCache, url)
				parent.ed.InvalidateImage(url)
			}
		}
	}
}

func (a *App) insertWhiteboard(parent *document) {
	if parent == nil || parent.ed.ReadOnly || parent.loadErr != nil {
		return
	}
	s := whiteboard.NewScene()
	raw, err := whiteboard.ExportSVG(s, whiteboard.SVGOptions{EmbedScene: true})
	if err != nil {
		a.notify("新建白板失败；%v", err)
		return
	}
	// Publish the image before its Markdown reference. Failure keeps the
	// existing document intact and cannot insert a broken reference.
	url, err := a.writeAssetFor(parent, path.Dir(parent.rel), "未命名白板.excalidraw.svg", []byte(raw))
	if err != nil {
		a.notify("新建白板失败；%v", err)
		return
	}
	b := model.NewBlock("image", "白板")
	b.URL = url
	parent.ed.Engine.Insert(b)
	parent.ed.FocusInsertedImage(b.ID)
	a.openBoardEmbed(parent, url)
}
func (a *App) openBoardEmbed(parent *document, url string) {
	if parent == nil || parent.loadErr != nil {
		return
	}
	if parent.space.IsExternal() {
		a.notify("请在普通空间中编辑内嵌白板")
		return
	}
	decoded := unescapeImagePath(strings.Split(url, "#")[0])
	rel := path.Clean(path.Join(path.Dir(parent.rel), decoded))
	if !vault.IsWhiteboard(rel) {
		return
	}
	if _, err := vault.Resolve(parent.space.Root, rel); err != nil {
		a.notify("白板路径无效；%v", err)
		return
	}
	if resident := a.residentDocument(parent.space.ID, rel); resident != nil {
		if resident.loadErr != nil {
			a.notify("白板无法读取；%v", resident.loadErr)
			return
		}
		parent.boardOverlay = resident
		resident.board.RequestFocus = true
		return
	}
	text, err := readFile(parent.space, rel)
	if err != nil {
		a.notify("白板无法读取；%v", err)
		return
	}
	d := openDocument(parent.space, rel, text, false)
	if d.loadErr != nil {
		a.notify("白板无法读取；%v", d.loadErr)
		return
	}
	parent.ed.UnmarkText()
	parent.boardOverlay = d
	d.board.RequestFocus = true
}
func (a *App) closeBoardOverlay(parent *document) {
	d := parent.boardOverlay
	if d == nil {
		return
	}
	if err := d.save(); err != nil {
		a.notify("保存白板失败；%v", err)
		return
	}
	parent.boardOverlay = nil
	parent.imgCache = nil
	parent.ed.ClearImages()
	parent.ed.RequestFocus = true
	a.retireClosedDocuments(d)
}
func encodeBoard(rel string, s *whiteboard.Scene) (string, error) {
	if strings.HasSuffix(strings.ToLower(rel), ".svg") {
		return editor.BoardSVG(s, editor.BoardOptions{})
	}
	return s.Encode()
}
func (d *document) writeWhiteboard(force bool) error {
	if !force {
		if d.conflict {
			return errConflict
		}
		if d.missing {
			return errMissing
		}
		if !d.whiteboardDirty() {
			return nil
		}
	}
	res := saveBoardSnapshot(d.space, d.rel, d.board.Snapshot(), d.diskText, force, d.historyOff, !d.historyStarted)
	res.version = d.boardGen
	d.applyWhiteboardSave(res)
	if res.conflict {
		return errConflict
	}
	if res.missing {
		return errMissing
	}
	return res.err
}

// saveBoardSnapshot owns all I/O and runs on a worker for ordinary autosaves.
// The current scene is never consulted after capturing this snapshot.
func saveBoardSnapshot(sp *space.Space, rel string, s *whiteboard.Scene, expected string, force, historyOff, initial bool) asyncSaveResult {
	r := asyncSaveResult{whiteboard: true}
	canonical, err := s.Encode()
	if err != nil {
		r.err = err
		return r
	}
	r.baseline = sha256.Sum256([]byte(canonical))
	r.text, r.err = encodeBoard(rel, s)
	if r.err != nil {
		return r
	}
	if !force {
		disk, err := readFile(sp, rel)
		switch {
		case isMissingDocFile(sp, rel, err):
			r.missing = true
			return r
		case err != nil:
			r.err = err
			return r
		case disk != expected:
			r.conflict = true
			r.diskNow = disk
			return r
		}
	}
	r.err = writeFile(sp, rel, r.text)
	if r.err != nil {
		return r
	}
	if !historyOff {
		if initial && expected != "" {
			_ = history.Record(sp.Root, rel, expected, time.Now().Add(-time.Second), true)
		}
		_ = history.Record(sp.Root, rel, r.text, time.Now(), false)
		r.historyRecorded = true
	}
	return r
}
func (d *document) applyWhiteboardSave(r asyncSaveResult) {
	switch {
	case r.missing:
		d.missing = true
		d.failedVersion = r.version
	case r.conflict:
		d.conflict = true
		d.diskNow = r.diskNow
		d.failedVersion = r.version
	case r.err != nil:
		d.saveErr = r.err
		d.failedVersion = r.version
	default:
		d.diskText = r.text
		d.boardBase = r.baseline
		if d.boardGen == r.version {
			d.boardDigestValue = r.baseline
			d.boardDigestRev = d.boardGen
			d.boardDigestKnown = true
		}
		d.saveErr = nil
		d.conflict = false
		d.missing = false
		d.diskNow = ""
		d.savedWrites++
		d.justSaved = true
		if r.historyRecorded {
			d.historyStarted = true
		}
	}
}
func (d *document) startWhiteboardSave(post func(func())) {
	s := d.board.Snapshot()
	sp := *d.space
	if d.space.ExternalFiles != nil {
		sp.ExternalFiles = map[string]string{}
		for k, v := range d.space.ExternalFiles {
			sp.ExternalFiles[k] = v
		}
	}
	rel, expected := d.rel, d.diskText
	off, initial := d.historyOff, !d.historyStarted
	gen := d.boardGen
	ch := make(chan asyncSaveResult, 1)
	d.saving = true
	d.saveDone = ch
	go func() {
		r := saveBoardSnapshot(&sp, rel, s, expected, false, off, initial)
		r.version = gen
		ch <- r
		post(func() { d.pollAsyncSave() })
	}()
}
func (a *App) newWhiteboard(dir string) {
	sp := a.state.ActiveSpace()
	if sp == nil || sp.IsExternal() {
		a.notify("请在普通空间中新建白板")
		return
	}
	if !a.flushForTask() {
		return
	}
	s := whiteboard.NewScene()
	text, err := s.Encode()
	if err != nil {
		a.notify("新建白板失败；%v", err)
		return
	}
	rel, err := vault.CreateFile(sp.Root, dir, "未命名白板", ".excalidraw", text)
	if err != nil {
		a.notify("新建白板失败；%v", err)
		return
	}
	a.refreshTree()
	a.open(rel)
	if a.doc != nil && a.doc.board != nil {
		a.doc.board.RequestFocus = true
	}
}
func (a *App) whiteboardView(c *ui.Context, d *document) {
	ui.Column(c.Key(d.viewKey() + "-board")).Grow(1).MinHeight(0).Children(func() {
		a.withDocument(d, func() { a.conflictBar(c) })
		o := a.boardOptions(c, d)
		o.Export = func(format string) { a.exportBoard(d, format) }
		o.History = func() {
			a.activateDocument(d)
			a.openVersions()
		}
		editor.BoardView(c, d.board, o)
	})
}
func (a *App) boardOptions(c *ui.Context, d *document) editor.BoardOptions {
	return a.boardOptionsWith(c.Theme(), d)
}

// boardOptionsWith are a board's options outside a view, as OneBot's
// whiteboard tools edit it; theme may be nil there.
func (a *App) boardOptionsWith(theme *ui.Theme, d *document) editor.BoardOptions {
	o := editor.BoardOptions{Theme: theme, Page: pageStyle(a.paperDark), ReadOnly: d.ed.ReadOnly, OnChange: func() { a.boardChanged(d) }, OpenLink: a.followLink, OpenWiki: func(name string, split bool) { a.withDocument(d, func() { a.openWiki(name) }) }, WikiSuggest: func(q string) []string {
		var names []string
		for _, item := range a.wikiSuggestFor(d, q) {
			names = append(names, item.Name)
		}
		return names
	}, PickImage: func() { a.pickBoardImages(d) }, DropFiles: func(paths []string) { a.insertBoardFiles(d, paths) }, UserLibrary: a.userBoardLibrary, SaveLibrary: a.saveBoardLibrary, ImportLibrary: a.importBoardLibrary, ExportLibrary: a.exportBoardLibrary}
	if a.win != nil {
		o.Dispatch = func(fn func()) { a.win.Update(fn) }
	}
	// Only a board document owns its results' paths; a Markdown document's
	// embedded preview gets no AI tools.
	if d.board != nil && d.loadErr == nil {
		o.AI = a.boardAI(d)
	}
	o.ResolveFile = func(f *whiteboard.FileData) ([]byte, error) {
		rel := path.Clean(path.Join(path.Dir(d.rel), f.Path))
		full, err := vault.Resolve(d.space.Root, rel)
		if err != nil {
			return nil, err
		}
		return readBoardBytes(full, 32<<20)
	}
	return o
}
func readBoardBytes(full string, limit int64) ([]byte, error) {
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		return nil, fmt.Errorf("白板文件超过大小限制")
	}
	return b, err
}
func (a *App) pickBoardImages(d *document) {
	done := func(paths []string) { a.insertBoardFiles(d, paths) }
	if a.pickFiles != nil {
		a.pickFiles(done)
		return
	}
	if a.win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: i18n.T("插入图片"), Multiple: true, Filters: []mygo.FileFilter{{Name: i18n.T("图片"), Extensions: imageExtensions}}})
		if err == nil {
			a.win.Update(func() { done(paths) })
		}
	}()
}
func (a *App) insertBoardFiles(d *document, paths []string) {
	if d == nil || d.board == nil || d.ed.ReadOnly {
		return
	}
	type loaded struct {
		data []byte
		err  error
	}
	work := func() []loaded {
		var files []loaded
		for _, p := range paths {
			data, err := readBoardBytes(p, 32<<20)
			files = append(files, loaded{data, err})
		}
		return files
	}
	done := func(files []loaded) {
		if d.closed.Load() {
			return
		}
		for i, f := range files {
			if f.err != nil {
				a.notify("插入图片失败；%v", f.err)
				continue
			}
			before := d.board.Revision()
			if err := d.board.InsertImage(f.data, whiteboard.Point{300 + float64(i)*25, 220 + float64(i)*25}); err != nil {
				a.notify("插入图片失败；%v", err)
			} else if d.board.Revision() != before {
				a.boardChanged(d)
			}
		}
	}
	if a.win != nil {
		go func() { files := work(); a.win.Update(func() { done(files) }) }()
	} else {
		done(work())
	}
}

type boardLibraryState struct {
	items  []whiteboard.LibraryItem
	loaded bool
	err    error
}

func (a *App) userBoardLibrary() []whiteboard.LibraryItem {
	if a.boardLibrary != nil {
		return a.boardLibrary.items
	}
	a.boardLibrary = &boardLibraryState{}
	full := filepath.Join(a.cfg.ConfigDir, "whiteboard-library.excalidrawlib")
	load := func() ([]whiteboard.LibraryItem, error) {
		data, err := readBoardBytes(full, whiteboard.MaxSceneBytes)
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return whiteboard.DecodeLibrary(string(data))
	}
	done := func(items []whiteboard.LibraryItem, err error) {
		a.boardLibrary.items = items
		a.boardLibrary.err = err
		a.boardLibrary.loaded = true
		if err != nil {
			a.notify("读取白板素材库失败；%v", err)
		}
	}
	if a.win != nil {
		go func() { items, err := load(); a.win.Update(func() { done(items, err) }) }()
	} else {
		done(load())
	}
	return a.boardLibrary.items
}
func (a *App) saveBoardLibrary(items []whiteboard.LibraryItem) {
	text, err := whiteboard.EncodeLibrary(items)
	if err != nil {
		a.notify("保存白板素材库失败；%v", err)
		return
	}
	full := filepath.Join(a.cfg.ConfigDir, "whiteboard-library.excalidrawlib")
	done := func(err error) {
		if err != nil {
			a.notify("保存白板素材库失败；%v", err)
			return
		}
		a.boardLibrary = &boardLibraryState{items: items, loaded: true}
	}
	if a.win != nil {
		go func() { err := fsutil.WriteFileAtomic(full, []byte(text), 0600); a.win.Update(func() { done(err) }) }()
	} else {
		done(fsutil.WriteFileAtomic(full, []byte(text), 0600))
	}
}
func (a *App) importBoardLibrary(done func([]whiteboard.LibraryItem, error)) {
	if a.win == nil {
		return
	}
	go func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: i18n.T("导入素材库"), Filters: []mygo.FileFilter{{Name: "Excalidraw library", Extensions: []string{"excalidrawlib"}}}})
		if err != nil || len(files) == 0 {
			return
		}
		data, err := readBoardBytes(files[0], whiteboard.MaxSceneBytes)
		var items []whiteboard.LibraryItem
		if err == nil {
			items, err = whiteboard.DecodeLibrary(string(data))
		}
		a.win.Update(func() { done(items, err) })
	}()
}
func (a *App) exportBoardLibrary(items []whiteboard.LibraryItem) {
	text, err := whiteboard.EncodeLibrary(items)
	if err != nil {
		a.notify("导出素材库失败；%v", err)
		return
	}
	a.saveBoardExport("whiteboard-library.excalidrawlib", []byte(text))
}
func (a *App) saveBoardExport(name string, data []byte) {
	if a.win == nil {
		return
	}
	go func() {
		full, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, Title: i18n.T("导出白板"), DefaultPath: name})
		if err == nil && full != "" {
			err = fsutil.WriteFileAtomic(full, data, 0644)
		}
		if err != nil {
			a.win.Update(func() { a.notify("导出白板失败；%v", err) })
		}
	}()
}
func (a *App) exportBoard(d *document, format string) {
	if d == nil || d.board == nil {
		return
	}
	scene := d.board.Scene.Clone()
	work := func() ([]byte, error) {
		if format == "canvas" {
			return whiteboard.ExportCanvas(scene)
		}
		if format == "png" {
			return editor.BoardPNG(scene, editor.BoardOptions{})
		}
		text, err := editor.BoardSVG(scene, editor.BoardOptions{})
		return []byte(text), err
	}
	done := func(data []byte, err error) {
		if err != nil {
			a.notify("导出白板失败；%v", err)
			return
		}
		a.saveBoardExport(d.title()+"."+format, data)
	}
	if a.win != nil {
		go func() { data, err := work(); a.win.Update(func() { done(data, err) }) }()
	} else {
		done(work())
	}
}

func (a *App) exportBoardMarkdown(d *document) {
	svg, err := editor.BoardSVG(d.board.Scene, editor.BoardOptions{})
	if err != nil {
		a.notify("导出失败；%v", err)
		return
	}
	url, err := a.writeAssetFor(d, path.Dir(d.rel), d.title()+".excalidraw.svg", []byte(svg))
	if err != nil {
		a.notify("导出失败；%v", err)
		return
	}
	root, name, err := fileLocation(d.space, d.rel)
	if err != nil {
		a.notify("导出失败；%v", err)
		return
	}
	content := "# " + d.title() + "\n\n![白板](" + url + ")\n"
	rel, err := vault.CreateFile(root, path.Dir(name), d.title()+" Export", ".md", content)
	if err != nil {
		a.notify("导出失败；%v", err)
		return
	}
	a.refreshTree()
	a.notify("已导出 %s", rel)
}
