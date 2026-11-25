# Dependencies and attribution

FreeFolio uses `github.com/egoist/mygo` v0.10.0 for native windows, UI primitives,
text shaping and system input transport. Its module cache stays untouched.
The Markdown editor is implemented in `internal/editor` and has no vendored
editor component or TeX engine.

The ChatGPT connection follows OpenAI's public Sign in with ChatGPT protocol.
It uses the official sign-in mark under separate brand terms, without the
DevKit's noncommercial application code. Protocol references, asset provenance
and OIDC dependency notices are in [chatgpt/](chatgpt/README.md).

The Space theme algorithms and theme editor follow Cra, under the MIT License,
Copyright (c) 2026 Victor Kildahl. Their implementation lives in
`internal/space/theme.go` and `internal/app/spaceeditor.go`. The space icon
picker (`internal/app/spaceicons.go`) follows Cra's, and its emoji table
(`internal/app/emojidata.go`) is generated from Cra's emoji catalog, which Cra
makes from Unicode's emoji-test.txt.
The sidebar's favorites (`internal/app/favorites.go`), its list dragging
(`internal/app/treedrag.go`), moving rows to another space through its
icon (`internal/app/crossspace.go`) and the paging between spaces
(`internal/app/spaceswitch.go`) port Cra's FavoritesGridView,
SpaceListView and SpacePagerView / PagedGradientView, with their
measurements of Arc.

The split layout, pane toolbar, divider behavior and segmented group row
(`internal/app/split.go`, `splitview.go`) follow Cra's SplitPaneLayout,
SplitDraggerHandleView, PaneToolbarView and split sidebar interaction.
The software update banners and toast
(`internal/app/updateview.go`, `internal/update`) port Cra's UpdateBanner,
UpdateNotice and ShinyToast, with their measurements of Arc.
The full MIT notice is in `cra/LICENSE`.

[Cra source](https://github.com/VictorKildahl/cra)

The image figures and missing-image recovery cards (`internal/editor/images.go`)
and formula presentation (`internal/editor/math.go`) follow Textus's image view,
missing-image cards, unboxed display math, source panel and live preview design,
under the MIT License,
Copyright (c) 2026 CatVinci Studio. They are implemented with native Go drawing
and controls. Inline math, clipboard formats, link popovers, image resizing,
bracket pairing, footnotes and reading controls also use Textus as a behavior
reference, implemented against FreeFolio's document model inherited from ArcMD.
The MIT notice is in `textus/LICENSE`.

[Textus source](https://github.com/CatVinci-Studio/Textus)

The hand-drawn text annotations (`internal/editor/annotate.go`) port Opensource
UI's AnnotatedText, under the MIT License, Copyright (c) 2026 Bidyut Kundu.
Its SVG paths are kept; its turbulence filter is replaced by a seeded
displacement of the flattened strokes, drawn natively. The MIT notice is in
`opensourceui/LICENSE`.

[Opensource UI source](https://github.com/bidyut10/opensourceui)

Formula presentation was checked against Textus commit
`21dc743b84c5c8f518cfdc95c9654fe31b61507e` (its math preview plugin and editor CSS).
The native renderer embeds the unmodified `KaTeX_Main-Regular.ttf`,
`KaTeX_Math-Italic.ttf`, `KaTeX_Main-Bold.ttf` and `KaTeX_Math-BoldItalic.ttf`
faces from KaTeX commit
`9b2280dee4acb4da7d866dbaccad120b00949c27` in `internal/editor/fonts/`.
These fonts are under the SIL Open Font License 1.1, with copyright and reserved
font names retained in `katex/OFL.txt` and in the font metadata. KaTeX's upstream
MIT notice is also retained in `katex/LICENSE`; its JavaScript renderer is not
included. Formula parsing and layout remain in FreeFolio's `internal/tex`
package, inherited from ArcMD.

[KaTeX source](https://github.com/KaTeX/KaTeX)

Interface icons are Solar Icons by 480 Design
([Figma community file](https://www.figma.com/community/file/1166831539721848736)),
under the Creative Commons Attribution 4.0 International License, as packaged by
Hakim Saoudi's [solar-icons](https://github.com/saoudi-h/solar-icons) (MIT) at
`ab241e1bf578dcfe45b0197aee59bd9a08b5fe68`. `tools/solaricons` verifies the pinned
archive, keeps the whole set under `.local/upstream/` for later additions, and
generates `internal/solar/icons_gen.go` from `internal/solar/manifest.txt`; black
is replaced by `currentColor`, otherwise the shapes are unchanged. The credit is
shown in Settings → About. Both notices are in `solar-icons/`.

A few glyphs Solar has no counterpart for (headings, sigma, wrap, sequence,
whiteboard and mind-map layouts, and five space symbols) remain Lucide, under the
ISC License, drawn at Solar's 1.5 stroke; attribution appears in
`internal/app/icons.go`. Space symbols keep their stored Lucide 1.70.0 names in
`internal/app/spacesymbols.go`.

The Q&A thinking indicator (`internal/app/qaloader.go`, `internal/app/qabot.go`)
ports the `Tail Chase`, `Dot Stream`, and `Mirage` loaders from
[LDRS](https://github.com/GriffinJohnston/ldrs/tree/f759f65beb8ab4f4a1f010438072180693ca01ec),
Copyright (c) 2022 Griffin Johnston, MIT. They are implemented in native Go
using MyGo vector circles and a reusable native Mirage ooze bitmap, with no
JavaScript or webview at runtime. The full notice is in `ldrs/LICENSE`; source
and port details are in `ldrs/README.md`.

The Q&A Markdown stream ports Lobe UI's `StreamingPlayground` and LobeHub
Streamdown's balanced smoother and per-character fade into native Go.
Pinned source links, adaptation details and both MIT notices are in
`lobe-streaming/`.

The Q&A inline source chips, supporting claim highlights, gliding preview
card and collapsible stacked-monogram sources list (`internal/app/qacitations.go`,
`internal/app/qamarkdown.go`, `internal/app/qamarkdownview.go`,
`internal/app/qaview.go`) port Kokonut UI's `ai-citations` component into
native Go, under the MIT License, Copyright (c) 2025 kokonutUI. The full
MIT notice is in `kokonutui/LICENSE`.

[Kokonut UI source](https://github.com/kokonut-labs/kokonutui)

## FreeFolio 上游基线与依赖清单

FreeFolio 基于上游 [Ciao1019/ArcMD](https://github.com/Ciao1019/ArcMD)（MIT License，提交 `27322d387268751932624ef83a7c580cb0b068d5`，标签 `v0.2.1`）持续演进，所有既有第三方声明、许可正文与署名保持完整保留。

- 上游来源与提交摘要登记在 [upstreams.json](manifests/upstreams.json)；
- Go 模块直接与传递依赖、哈希与许可登记在 [go-mod.json](manifests/go-mod.json)；
- 当 `go.mod` 或 `go.sum` 变更时，需运行 `python -X utf8 tools/update_go_inventory.py` 重新核对依赖并同步更新清单。

FreeFolio additionally adopts selected fixes and native features from ArcMD
`5b2a59ae6ac5bc06009a70ab2a26a1c625593d08` (v0.2.4): editor input,
HTML comment preservation, document and image lifecycle, attachment folders,
sidebar multi-selection, document mind maps, Mermaid viewing and equation tags.
The original MIT notice remains in `LICENSE`. The selected scope is recorded in
`manifests/upstreams.json`; this is not a wholesale replacement of the baseline.
Editor parity with ArcMD `3bb4a3342b3f5ffe8cf65898b6bbd2a8f3f85376` (v0.2.5)
adds keyboard keys, live inline HTML tags, wrapped and crisp formulas,
content-sized tables, literal copying and the autosave switch, under the same
MIT notice.
From ArcMD `3c94584def6aaa5e6406ae2e30dd76c2990cb165` (v0.2.8), FreeFolio ports
the inline menu paste fixes, mind map list ordinals, the block handle gutter,
continuous whiteboard freedraw, vertical caret movement, the macOS mind map
shortcut, whole-block whiteboard and picture deletion, the failed-picture
placeholder menu, the KDE Wayland client window controls and the PDF reader
(`internal/pdfdoc`, `internal/editor/pdfview.go`), system proxy detection
(`internal/sysproxy`), document import by drag and drop, full-width pages and
the HTML export's code highlighting and Mermaid SVG (`internal/mermaid/svg.go`),
under the same MIT notice. The KDE window frame calls GTK at run time through
[purego](https://github.com/ebitengine/purego) (Apache-2.0), recorded in
`manifests/go-mod.json`.

The WebDAV/S3 transport, Windows DPAPI credential protection and PBKDF2/AES-GCM
content encryption in `internal/cloudsync` reuse BIGFA's implementation from
[FreeType](https://github.com/OUBIGFA/FreeType) at
`6cc7a6713c56e229b5bc0dbdb826282362d57195`. These are maintainer-authored Go files,
adapted for FreeFolio's per-space synchronization, conditional writes, conflict
copies, credential separation and cancellation. Source file hashes are recorded
in `manifests/upstreams.json`. No FreeType input engine, dictionary, language
model, or GPL reference implementation is included. Synchronization uses the Go
standard library and the existing `golang.org/x/sys` dependency; it does not
include ArcMD's WASM runtime or bundled Cloud Sync plugin.

Cloud sync's connection idle timeout in `internal/cloudsync/timeout.go` adapts
[rclone](https://github.com/rclone/rclone)'s `fs/fshttp/dialer.go` at
`e982ff36668f5130d94b4f70409babb3d964df42` (v1.76.0), under the MIT License.
The full notice is in `rclone/LICENSE`, with source evidence in
`manifests/upstreams.json`. Its WebDAV property handling, cancellation checks and
bisync no-change result also informed this repair. Remote inventory now adapts
the bounded directory walking strategy in `fs/walk/walk.go` and recursive S3
listing in `backend/s3/s3.go` from the same pinned revision. The adaptation uses
four WebDAV workers with shared cancellation and S3 pagination without a delimiter;
FreeFolio's path, size, duplicate and completeness checks remain mandatory. FreeFolio retains its own guarded
sync engine; no rclone executable, backend registry or module dependency is bundled.

OneBot's `read/edit/write/patch` tools port the MIT OpenCode implementation at
`85951cb08614dfe6443905552cae49d59d2d6615`. The exact matching and patch parser are
in `internal/fileedit`; provider-independent tool adapters are in
`internal/agent/edit.go`. See [source mapping, compatibility fixtures and host
adaptations](opencode/README.md) and [the MIT notice](opencode/LICENSE).

The whiteboard AI workflow (`internal/whiteboard/ai*.go`, `canvas.go`,
`internal/editor/board_ai*.go`) ports LiteGraph's graph rules, the core of
ComfyUI, from the MIT [Comfy-Org/litegraph.js](https://github.com/Comfy-Org/litegraph.js)
at `ea0e1a85c612454540152aa56b0d1e104738e6dc`: typed slots, one link per
input, free-slot search, widget inputs disabled while linked, spline links and
link drag and drop. See [the port mapping](litegraph/README.md) and [the MIT
notice](litegraph/LICENSE). Its node bar and per-node result history follow
the design of [Vibe-Workflow](https://github.com/SamurAIGPT/Vibe-Workflow)
(MIT) at `121bde4ef0499ea000be98bdeaf1b244b928a8f3`. Board-aware OneBot
tools follow Dim0's described design; the scheduler's cascading cancellation
follows Azure/go-workflow's description. Apart from the LiteGraph port, no code of these projects, nor of the
GPL, AGPL or commercially restricted projects named in the research report
(ComfyUI, NodeTool, Jaaz, Refly, tldraw), is included. JSON Canvas import and
export implement the open [JSON Canvas 1.0](https://jsoncanvas.org) format
(MIT). Image and video generation call OpenAI's Images, Videos and Responses
APIs and Google's Gemini, Imagen and Veo APIs directly from their public
documentation, with the request conventions OpenAI-compatible gateways such as
new-api and LiteLLM accept; no SDK or gateway code is bundled.
