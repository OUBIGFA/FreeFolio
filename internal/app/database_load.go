package app

import (
	"errors"
	"path"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/database"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/editor/dbview"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo/ui"
)

type databaseLoad struct {
	store   *database.Store
	state   *dbview.State
	err     error
	pending bool
}

type databaseCatalog struct {
	items   []database.SpaceDBInfo
	err     error
	pending bool
}

// databaseCatalogFor loads filesystem metadata once, outside native frames.
func (a *App) databaseCatalogFor(sp *space.Space) []database.SpaceDBInfo {
	if sp == nil || sp.IsExternal() {
		return nil
	}
	if a.dbCatalog == nil {
		a.dbCatalog = map[string]*databaseCatalog{}
	}
	if cached := a.dbCatalog[sp.ID]; cached != nil {
		return cached.items
	}
	entry := &databaseCatalog{pending: true}
	a.dbCatalog[sp.ID] = entry
	root, id := sp.Root, sp.ID
	a.backgroundResult(func() func() {
		items, err := database.ListSpaceDatabases(root)
		return func() {
			if a.dbCatalog[id] != entry {
				return
			}
			entry.items, entry.err, entry.pending = items, err, false
			if err != nil {
				a.notify("读取数据库列表失败；%v", err)
			}
		}
	})
	return entry.items
}

func (a *App) cachedDatabase(sp *space.Space, rel string) *databaseLoad {
	if a.dbLoads == nil {
		a.dbLoads = map[string]*databaseLoad{}
	}
	key := sp.ID + "\x00" + rel
	if found := a.dbLoads[key]; found != nil {
		return found
	}
	entry := &databaseLoad{pending: true}
	a.dbLoads[key] = entry
	root := sp.Root
	live := a.liveDocsForSpace(sp)
	a.backgroundResult(func() func() {
		store, err := database.Open(root, rel, live)
		return func() {
			if a.dbLoads[key] != entry {
				return
			}
			entry.store, entry.err, entry.pending = store, err, false
			if store != nil {
				entry.state = dbview.NewState(store)
				a.attachRelationHost(sp, store)
			}
			for _, d := range a.residentDocuments() {
				if d.space == sp && d.ed != nil {
					d.ed.ClearImages()
				}
				if d.isDatabase() {
					d.dbView.Store.InvalidateComputed()
				}
			}
		}
	})
	return entry
}

func (a *App) cachedOwningDatabase(d *document) (*database.Store, string, *dbview.State, *document) {
	for _, info := range a.databaseCatalogFor(d.space) {
		loaded := a.cachedDatabase(d.space, info.RelPath)
		if loaded.store != nil {
			if rec := loaded.store.RecordByPath(d.rel); rec != nil {
				return loaded.store, rec.ID, loaded.state, nil
			}
		}
	}
	return nil, "", nil, nil
}

func (a *App) cachedDatabaseRef(d *document, target string) (*space.Space, string, bool) {
	clean := path.Clean(strings.ReplaceAll(target, `\`, "/"))
	relative := path.Clean(path.Join(path.Dir(d.rel), clean))
	for _, info := range a.databaseCatalogFor(d.space) {
		if info.RelPath == relative || info.RelPath == clean || info.ID == target || info.Title == target {
			return d.space, info.RelPath, true
		}
	}
	return nil, "", false
}

// databaseImage schedules resolution and decoding once per resource key. File
// watch invalidation drops the cache, so frames never stat or decode images.
func (a *App) databaseImage(sp *space.Space, rel, url string) *ui.Bitmap {
	key := sp.ID + "\x00" + rel + "\x00" + url
	if found, ok := a.dbThumbCache[key]; ok {
		return found.resource.Bitmap
	}
	if a.dbThumbCache == nil {
		a.dbThumbCache = map[string]cachedImage{}
	}
	a.dbThumbCache[key] = cachedImage{loading: true}
	a.dbThumbOrder = append(a.dbThumbOrder, key)
	copySpace := *sp
	a.backgroundResult(func() func() {
		full, err := resolveSpaceImage(&copySpace, rel, url)
		var res editor.ImageResource
		if err == nil {
			res = decodeLocalImageThumb(full)
		}
		return func() {
			if current, ok := a.dbThumbCache[key]; !ok || !current.loading {
				return
			}
			a.dbThumbCache[key] = cachedImage{resource: res}
			for len(a.dbThumbOrder) > maxDBThumbCache {
				old := a.dbThumbOrder[0]
				a.dbThumbOrder = a.dbThumbOrder[1:]
				if cached := a.dbThumbCache[old]; cached.resource.Bitmap != nil {
					cached.resource.Bitmap.ReleasePixels()
				}
				delete(a.dbThumbCache, old)
			}
		}
	})
	return a.dbThumbCache[key].resource.Bitmap
}

var errDatabaseLoading = errors.New("数据库正在加载")
