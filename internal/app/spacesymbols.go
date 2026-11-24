// Space symbols keep the Lucide 1.70.0 names they are stored under and
// show Solar's glyph for each (see internal/solar); the few Solar has
// no counterpart for keep Lucide's shapes (ISC License, see icons.go).

package app

import (
	"sync"

	"github.com/OUBIGFA/FreeFolio/internal/solar"
	"github.com/egoist/mygo/ui"
)

// spaceSymbols are the glyphs of the space icon picker's Icon tab, by Lucide
// name, in the order of Arc's 56 (as Cra lists them), each the nearest
// Lucide glyph.
var spaceSymbols = []string{
	"star", "bookmark", "flag", "zap", "triangle", "asterisk", "bell",
	"lightbulb", "shapes", "layout-grid", "grip", "layers", "database", "archive", "copy",
	"folder", "inbox", "calendar", "mail", "square-check", "file", "book", "message-circle-more",
	"users", "terminal", "wrench", "square", "egg", "circle", "moon", "sun",
	"earth", "leaf", "cloud", "paw-print", "shopping-basket", "gift", "bed-double", "utensils",
	"dumbbell", "plane", "eighth-note", "palette", "video", "bandage", "code-xml", "volleyball",
	"cloudy", "map", "flame", "cake", "drama", "scroll", "thumbs-up", "tram-front",
}

// lucideIconBodies are the Lucide shapes of the symbols Solar has no glyph for.
var lucideIconBodies = map[string]string{
	"triangle": `<path d="M13.73 4a2 2 0 0 0-3.46 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/>`,
	"shapes":   `<path d="M8.3 10a.7.7 0 0 1-.626-1.079L11.4 3a.7.7 0 0 1 1.198-.043L16.3 8.9a.7.7 0 0 1-.572 1.1Z"/> <rect x="3" y="14" width="7" height="7" rx="1"/> <circle cx="17.5" cy="17.5" r="3.5"/>`,
	"square":   `<rect width="18" height="18" x="3" y="3" rx="2"/>`,
	"egg":      `<path d="M12 2C8 2 4 8 4 14a8 8 0 0 0 16 0c0-6-4-12-8-12"/>`,
	"scroll":   `<path d="M19 17V5a2 2 0 0 0-2-2H4"/> <path d="M8 21h12a2 2 0 0 0 2-2v-1a1 1 0 0 0-1-1H11a1 1 0 0 0-1 1v1a2 2 0 1 1-4 0V5a2 2 0 1 0-4 0v2a1 1 0 0 0 1 1h3"/>`,
}

// solarSymbols are the Solar names of the symbols' glyphs, by Lucide name.
var solarSymbols = map[string]string{
	"star":                  "star",
	"bookmark":              "bookmark",
	"flag":                  "flag",
	"zap":                   "bolt",
	"asterisk":              "asterisk",
	"bell":                  "bell",
	"lightbulb":             "lightbulb",
	"layout-grid":           "widget",
	"grip":                  "grip",
	"layers":                "layers",
	"database":              "database",
	"archive":               "archive",
	"copy":                  "copy",
	"folder":                "folder",
	"inbox":                 "inbox",
	"calendar":              "calendar",
	"mail":                  "letter",
	"square-check":          "check-square",
	"file":                  "file",
	"book":                  "book",
	"message-circle-more":   "chat-round-dots",
	"users":                 "users-group-rounded",
	"terminal":              "programming",
	"wrench":                "wrench",
	"circle":                "circle",
	"moon":                  "moon",
	"sun":                   "sun",
	"earth":                 "earth",
	"leaf":                  "leaf",
	"cloud":                 "cloud",
	"paw-print":             "paw",
	"shopping-basket":       "cart",
	"gift":                  "gift",
	"bed-double":            "bed",
	"utensils":              "fork-knife",
	"dumbbell":              "dumbbell",
	"plane":                 "airplane",
	"eighth-note":           "music-note",
	"palette":               "palette",
	"video":                 "videocamera",
	"bandage":               "adhesive-plaster",
	"code-xml":              "code",
	"volleyball":            "volleyball",
	"cloudy":                "clouds",
	"map":                   "map",
	"flame":                 "flame",
	"cake":                  "cake",
	"drama":                 "masks",
	"thumbs-up":             "like",
	"tram-front":            "tram",
	"face-slightly-smiling": "smile-circle",
	"coffee":                "cup-hot",
	"trophy":                "cup-star",
	"truck":                 "truck",
	"search":                "magnifier",
	"trash":                 "trash-bin-minimalistic",
}

var (
	symbolIconMu    sync.Mutex
	symbolIconCache = map[string]*ui.SVG{}
)

func symbolIcon(name string) *ui.SVG {
	symbolIconMu.Lock()
	defer symbolIconMu.Unlock()
	if svg, ok := symbolIconCache[name]; ok {
		return svg
	}
	var svg *ui.SVG
	if s, ok := solarSymbols[name]; ok {
		svg = solar.Must(s)
	} else if body, ok := lucideIconBodies[name]; ok {
		svg = lucide(body)
	} else {
		return nil
	}
	symbolIconCache[name] = svg
	return svg
}
