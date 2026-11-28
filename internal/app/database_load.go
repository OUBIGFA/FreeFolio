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
func (df *databaseFeature) databaseCatalogFor(sp *space.Space) []database.SpaceDBInfo {
	if sp == nil || sp.IsExternal() {
		return nil
	}
	if df.dbCatalog == nil {
		df.dbCatalog = map[string]*databaseCatalog{}
	}
	if cached := df.dbCatalog[sp.ID]; cached != nil {
		return cached.items
	}
	entry := &databaseCatalog{pending: true}
	df.dbCatalog[sp.ID] = entry
	root, id := sp.Root, sp.ID
	df.host.backgroundResult(func() func() {
		items, err := database.ListSpaceDatabases(root)
		return func() {
			if df.dbCatalog[id] != entry {
				return
			}
			entry.items, entry.err, entry.pending = items, err, false
			if err != nil {
				df.host.notify("读取数据库列表失败；%v", err)
			}
		}
	})
	return entry.items
}

func (df *databaseFeature) cachedDatabase(sp *space.Space, rel string) *databaseLoad {
	if df.dbLoads == nil {
		df.dbLoads = map[string]*databaseLoad{}
	}
	key := sp.ID + "\x00" + rel
	if found := df.dbLoads[key]; found != nil {
		return found
	}
	entry := &databaseLoad{pending: true}
	df.dbLoads[key] = entry
	root := sp.Root
	live := df.liveDocsForSpace(sp)
	df.host.backgroundResult(func() func() {
		store, err := database.Open(root, rel, live)
		return func() {
			if df.dbLoads[key] != entry {
				return
			}
			entry.store, entry.err, entry.pending = store, err, false
			if store != nil {
				entry.state = dbview.NewState(store)
				df.host.attachRelationHost(sp, store)
			}
			for _, d := range df.host.residentDocuments() {
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

func (df *databaseFeature) cachedOwningDatabase(d *document) (*database.Store, string, *dbview.State, *document) {
	for _, info := range df.databaseCatalogFor(d.space) {
		loaded := df.cachedDatabase(d.space, info.RelPath)
		if loaded.store != nil {
			if rec := loaded.store.RecordByPath(d.rel); rec != nil {
				return loaded.store, rec.ID, loaded.state, nil
			}
		}
	}
	return nil, "", nil, nil
}

func (df *databaseFeature) cachedDatabaseRef(d *document, target string) (*space.Space, string, bool) {
	clean := path.Clean(strings.ReplaceAll(target, `\`, "/"))
	relative := path.Clean(path.Join(path.Dir(d.rel), clean))
	for _, info := range df.databaseCatalogFor(d.space) {
		if info.RelPath == relative || info.RelPath == clean || info.ID == target || info.Title == target {
			return d.space, info.RelPath, true
		}
	}
	return nil, "", false
}

// databaseImage schedules resolution and decoding once per resource key. File
// watch invalidation drops the cache, so frames never stat or decode images.
func (df *databaseFeature) databaseImage(sp *space.Space, rel, url string) *ui.Bitmap {
	key := sp.ID + "\x00" + rel + "\x00" + url
	if found, ok := df.dbThumbCache[key]; ok {
		return found.resource.Bitmap
	}
	if df.dbThumbCache == nil {
		df.dbThumbCache = map[string]cachedImage{}
	}
	df.dbThumbCache[key] = cachedImage{loading: true}
	df.dbThumbOrder = append(df.dbThumbOrder, key)
	copySpace := *sp
	df.host.backgroundResult(func() func() {
		full, err := resolveSpaceImage(&copySpace, rel, url)
		var res editor.ImageResource
		if err == nil {
			res = decodeLocalImageThumb(full)
		}
		return func() {
			if current, ok := df.dbThumbCache[key]; !ok || !current.loading {
				return
			}
			df.dbThumbCache[key] = cachedImage{resource: res}
			for len(df.dbThumbOrder) > maxDBThumbCache {
				old := df.dbThumbOrder[0]
				df.dbThumbOrder = df.dbThumbOrder[1:]
				if cached := df.dbThumbCache[old]; cached.resource.Bitmap != nil {
					cached.resource.Bitmap.ReleasePixels()
				}
				delete(df.dbThumbCache, old)
			}
		}
	})
	return df.dbThumbCache[key].resource.Bitmap
}

var errDatabaseLoading = errors.New("数据库正在加载")
