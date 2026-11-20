# OpenCode native file tools

Source: [anomalyco/opencode](https://github.com/anomalyco/opencode), MIT,
Copyright (c) 2025 opencode.
Pinned revision: `85951cb08614dfe6443905552cae49d59d2d6615`.

OneBot uses native Go ports of OpenCode's actual file-edit algorithms and
model-facing tool contracts. It does not require Node, Bun, a web view or the
OpenCode process. The copyright notice is retained in [LICENSE](LICENSE).

| Upstream source at the pinned revision | FreeMD destination | Reused behavior |
| --- | --- | --- |
| `packages/core/src/tool/plugin/edit.ts` | `internal/fileedit/edit.go`, `internal/agent/edit.go` | `edit(path, oldString, newString, replaceAll)`, matching tiers, ambiguity checks, LF/CRLF mapping, region line alignment, miss hints |
| `packages/util/src/patch.ts` | `internal/fileedit/patch.go` | Envelope/heredoc parser, add/update/delete/move hunks, context/EOF matching, BOM and trailing newline handling |
| `packages/core/src/tool/plugin/patch.ts` | `internal/agent/edit.go` | `patch(patchText)`, prepare the complete batch before mutation, sequential updates to a path |
| `packages/core/src/tool/patch.txt` | `internal/agent/edit.go` | Full upstream model-facing patch description |
| `packages/core/src/tool/plugin/read.ts` | `internal/agent/edit.go` | `read(path, offset, limit)` and numbered text results |
| `packages/core/src/tool/plugin/write.ts` | `internal/agent/edit.go` | `write(path, content)` and preference for local edits |
| `packages/core/test/patch.test.ts` | `internal/fileedit/testdata/opencode-patch.json` | 85 invocations recorded from the upstream tests and original parser/deriver |
| `packages/core/test/tool-edit.test.ts` | `internal/fileedit/testdata/opencode-edit.json` | 36 matching fixtures executed against the upstream edit helpers, including one additional CJK/emoji fixture |

`internal/fileedit/opencode_test.go` compares the Go implementation to these
recorded upstream results. Tests need no network or JavaScript runtime. The
source hashes are recorded in `third_party/manifests/upstreams.json`.

## Native host adaptations

- Tool mutations are applied to an isolated snapshot. User acceptance or the
  selected direct-edit mode commits through FreeMD's document engine, history
  checkpoint and atomic save. Tool output states that files are not yet saved.
- The host resolves only the user's selected document paths. Reading references
  does not grant write access. Selection mode exposes only that excerpt under
  its document path. File deletion/rename is parsed by the upstream engine but
  denied by this content-edit scope; it is not reinterpreted as text deletion.
- File size is bounded to 8 MiB. A line-alignment matrix over 16 million cells
  returns an explicit request to split the edit; matching semantics do not
  silently change. Go uses rune offsets for the upstream one-to-one Unicode
  matching and byte strings for exact file contents.
- Read handles selected text snapshots; existing FreeMD attachment handling
  supplies binary image/PDF/media input. OpenCode's machine/environment routing,
  terminal, formatter/LSP, external-directory permissions and plugin runtime
  are not needed by this document-edit feature and are not embedded.
- `internal/editor/aifileedit.go` maps the resulting Markdown differences into
  native blocks, preserving unchanged structures and local identity. Its
  transaction representation is never exposed as a model response protocol.

## Conversation layout reference

The original FreeMD Q&A timeline remains the interface. The compact composer,
combined provider/model menu and inline expandable file review follow these
MIT OpenCode desktop sources at the same revision:

- `packages/app/src/composer/composer.tsx`
- `packages/app/src/composer/editor/quote-comment.tsx`
- `packages/app/src/session/timeline/message-timeline.tsx`
- `packages/session-ui/src/components/session-review.tsx`

These are interaction/layout references, rebuilt with the existing native
MyGo controls; no React/Solid component or web runtime is included.
