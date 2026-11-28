package app

import (
	"github.com/OUBIGFA/FreeFolio/internal/solar"
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
	iconLocked     = solar.Must("lock")
	iconWindowBack = solar.Must("window-frame")
	iconSidebar    = solar.Must("panel-left")
	iconSort       = solar.Must("sort-vertical")
	iconBack       = solar.Must("arrow-left")
	iconFileImport = solar.Must("file-download")
	iconPageWidth  = solar.Must("move-horizontal")
	iconForward    = solar.Must("arrow-right")
	iconReload     = solar.Must("restart")
	iconCloudSync  = solar.Must("refresh")
	iconFolder     = solar.Must("folder")
	iconFolderOpen = solar.Must("folder-open")
	iconFile       = solar.Must("file-text")
	iconFileSimple = solar.Must("file")
	iconEye        = solar.Must("eye")
	iconEyeOff     = solar.Must("eye-closed")
	iconCopy       = solar.Must("copy")
	iconFilePlus   = solar.Must("file-add")
	iconBoardPlus  = lucide(`<path d="M2 3h20"/><path d="M21 3v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V3"/><path d="m7 21 5-5 5 5"/><path d="M12 6.5v6"/><path d="M9 9.5h6"/>`)
	iconPlus       = solar.Must("add")
	iconFolderPlus = solar.Must("add-folder")
	iconSearch     = solar.Must("magnifier")
	iconX          = solar.Must("close")
	iconWhiteboard = lucide(`<path d="M2 3h20"/><path d="M21 3v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V3"/><path d="m7 21 5-5 5 5"/>`)
	iconPencil     = solar.Must("pen")
	iconPin        = solar.Must("pin")
	iconStar       = solar.Must("star")
	iconPlugins    = solar.Must("widget")
	iconMinus      = solar.Must("minus")
	iconUndo       = solar.Must("undo-left")
	iconAlert      = solar.Must("danger-triangle")
	iconCheck      = solar.Must("check")
	iconDownload   = solar.Must("download-minimalistic")
	iconInfo       = solar.Must("info-circle")
	iconEllipsis   = solar.Must("menu-dots")
	iconMinimize   = solar.Must("minimize")
	iconHistory    = solar.Must("history")
	iconTrash      = solar.Must("trash-bin-minimalistic")
	iconLink       = solar.Must("link")
	iconMove       = solar.Must("move")
	iconHash       = solar.Must("hashtag")
	iconImage      = solar.Must("gallery")
	iconEraser     = solar.Must("eraser")
	iconGitBranch  = solar.Must("git-branch")
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
	"folder":   iconFolder,
	"cloud":    solar.Must("cloud"),
	"refresh":  iconReload,
	"settings": solar.Must("settings"),
	"layers":   symbolIcon("layers"),
	"files":    solar.Must("documents"),
	"keyboard": solar.Must("keyboard"),
	"code":     iconCode,
	"ask":      iconAsk,
	"info":     iconInfo,
}

var iconFilePDF = lucide(`<path d="M21 7h-3a2 2 0 0 1-2-2V2"/><path d="M21 6v6.5c0 .8-.7 1.5-1.5 1.5h-7c-.8 0-1.5-.7-1.5-1.5v-9c0-.8.7-1.5 1.5-1.5H17Z"/><path d="M7 8v8.8c0 .3.2.6.4.8.2.2.5.4.8.4H15"/><path d="M3 12v8.8c0 .3.2.6.4.8.2.2.5.4.8.4H11"/>`)
var iconHeart = lucide(`<path d="M2 9.5a5.5 5.5 0 0 1 9.591-3.676.56.56 0 0 0 .818 0A5.49 5.49 0 0 1 22 9.5c0 2.29-1.5 4-3 5.5l-5.492 5.313a2 2 0 0 1-3 .019L5 15c-1.5-1.5-3-3.2-3-5.5"/>`)
var iconCloud = solar.Must("cloud")
var iconUsers = solar.Must("users-group-rounded")
var iconRefresh = lucide(`<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>`)
var iconGitCompare = lucide(`<circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M13 6h3a2 2 0 0 1 2 2v7"/><path d="M11 18H8a2 2 0 0 1-2-2V9"/>`)
var iconGitMerge = lucide(`<circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M6 21V9a9 9 0 0 0 9 9"/>`)
var iconChevronsUpDown = lucide(`<path d="m7 15 5 5 5-5"/><path d="m7 9 5-5 5 5"/>`)
var iconChevronsRight = lucide(`<path d="m6 17 5-5-5-5"/><path d="m13 17 5-5-5-5"/>`)
var iconChevronsLeft = lucide(`<path d="m11 17-5-5 5-5"/><path d="m18 17-5-5 5-5"/>`)
var iconUnfoldHorizontal = lucide(`<path d="M16 12h6"/><path d="M8 12H2"/><path d="M12 2v2"/><path d="M12 8v2"/><path d="M12 14v2"/><path d="M12 20v2"/><path d="m19 15 3-3-3-3"/><path d="m5 9-3 3 3 3"/>`)
var iconFoldHorizontal = lucide(`<path d="M2 12h6"/><path d="M22 12h-6"/><path d="M12 2v2"/><path d="M12 8v2"/><path d="M12 14v2"/><path d="M12 20v2"/><path d="m19 9-3 3 3 3"/><path d="m5 15 3-3-3-3"/>`)
var iconBookOpen = lucide(`<path d="M12 7v14"/><path d="M3 18a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-6a3 3 0 0 0-3 3 3 3 0 0 0-3-3z"/>`)
var iconFileCode = lucide(`<path d="M10 12.5 8 15l2 2.5"/><path d="m14 12.5 2 2.5-2 2.5"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7z"/>`)
var iconFileArchive = lucide(`<path d="M10 12v-1"/><path d="M10 18v-2"/><path d="M10 7V6"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M15.5 22H18a2 2 0 0 0 2-2V7l-5-5H6a2 2 0 0 0-2 2v16a2 2 0 0 0 .274 1.01"/><circle cx="10" cy="20" r="2"/>`)
var iconCode = lucide(`<path d="m16 18 6-6-6-6"/><path d="m8 6-6 6 6 6"/>`)
var iconExternalLink = lucide(`<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>`)
var iconChevronUp = lucide(`<path d="m18 15-6-6-6 6"/>`)
var iconLock = lucide(`<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`)
var iconUnlock = lucide(`<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/>`)
var iconColumns2 = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M12 3v18"/>`)
var iconPanelLeftClose = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/><path d="m16 15-3-3 3-3"/>`)
var iconPanelLeftOpen = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/><path d="m14 9 3 3-3 3"/>`)
var iconPanelRightClose = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M15 3v18"/><path d="m8 9 3 3-3 3"/>`)
var iconPanelRightOpen = lucide(`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M15 3v18"/><path d="m10 15-3-3 3-3"/>`)
var iconArrowLeftRight = lucide(`<path d="M8 3 4 7l4 4"/><path d="M4 7h16"/><path d="m16 21 4-4-4-4"/><path d="M20 17H4"/>`)
var iconDatabase = lucide(`<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/>`)
var iconMaximize2 = lucide(`<polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" x2="14" y1="3" y2="10"/><line x1="3" x2="10" y1="21" y2="14"/>`)
