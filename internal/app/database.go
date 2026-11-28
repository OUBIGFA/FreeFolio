package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/database"
	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/editor/dbview"
	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/trashbin"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// databaseHost is what databases need from the window: documents, trash, panes and the shared chrome.
type databaseHost interface {
	activeDocument() *document
	attachRelationHost(sp *space.Space, st *database.Store)
	autosaveEnabled() bool
	backgroundResult(work func() func())
	cancelRenameUnder(spaceID, rel string)
	checkControlWrite(sp *space.Space, rel string) error
	closeOpen(sp *space.Space, rel string)
	currentDir() string
	editorOptionsFor(c *ui.Context, width float32, focus bool, d *document) editor.Options
	findTreeNode(sp *space.Space, rel string) *vault.Node
	flush() bool
	followCopied(fromSpace, fromRel, toSpace, toRel string)
	forgetCopied(spID, rel string)
	forgetPageTree(id string)
	forgetPicks(spID, rel string)
	keepDeleted(spaceID, spaceName, root, rel string) (trashbin.Item, bool)
	keepResolvedImage(owner *document, key imageKey, r resolvedImage)
	motionReduced() bool
	moveOpenDocuments(sp *space.Space, from, to string)
	navigateInPane(d *document, fn func())
	newDatabaseNamed(dir, title string)
	notify(format string, args ...any)
	openIn(sp *space.Space, rel string)
	openSplit(sp *space.Space, rel string, leading bool, vertical bool)
	paperIsDark() bool
	refreshSearch()
	refreshTree()
	relationHost(sp *space.Space, store *database.Store) *appRelationHost
	releasePDFHandles(sp *space.Space, rel string)
	reopenResidentPDFs(sp *space.Space, rel string)
	replaceReferences(old, replacement *document)
	residentDocument(id, rel string) *document
	residentDocuments() []*document
	resolvedImageCached(key imageKey) (resolvedImage, bool)
	saveState()
	spaces() *space.State
	trashBin() *trashbin.Bin
	trashItem(p string) error
	treeOf(sp *space.Space) ([]*vault.Node, error)
	window() *mygo.Window
}

// databaseFeature opens and edits multidimensional databases: their record
// peek, sidebar views and thumbnails, embedded and page-level databases.
type databaseFeature struct {
	host              databaseHost
	dbCatalog         map[string]*databaseCatalog
	dbLoads           map[string]*databaseLoad
	dbPeek            dbPeekState
	pageDBCache       map[string]*pageDBEntry
	embedDBs          map[string]*embedDBEntry
	dbViewListCache   map[string]*sidebarDBMeta
	dbThumbCache      map[string]cachedImage
	dbThumbOrder      []string
	dbViewRename      sidebarDBViewRename
	pagePropsExpanded map[string]bool
}

const untitledDatabase = "未命名数据库"

type dbPeekState struct {
	open           bool
	mode           string // dbview.OpenSidePeek | dbview.OpenCenterPeek
	pendingMode    string
	dbDoc          *document
	recordID       string
	doc            *document
	inlineBody     string
	inlineTargetID string
	propsExpanded  bool
	panelWidth     float32
	lastDocVer     uint64
	resizing       bool
}

type pageDBEntry struct {
	dbRel string
	store *database.Store
	state *dbview.State
}

type sidebarDBView struct {
	ID      string
	Name    string
	Type    database.ViewType
	Primary bool
}

type sidebarDBMeta struct {
	nodeModNano int64
	modTime     time.Time
	size        int64
	views       []sidebarDBView
}

type sidebarDBViewRename struct {
	spaceID string
	dbRel   string
	viewID  string
	draft   string
	pending bool
}

func (d *document) isDatabase() bool {
	return d != nil && d.dbView != nil && d.dbView.Store != nil
}

func databaseSpaceRoot(sp *space.Space, rel string) (spaceRoot, dbRel string) {
	if sp == nil {
		return "", rel
	}
	if sp.IsExternal() {
		if full, ok := sp.ExternalFiles[rel]; ok && full != "" {
			return filepath.Dir(full), filepath.Base(full)
		}
		return "", filepath.Base(rel)
	}
	return sp.Root, filepath.ToSlash(rel)
}

func openDatabaseDocument(sp *space.Space, rel, text string) *document {
	d := &document{space: sp, rel: rel, diskText: text}
	db, err := database.DecodeDatabaseMarkdown(text)
	if err != nil {
		d.loadErr = err
		db = database.NewDefault(vault.Title(rel), database.SourceConfig{Type: database.SourceFolder})
	}
	spaceRoot, dbRel := databaseSpaceRoot(sp, rel)
	store := database.NewStore(spaceRoot, dbRel, db)
	store.MarkSourceText(text)
	if d.loadErr == nil && spaceRoot != "" {
		if _, rerr := store.Refresh(nil); rerr != nil {
			d.saveErr = rerr
		}
	}
	d.dbView = dbview.NewState(store)
	d.dbBaseline = db.Digest()
	d.dbSavedRev = store.Revision()
	d.dbDirtySnapshot = false
	d.ed = editor.FromOwnedDocument(model.New())
	d.initLazyBaseline()
	d.source = false
	return d
}

func (d *document) databaseDigest() [32]byte {
	if !d.isDatabase() {
		return [32]byte{}
	}
	return d.dbView.Store.Database().Digest()
}

func (d *document) databaseDirty() bool {
	if !d.isDatabase() {
		return false
	}
	return d.dbDirtySnapshot || d.databaseDigest() != d.dbBaseline
}

func (d *document) writeDatabase(force bool) error {
	if !d.isDatabase() {
		return nil
	}
	if !d.databaseDirty() && !force {
		d.saveErr = nil
		return nil
	}
	if !force {
		disk, err := readFile(d.space, d.rel)
		switch {
		case isMissingDocFile(d.space, d.rel, err):
			d.missing = true
			return errMissing
		case err != nil:
			d.saveErr = err
			return err
		case disk != d.diskText:
			d.conflict, d.diskNow = true, disk
			return errConflict
		}
	}
	text, err := d.dbView.Store.EncodeMarkdown(time.Now())
	if err == nil {
		err = writeFile(d.space, d.rel, text)
	}
	if err != nil {
		d.saveErr = err
		return err
	}
	before := d.diskText
	d.diskText = text
	d.dbView.Store.MarkSourceText(text)
	d.dbBaseline = d.databaseDigest()
	d.dbSavedRev = d.dbView.Store.Revision()
	d.dbDirtySnapshot = false
	d.saveErr = nil
	d.recordVersion(before)
	d.conflict, d.missing, d.diskNow = false, false, ""
	d.dirtyRev, d.dirtyKnown, d.dirtyNow = d.dbView.Store.Revision(), true, false
	d.dirtyVersion = d.dbView.Version()
	d.justSaved = true
	return nil
}

// newDatabase creates a new amphibious .db.md database in dir of the active
// space and opens it immediately.
func (df *databaseFeature) newDatabase(dir string) {
	df.host.newDatabaseNamed(dir, "")
}

func (df *databaseFeature) liveDocsForSpace(sp *space.Space) []database.LiveDoc {
	if sp == nil {
		return nil
	}
	now := time.Now()
	var out []database.LiveDoc
	for _, res := range df.host.residentDocuments() {
		if res == nil || res.space != sp || res.loadErr != nil {
			continue
		}
		if res.isDatabase() || res.isWhiteboard() || res.isPDF() {
			continue
		}
		if !vault.IsMarkdown(res.rel) {
			continue
		}
		if !res.dirty() && !(df.dbPeek.open && df.dbPeek.doc == res) && res != df.host.activeDocument() {
			continue
		}
		out = append(out, database.LiveDoc{
			Path:      filepath.ToSlash(res.rel),
			Text:      res.text(),
			UpdatedAt: now,
		})
	}
	return out
}

func (df *databaseFeature) refreshOpenDatabases(spaceID string) {
	df.refreshOpenDatabasesExcept(spaceID)
}

func (df *databaseFeature) refreshOpenDatabasesExcept(spaceID string, skip ...*database.Store) {
	sp := df.host.spaces().Get(spaceID)
	if sp == nil {
		return
	}
	live := df.liveDocsForSpace(sp)
	refreshStore := func(st *database.Store) {
		if st == nil {
			return
		}
		if !slices.Contains(skip, st) {
			_, _ = st.Refresh(live)
		}
		st.InvalidateComputed()
	}
	for _, res := range df.host.residentDocuments() {
		if res != nil && res.space == sp && res.loadErr == nil && res.isDatabase() {
			refreshStore(res.dbView.Store)
		}
	}
	if df.dbPeek.open && df.dbPeek.dbDoc != nil && df.dbPeek.dbDoc.space == sp && df.dbPeek.dbDoc.isDatabase() {
		refreshStore(df.dbPeek.dbDoc.dbView.Store)
	}
	for k, entry := range df.pageDBCache {
		if entry != nil && entry.store != nil && strings.HasPrefix(k, spaceID+"\x00") {
			refreshStore(entry.store)
		}
	}
	for k, entry := range df.embedDBs {
		if entry != nil && entry.store != nil && strings.HasPrefix(k, spaceID+"\x00") {
			refreshStore(entry.store)
		}
	}
}

type appRelationHost struct {
	app *App
	sp  *space.Space
	src *database.Store
}

func (h *appRelationHost) resolveTargetRel(spaceRoot, targetDB string) string {
	rel := filepath.ToSlash(strings.TrimSpace(targetDB))
	if rel == "" {
		return ""
	}
	if vault.IsDatabase(rel) {
		return rel
	}
	if infos := h.app.databases.databaseCatalogFor(h.sp); infos != nil {
		for _, info := range infos {
			if info.ID == targetDB || info.Title == targetDB || strings.TrimSuffix(info.RelPath, vault.ExtDatabase) == rel {
				return info.RelPath
			}
		}
	}
	return rel
}

func (h *appRelationHost) findOpenStore(targetDB string) (*database.Store, *document, string) {
	if h == nil || h.app == nil || h.sp == nil {
		return nil, nil, ""
	}
	targetClean := filepath.ToSlash(strings.TrimSpace(targetDB))
	for _, res := range h.app.residentDocuments() {
		if res != nil && res.space == h.sp && res.loadErr == nil && res.isDatabase() {
			db := res.dbView.Store.Database()
			if filepath.ToSlash(res.rel) == targetClean ||
				(db != nil && (db.ID == targetDB || db.Title == targetDB)) ||
				strings.TrimSuffix(filepath.ToSlash(res.rel), vault.ExtDatabase) == targetClean {
				return res.dbView.Store, res, res.rel
			}
		}
	}
	for k, entry := range h.app.databases.pageDBCache {
		if entry != nil && entry.store != nil && strings.HasPrefix(k, h.sp.ID+"\x00") {
			db := entry.store.Database()
			if filepath.ToSlash(entry.dbRel) == targetClean ||
				(db != nil && (db.ID == targetDB || db.Title == targetDB)) ||
				strings.TrimSuffix(filepath.ToSlash(entry.dbRel), vault.ExtDatabase) == targetClean {
				return entry.store, nil, entry.dbRel
			}
		}
	}
	for k, entry := range h.app.databases.embedDBs {
		if entry != nil && entry.store != nil && strings.HasPrefix(k, h.sp.ID+"\x00") {
			db := entry.store.Database()
			if filepath.ToSlash(entry.dbRel) == targetClean ||
				(db != nil && (db.ID == targetDB || db.Title == targetDB)) ||
				strings.TrimSuffix(filepath.ToSlash(entry.dbRel), vault.ExtDatabase) == targetClean {
				return entry.store, nil, entry.dbRel
			}
		}
	}
	return nil, nil, ""
}

func (h *appRelationHost) ResolveDatabase(spaceRoot, targetDB string) (*database.Database, []*database.Record, error) {
	if st, _, _ := h.findOpenStore(targetDB); st != nil {
		return st.Database(), st.Records(), nil
	}
	targetRel := h.resolveTargetRel(spaceRoot, targetDB)
	if targetRel == "" {
		return nil, nil, fmt.Errorf("database: relation target %q not found", targetDB)
	}
	if h.app.win != nil {
		loaded := h.app.databases.cachedDatabase(h.sp, targetRel)
		if loaded.pending {
			return nil, nil, errDatabaseLoading
		}
		if loaded.err != nil {
			return nil, nil, loaded.err
		}
		return loaded.store.Database(), loaded.store.Records(), nil
	}
	extStore, err := database.Open(spaceRoot, targetRel, h.app.databases.liveDocsForSpace(h.sp))
	if err != nil {
		return nil, nil, err
	}
	h.app.attachRelationHost(h.sp, extStore)
	return extStore.Database(), extStore.Records(), nil
}

func (h *appRelationHost) SyncExternalRelation(spaceRoot, targetDB, syncedPropID, sourceRef string, addTargets, removeTargets []string) ([]string, error) {
	if extStore, extDoc, _ := h.findOpenStore(targetDB); extStore != nil {
		affected, err := extStore.ApplySymmetricRelationDelta(syncedPropID, sourceRef, addTargets, removeTargets)
		if err == nil && len(affected) > 0 {
			h.app.databases.flushDatabaseEditsToDocs(h.sp, extDoc, extStore, affected)
			h.app.databases.refreshOpenDatabasesExcept(h.sp.ID, h.src, extStore)
		}
		return affected, err
	}
	targetRel := h.resolveTargetRel(spaceRoot, targetDB)
	if targetRel == "" || spaceRoot == "" {
		return nil, nil
	}
	extStore, err := database.Open(spaceRoot, targetRel, h.app.databases.liveDocsForSpace(h.sp))
	if err != nil {
		return nil, err
	}
	affected, err := extStore.ApplySymmetricRelationDelta(syncedPropID, sourceRef, addTargets, removeTargets)
	if err == nil {
		if len(affected) > 0 {
			h.app.databases.flushDatabaseEditsToDocs(h.sp, nil, extStore, affected)
		}
		if !h.sp.IsExternal() {
			if encoded, encErr := extStore.EncodeMarkdown(time.Now()); encErr == nil {
				if disk, rErr := readFile(h.sp, targetRel); rErr == nil && disk != encoded {
					_ = writeFile(h.sp, targetRel, encoded)
				}
			}
		}
		h.app.databases.refreshOpenDatabasesExcept(h.sp.ID, h.src)
	}
	return affected, err
}

func (df *databaseFeature) syncDatabaseEditsToDocs(sp *space.Space, dbDoc *document, store *database.Store, affectedPaths []string) {
	if sp == nil || store == nil {
		return
	}
	df.flushDatabaseEditsToDocs(sp, dbDoc, store, affectedPaths)
	df.refreshOpenDatabases(sp.ID)
	df.host.refreshSearch()
}

func (df *databaseFeature) flushDatabaseEditsToDocs(sp *space.Space, dbDoc *document, store *database.Store, affectedPaths []string) error {
	var failures error
	if sp == nil || store == nil {
		return nil
	}
	if dbDoc != nil {
		dbDoc.dbDirtySnapshot = true
	}
	db := store.Database()
	var schema []database.Property
	if db != nil {
		schema = db.Properties
	}
	for _, rel := range affectedPaths {
		rel = filepath.ToSlash(rel)
		rec := store.RecordByPath(rel)
		if rec == nil {
			continue
		}
		if res := df.host.residentDocument(sp.ID, rel); res != nil && res.loadErr == nil && !res.isDatabase() && !res.isWhiteboard() && !res.isPDF() && res.ed != nil {
			if res.ed.ReadOnly {
				failures = errors.Join(failures, fmt.Errorf("%s: document is read-only", rel))
				continue
			}
			if err := df.host.checkControlWrite(sp, rel); err != nil {
				failures = errors.Join(failures, err)
				continue
			}
			if !res.ed.ReadOnly {
				wasDirty := res.dirty()
				var fm *database.FrontmatterDoc
				if strings.TrimSpace(res.ed.Doc.Frontmatter) != "" {
					fm, _, _, _ = database.SplitFrontmatter("---\n" + res.ed.Doc.Frontmatter + "\n---\n")
				} else {
					fm = &database.FrontmatterDoc{}
				}
				database.WritePropsToFrontmatter(schema, fm, rec.Props)
				res.ed.Engine.SetFrontmatter(fm.Format())

				if rec.Title != "" && len(res.ed.Doc.Blocks) > 0 &&
					res.ed.Doc.Blocks[0].Kind == "heading" &&
					res.ed.Doc.Blocks[0].Level == 1 &&
					res.ed.Doc.Blocks[0].Text() != rec.Title {
					blk := res.ed.Doc.Blocks[0]
					anchor, caret := res.ed.Engine.Anchor, res.ed.Engine.Caret
					res.ed.Engine.ReplaceBlockRuns(blk.ID, []model.Run{{Text: rec.Title}}, anchor, caret, res.ed.Engine.Typing, res.ed.Engine.Explicit)
				}
				if (df.host.autosaveEnabled() || !wasDirty) && !res.ed.Composing() {
					if err := res.write(false); err != nil {
						failures = errors.Join(failures, err)
						df.host.notify("保存数据库记录失败；%v", err)
					}
				}
				if df.dbPeek.open && df.dbPeek.doc == res {
					df.dbPeek.lastDocVer = res.ed.Version()
				}
			}
			continue
		}
		if err := store.FlushRecord(rec.ID); err != nil {
			failures = errors.Join(failures, err)
			df.host.notify("保存失败，%v", err)
		}
	}
	if dbDoc == nil && !sp.IsExternal() {
		df.flushCachedDatabaseMarkdown(sp, store)
	}
	return failures
}

func (df *databaseFeature) flushCachedDatabaseMarkdown(sp *space.Space, store *database.Store) {
	if sp == nil || sp.IsExternal() || store == nil {
		return
	}
	flushRel := func(dbRel string) {
		encoded, err := store.EncodeMarkdown(time.Now())
		if err != nil {
			return
		}
		disk, err := readFile(sp, dbRel)
		if err == nil {
			err = store.CheckSourceText(disk)
		}
		if err != nil {
			df.host.notify("保存数据库失败；%v", err)
			return
		}
		if disk == encoded {
			return
		}
		if err := writeFile(sp, dbRel, encoded); err != nil {
			df.host.notify("保存数据库失败；%v", err)
		} else {
			store.MarkSourceText(encoded)
		}
	}
	for _, entry := range df.pageDBCache {
		if entry != nil && entry.store == store && entry.dbRel != "" {
			flushRel(entry.dbRel)
			return
		}
	}
	for _, entry := range df.embedDBs {
		if entry != nil && entry.store == store && entry.dbRel != "" {
			flushRel(entry.dbRel)
			return
		}
	}
}

func (df *databaseFeature) followDatabaseRecordMove(sp *space.Space, from, to string) {
	if sp == nil || from == to {
		return
	}
	df.invalidatePageDBCache(sp.ID)

	if vault.IsDatabase(from) && vault.IsDatabase(to) {
		oldDir := strings.TrimSuffix(from, vault.ExtDatabase)
		newDir := strings.TrimSuffix(to, vault.ExtDatabase)
		if oldDir != "" && newDir != "" && oldDir != newDir {
			if fullOld, err := resolveFile(sp, oldDir); err == nil {
				if info, serr := os.Stat(fullOld); serr == nil && info.IsDir() {
					if fullNew, nerr := resolveFile(sp, newDir); nerr == nil {
						if _, existErr := os.Stat(fullNew); errors.Is(existErr, fs.ErrNotExist) {
							df.host.releasePDFHandles(sp, oldDir)
							var movedDir string
							var mvErr error
							if parentDir(oldDir) == parentDir(newDir) {
								movedDir, mvErr = vault.Rename(sp.Root, oldDir, path.Base(newDir))
							} else {
								movedDir, mvErr = vault.Move(sp.Root, oldDir, parentDir(newDir))
							}
							if mvErr == nil && movedDir != "" {
								df.host.spaces().Renamed(sp.ID, oldDir, movedDir)
								df.followOpenDocMove(sp, oldDir, movedDir)
								df.host.followCopied(sp.ID, oldDir, sp.ID, movedDir)
								df.host.saveState()
							} else {
								df.host.reopenResidentPDFs(sp, oldDir)
							}
						}
					}
				}
			}
		}
		for _, res := range df.host.residentDocuments() {
			if res != nil && res.space == sp && res.isDatabase() && res.rel == to {
				db := res.dbView.Store.Database()
				oldTitle := vault.Title(from)
				newTitle := vault.Title(to)
				if db != nil && db.Title == oldTitle && newTitle != "" && newTitle != db.Title {
					_ = res.dbView.Store.SetTitle(newTitle)
				}
				spaceRoot, dbRel := databaseSpaceRoot(sp, to)
				fresh := database.NewStore(spaceRoot, dbRel, res.dbView.Store.Database())
				_, _ = fresh.Refresh(df.liveDocsForSpace(sp))
				res.dbView.Store = fresh
				res.dbDirtySnapshot = true
			}
		}
		return
	}

	for _, res := range df.host.residentDocuments() {
		if res != nil && res.space == sp && res.loadErr == nil && res.isDatabase() {
			res.dbView.Store.RenameRecordPath(from, to)
			_, _ = res.dbView.Store.Refresh(df.liveDocsForSpace(sp))
			res.dbDirtySnapshot = true
		}
	}
	if df.dbPeek.open && df.dbPeek.dbDoc != nil && df.dbPeek.dbDoc.space == sp && df.dbPeek.doc != nil && under(df.dbPeek.doc.rel, from) {
		relTail := df.dbPeek.doc.rel[len(from):]
		df.dbPeek.doc.rel = to + relTail
		if df.dbPeek.recordID == from {
			df.dbPeek.recordID = to + relTail
		}
	}
}

func (df *databaseFeature) followOpenDocMove(sp *space.Space, from, to string) {
	df.host.moveOpenDocuments(sp, from, to)
}

const maxDBThumbCache = 128

func (df *databaseFeature) clearDBThumbCache(spaceID string) {
	if len(df.dbThumbCache) == 0 {
		df.dbThumbOrder = nil
		return
	}
	if spaceID == "" {
		for _, c := range df.dbThumbCache {
			if c.resource.Bitmap != nil {
				c.resource.Bitmap.ReleasePixels()
			}
		}
		clear(df.dbThumbCache)
		df.dbThumbOrder = nil
		return
	}
	prefix := spaceID + "\x00"
	for k, c := range df.dbThumbCache {
		if strings.HasPrefix(k, prefix) {
			if c.resource.Bitmap != nil {
				c.resource.Bitmap.ReleasePixels()
			}
			delete(df.dbThumbCache, k)
		}
	}
	df.dbThumbOrder = slices.DeleteFunc(df.dbThumbOrder, func(k string) bool {
		return strings.HasPrefix(k, prefix)
	})
}

func (df *databaseFeature) invalidatePageDBCache(spaceID string) {
	for id := range df.dbCatalog {
		if spaceID == "" || id == spaceID {
			delete(df.dbCatalog, id)
		}
	}
	for key := range df.dbLoads {
		if spaceID == "" || strings.HasPrefix(key, spaceID+"\x00") {
			delete(df.dbLoads, key)
		}
	}
	df.clearDBThumbCache(spaceID)
	if spaceID == "" {
		if df.pageDBCache != nil {
			clear(df.pageDBCache)
		}
		if df.embedDBs != nil {
			clear(df.embedDBs)
		}
		if df.dbViewListCache != nil {
			clear(df.dbViewListCache)
		}
		return
	}
	prefix := spaceID + "\x00"
	for k := range df.pageDBCache {
		if strings.HasPrefix(k, prefix) {
			delete(df.pageDBCache, k)
		}
	}
	for k := range df.embedDBs {
		if strings.HasPrefix(k, prefix) {
			delete(df.embedDBs, k)
		}
	}
	for k := range df.dbViewListCache {
		if strings.HasPrefix(k, prefix) {
			delete(df.dbViewListCache, k)
		}
	}
}

func (df *databaseFeature) trashDatabaseBackingPath(sp *space.Space, rel string) bool {
	if sp == nil || sp.IsExternal() || rel == "" {
		return false
	}
	if df.host.autosaveEnabled() && !df.host.flush() {
		return false
	}
	p, err := resolveFile(sp, rel)
	if err != nil {
		df.host.notify("将 %s 移到废纸篓失败，%v", path.Base(rel), err)
		return false
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return true
	}
	df.host.cancelRenameUnder(sp.ID, rel)
	kept, ok := df.host.keepDeleted(sp.ID, sp.Name, sp.Root, rel)
	if !ok {
		return false
	}
	df.host.releasePDFHandles(sp, rel)
	if err := df.host.trashItem(p); err != nil {
		df.host.reopenResidentPDFs(sp, rel)
		if ok {
			df.host.trashBin().Forget(kept)
		}
		df.host.notify("将 %s 移到废纸篓失败，%v", path.Base(rel), err)
		return false
	}
	for _, d := range df.host.residentDocuments() {
		if d.space == sp && within(d.rel, rel) {
			if d == df.dbPeek.doc {
				df.closeRecordPeek()
			} else {
				df.host.closeOpen(sp, d.rel)
			}
			df.host.replaceReferences(d, nil)
		}
	}
	df.host.forgetPicks(sp.ID, rel)
	df.host.forgetCopied(sp.ID, rel)
	df.host.spaces().Forget(sp.ID, rel)
	df.host.forgetPageTree(sp.ID)
	df.host.saveState()
	return true
}

func (df *databaseFeature) databaseOptionsFor(c *ui.Context, d *document) dbview.Options {
	if d == nil || !d.isDatabase() {
		return dbview.Options{}
	}
	return df.databaseOptionsForStore(c, d.space, d.dbView.Store, d, d)
}

func (df *databaseFeature) databaseOptionsForStore(c *ui.Context, sp *space.Space, store *database.Store, dbDoc, hostDoc *document) dbview.Options {
	var now time.Time
	if c != nil {
		now = c.Now()
	} else {
		now = time.Now()
	}
	ro := sp == nil || sp.IsExternal() || hostDoc != nil && hostDoc.ed != nil && hostDoc.ed.ReadOnly || dbDoc != nil && dbDoc.ed != nil && dbDoc.ed.ReadOnly
	if !ro && store != nil && !store.HasRelationResolver() {
		host := df.host.relationHost(sp, store)
		store.SetRelationResolver(host)
		store.SetRelationSyncer(host)
	}
	return dbview.Options{
		ReadOnly: ro,
		Now:      now,
		ListSpaceDBs: func() []database.SpaceDBInfo {
			if sp == nil || sp.IsExternal() || sp.Root == "" {
				return nil
			}
			return df.databaseCatalogFor(sp)
		},
		LoadImage: func(recPath, imgURL string) *ui.Bitmap {
			return df.loadDatabaseRecordImage(sp, dbDoc, hostDoc, recPath, imgURL)
		},
		OnChanged: func(affected []string) {
			df.syncDatabaseEditsToDocs(sp, dbDoc, store, affected)
		},
		OnAfterCreate: func(rec *database.Record) {
			if dbDoc != nil {
				dbDoc.dbDirtySnapshot = true
			} else if sp != nil {
				df.flushCachedDatabaseMarkdown(sp, store)
			}
			df.invalidatePageDBCache(spaceIDOf(sp))
			df.host.refreshTree()
			df.host.refreshSearch()
		},
		OnOpenRecord: func(rec *database.Record, mode string) {
			df.openRecordPeek(dbDoc, store, sp, rec, mode)
		},
		OnOpenRelationTarget: func(targetDB, ref string) {
			if sp == nil {
				return
			}
			_, tRecs, rerr := store.ResolveTargetDatabase(targetDB)
			if rerr == nil {
				if tr := database.MatchRelationRecord(tRecs, ref); tr != nil {
					if targetDB == "" || (dbDoc != nil && filepath.ToSlash(targetDB) == filepath.ToSlash(dbDoc.rel)) {
						df.openRecordPeek(dbDoc, store, sp, tr, dbview.OpenSidePeek)
						return
					}
					if tr.Path != "" {
						df.host.openIn(sp, tr.Path)
						return
					}
				}
			}
			if !strings.HasSuffix(ref, ".md") {
				cand := filepath.ToSlash(strings.TrimSpace(ref) + ".md")
				if cand != ".md" {
					df.host.openIn(sp, cand)
				}
			}
		},
		OnTrashRecords: func(backingPaths []string) {
			if sp == nil || sp.IsExternal() {
				return
			}
			anyDeleted := false
			for _, rel := range backingPaths {
				if df.trashDatabaseBackingPath(sp, rel) {
					anyDeleted = true
				}
			}
			if df.dbPeek.open && df.dbPeek.dbDoc == dbDoc && dbDoc != nil {
				if store.Record(df.dbPeek.recordID) == nil {
					df.closeRecordPeek()
				}
			}
			if dbDoc != nil {
				dbDoc.dbDirtySnapshot = true
			}
			if anyDeleted {
				df.host.refreshTree()
				df.host.refreshSearch()
			}
			df.refreshOpenDatabases(spaceIDOf(sp))
		},
		OnOpenURL: func(rawURL string) {
			if rawURL != "" && df.host.window() != nil {
				mygo.Shell.OpenExternal(rawURL)
			}
		},
		OnNotify: func(msg string) {
			df.host.notify("%s", msg)
		},
	}
}

func spaceIDOf(sp *space.Space) string {
	if sp == nil {
		return ""
	}
	return sp.ID
}

func (df *databaseFeature) loadDatabaseRecordImage(sp *space.Space, dbDoc, hostDoc *document, recPath, imgURL string) *ui.Bitmap {
	imgURL = strings.TrimSpace(imgURL)
	if imgURL == "" || sp == nil || sp.Root == "" {
		return nil
	}
	if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") || strings.HasPrefix(imgURL, "data:") {
		return nil
	}

	rel := filepath.ToSlash(recPath)
	if rel == "" && dbDoc != nil {
		rel = filepath.ToSlash(dbDoc.rel)
	}

	owner := dbDoc
	if owner == nil {
		owner = hostDoc
	}
	if owner == nil {
		owner = df.host.activeDocument()
	}
	if df.host.window() != nil {
		return df.databaseImage(sp, rel, imgURL)
	}
	resKey := imageKey{doc: owner, url: "db:" + sp.ID + "\x00" + rel + "\x00" + imgURL}
	resolvedEntry, cached := df.host.resolvedImageCached(resKey)
	if !cached {
		var full string
		var err error
		if rel != "" {
			full, err = resolveSpaceImage(sp, rel, imgURL)
		} else {
			full, err = vault.Resolve(sp.Root, filepath.ToSlash(imgURL))
		}
		if err == nil && full != "" {
			if relToRoot, relErr := filepath.Rel(sp.Root, full); relErr == nil && filepath.IsLocal(relToRoot) && relToRoot != "." {
				if resolved, resErr := vault.Resolve(sp.Root, filepath.ToSlash(relToRoot)); resErr == nil {
					if info, stErr := os.Stat(resolved); stErr == nil && !info.IsDir() {
						resolvedEntry = resolvedImage{full: resolved}
					} else {
						resolvedEntry = resolvedImage{err: errMissing}
					}
				} else {
					resolvedEntry = resolvedImage{err: resErr}
				}
			} else {
				resolvedEntry = resolvedImage{err: vault.ErrOutside}
			}
		} else {
			if err == nil {
				err = errMissing
			}
			resolvedEntry = resolvedImage{err: err}
		}
		df.host.keepResolvedImage(owner, resKey, resolvedEntry)
	}
	full := resolvedEntry.full
	if resolvedEntry.err != nil || full == "" {
		return nil
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return nil
	}

	modNano, size := info.ModTime().UnixNano(), info.Size()
	if dbDoc != nil {
		if dbDoc.closed.Load() {
			return nil
		}
		if dbDoc.imgCache == nil {
			dbDoc.imgCache = make(map[string]cachedImage)
		}
		cacheKey := "dbthumb:" + full
		if c, ok := dbDoc.imgCache[cacheKey]; ok && c.modNano == modNano && c.size == size {
			return c.resource.Bitmap
		}
		res := decodeLocalImageThumb(full)
		if res.Bitmap != nil {
			res.LoadedAt = time.Now()
		}
		dbDoc.imgCache[cacheKey] = cachedImage{
			modNano:  modNano,
			size:     size,
			resource: res,
		}
		return res.Bitmap
	}

	thumbKey := sp.ID + "\x00" + full
	if c, ok := df.dbThumbCache[thumbKey]; ok && c.modNano == modNano && c.size == size {
		if idx := slices.Index(df.dbThumbOrder, thumbKey); idx >= 0 {
			df.dbThumbOrder = slices.Delete(df.dbThumbOrder, idx, idx+1)
			df.dbThumbOrder = append(df.dbThumbOrder, thumbKey)
		}
		return c.resource.Bitmap
	}
	res := decodeLocalImageThumb(full)
	if res.Bitmap != nil {
		res.LoadedAt = time.Now()
	}
	if df.dbThumbCache == nil {
		df.dbThumbCache = make(map[string]cachedImage, maxDBThumbCache)
	}
	if prev, ok := df.dbThumbCache[thumbKey]; ok && prev.resource.Bitmap != nil && prev.resource.Bitmap != res.Bitmap {
		prev.resource.Bitmap.ReleasePixels()
	}
	df.dbThumbCache[thumbKey] = cachedImage{
		modNano:  modNano,
		size:     size,
		resource: res,
	}
	if idx := slices.Index(df.dbThumbOrder, thumbKey); idx >= 0 {
		df.dbThumbOrder = slices.Delete(df.dbThumbOrder, idx, idx+1)
	}
	df.dbThumbOrder = append(df.dbThumbOrder, thumbKey)
	for len(df.dbThumbCache) > maxDBThumbCache && len(df.dbThumbOrder) > 0 {
		oldest := df.dbThumbOrder[0]
		df.dbThumbOrder = df.dbThumbOrder[1:]
		if old, ok := df.dbThumbCache[oldest]; ok {
			if old.resource.Bitmap != nil {
				old.resource.Bitmap.ReleasePixels()
			}
			delete(df.dbThumbCache, oldest)
		}
	}
	return res.Bitmap
}

func (df *databaseFeature) openRecordPeek(dbDoc *document, store *database.Store, sp *space.Space, rec *database.Record, mode string) {
	if rec == nil || sp == nil {
		return
	}
	if mode == "" {
		mode = dbview.OpenSidePeek
	}
	if dbDoc == nil && rec.Path != "" {
		df.closeRecordPeek()
		df.host.openIn(sp, rec.Path)
		return
	}
	if mode == dbview.OpenFullPage {
		targetPath := rec.Path
		df.closeRecordPeek()
		if targetPath != "" {
			df.host.openIn(sp, targetPath)
		}
		return
	}

	// Switching records: detach previous peek doc cleanly without leaking bitmaps.
	if df.dbPeek.doc != nil && df.dbPeek.doc.rel != rec.Path {
		df.detachPeekDoc()
	}

	if !df.dbPeek.open || df.dbPeek.recordID != rec.ID {
		df.dbPeek.propsExpanded = false
	}
	df.dbPeek.open = true
	df.dbPeek.mode = mode
	df.dbPeek.dbDoc = dbDoc
	df.dbPeek.recordID = rec.ID

	if rec.Path == "" {
		df.dbPeek.doc = nil
		df.dbPeek.inlineBody = rec.Body
		df.dbPeek.inlineTargetID = rec.ID
		df.dbPeek.lastDocVer = 0
		return
	}

	if df.dbPeek.doc == nil || df.dbPeek.doc.space != sp || df.dbPeek.doc.rel != rec.Path {
		if existing := df.host.residentDocument(sp.ID, rec.Path); existing != nil && existing.loadErr == nil {
			df.dbPeek.doc = existing
		} else {
			text, err := readFile(sp, rec.Path)
			if err != nil {
				df.host.notify("打开记录失败，%v", err)
				df.closeRecordPeek()
				return
			}
			doc := openDocument(sp, rec.Path, text, false)
			df.dbPeek.doc = doc
		}
	}
	if df.dbPeek.doc != nil && df.dbPeek.doc.ed != nil {
		df.dbPeek.lastDocVer = df.dbPeek.doc.ed.Version()
		if df.dbPeek.doc.loadErr == nil {
			df.dbPeek.doc.ed.RequestFocus = true
		}
	}
}

func (df *databaseFeature) detachPeekDoc() {
	prev := df.dbPeek.doc
	df.dbPeek.doc = nil
	df.dbPeek.lastDocVer = 0
	if prev == nil {
		return
	}
	if df.host.autosaveEnabled() && prev.dirty() && prev.loadErr == nil {
		_ = prev.save()
	}
	for _, r := range df.host.residentDocuments() {
		if r == prev {
			return
		}
	}
	prev.close()
}

func (df *databaseFeature) closeRecordPeek() {
	if !df.dbPeek.open && df.dbPeek.doc == nil {
		return
	}
	df.detachPeekDoc()
	df.dbPeek.open = false
	df.dbPeek.mode = ""
	df.dbPeek.pendingMode = ""
	df.dbPeek.dbDoc = nil
	df.dbPeek.recordID = ""
	df.dbPeek.inlineBody = ""
	df.dbPeek.inlineTargetID = ""
	df.dbPeek.propsExpanded = false
	df.dbPeek.resizing = false
}

func (df *databaseFeature) applyPendingDatabasePeekMode() {
	if df.dbPeek.pendingMode == "" {
		return
	}
	df.dbPeek.mode = df.dbPeek.pendingMode
	df.dbPeek.pendingMode = ""
}

func (df *databaseFeature) syncPeekBodyEdits(c *ui.Context) {
	if !df.dbPeek.open || df.dbPeek.doc == nil {
		return
	}
	pd := df.dbPeek.doc
	if pd.loadErr != nil || pd.ed == nil {
		return
	}
	if ver := pd.ed.Version(); ver != df.dbPeek.lastDocVer {
		df.dbPeek.lastDocVer = ver
		if pd.space != nil {
			df.refreshOpenDatabases(pd.space.ID)
		}
	}
	if pd.justSaved {
		pd.justSaved = false
		if pd.space != nil {
			df.refreshOpenDatabases(pd.space.ID)
		}
		df.host.refreshSearch()
	}
}

func (df *databaseFeature) databaseWorkspace(c *ui.Context, d *document, cardWidth float32) {
	if d == nil || !d.isDatabase() {
		return
	}
	opts := df.databaseOptionsFor(c, d)
	opts.HideViewTabs = true
	peekOpen := df.dbPeek.open && df.dbPeek.dbDoc == d && df.dbPeek.mode == dbview.OpenSidePeek
	if peekOpen {
		df.syncPeekBodyEdits(c)
	}

	if df.dbPeek.panelWidth <= 0 {
		df.dbPeek.panelWidth = min(float32(500), max(float32(340), cardWidth*0.44))
	}
	maxSide := max(float32(280), cardWidth-280)
	minSide := min(maxSide, float32(300))
	sideW := min(maxSide, max(minSide, df.dbPeek.panelWidth))

	peekSpring := ui.Animation{Spring: ui.SpringOptions{Duration: 240 * time.Millisecond, Bounce: 0.03}}

	ui.Row(c.Key(d.viewKey() + ":ws")).Grow(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Clip().Children(func() {
		ui.Column(c.Key(d.viewKey() + ":main")).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			dbview.View(c.Key(d.viewKey()), d.dbView, opts)
		})

		if !peekOpen {
			df.dbPeek.resizing = false
			return
		}

		t := c.Theme()
		div := ui.Box(c.Key("db-peek-divider")).Width(6).HeightPercent(100).Center().Focusable().Role(ui.RoleSplitter).
			Label(i18n.T("调整预览面板宽度")).Tooltip(i18n.T("调整预览面板宽度")).Cursor(ui.CursorResizeEW)
		dx, _, held := div.Dragged()
		df.dbPeek.resizing = held
		if held && dx != 0 {
			df.dbPeek.panelWidth = min(maxSide, max(minSide, sideW-dx))
			c.After(0)
		}
		active := held || div.Hovered()
		lineCol := t.Border
		if active {
			lineCol = t.Accent.Alpha(0.55)
		}
		div.Children(func() {
			ui.Box(c).Width(1).HeightPercent(100).Background(lineCol)
		})

		dt := documentTheme(df.host.paperIsDark())
		drawer := ui.Themed(c.Key("db-side-peek"), dt).Width(sideW).Shrink(0).MinHeight(0).
			BorderWidth(0, 0, 0, 1).BorderColor(dt.Border).
			Shadow(-6, 0, 20, -4, black.Alpha(0.08)).Clip()
		if !df.host.motionReduced() {
			drawer.Transition(ui.ElementTransition{
				Animation:    &peekSpring,
				Width:        !df.dbPeek.resizing,
				ExitDuration: 150 * time.Millisecond,
				ExitEase:     ui.EaseIn,
				Enter:        &ui.Motion{X: 24, Opacity: 0.88},
				Exit:         &ui.Motion{X: 24, Collapse: true, Opacity: 0},
			})
		}
		drawer.Children(func() {
			df.databasePeekContent(c, d, sideW)
		})
	})
}

func (df *databaseFeature) databasePeekModal(c *ui.Context) {
	if !df.dbPeek.open || df.dbPeek.mode != dbview.OpenCenterPeek || df.dbPeek.dbDoc == nil {
		return
	}
	d := df.dbPeek.dbDoc
	if !d.isDatabase() {
		df.closeRecordPeek()
		return
	}
	df.syncPeekBodyEdits(c)

	open := true
	dt := documentTheme(df.host.paperIsDark())
	modalSpring := ui.Animation{Spring: ui.SpringOptions{Duration: 220 * time.Millisecond, Bounce: 0.04}}
	ui.DialogBase(c.Key("db-center-peek"), &open, func(backdrop, panel ui.Element) {
		backdrop.Background(black.Alpha(0.38)).Padding(28, 32)
		panel.Width(760).MaxWidthPercent(90).HeightPercent(86).MinHeight(360).
			Background(dt.Background).Radius(14).
			Border(1, dt.Border).
			Shadow(0, 20, 56, -6, black.Alpha(0.28)).Clip()
		if !df.host.motionReduced() {
			backdrop.Transition(ui.ElementTransition{
				Duration: 150 * time.Millisecond,
				Enter:    &ui.Motion{Opacity: 0},
				Exit:     &ui.Motion{Opacity: 0},
			})
			panel.Transition(ui.ElementTransition{
				Animation:    &modalSpring,
				ExitDuration: 130 * time.Millisecond,
				Enter:        &ui.Motion{Y: 12, Scale: 0.96, Opacity: 0.85},
				Exit:         &ui.Motion{Y: 10, Scale: 0.97, Opacity: 0},
			})
		}
		panel.Children(func() {
			ui.Themed(c.Key("db-center-peek-theme"), dt).Grow(1).MinWidth(0).MinHeight(0).
				Background(ui.Transparent).Children(func() {
				df.databasePeekContent(c, d, 760)
			})
		})
	})
	if !open {
		df.closeRecordPeek()
	}
}

func (df *databaseFeature) databasePeekContent(c *ui.Context, dbDoc *document, width float32) {
	if dbDoc == nil || !dbDoc.isDatabase() {
		return
	}
	store := dbDoc.dbView.Store
	rec := store.Record(df.dbPeek.recordID)
	if rec == nil {
		df.closeRecordPeek()
		return
	}
	t := c.Theme()
	opts := df.databaseOptionsFor(c, dbDoc)

	// Peek header bar with mode switcher (Side / Center / Full Page) & Close.
	ui.Row(c.Key("peek-header")).Height(40).Padding(0, 12, 0, 16).Gap(6).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		title := rec.Title
		if title == "" {
			title = vault.Title(rec.Path)
		}
		if title == "" {
			title = i18n.T("无标题")
		}
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)

		if iconButton(c, "peek-side", iconSidebar, i18n.T("侧边预览"), i18n.T("侧边预览"), df.dbPeek.mode == dbview.OpenSidePeek).Size(26, 26).Clicked() {
			if df.dbPeek.mode != dbview.OpenSidePeek {
				df.dbPeek.pendingMode = dbview.OpenSidePeek
				c.After(0)
			}
		}
		if iconButton(c, "peek-center", iconChevronsUpDown, i18n.T("居中弹窗"), i18n.T("居中弹窗"), df.dbPeek.mode == dbview.OpenCenterPeek).Size(26, 26).Clicked() {
			if df.dbPeek.mode != dbview.OpenCenterPeek {
				df.dbPeek.pendingMode = dbview.OpenCenterPeek
				c.After(0)
			}
		}
		if rec.Path != "" {
			if iconButton(c, "peek-full", iconMaximize2, i18n.T("打开为整页"), i18n.T("打开为整页"), false).Size(26, 26).Clicked() {
				df.openRecordPeek(dbDoc, store, dbDoc.space, rec, dbview.OpenFullPage)
			}
		}
		if iconButton(c, "peek-close", iconX, i18n.T("关闭预览"), i18n.T("关闭预览"), false).Size(26, 26).Clicked() {
			df.closeRecordPeek()
		}
	})

	// Collapsible property table above document body (collapsed by default).
	peekCollapsed := !df.dbPeek.propsExpanded
	dbview.PagePropertiesView(c.Key("peek-props"), store, rec.ID, dbDoc.dbView, &peekCollapsed, opts)
	df.dbPeek.propsExpanded = !peekCollapsed

	// Record body editor or inline note editor.
	if pd := df.dbPeek.doc; pd != nil {
		if pd.loadErr != nil {
			ui.Box(c).Grow(1).Center().Padding(24).Children(func() {
				ui.Text(c, pd.loadErr.Error()).FontSize(13).TextColor(t.Danger).Wrap()
			})
			return
		}
		edOpts := df.host.editorOptionsFor(c, max(float32(220), width-24), false, pd)
		edOpts.Outline = false
		edOpts.SetOutline = nil
		edOpts.FullWidth = true
		edOpts.HideFrontmatter = true
		edOpts.Column = max(float32(220), width-36)
		ui.Column(c.Key("peek-body")).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			editor.View(c.Key(pd.viewKey()+":peek"), pd.ed, edOpts)
		})
		return
	}

	// Inline database record body editor.
	ui.Column(c.Key("peek-inline-body")).Grow(1).MinWidth(0).MinHeight(0).Padding(16).Children(func() {
		ta := ui.TextArea(c.Key("inline-body"), &df.dbPeek.inlineBody).Grow(1).MinHeight(120).
			Placeholder(i18n.T("行内笔记内容…")).Padding(10).Radius(8).
			Background(t.Text.Alpha(0.03)).Border(1, t.Border).TextColor(t.Text)
		if ta.Changed() && !opts.ReadOnly {
			if r := store.Record(rec.ID); r != nil {
				r.Body = df.dbPeek.inlineBody
				dbDoc.dbDirtySnapshot = true
			}
		}
	})
}

func (df *databaseFeature) databaseCorner(c *ui.Context, d *document) {
	if !d.isDatabase() {
		return
	}
	res := d.dbView.Store.Query(d.dbView.Store.SchemaSnapshot().ActiveView, database.QueryOptions{Now: c.Now()})
	label := fmt.Sprintf(i18n.T("%d 条记录"), res.TotalCount)
	ui.Box(c.Key("db-corner-count")).Height(22).PaddingX(8).Radius(11).Center().
		Background(c.Theme().Text.Alpha(0.06)).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(c.Theme().TextMuted).NoWrap()
	})
}

func (df *databaseFeature) owningDatabaseFor(d *document) (*database.Store, string, *dbview.State, *document) {
	if d == nil || d.space == nil || d.loadErr != nil || d.isDatabase() || d.isWhiteboard() || d.isPDF() {
		return nil, "", nil, nil
	}
	if !vault.IsMarkdown(d.rel) {
		return nil, "", nil, nil
	}
	rel := filepath.ToSlash(d.rel)

	// 1. Prefer any open resident database in memory.
	for _, res := range df.host.residentDocuments() {
		if res != nil && res.space == d.space && res.loadErr == nil && res.isDatabase() {
			if rec := res.dbView.Store.RecordByPath(rel); rec != nil {
				return res.dbView.Store, rec.ID, res.dbView, res
			}
		}
	}

	if df.host.window() != nil {
		return df.cachedOwningDatabase(d)
	}
	// 2. Consult cached lookup for this document.
	key := d.space.ID + "\x00" + rel
	if entry, ok := df.pageDBCache[key]; ok {
		if entry == nil || entry.store == nil {
			return nil, "", nil, nil
		}
		if rec := entry.store.RecordByPath(rel); rec != nil {
			return entry.store, rec.ID, entry.state, nil
		}
		return nil, "", nil, nil
	}

	// 3. Probe candidate .db.md databases: first sibling <parentDir>.db.md, then any .db.md in space tree.
	var candidates []string
	if dir := parentDir(rel); dir != "" {
		candidates = append(candidates, dir+vault.ExtDatabase)
	}
	var walkTree func(nodes []*vault.Node)
	walkTree = func(nodes []*vault.Node) {
		for _, n := range nodes {
			if n.Dir {
				walkTree(n.Children)
			} else if vault.IsDatabase(n.Path) && !slices.Contains(candidates, n.Path) {
				candidates = append(candidates, n.Path)
			}
		}
	}
	tree, _ := df.host.treeOf(d.space)
	walkTree(tree)

	if df.pageDBCache == nil {
		df.pageDBCache = make(map[string]*pageDBEntry)
	}
	if len(candidates) == 0 {
		df.pageDBCache[key] = &pageDBEntry{}
		return nil, "", nil, nil
	}

	var live []database.LiveDoc
	for _, cand := range candidates {
		raw, err := readFile(d.space, cand)
		if err != nil {
			continue
		}
		db, err := database.DecodeDatabaseMarkdown(raw)
		if err != nil {
			continue
		}
		spaceRoot, dbRel := databaseSpaceRoot(d.space, cand)
		store := database.NewStore(spaceRoot, dbRel, db)
		store.MarkSourceText(raw)
		srcDir := store.ResolveSourceDir()
		if db.Source.Type != database.SourceSpace && srcDir != "" && !strings.HasPrefix(rel, srcDir+"/") {
			continue
		}
		if live == nil {
			live = []database.LiveDoc{{Path: rel, Text: d.text(), UpdatedAt: time.Now()}}
		}
		_, _ = store.Refresh(live)
		if rec := store.RecordByPath(rel); rec != nil {
			st := dbview.NewState(store)
			df.pageDBCache[key] = &pageDBEntry{dbRel: cand, store: store, state: st}
			return store, rec.ID, st, nil
		}
	}

	df.pageDBCache[key] = &pageDBEntry{}
	return nil, "", nil, nil
}

func (df *databaseFeature) pagePropertiesHeader(c *ui.Context, d *document) {
	if d == nil || d.source || d.loadErr != nil || d.isDatabase() || d.isWhiteboard() || d.isPDF() {
		return
	}
	store, recID, st, dbDoc := df.owningDatabaseFor(d)
	if store == nil || recID == "" {
		return
	}
	if df.pagePropsExpanded == nil {
		df.pagePropsExpanded = make(map[string]bool)
	}
	key := d.space.ID + "\x00" + d.rel
	collapsed := !df.pagePropsExpanded[key]
	opts := df.databaseOptionsForStore(c, d.space, store, dbDoc, d)
	ui.Themed(c.Key("page-props:"+key), documentTheme(df.host.paperIsDark())).Background(ui.Transparent).Children(func() {
		dbview.PagePropertiesView(c.Key("page-props-view:"+key), store, recID, st, &collapsed, opts)
	})
	df.pagePropsExpanded[key] = !collapsed
}

// managedFolderFor returns the sibling folder managed by dbNode (i.e. "<stem>/"
// alongside "<stem>.db.md") so the sidebar can fold it cleanly underneath.
func (df *databaseFeature) managedFolderFor(sp *space.Space, dbNode *vault.Node) *vault.Node {
	if sp == nil || dbNode == nil || dbNode.Dir || !vault.IsDatabase(dbNode.Path) {
		return nil
	}
	target := strings.TrimSuffix(dbNode.Path, vault.ExtDatabase)
	if n := df.host.findTreeNode(sp, target); n != nil && n.Dir {
		return n
	}
	return nil
}

func sidebarViewsFromDatabase(db *database.Database) []sidebarDBView {
	if db == nil {
		return nil
	}
	primaryID := db.PrimaryViewID()
	views := make([]sidebarDBView, 0, len(db.Views))
	for _, v := range db.Views {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			if spec, ok := dbview.ViewSpecFor(v.Type); ok {
				name = spec.Label()
			} else {
				name = string(v.Type)
			}
		}
		views = append(views, sidebarDBView{
			ID:      v.ID,
			Name:    name,
			Type:    v.Type,
			Primary: v.ID == primaryID,
		})
	}
	return views
}

// sidebarViewsFor returns the cached view list of dbRel in sp without doing
// redundant disk I/O per frame. Resident open databases always return live views.
func (df *databaseFeature) sidebarViewsFor(sp *space.Space, dbRel string, nodeMod time.Time) []sidebarDBView {
	if sp == nil || !vault.IsDatabase(dbRel) {
		return nil
	}
	if res := df.host.residentDocument(sp.ID, dbRel); res != nil && res.loadErr == nil && res.isDatabase() {
		return sidebarViewsFromDatabase(res.dbView.Store.Database())
	}
	key := sp.ID + "\x00" + dbRel
	nodeModNano := nodeMod.UnixNano()
	if !nodeMod.IsZero() && df.dbViewListCache != nil {
		if entry, ok := df.dbViewListCache[key]; ok && entry != nil &&
			(entry.nodeModNano == nodeModNano || entry.modTime.UnixNano() == nodeModNano) {
			return entry.views
		}
	}
	if df.host.window() != nil {
		loaded := df.cachedDatabase(sp, dbRel)
		if loaded.store != nil {
			return sidebarViewsFromDatabase(loaded.store.SchemaSnapshot())
		}
		return nil
	}
	full, err := resolveFile(sp, dbRel)
	if err != nil {
		return nil
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return nil
	}
	if df.dbViewListCache != nil {
		if entry, ok := df.dbViewListCache[key]; ok && entry != nil &&
			entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
			entry.nodeModNano = nodeModNano
			return entry.views
		}
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil
	}
	db, err := database.DecodeDatabaseMarkdown(string(raw))
	if err != nil {
		return nil
	}
	views := sidebarViewsFromDatabase(db)
	if df.dbViewListCache == nil {
		df.dbViewListCache = make(map[string]*sidebarDBMeta)
	}
	df.dbViewListCache[key] = &sidebarDBMeta{
		nodeModNano: nodeModNano,
		modTime:     info.ModTime(),
		size:        info.Size(),
		views:       views,
	}
	return views
}

func (df *databaseFeature) openDatabaseView(sp *space.Space, dbRel, viewID string) {
	if sp == nil || !vault.IsDatabase(dbRel) {
		return
	}
	if df.host.activeDocument() == nil || df.host.activeDocument().space != sp || df.host.activeDocument().rel != dbRel {
		df.host.openIn(sp, dbRel)
	}
	if df.host.activeDocument() != nil && df.host.activeDocument().space == sp && df.host.activeDocument().rel == dbRel && df.host.activeDocument().isDatabase() {
		if viewID != "" {
			df.host.activeDocument().dbView.SetActiveView(viewID)
			df.host.activeDocument().dbView.RequestFocus = true
			df.syncDatabaseEditsToDocs(sp, df.host.activeDocument(), df.host.activeDocument().dbView.Store, nil)
		}
	}
}

func (df *databaseFeature) addDatabaseViewInSidebar(sp *space.Space, dbRel string, vt database.ViewType) {
	if sp == nil || sp.IsExternal() || !vault.IsDatabase(dbRel) {
		return
	}
	df.host.openIn(sp, dbRel)
	if df.host.activeDocument() == nil || df.host.activeDocument().space != sp || df.host.activeDocument().rel != dbRel || !df.host.activeDocument().isDatabase() {
		return
	}
	st := df.host.activeDocument().dbView
	db := st.Store.Database()
	vc := dbview.NewViewConfig(vt, "", db)
	if err := st.Store.AddView(vc, true); err != nil {
		df.host.notify("%v", err)
		return
	}
	sp.SetExpanded(dbRel, true)
	df.host.saveState()
	df.syncDatabaseEditsToDocs(sp, df.host.activeDocument(), st.Store, nil)
	st.RequestFocus = true
}

func (df *databaseFeature) duplicateDatabaseViewInSidebar(sp *space.Space, dbRel, viewID string) {
	if sp == nil || sp.IsExternal() || !vault.IsDatabase(dbRel) {
		return
	}
	df.host.openIn(sp, dbRel)
	if df.host.activeDocument() == nil || df.host.activeDocument().space != sp || df.host.activeDocument().rel != dbRel || !df.host.activeDocument().isDatabase() {
		return
	}
	if _, err := df.host.activeDocument().dbView.Store.DuplicateView(viewID); err != nil {
		df.host.notify("%v", err)
		return
	}
	sp.SetExpanded(dbRel, true)
	df.host.saveState()
	df.syncDatabaseEditsToDocs(sp, df.host.activeDocument(), df.host.activeDocument().dbView.Store, nil)
}

func (df *databaseFeature) deleteDatabaseViewInSidebar(sp *space.Space, dbRel, viewID string) {
	if sp == nil || sp.IsExternal() || !vault.IsDatabase(dbRel) {
		return
	}
	df.host.openIn(sp, dbRel)
	if df.host.activeDocument() == nil || df.host.activeDocument().space != sp || df.host.activeDocument().rel != dbRel || !df.host.activeDocument().isDatabase() {
		return
	}
	if err := df.host.activeDocument().dbView.Store.DeleteView(viewID); err != nil {
		df.host.notify("%v", err)
		return
	}
	df.syncDatabaseEditsToDocs(sp, df.host.activeDocument(), df.host.activeDocument().dbView.Store, nil)
}

func (df *databaseFeature) renameDatabaseViewInSidebar(sp *space.Space, dbRel, viewID, newName string) {
	newName = strings.TrimSpace(newName)
	if newName == "" || sp == nil || sp.IsExternal() || !vault.IsDatabase(dbRel) {
		return
	}
	df.host.openIn(sp, dbRel)
	if df.host.activeDocument() == nil || df.host.activeDocument().space != sp || df.host.activeDocument().rel != dbRel || !df.host.activeDocument().isDatabase() {
		return
	}
	db := df.host.activeDocument().dbView.Store.Database()
	for _, v := range db.Views {
		if v.ID == viewID {
			if v.Name == newName {
				return
			}
			updated := v.Clone()
			updated.Name = newName
			if err := df.host.activeDocument().dbView.Store.UpdateView(updated); err != nil {
				df.host.notify("%v", err)
				return
			}
			df.syncDatabaseEditsToDocs(sp, df.host.activeDocument(), df.host.activeDocument().dbView.Store, nil)
			return
		}
	}
}

// databaseNodeMenu appends database-specific view creation and folder toggle
// entries to a database node's sidebar context menu.
func (df *databaseFeature) databaseNodeMenu(m *ui.Menu, sp *space.Space, n *vault.Node) {
	if sp == nil || n == nil || n.Dir || !vault.IsDatabase(n.Path) {
		return
	}
	if !sp.IsExternal() {
		m.Submenu(i18n.T("新建视图"), func(sub *ui.Menu) {
			for _, spec := range dbview.EnabledViewSpecs() {
				vt := spec.Type
				if sub.Item(spec.Label()).Chosen() {
					df.addDatabaseViewInSidebar(sp, n.Path, vt)
				}
			}
		})
	}
	viewLabel := i18n.T("展开视图")
	if sp.IsExpanded(n.Path) {
		viewLabel = i18n.T("收起视图")
	}
	if m.Item(viewLabel).Chosen() {
		sp.SetExpanded(n.Path, !sp.IsExpanded(n.Path))
		df.host.saveState()
	}
	if mdir := df.managedFolderFor(sp, n); mdir != nil && !sp.IsPinned(mdir.Path) {
		label := i18n.T("展开数据文件")
		if sp.IsExpanded(mdir.Path) {
			label = i18n.T("收起数据文件")
		}
		if m.Item(label).Chosen() {
			sp.SetExpanded(mdir.Path, !sp.IsExpanded(mdir.Path))
			df.host.saveState()
		}
	}
	m.Separator()
}

// databaseViewTree renders a Feishu/WPS-style collapsible child tree of views
// directly beneath a database node in the sidebar.
func (df *databaseFeature) databaseViewTree(c *ui.Context, sp *space.Space, n *vault.Node, depth int, live bool) {
	if sp == nil || n == nil || n.Dir || !vault.IsDatabase(n.Path) || !sp.IsExpanded(n.Path) {
		return
	}
	views := df.sidebarViewsFor(sp, n.Path, n.Modified)
	if len(views) == 0 {
		return
	}
	t := c.Theme()
	indent := float32(depth * 16)
	isDocActive := df.host.activeDocument() != nil && df.host.activeDocument().space == sp && df.host.activeDocument().rel == n.Path && df.host.activeDocument().isDatabase()
	activeViewID := ""
	if isDocActive {
		activeViewID = df.host.activeDocument().dbView.ActiveViewID()
	}

	ui.Column(c.Key("db-views:" + sp.ID + "/" + n.Path)).Gap(1).Children(func() {
		for _, v := range views {
			vw := v
			chosen := isDocActive && activeViewID == vw.ID
			renaming := live && df.dbViewRename.spaceID == sp.ID && df.dbViewRename.dbRel == n.Path && df.dbViewRename.viewID == vw.ID
			if renaming {
				ui.Row(c.Key("dbv-ren:"+sp.ID+"/"+n.Path+"/"+vw.ID)).Height(32).
					Margin(0, 0, 0, indent).Padding(0, 8, 0, 10).Radius(8).Gap(7).
					Background(glass(t)).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, dbview.ViewIcon(vw.Type)).FontSize(13).TextColor(t.Accent)
					in := ui.TextInput(c.Key("dbv-in:"+vw.ID), &df.dbViewRename.draft).
						Grow(1).MinWidth(0).Height(24).Padding(0, 6).Radius(5).
						Background(t.Background).Border(1, t.Accent).FontSize(13).TextColor(t.Text).
						AutoFocus().Label(i18n.T("视图名称"))
					if df.dbViewRename.pending {
						in.Focus()
						df.dbViewRename.pending = false
					}
					if in.Shortcut(0, ui.KeyEscape) {
						df.dbViewRename = sidebarDBViewRename{}
					} else if in.Submitted() || (!in.Focused() && !df.dbViewRename.pending) {
						next := df.dbViewRename.draft
						df.dbViewRename = sidebarDBViewRename{}
						df.renameDatabaseViewInSidebar(sp, n.Path, vw.ID, next)
					}
				})
				continue
			}

			row := ui.Row(c.Key("dbv:"+sp.ID+"/"+n.Path+"/"+vw.ID)).Height(32).
				Margin(0, 0, 0, indent).Padding(0, 8, 0, 10).Radius(8).Gap(7).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
				Role(ui.RoleTreeItem).Selected(chosen).Label(vw.Name)
			switch {
			case chosen:
				row.Background(glass(t))
			case live && row.Hovered():
				row.Background(hoverFill(t))
			}
			row.Children(func() {
				iconCol := t.TextMuted
				textCol := t.Text
				if chosen {
					iconCol = t.Accent
				}
				ui.Icon(c, dbview.ViewIcon(vw.Type)).FontSize(13).TextColor(iconCol)
				lbl := ui.Text(c, vw.Name).FontSize(13).TextColor(textCol).SingleLine().Grow(1).MinWidth(0)
				if chosen {
					lbl.FontWeight(500)
				}
				if vw.Primary {
					ui.Box(c).Padding(1, 5).Radius(4).Background(t.Text.Alpha(0.06)).Shrink(0).Children(func() {
						ui.Text(c, i18n.T("主视图")).FontSize(10).TextColor(t.TextMuted).NoWrap()
					})
				}
			})
			if live && row.Clicked() {
				df.openDatabaseView(sp, n.Path, vw.ID)
			}
			if live && !sp.IsExternal() {
				row.ContextMenu(func(m *ui.Menu) {
					if m.Item(i18n.T("重命名视图…")).Chosen() {
						df.dbViewRename = sidebarDBViewRename{
							spaceID: sp.ID,
							dbRel:   n.Path,
							viewID:  vw.ID,
							draft:   vw.Name,
							pending: true,
						}
					}
					if m.Item(i18n.T("复制视图")).Chosen() {
						df.duplicateDatabaseViewInSidebar(sp, n.Path, vw.ID)
					}
					if !vw.Primary && len(views) > 1 {
						m.Separator()
						if m.Item(i18n.T("删除视图")).Chosen() {
							df.deleteDatabaseViewInSidebar(sp, n.Path, vw.ID)
						}
					}
				})
			}
		}

		if live && !sp.IsExternal() {
			addRow := ui.Row(c.Key("dbv-add:"+sp.ID+"/"+n.Path)).Height(28).
				Margin(0, 0, 0, indent).Padding(0, 8, 0, 10).Radius(8).Gap(6).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
				Role(ui.RoleButton).Label(i18n.T("新建视图"))
			if addRow.Hovered() {
				addRow.Background(hoverFill(t))
			}
			addRow.Children(func() {
				ui.Icon(c, iconPlus).FontSize(12).TextColor(t.TextMuted)
				ui.Text(c, i18n.T("新建视图")).FontSize(12).TextColor(t.TextMuted).SingleLine()
			})
			addRow.PopoverMenuButton(func(m *ui.Menu) {
				for _, spec := range dbview.EnabledViewSpecs() {
					vt := spec.Type
					if m.Item(spec.Label()).Chosen() {
						df.addDatabaseViewInSidebar(sp, n.Path, vt)
					}
				}
			})
		}
	})
}

// isFoldedDBDir reports whether folder path dir is hidden at its own level
// because its sibling "<dir>.db.md" row is visible in the tree and folds it
// underneath.
func (df *databaseFeature) isFoldedDBDir(sp *space.Space, dir string) bool {
	if sp == nil || dir == "" || dir == "." || sp.IsPinned(dir) {
		return false
	}
	dbPath := dir + vault.ExtDatabase
	if n := df.host.findTreeNode(sp, dbPath); n != nil && !n.Dir {
		return !df.host.spaces().IsFavorite(sp.ID, dbPath)
	}
	return false
}

// isFoldedDBFolder reports whether folder n is hidden at its own level because
// its sibling "<name>.db.md" row is visible in the tree and folds it underneath.
func (df *databaseFeature) isFoldedDBFolder(sp *space.Space, n *vault.Node) bool {
	if n == nil || !n.Dir {
		return false
	}
	return df.isFoldedDBDir(sp, n.Path)
}

type embedDBEntry struct {
	dbRel string
	store *database.Store
}

func (df *databaseFeature) resolveDatabaseRef(d *document, target string) (*space.Space, string, bool) {
	if d == nil || d.space == nil || d.space.IsExternal() {
		return nil, "", false
	}
	sp := d.space
	if df.host.window() != nil {
		return df.cachedDatabaseRef(d, target)
	}
	clean := path.Clean(strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(target)), "/"))
	if clean == "" || clean == "." {
		return nil, "", false
	}
	var cands []string
	if dir := path.Dir(filepath.ToSlash(d.rel)); dir != "" && dir != "." {
		relCand := path.Clean(path.Join(dir, clean))
		if filepath.IsLocal(filepath.FromSlash(relCand)) {
			cands = append(cands, relCand)
		}
	}
	if filepath.IsLocal(filepath.FromSlash(clean)) {
		cands = append(cands, clean)
	}
	for _, cand := range cands {
		if !vault.IsDatabase(cand) {
			continue
		}
		if full, err := resolveFile(sp, cand); err == nil {
			if st, serr := os.Stat(full); serr == nil && !st.IsDir() {
				return sp, cand, true
			}
		}
	}
	if infos, err := database.ListSpaceDatabases(sp.Root); err == nil {
		for _, info := range infos {
			if info.ID == target || info.Title == target || info.RelPath == clean || strings.TrimSuffix(info.RelPath, vault.ExtDatabase) == clean {
				return sp, info.RelPath, true
			}
		}
	}
	return nil, "", false
}

func (df *databaseFeature) loadDatabaseFor(d *document, target string) (*database.Store, dbview.Options, uint64, error) {
	sp, dbRel, ok := df.resolveDatabaseRef(d, target)
	if !ok {
		return nil, dbview.Options{}, 0, fmt.Errorf(i18n.T("找不到关联数据库：%s"), target)
	}
	if res := df.host.residentDocument(sp.ID, dbRel); res != nil && res.loadErr == nil && res.isDatabase() {
		opts := df.databaseOptionsForStore(nil, sp, res.dbView.Store, res, d)
		opts.Embedded = true
		return res.dbView.Store, opts, res.dbView.Store.Revision(), nil
	}
	key := sp.ID + "\x00" + dbRel
	if df.embedDBs != nil {
		if entry, exists := df.embedDBs[key]; exists && entry != nil && entry.store != nil {
			opts := df.databaseOptionsForStore(nil, sp, entry.store, nil, d)
			opts.Embedded = true
			return entry.store, opts, entry.store.Revision(), nil
		}
	}
	if df.host.window() != nil {
		loaded := df.cachedDatabase(sp, dbRel)
		if loaded.pending {
			return nil, dbview.Options{}, 0, errDatabaseLoading
		}
		if loaded.err != nil {
			return nil, dbview.Options{}, 0, loaded.err
		}
		opts := df.databaseOptionsForStore(nil, sp, loaded.store, nil, d)
		opts.Embedded = true
		return loaded.store, opts, loaded.store.Revision(), nil
	}
	store, err := database.Open(sp.Root, dbRel, df.liveDocsForSpace(sp))
	if err != nil {
		return nil, dbview.Options{}, 0, err
	}
	if df.embedDBs == nil {
		df.embedDBs = make(map[string]*embedDBEntry)
	}
	df.embedDBs[key] = &embedDBEntry{dbRel: dbRel, store: store}
	opts := df.databaseOptionsForStore(nil, sp, store, nil, d)
	opts.Embedded = true
	return store, opts, store.Revision(), nil
}

func (df *databaseFeature) openDatabaseFrom(d *document, target, viewID string, split bool) {
	sp, rel, ok := df.resolveDatabaseRef(d, target)
	if !ok {
		df.host.notify("找不到关联数据库：%s", target)
		return
	}
	applyView := func(td *document) {
		if td != nil && td.isDatabase() && viewID != "" {
			td.dbView.SetActiveView(viewID)
		}
	}
	if split {
		df.host.openSplit(sp, rel, false, false)
		applyView(df.host.residentDocument(sp.ID, rel))
		return
	}
	df.host.navigateInPane(d, func() {
		df.host.openIn(sp, rel)
		if df.host.activeDocument() != nil && df.host.activeDocument().rel == rel {
			applyView(df.host.activeDocument())
		}
	})
}

func (df *databaseFeature) createEmbeddedDatabaseFor(d *document, title string) (string, string, string, error) {
	if d == nil || d.space == nil || d.space.IsExternal() {
		return "", "", "", errors.New("external space is read-only")
	}
	sp := d.space
	docDir := path.Dir(filepath.ToSlash(d.rel))
	if docDir == "." {
		docDir = ""
	}
	baseTitle := strings.TrimSpace(title)
	if baseTitle == "" {
		baseTitle = i18n.T(untitledDatabase)
	}
	db := database.NewDefault(baseTitle, database.SourceConfig{Type: database.SourceFolder})
	content, err := database.EncodeDatabaseMarkdownRelative(docDir, db, nil)
	if err != nil {
		return "", "", "", err
	}
	rel, err := vault.CreateFile(sp.Root, docDir, baseTitle, vault.ExtDatabase, content)
	if err != nil {
		return "", "", "", err
	}
	rel = filepath.ToSlash(rel)
	if actualTitle := vault.Title(rel); actualTitle != "" && actualTitle != db.Title {
		db.Title = actualTitle
		if updated, encErr := database.EncodeDatabaseMarkdownRelative(docDir, db, nil); encErr == nil {
			_ = writeFile(sp, rel, updated)
		}
	}
	if df.embedDBs == nil {
		df.embedDBs = make(map[string]*embedDBEntry)
	}
	store := database.NewStore(sp.Root, rel, db)
	df.embedDBs[sp.ID+"\x00"+rel] = &embedDBEntry{dbRel: rel, store: store}
	df.host.refreshTree()
	df.host.refreshSearch()
	snap := database.RenderGFMSnapshot(docDir, db, nil)
	return rel, db.ActiveView, snap, nil
}

func (df *databaseFeature) databaseSnapshotFor(d *document, target, viewID string) string {
	store, _, _, err := df.loadDatabaseFor(d, target)
	if err != nil || store == nil || store.Database() == nil {
		return ""
	}
	if d != nil && d.space != nil {
		_, _ = store.Refresh(df.liveDocsForSpace(d.space))
	}
	docDir := ""
	if d != nil {
		docDir = path.Dir(filepath.ToSlash(d.rel))
		if docDir == "." {
			docDir = ""
		}
	}
	db := store.Database()
	res := store.Query(viewID, database.QueryOptions{Now: time.Now()})
	return database.RenderGFMSnapshot(docDir, db, res)
}

// documentForExport returns d's committed document with any embedded database
// Linked View snapshots refreshed from their live databases. The live editor
// buffer and undo history are not modified.
func (df *databaseFeature) documentForExport(d *document) *model.Document {
	if d == nil || d.ed == nil {
		return nil
	}
	doc := d.committedForRead()
	if doc == nil {
		return nil
	}
	var hasEmbed func(blocks []*model.Block) bool
	hasEmbed = func(blocks []*model.Block) bool {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			if b.Kind == "raw" {
				if spec, _, ok := editor.ParseDatabaseEmbed(b.Text()); ok && spec.Target != "" {
					return true
				}
			}
			if len(b.Children) > 0 && hasEmbed(b.Children) {
				return true
			}
		}
		return false
	}
	if !hasEmbed(doc.Blocks) {
		return doc
	}
	cloned := doc.Clone()
	var refreshBlocks func(blocks []*model.Block)
	refreshBlocks = func(blocks []*model.Block) {
		for _, b := range blocks {
			if b == nil {
				continue
			}
			if b.Kind == "raw" {
				if spec, _, ok := editor.ParseDatabaseEmbed(b.Text()); ok && spec.Target != "" {
					if snap := strings.TrimSpace(df.databaseSnapshotFor(d, spec.Target, spec.ViewID)); snap != "" {
						formatted := editor.FormatDatabaseEmbed(spec, snap)
						if formatted != b.Text() {
							b.Runs = []model.Run{{Text: formatted}}
						}
					}
				}
			}
			if len(b.Children) > 0 {
				refreshBlocks(b.Children)
			}
		}
	}
	refreshBlocks(cloned.Blocks)
	return cloned
}

func (df *databaseFeature) exportDatabaseCSV(d *document) (string, error) {
	if d == nil || !d.isDatabase() || d.space == nil || d.loadErr != nil {
		return "", errors.New("no active database")
	}
	viewID := ""
	if v := d.dbView.ActiveView(); v != nil {
		viewID = v.ID
	}
	csvBytes, err := d.dbView.Store.ExportViewCSV(viewID, database.QueryOptions{Now: time.Now()}, database.CSVExportOptions{IncludeBOM: true})
	if err != nil {
		df.host.notify("导出失败，%v", err)
		return "", err
	}
	root, nameOnDisk, err := fileLocation(d.space, d.rel)
	if err != nil {
		df.host.notify("导出失败，%v", err)
		return "", err
	}
	dir := path.Dir(nameOnDisk)
	if dir == "." {
		dir = ""
	}
	baseName := sanitizeName(strings.TrimSuffix(d.title(), vault.ExtDatabase))
	if baseName == "" {
		baseName = "database"
	}
	for i := 1; i < 1000; i++ {
		name := baseName
		if i > 1 {
			name = fmt.Sprintf("%s %d", baseName, i)
		}
		rel := path.Join(dir, name+".csv")
		full, rerr := vault.Resolve(root, rel)
		if rerr != nil {
			df.host.notify("导出失败，%v", rerr)
			return "", rerr
		}
		err = fsutil.CreateFileAtomic(full, csvBytes, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			df.host.notify("导出失败，%v", err)
			return "", err
		}
		df.host.notify("已导出 %s", rel)
		df.host.refreshTree()
		if df.host.window() != nil {
			mygo.Shell.ShowItemInFolder(full)
		}
		return filepath.ToSlash(rel), nil
	}
	err = errors.New("too many csv exports")
	df.host.notify("导出失败，%v", err)
	return "", err
}

func (df *databaseFeature) importCSVFromPath(sp *space.Space, dir, csvPath string, intoDoc *document) (string, database.CSVImportResult, error) {
	var res database.CSVImportResult
	if sp == nil || sp.IsExternal() || sp.Root == "" {
		return "", res, errors.New("external space is read-only")
	}
	f, err := os.Open(csvPath)
	if err != nil {
		df.host.notify("导入 CSV 失败，%v", err)
		return "", res, err
	}
	defer f.Close()

	if intoDoc != nil && intoDoc.isDatabase() && intoDoc.space == sp && intoDoc.loadErr == nil {
		res, err = database.ImportCSVIntoStore(intoDoc.dbView.Store, f)
		if err != nil {
			df.host.notify("导入 CSV 失败，%v", err)
			return intoDoc.rel, res, err
		}
		intoDoc.dbDirtySnapshot = true
		if err := intoDoc.writeDatabase(false); err != nil {
			return intoDoc.rel, res, err
		}
		df.refreshOpenDatabases(sp.ID)
		df.host.refreshTree()
		df.host.refreshSearch()
		df.host.notify(fmt.Sprintf(i18n.T("已导入 %d 条记录"), res.CreatedRecords))
		return intoDoc.rel, res, nil
	}

	title := strings.TrimSuffix(filepath.Base(csvPath), filepath.Ext(csvPath))
	dbRel, res, err := database.ImportCSVAsNewDatabase(sp.Root, dir, title, f)
	if err != nil {
		df.host.notify("导入 CSV 失败，%v", err)
		return "", res, err
	}
	df.invalidatePageDBCache(sp.ID)
	df.host.refreshTree()
	df.host.refreshSearch()
	if dir != "" {
		sp.SetExpanded(dir, true)
		df.host.saveState()
	}
	df.host.openIn(sp, dbRel)
	df.host.notify(fmt.Sprintf(i18n.T("已导入 %d 条记录"), res.CreatedRecords))
	return dbRel, res, nil
}

func (df *databaseFeature) importNotionFromPath(sp *space.Space, dir, srcPath string) (database.NotionImportResult, error) {
	var res database.NotionImportResult
	if sp == nil || sp.IsExternal() || sp.Root == "" {
		return res, errors.New("external space is read-only")
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		df.host.notify("导入 Notion 失败，%v", err)
		return res, err
	}
	if info.IsDir() {
		res, err = database.ImportNotionDir(sp.Root, dir, srcPath)
	} else {
		res, err = database.ImportNotionZIP(sp.Root, dir, srcPath)
	}
	if err != nil {
		df.host.notify("导入 Notion 失败，%v", err)
		return res, err
	}
	df.invalidatePageDBCache(sp.ID)
	df.host.refreshTree()
	df.host.refreshSearch()
	if len(res.Databases) > 0 {
		df.host.openIn(sp, res.Databases[0])
	} else if len(res.StandaloneNotes) > 0 {
		df.host.openIn(sp, res.StandaloneNotes[0])
	}
	df.host.notify(fmt.Sprintf(i18n.T("已导入 %d 个数据库，%d 条记录，%d 篇独立文档"), len(res.Databases), res.Records, len(res.StandaloneNotes)))
	return res, nil
}

func (df *databaseFeature) pickAndImportCSV(intoDoc *document) {
	sp := df.host.spaces().ActiveSpace()
	if sp == nil || sp.IsExternal() || df.host.window() == nil {
		return
	}
	dir := df.host.currentDir()
	win := df.host.window()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  win,
			Title:   i18n.T("选择 CSV 文件…"),
			Filters: []mygo.FileFilter{{Name: "CSV", Extensions: []string{"csv"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		p := paths[0]
		win.Update(func() {
			_, _, _ = df.importCSVFromPath(sp, dir, p, intoDoc)
		})
	}()
}

func (df *databaseFeature) pickAndImportNotionZIP() {
	sp := df.host.spaces().ActiveSpace()
	if sp == nil || sp.IsExternal() || df.host.window() == nil {
		return
	}
	dir := df.host.currentDir()
	win := df.host.window()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  win,
			Title:   i18n.T("选择 Notion 导出 ZIP…"),
			Filters: []mygo.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		p := paths[0]
		win.Update(func() {
			_, _ = df.importNotionFromPath(sp, dir, p)
		})
	}()
}

func (df *databaseFeature) pickAndImportNotionFolder() {
	sp := df.host.spaces().ActiveSpace()
	if sp == nil || sp.IsExternal() || df.host.window() == nil {
		return
	}
	dir := df.host.currentDir()
	win := df.host.window()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    win,
			Title:     i18n.T("选择 Notion 导出文件夹…"),
			Directory: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		p := paths[0]
		win.Update(func() {
			_, _ = df.importNotionFromPath(sp, dir, p)
		})
	}()
}
