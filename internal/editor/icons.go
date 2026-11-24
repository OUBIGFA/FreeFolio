package editor

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

// Icons of the floating format bar and menus.
var (
	iconBold          = solar.Must("text-bold")
	iconItalic        = solar.Must("text-italic")
	iconUnderline     = solar.Must("text-underline")
	iconStrike        = solar.Must("text-cross")
	iconCode          = solar.Must("code")
	iconLink          = solar.Must("link")
	iconPlus          = solar.Must("add")
	iconFormat        = solar.Must("text-format")
	iconSparkles      = solar.Must("sparkles")
	iconChevron       = solar.Must("alt-arrow-down")
	iconChevronUp     = solar.Must("alt-arrow-up")
	iconChevronRight  = solar.Must("alt-arrow-right")
	iconChevronsRight = solar.Must("double-alt-arrow-right")
	iconOutline       = solar.Must("text-align-left")
	iconAlignLeft     = solar.Must("text-align-left")
	iconAlignCenter   = solar.Must("text-align-center")
	// The read-only toggle: Lucide's lock and lock-open.
	iconLocked     = solar.Must("lock")
	iconUnlocked   = solar.Must("lock-unlocked")
	iconAlignRight = solar.Must("text-align-right")
)

// Icons drawn on the page.
var (
	iconGrip   = solar.Must("grip-vertical")
	iconBulb   = solar.Must("lightbulb")
	iconCheck  = solar.Must("check")
	iconGone   = solar.Must("danger-circle")
	iconInfo   = solar.Must("info-circle")
	iconNotice = solar.Must("chat-square-warning")
	iconWarn   = solar.Must("danger-triangle")
	iconStop   = solar.Must("danger")
	iconCopy   = solar.Must("copy")
)

// Icons of the content types, in the insert and slash menus.
var (
	iconParagraph = solar.Must("paragraph")
	iconHeading1  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="m17 12 3-2v8"/>`)
	iconHeading2  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="M21 18h-4c0-4 4-3 4-6 0-1.5-2-2.5-4-1"/>`)
	iconHeading3  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="M17.5 10.5c1.7-1 3.5 0 3.5 1.5a2 2 0 0 1-2 2"/><path d="M17 17.5c2 1.5 4 .3 4-1.5a2 2 0 0 0-2-2"/>`)
	iconList      = solar.Must("list-unordered")
	iconOrdered   = solar.Must("list-ordered")
	iconTodo      = solar.Must("checklist")
	iconQuote     = solar.Must("quote")
	iconCodeBlock = solar.Must("code-square")
	iconTable     = solar.Must("table")
	iconColumns   = solar.Must("columns-2")
	iconSortUp    = solar.Must("sort-from-bottom-to-top")
	iconSortDown  = solar.Must("sort-from-top-to-bottom")
	iconToggle    = solar.Must("square-alt-arrow-right")
	iconRule      = solar.Must("separator-horizontal")
	iconHeading4  = lucide(`<path d="M12 18V6"/><path d="M17 10v3a1 1 0 0 0 1 1h3"/><path d="M21 10v8"/><path d="M4 12h8"/><path d="M4 18V6"/>`)
	iconHeading5  = lucide(`<path d="M4 12h8"/><path d="M4 18V6"/><path d="M12 18V6"/><path d="M17 13v-3h4"/><path d="M17 17.7c.4.2.8.3 1.3.3 1.5 0 2.7-1.1 2.7-2.5S19.8 13 18.3 13H17"/>`)
	iconImage     = solar.Must("gallery")
)

// Icons of the block menu.
var (
	iconIndent        = solar.Must("list-indent-increase")
	iconOutdent       = solar.Must("list-indent-decrease")
	iconPalette       = solar.Must("palette")
	iconScissors      = solar.Must("scissors")
	iconDuplicate     = solar.Must("copy-add")
	iconTrash         = solar.Must("trash-bin-minimalistic")
	iconArrowUp       = solar.Must("arrow-up")
	iconArrowDown     = solar.Must("arrow-down")
	iconInsertLeft    = solar.Must("arrow-left-to-line")
	iconInsertRight   = solar.Must("arrow-right-to-line")
	iconInsertAbove   = solar.Must("arrow-up-to-line")
	iconInsertBelow   = solar.Must("arrow-down-to-line")
	iconSquarePlus    = solar.Must("add-square")
	iconTriangleDown  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor"><path d="M6.5 9h11a.6.6 0 0 1 .45 1l-5.5 6.2a.6.6 0 0 1-.9 0L6.05 10a.6.6 0 0 1 .45-1z"/></svg>`))
	iconTriangleRight = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor"><path d="M9 6.5v11a.6.6 0 0 0 1 .45l6.2-5.5a.6.6 0 0 0 0-.9L10 6.05a.6.6 0 0 0-1 .45z"/></svg>`))
	iconClose         = solar.Must("close")
	iconPaintBucket   = solar.Must("paint-bucket")
	iconMerge         = solar.Must("table-cells-merge")
	iconSplit         = solar.Must("table-cells-split")
	iconSlash         = lucide(`<path d="M4 20 20 4"/>`)
	iconSmile         = solar.Must("smile-circle")
	iconMoveVertical  = solar.Must("move-vertical")
	iconSigma         = lucide(`<path d="M18 7V5a1 1 0 0 0-1-1H6.5a.5.5 0 0 0-.4.8l4.5 6a2 2 0 0 1 0 2.4l-4.5 6a.5.5 0 0 0 .4.8H17a1 1 0 0 0 1-1v-2"/>`)
	iconWrapText      = lucide(`<path d="M3 6h18"/><path d="M3 12h15a3 3 0 1 1 0 6h-4"/><polyline points="16 16 14 18 16 20"/><path d="M3 18h7"/>`)
	iconWorkflow      = solar.Must("workflow")
	iconSequence      = lucide(`<path d="M6 4v16"/><path d="M18 4v16"/><path d="M6 9h12"/><path d="m14 6 4 3-4 3"/><path d="M18 15H6"/><path d="m10 12-4 3 4 3"/>`)
	iconPencil        = solar.Must("pen")
)

var iconMaximize = solar.Must("maximize")

var iconMapCenter = lucide(`<path d="M8 3H5a2 2 0 0 0-2 2v3m13-5h3a2 2 0 0 1 2 2v3M3 16v3a2 2 0 0 0 2 2h3m8 0h3a2 2 0 0 0 2-2v-3"/><circle cx="12" cy="12" r="2.4" fill="currentColor" stroke="none"/>`)

var iconMapOneSide = lucide(`<rect x="2" y="9" width="5" height="6" rx="1.5"/><path d="M7 12h2c4 0 1-7 6-7h6M9 12c4 0 1 7 6 7h6"/>`)

var iconMapBothSides = lucide(`<rect x="9.5" y="9" width="5" height="6" rx="1.5"/><path d="M9.5 12C6 12 8 5 4 5H2m7.5 7C6 12 8 19 4 19H2m12.5-7C18 12 16 5 20 5h2m-7.5 7C18 12 16 19 20 19h2"/>`)

var iconMapCollapse = solar.Must("chevrons-down-up")

var iconMapExpand = solar.Must("chevrons-up-down")

var iconMinusLine = solar.Must("minus")
