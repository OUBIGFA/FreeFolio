package app

import (
	"github.com/OUBIGFA/FreeMD/internal/solar"
	"github.com/egoist/mygo/ui"
)

// Icons are Solar Icons by 480 Design, CC BY 4.0, from package solar
// (see third_party/solar-icons). Glyphs Solar has no counterpart for are
// Lucide's (https://lucide.dev), ISC License, drawn at Solar's 1.5 stroke,
// Copyright (c) for portions of Lucide are held by Cole Bemis 2013-2022 as
// part of Feather (MIT). All other copyright (c) for Lucide are held by
// Lucide Contributors 2022.

func lucide(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var (
	iconLocked       = solar.Must("lock")
	iconWindowBack   = solar.Must("window-frame")
	iconSidebar      = solar.Must("panel-left")
	iconSidebarRight = solar.Must("panel-right")
	iconSort         = solar.Must("sort-vertical")
	iconBack         = solar.Must("arrow-left")
	iconFileImport   = solar.Must("file-download")
	iconForward      = solar.Must("arrow-right")
	iconReload       = solar.Must("restart")
	iconCloudSync    = solar.Must("refresh")
	iconFolder       = solar.Must("folder")
	iconFolderOpen   = solar.Must("folder-open")
	iconFile         = solar.Must("file-text")
	iconFileSimple   = solar.Must("file")
	iconEye          = solar.Must("eye")
	iconEyeOff       = solar.Must("eye-closed")
	iconCopy         = solar.Must("copy")
	iconFilePlus     = solar.Must("file-add")
	iconBoardPlus    = lucide(`<path d="M2 3h20"/><path d="M21 3v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V3"/><path d="m7 21 5-5 5 5"/><path d="M12 6.5v6"/><path d="M9 9.5h6"/>`)
	iconPlus         = solar.Must("add")
	iconFolderPlus   = solar.Must("add-folder")
	iconSearch       = solar.Must("magnifier")
	iconX            = solar.Must("close")
	iconWhiteboard   = lucide(`<path d="M2 3h20"/><path d="M21 3v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V3"/><path d="m7 21 5-5 5 5"/>`)
	iconPencil       = solar.Must("pen")
	iconPin          = solar.Must("pin")
	iconStar         = solar.Must("star")
	iconPlugins      = solar.Must("widget")
	iconMinus        = solar.Must("minus")
	iconUndo         = solar.Must("undo-left")
	iconAlert        = solar.Must("danger-triangle")
	iconCheck        = solar.Must("check")
	iconDownload     = solar.Must("download-minimalistic")
	iconInfo         = solar.Must("info-circle")
	iconEllipsis     = solar.Must("menu-dots")
	iconMinimize     = solar.Must("minimize")
	iconHistory      = solar.Must("history")
	iconTrash        = solar.Must("trash-bin-minimalistic")
	iconLink         = solar.Must("link")
	iconMove         = solar.Must("move")
	iconHash         = solar.Must("hashtag")
	iconImage        = solar.Must("gallery")
	iconEraser       = solar.Must("eraser")
	iconGitBranch    = solar.Must("git-branch")
)

// iconSpaces follows the square.on.square outline used by Cra's Spaces
// library page. Only the exposed edges of the rear square are drawn, so the
// overlap stays clear on translucent themes without an opaque fill.
var iconSpaces = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
<path d="M7 17H4.5A2.5 2.5 0 0 1 2 14.5v-10A2.5 2.5 0 0 1 4.5 2h10A2.5 2.5 0 0 1 17 4.5V7"/>
<rect x="7" y="7" width="15" height="15" rx="2.5"/>
</svg>`))

// Icons of the space's color editor.
var (
	iconSparkles     = solar.Must("sparkles")
	iconSun          = solar.Must("sun")
	iconMoon         = solar.Must("moon")
	iconChevronDown  = solar.Must("alt-arrow-down")
	iconChevronLeft  = solar.Must("alt-arrow-left")
	iconChevronRight = solar.Must("alt-arrow-right")
	iconPalette      = solar.Must("palette")
)

// settingsIcons are the glyphs of the settings' tabs, by Lucide name.
var settingsIcons = map[string]*ui.SVG{
	"pencil":   iconPencil,
	"palette":  iconPalette,
	"cloud":    solar.Must("cloud"),
	"refresh":  iconReload,
	"settings": solar.Must("settings"),
	"layers":   symbolIcon("layers"),
	"files":    solar.Must("documents"),
	"keyboard": solar.Must("keyboard"),
	"ask":      iconAsk,
	"info":     iconInfo,
}
