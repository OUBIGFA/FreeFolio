# canvas-harness

Source: [winlp4ever/canvas-harness](https://github.com/winlp4ever/canvas-harness), MIT.
Package: `@canvas-harness/core` **0.2.1**, pinned by dim0's lockfile at
`1bf00bae37bf06234271b6e16e014f2395b3d335`.
The npm provenance identifies commit `8f081358f92007dfaa15d4bcaf008e9f1d3f63fb`.

Package integrity (verified before extraction):
`sha512-WZ63eSvRk3yLeEAZFwv4vfREocrRCuV/ysyS8Eohyv9jeUtbRsLSmaBu2xh5kMIv59V7eGB9CVk3zhejSH+upw==`.

FreeFolio ports the SVG shape builders, composite dimensions, text insets,
fill misregistration, three arrowhead outlines, cubic sampling/control rules
and brush pressure/options into `internal/whiteboard/dim0*.go`.
The native AI graph uses the control offset and filled arrowhead; its typed
connections continue to follow LiteGraph. No web renderer is embedded.

`reference/` contains the original MIT geometry sources recovered from the
published package's source map. Their SHA-256 values are in the upstream manifest.
`generate-fixtures.mjs` executes the original shape and arrowhead functions with
Node 24+, producing the JSON fixtures checked by the Go tests.

Brush fixture generation additionally takes the extracted, checksum-verified
perfect-freehand **1.2.3** `dist/esm/index.mjs` path as its first argument.
Its package integrity is
`sha512-bHZSfqDHGNlPpgH2yxXgPHlQSPpEbo+qg7li0M78J9vNAi2yjwLeA4x79BEQhX44lEWpCLSFCeRZwpw0niiXPA==`.
The existing Go perfect-freehand port gains optional tapers. Default pen
behavior remains untapered. The brush fixture compares the actual outline ring.

Adaptations: native paths remain stable across zoom rather than using the
browser's zoom-dependent detail fallback. FreeFolio keeps its color picker,
fonts and theme conversion. Bound connector endpoints follow FreeFolio's
existing binding model. Imported legacy Excalidraw arrows retain their renderer
until the user chooses a dim0 arrow style. Extra endpoint decorations retain
the original renderer. These are compatibility choices, not a full engine port.
