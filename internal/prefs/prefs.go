// Package prefs keeps FreeMD's preferences beyond the spaces: how focus
// mode writes and the keyboard shortcuts rebound, in prefs.json beside
// spaces.json. The package has no dependency on the window.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/OUBIGFA/FreeMD/internal/fsutil"
)

// CurrentVersion is the version of prefs.json this FreeMD writes.
// Version 4 adds SidebarWidth and KeepRunningOff, whose zero values are
// the defaults, so version 3 migrates unchanged. Version 5 adds
// StartAtHome, whose zero value is the default, so version 4 migrates
// unchanged.
// Version 6 adds document mind-map visibility and layout choices.
// Version 7 replaces legacy AI preferences with OneBot connections.
// Version 8 adds OneBotWidth; zero keeps the standard panel width.
// Version 9 adds GrayFolderIcons; zero keeps the space accent color.
// Version 10 adds each OneBot connection's KeyStrategy; empty is round
// robin, which a single key never notices, so version 9 migrates unchanged.
// Version 11 adds NoAutosave; zero keeps background autosave, so version 10
// migrates unchanged.
// Version 12 adds the ChatGPT first-use disclosure flag; existing connections
// and all other preferences migrate unchanged.
const CurrentVersion = 12

// Prefs are the preferences.
type Prefs struct {
	MindMapOpen      bool `json:"mindMapOpen,omitempty"`
	MindMapBothSides bool `json:"mindMapBothSides,omitempty"`
	// SidebarLeft opts out of the default right-hand workspace sidebar.
	SidebarLeft bool `json:"sidebarLeft,omitempty"`
	// GrayFolderIcons renders sidebar folder icons in a uniform light gray
	// instead of the active space's accent color.
	GrayFolderIcons bool `json:"grayFolderIcons,omitempty"`
	// SidebarWidth is the docked sidebar's width as dragged; 0 is the
	// standard width.
	SidebarWidth float32 `json:"sidebarWidth,omitempty"`
	// OneBotWidth remembers the user's preferred panel width. Window-size
	// limits are applied by the view without overwriting this preference.
	OneBotWidth float32 `json:"oneBotWidth,omitempty"`
	// KeepRunningOff quits FreeMD when its window closes instead of
	// hiding it in the notification area, which the zero value keeps.
	KeepRunningOff bool `json:"keepRunningOff,omitempty"`
	// StartAtHome opens the space's home page when FreeMD starts or its
	// window comes back, instead of the document last read, at the place
	// it was read to, which the zero value keeps.
	StartAtHome bool    `json:"startAtHome,omitempty"`
	Version     int     `json:"version"`
	FontSize    float32 `json:"fontSize,omitempty"`
	LineHeight  float32 `json:"lineHeight,omitempty"`
	NoAutoPair  bool    `json:"noAutoPair,omitempty"`
	// NoAutosave turns background saving off: edits stay in memory until
	// saved with the save shortcut or confirmed when a document closes.
	NoAutosave bool `json:"noAutosave,omitempty"`
	// AutoSpace puts a space between Chinese and Latin text as it is typed.
	AutoSpace bool `json:"autoSpace,omitempty"`
	// CodeNoWrap disables soft wrapping in code blocks by default so long
	// lines scroll horizontally instead.
	CodeNoWrap bool `json:"codeNoWrap,omitempty"`
	// RecentCommands are the command bar's commands used last, latest
	// first, which it lists first.
	RecentCommands []string `json:"recentCommands,omitempty"`
	// Language is the interface's language, "zh-Hans" or "en"; "" follows
	// the system.
	Language string `json:"language,omitempty"`
	// Font is the body text's typeface, one of Fonts ("" the system's);
	// Page the text column's width, one of Pages ("" the standard 820).
	Font string `json:"font,omitempty"`
	Page string `json:"page,omitempty"`
	// Typewriter keeps the line being written in the middle of the page.
	Typewriter bool `json:"typewriter,omitempty"`
	// DrawAnnotations lets the hand-drawn marks in view draw themselves,
	// one after another, when a document opens.
	DrawAnnotations bool `json:"drawAnnotations,omitempty"`
	// NoDim keeps the other paragraphs bright in focus mode, which fades
	// them by default.
	NoDim bool `json:"noDim,omitempty"`
	// OutlineClosed collapses the outline right of the page, which is open
	// by default.
	OutlineClosed bool `json:"outlineClosed,omitempty"`
	// Keys rebind commands, by their IDs, to shortcuts written as
	// "Cmd+Shift+K"; a command left out keeps its default.
	Keys map[string]string `json:"keys,omitempty"`
	// NoAutoUpdate leaves new versions to the user: a background check
	// then only says one is out, instead of downloading it.
	NoAutoUpdate bool `json:"noAutoUpdate,omitempty"`
	// PendingUpdate is the version FreeMD restarted to install; the launch
	// that runs it says FreeMD was updated, then clears it.
	PendingUpdate string `json:"pendingUpdate,omitempty"`
	// TourSeen is set once the tour of the window was finished or skipped.
	// Until then every launch shows it: the first of a new installation,
	// and the first after an update from a version that had no tour.
	TourSeen bool `json:"tourSeen,omitempty"`
	// AI is the model the space's questions are asked of. Its API key is
	// not here but in a separate local file managed by Secrets.
	AI AI `json:"ai,omitzero"`
	// Files are the file browser's choices, the mode of loose files opened
	// outside any space.
	Files Files `json:"files,omitzero"`
}

// Files are named so that their zero values are the defaults: the browser
// closes with its last tab, a double click closes a tab, and every tab
// shows its close button.
type Files struct {
	// KeepOpen leaves an empty browser when the last tab closes instead of
	// closing the window.
	KeepOpen bool `json:"keepOpen,omitempty"`
	// NoDoubleClickClose stops a double click on a tab from closing it.
	NoDoubleClickClose bool `json:"noDoubleClickClose,omitempty"`
	// HideTabClose hides the close button on every tab.
	HideTabClose bool `json:"hideTabClose,omitempty"`
	// TabAfterCurrent places a newly opened file right of the current tab
	// rather than at the end.
	TabAfterCurrent bool `json:"tabAfterCurrent,omitempty"`
	// ReadOnly opens files locked against editing until unlocked.
	ReadOnly bool `json:"readOnly,omitempty"`
}

// Store reads and writes prefs.json.
type Store struct{ Path string }

// Load reads the preferences; none yet are the defaults. A newer version
// is refused rather than overwritten.
func (s Store) Load() (*Prefs, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return &Prefs{Version: CurrentVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var p Prefs
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("prefs: %w", err)
	}
	if p.Version > CurrentVersion || p.Version < 1 {
		return nil, fmt.Errorf("prefs: unsupported version %d", p.Version)
	}
	if p.Version == 1 {
		// Version 1 had no side preference. Preserve all its other choices.
		p.SidebarLeft = false
	}
	// Version 2 had no file browser choices; their zero values are the
	// defaults, so it migrates unchanged.
	p.Version = CurrentVersion
	if err := p.AI.Validate(); err != nil {
		return nil, err
	}
	p.normalizeReading()
	return &p, nil
}

// Save writes the preferences.
func (s Store) Save(p *Prefs) error {
	if err := p.AI.Validate(); err != nil {
		return err
	}
	p.normalizeReading()
	p.Version = CurrentVersion
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.Path, append(data, '\n'), 0o644)
}

func (p *Prefs) ReadingSize() float32 {
	if p.FontSize >= 12 && p.FontSize <= 28 {
		return p.FontSize
	}
	return 16
}
func (p *Prefs) ReadingLine() float32 {
	if p.LineHeight >= 1.2 && p.LineHeight <= 2.2 {
		return p.LineHeight
	}
	return 26.0 / 16
}

// Fonts are the typefaces body text may be set in, and Pages the widths
// of the text column, by the names prefs.json keeps them under.
var (
	Fonts = []string{"", "serif", "kai", "mono"}
	Pages = []string{"narrow", "", "wide"}
)

// fontFamilies are the family lists of Fonts: the first a system has sets
// the text, Latin before CJK so each script gets a face of its kind.
var fontFamilies = map[string]string{
	"serif": "Georgia, Songti SC, STSong, Noto Serif CJK SC, Source Han Serif SC, SimSun, serif",
	"kai":   "Kaiti SC, STKaiti, KaiTi, Noto Serif CJK SC, serif",
	"mono":  "monospace",
}

// BodyFamily is the family list body text is set in, "" for the system's.
func (p *Prefs) BodyFamily() string { return fontFamilies[p.Font] }

// Column is the width of the text column: 680 narrow, 1040 wide, 0 for
// the standard 820.
func (p *Prefs) Column() float32 {
	switch p.Page {
	case "narrow":
		return 680
	case "wide":
		return 1040
	}
	return 0
}

func (p *Prefs) normalizeReading() {
	if p.Language != "zh-Hans" && p.Language != "en" {
		p.Language = ""
	}
	if _, ok := fontFamilies[p.Font]; !ok {
		p.Font = ""
	}
	if p.Page != "narrow" && p.Page != "wide" {
		p.Page = ""
	}
	if !(p.FontSize >= 12 && p.FontSize <= 28) {
		p.FontSize = 0
	}
	if !(p.LineHeight >= 1.2 && p.LineHeight <= 2.2) {
		p.LineHeight = 0
	}
}
