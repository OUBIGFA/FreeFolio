// Package solar holds the Solar icons FreeMD shows, by Solar name.
//
// Icons are Solar Icons by 480 Design (https://www.figma.com/community/file/1166831539721848736),
// under the Creative Commons Attribution 4.0 International License, as
// packaged by Hakim Saoudi's solar-icons (MIT). See third_party/solar-icons.
//
// icons_gen.go is generated from manifest.txt by tools/solaricons; to add an
// icon, list it there and regenerate. Each icon is parsed the first time it
// is asked for, so the set costs nothing at startup.
package solar

import (
	"sync"

	"github.com/egoist/mygo/ui"
)

const (
	svgOpen  = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none">`
	svgClose = `</svg>`
)

var (
	mu    sync.Mutex
	cache = map[string]*ui.SVG{}
)

// Icon returns the icon of name, a Solar name such as "magnifier", in the
// Linear style, or "play@bold" for another style the manifest lists. It
// returns nil for a name the manifest does not list.
func Icon(name string) *ui.SVG {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := cache[name]; ok {
		return s
	}
	body, ok := bodies[name]
	if !ok {
		return nil
	}
	s := ui.MustParseSVG([]byte(svgOpen + body + svgClose))
	cache[name] = s
	return s
}

// Must is Icon for names that are part of the program: it panics when the
// manifest does not list name.
func Must(name string) *ui.SVG {
	s := Icon(name)
	if s == nil {
		panic("solar: no icon " + name)
	}
	return s
}

// Has reports whether the manifest lists name.
func Has(name string) bool {
	_, ok := bodies[name]
	return ok
}

// Count returns the number of icons embedded.
func Count() int { return len(bodies) }
