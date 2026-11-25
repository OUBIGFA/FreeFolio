# Whiteboard sources and adaptation

FreeFolio's native whiteboard follows Quiet.md commit
`e59c49adc3eb0bf8dabaec2df7e7d64f5a97e00a` and its pinned Excalidraw
`3a317b55f508815d10a4c12acca99df30f026521` baseline. Quiet.md's MIT notice is
retained in [../quiet-md/LICENSE](../quiet-md/LICENSE). Excalidraw and the
ported drawing algorithm notices are in [LICENSE](LICENSE).

The Go domain model retains standard scene fields and unknown fields rather
than flattening bound text or discarding images. The rough.js 4.6.4 generator,
renderer, scanline fillers, SVG path parser and curve sampling are translated
from Quiet.md's `crates/core/src/board/rough/`. `testdata/rough.json` contains
external rough.js results. The tests compare all 86 cases at 1e-6 tolerance.
The freehand outline adapts perfect-freehand 1.2.0; text wrapping and TrueType
subsetting follow the corresponding Quiet.md modules. Fractional order-key
integer generation uses the CC0 algorithm, whose notice is [CC0.txt](CC0.txt).

The scanline trig helper preserves fdlibm rounding because an ULP difference
can change the number of dashes and all subsequent seeded paths. Its notice
is [FDLIBM-NOTICE.txt](FDLIBM-NOTICE.txt).

The fonts in `internal/editor/boardfonts/` are copied without modification
from Quiet.md's `assets/fonts/excalidraw/`. [OFL.txt](OFL.txt) identifies the
OFL faces and reserved names. Comic Shanns uses its separate
[MIT notice](LICENSE-ComicShanns.txt). Font IDs match Excalidraw. Xiaolai is
loaded from the original zlib file and subset for SVG exports. Helvetica and
Liberation Sans IDs use a compatible system sans face; the GPL font is not
bundled. Every bundled font is registered in the repository binary manifest.

The SVG icons in `internal/editor/boardicons/` are the corresponding pinned
Quiet.md files. Each retains the Excalidraw/Tabler source comment. No Nucleo
icons, GPUI runtime, Rust library, vello renderer or platform patch is included.
The AI node icons `sparkles.svg`, `player-play.svg` and `player-stop.svg` are
Tabler Icons paths under the [MIT License](LICENSE-Tabler.txt), each with its
source comment.

Markdown, wiki links, library and Mermaid integration use FreeFolio's existing
native services and reference ArcMD `70711e1694699207400f751c01d28b11bdaefac5`.
Its existing MIT notice remains at the repository root. Architecture symbols
are composed of standard elements rather than exported as private shape types.

Source-file hashes and licenses are recorded in
[../manifests/upstreams.json](../manifests/upstreams.json).
