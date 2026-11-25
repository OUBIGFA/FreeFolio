package app

// Where loose files were read to: the file browser opens a file again at
// the place it was left, unless the user turned that off. The places live
// in file-places.json of the configuration folder, by the file's path.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
)

// maxFilePlaces bounds the places kept; the oldest go first.
const maxFilePlaces = 300

// filePlace is where a file was read to: the top-level block at the top of
// the view and how far into it, or the page of a PDF.
type filePlace struct {
	Block int       `json:"block,omitempty"`
	Frac  float32   `json:"frac,omitempty"`
	Page  int       `json:"page,omitempty"`
	At    time.Time `json:"at"`
}

type filePlaces struct {
	path   string
	places map[string]filePlace
}

// placeKey is the key of the file at full: its clean path, in one case on
// Windows, whose paths ignore it.
func placeKey(full string) string {
	full = filepath.Clean(full)
	if runtime.GOOS == "windows" {
		full = strings.ToLower(full)
	}
	return full
}

// filePlaceStore reads the places the first time they are needed; a file
// that cannot be read starts empty.
func (a *App) filePlaceStore() *filePlaces {
	if a.places == nil {
		p := &filePlaces{path: filepath.Join(a.cfg.ConfigDir, "file-places.json"), places: map[string]filePlace{}}
		if data, err := os.ReadFile(p.path); err == nil {
			_ = json.Unmarshal(data, &p.places)
		}
		if p.places == nil {
			p.places = map[string]filePlace{}
		}
		a.places = p
	}
	return a.places
}

func (p *filePlaces) save() error {
	for len(p.places) > maxFilePlaces {
		oldest, at := "", time.Time{}
		for k, v := range p.places {
			if oldest == "" || v.At.Before(at) {
				oldest, at = k, v.At
			}
		}
		delete(p.places, oldest)
	}
	data, err := json.Marshal(p.places)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(p.path, data, 0o600)
}

// placeOf is where the document d is read to, and whether it has a place
// to keep.
func placeOf(d *document) (filePlace, bool) {
	if d == nil || d.loadErr != nil || d.boardLoading || d.board != nil || d.boardOverlay != nil {
		return filePlace{}, false
	}
	if d.isPDF() {
		if d.pdf == nil {
			return filePlace{}, false
		}
		return filePlace{Page: d.pdf.CurrentPage}, true
	}
	block, frac := d.ed.ReadingPlace()
	return filePlace{Block: block, Frac: frac}, true
}

// noteFilePlace records where the file browser's document d was read to.
func (a *App) noteFilePlace(d *document) {
	if a.prefs.Files.NoRememberPlace || a.files == nil || d == nil || d.space != a.files.locations {
		return
	}
	place, ok := placeOf(d)
	full := d.fullPath()
	if !ok || full == "" {
		return
	}
	p := a.filePlaceStore()
	key := placeKey(full)
	old, seen := p.places[key]
	if place.Block == 0 && place.Frac == 0 && place.Page == 0 {
		if !seen {
			return
		}
		delete(p.places, key)
	} else {
		if seen && old.Block == place.Block && old.Frac == place.Frac && old.Page == place.Page {
			return
		}
		place.At = time.Now().UTC()
		p.places[key] = place
	}
	if err := p.save(); err != nil && !errors.Is(err, os.ErrNotExist) {
		a.notify("无法记住文件的浏览位置；%v", err)
	}
}

// restoreFilePlace scrolls the file browser's document d to where it was
// read to last.
func (a *App) restoreFilePlace(d *document) {
	if a.prefs.Files.NoRememberPlace || d == nil || d.loadErr != nil || d.boardLoading || d.board != nil {
		return
	}
	place, ok := a.filePlaceStore().places[placeKey(d.fullPath())]
	if !ok {
		return
	}
	if d.isPDF() {
		if d.pdf != nil && place.Page > 0 {
			d.pdf.GoToPage(place.Page, true)
		}
		return
	}
	d.ed.ScrollToReadingPlace(place.Block, place.Frac)
}
