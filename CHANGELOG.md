# Changelog

## FreeMD 0.1.1

- Add verified Windows in-app updates: download and validate the official installer, then install it from Settings → Update. Show update status and progress in that page instead of a separate popup. Other platforms open the release page when no signed update source is configured.
- Improve file browsing with centered, adaptive tabs, a visible Open File button, and outlines that stay clear of wide content. Keep links feeling clickable in read-only documents.
- Respect scheduled cloud sync when enabling or disabling sync, and keep document scrollbars usable while dragging.
- Include the sidebar icon and folder-label refinements.
- Continue to support native Markdown and legacy `.arcmd` documents. Back up notes and configuration before upgrading.
- Distribute only the locally built Windows x64 installer, `FreeMD-0.1.1-Setup.exe`. Checksums and build records stay local; no automatic-update assets are included.

## FreeMD 0.1.0

FreeMD's first public release: a local-first native Markdown editor based on ArcMD, built with Go and MyGo.

- Open Markdown files outside a space in separate tabs, save edits in place or save a copy, and keep file browsing limited to the files explicitly opened.
- Create native whiteboards from the home page or title bar, organize ideas with notes, shapes and arrows, and open whiteboards in split panes.
- Ask OneBot about notes and review suggested edits. Quote selected text into the conversation, configure multiple providers and API keys, and distinguish models by provider name.
- Enable WebDAV or S3 synchronization for selected spaces, with conflict copies, recovery backups and optional content encryption. Synchronization remains off by default.
- Reduce file-system work during startup and drawing, build sidebar rows near the viewport, improve SVG images and README badges, and keep long code blocks' Collapse button visible.
- Continue to support native Markdown and legacy `.arcmd` documents. Current formats are document 11, spaces configuration 14 and preferences 11. Back up notes and configuration before upgrading.
- Distribute only the locally built Windows x64 installer, `FreeMD-0.1.0-Setup.exe`. Checksums and build records stay local; no automatic-update assets are included.

## FreeMD 0.0.1

- Add per-space attachment folders with guarded relinking, sidebar multi-selection and batch actions, a navigable native document mind map, and hierarchical Select All.
- Add opt-in WebDAV and S3 cloud synchronization with per-file baselines, conditional writes, conflict copies and explicit resolution, recovery backups, local credential protection, optional content encryption, and manual or scheduled sync.
- Fix code and list indentation, code paste recovery, link boundary input, HTML comment preservation, read-only Space paging, document/preview image retention, and first-edit baseline work on large documents.
- Improve tall Mermaid sequence diagrams with phase shading and a vector zoom viewer; add display equation tags in the editor and PDF output.
- Advance document format to 10, spaces configuration to 14, and preferences to 6 while preserving FreeMD's existing fields. Existing supported files migrate in memory and write the current format when edited.

- Dock the workspace sidebar on the right by default. Choose either edge in Settings → Appearance.
- Place the document outline on the right and show the drag cursor over block handles.
- Group search and file opening, improve favorite label contrast, and keep the settings exit in the navigation footer.
- Build the local Windows x64 test installer with `python -X utf8 tools/build_test_installer.py`.
- Start faster and keep frames light: read a space's folders side by side without resolving every folder again, set the window and tray icons after the first frame, and stop resolving the open document's path in every frame. Command-bar search reads documents in place instead of copying each one, pinyin scoring allocates nothing, a folder's hover card no longer resolves every document, and pictures named without a folder share one reading of the space per frame.
- Build only the sidebar rows near the visible part of the list; the rest are held by spacers of the same height, so long open folders no longer slow typing, hovering and scrolling. Drags still build every row.
- Remove the inline "New Document Here" slot that opened when the pointer rested between root rows or on a folder's edge.
- Read prose closer to Obsidian and mark2: paragraphs keep 1em below them. Show SVG pictures, such as shields.io badges (embedded raster logos inside an SVG are left out). Show `<img>` written inside a paragraph or a link as the image; a paragraph of images alone, as README badges, flows side by side. Images inside HTML blocks sit inline as in a browser, so `<p align="center">` badges line up and `<br>` breaks them.
- Keep an open long code block's Collapse button at the bottom of the view while the block runs past it.
- Start a whiteboard from the home page and from the title bar. Open whiteboards in split panes; the pane gives its keys to the canvas.
- Show every resize handle (split panes, sidebar, OneBot, mind map) the split view's way, with no dividing line; one shared style in `theme.go` sets them all.
- Follow ArcMD 0.2.5 in the editor: `<kbd>` keys show as keycaps and write back as `<kbd>` (no longer as code); typing `<u>…</u>`, `<sup>`, `<kbd>`, `<font color>` and similar tags formats the text, `<br>` breaks the line and `<hr>` draws a rule; quotes typed inside an HTML tag or comment stay straight. Long formulas wrap at relations and operators before they shrink and draw sharp at any scale; tables size columns by their content and widen with the window for wide formulas; copying from cells, inline code and plain prose puts the literal characters on the clipboard; `snake_case` no longer writes back as `snake\_case`; selecting a row of a table with more rows than columns no longer quits FreeMD.
- Turn autosave off in Settings → Writing or the command bar. Edits then stay in memory until you press the save shortcut; leaving, closing or quitting asks to save, discard or cancel.
- Draw Mermaid flowcharts with a layered layout like Mermaid's: edge labels get a rank of their own, long and back edges route around nodes instead of through them, and subgraphs stay together. Sequence `->` and `-->` lines have no arrowhead, as in Mermaid. Draw Mermaid `gantt` charts (dates, durations, `after`/`until`, `done`/`active`/`crit`/`milestone`, `excludes weekends`, `axisFormat`) and `pie` charts, and ```` ```echarts ```` options written as JSON: pie, bar, line and scatter series with titles, legends and the option's colors.
- Keep Markdown faithful when blocks are edited: paragraph text that looks like a heading, list, quote, rule or math fence (`# x`, `1. x`, `- x`, `> x`, `---`, `$$`) is escaped instead of turning into that block; nested quotes (`> >`) show and write back nested, and the paragraphs of one quote stay one quote; `***bold italic***` stays Markdown instead of `<em>`; `<https://…>` and bare URLs and e-mail addresses write back as autolinks; an explicit `:---` column keeps its alignment; escaped `\~\~` no longer turns into strikethrough. Keycap emoji such as 1️⃣ show in color.
- Advance document format to 11 (keyboard keys) and preferences to 11 (autosave switch). Older files and preferences load unchanged.

## Upstream provenance

ArcMD's historical releases are not FreeMD releases. See the [third-party credits](third_party/README.md) and [pinned source records](third_party/manifests/upstreams.json) for the upstream baseline, selected adaptations and licenses.
