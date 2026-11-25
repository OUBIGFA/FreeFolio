# LiteGraph port

The whiteboard's node graph ports rules of LiteGraph, the graph core of
ComfyUI, from [Comfy-Org/litegraph.js](https://github.com/Comfy-Org/litegraph.js)
at `ea0e1a85c612454540152aa56b0d1e104738e6dc`, under the
[MIT License](LICENSE). Only the rules below are translated to Go; no
TypeScript, canvas runtime or ComfyUI frontend (GPL-3.0) code is included.

| LiteGraph source | FreeFolio port |
| --- | --- |
| `LiteGraphGlobal.isValidConnection` | `whiteboard.AIValidConnection` |
| `utils/collections.findFreeSlotOfType`, `LGraphNode.findInputByType` | `whiteboard.AIFindInputByType` |
| `LGraphNode.connectSlots` (an input holds one link; a new one replaces it) | `whiteboard.AINode.Connect` |
| `LGraphNode.drawWidgets` (`computedDisabled` while a widget's input is linked) | in-node instruction field, `internal/editor/board_ai_card.go` |
| `LGraphCanvas.renderLink`, `#addSplineOffset`, `computeConnectionPoint` | `aiSpline`, `aiSplineAt`, `paintAILink` and circular link centre markers in `internal/editor/board_ai_graph.go`; FreeFolio keeps a screen-sized selection target and disconnects only through Delete or the context menu |
| `canvas/LinkConnector` drop rules, `MovingInputLink`, `release_link_on_empty_shows_menu` | `aiBeginDrag`, `aiDragTo`, `aiDrop`, `aiPickerRoles` in `internal/editor/board_ai_graph.go` |

Growing list inputs follow ComfyUI's growing inputs as a behavior; slot
types, colors, card layout and the board integration are FreeFolio's own.
