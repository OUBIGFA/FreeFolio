# dim0

Source: [vcmf/dim0](https://github.com/vcmf/dim0/tree/1bf00bae37bf06234271b6e16e014f2395b3d335), MIT.
Reviewed revision: `1bf00bae37bf06234271b6e16e014f2395b3d335` (0.3.103).

Adopted scope:

- Its actual `@canvas-harness/core` 0.2.1 geometry; see [canvas-harness](../canvas-harness/README.md).
- `harness/chrome/style-panel/edge-glyph-options.ts`: straight, bezier, polyline;
  no head, open arrow, filled arrow and barb.
- `lib/graph/settings.ts`: LR/RL/TB/BT and node/rank spacing 75/150.
  FreeFolio implements a native stable layered layout, not Dagre parity.
- `search/notes-search.ts`: whole-board title/body/ID search and node navigation.
- `harness/agent/use-harness-apply-mindmap.ts`: editable nodes/edges inserted in
  one undoable operation. FreeFolio builds these from a local text outline.

FreeFolio retains its scene, file format, history, AI connections and document
services. Nested boards, rich document hosts, JSX mini-apps, code execution,
online image/icon searches, shared accounts, billing and multiplayer are not
part of this adoption. The arrows and shapes are source ports; the layout,
search and outline features adapt behavior to the native application.
