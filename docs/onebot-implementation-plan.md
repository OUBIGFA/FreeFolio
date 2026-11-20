# OneBot Implementation Plan

> Execute inline in the existing main checkout. The approved project rules prohibit unsolicited branches and subagents. Track implementation evidence in the ignored task workspace; publish only durable source, tests and documentation.

**Goal:** Deliver the approved document editing workflow and OneBot multi-provider model management together.

**Architecture:** Keep provider-neutral conversations in `internal/agent`, OpenCode file-tool ports in `internal/fileedit`, native transactions in `internal/document`, and native interaction and persistence coordination in `internal/app`. Reuse the existing editor, history, credential store and atomic file writer.

**Tech Stack:** Go and MyGo versions from `go.mod`, no cgo or web UI, standard HTTP/JSON streaming clients.

**Spec:** [AI editing design](ai-editing-design.md).

## Global constraints

- Existing main checkout, preserve unrelated changes, no tag or public release.
- No credentials in preferences, documents, logs or repository.
- OneBot starts with fresh AI configuration; do not migrate legacy single-provider fields or keys. Preserve unrelated application preferences.
- AI drafts do not modify the document before application; direct mode applies only completed, validated proposals.
- Document modifications execute on the UI thread and use existing save and history paths.
- Tests use local HTTP servers, isolated files and headless UI. Run required whole-project checks and produce the local Windows installer before final delivery.

## Review focus

- A late model-discovery response from a removed or switched provider must not replace the current provider's model selection.
- A provider switch during a tool conversation must not reuse provider-specific reasoning payloads incorrectly.
- Identical text in two blocks must not cause edits to the wrong block after a pane switch.
- A failed history snapshot, partial stream or cancelled request must never be reported as an applied edit.
- Partial acceptance and multi-file failure must preserve later manual edits and expose precise per-document state.

## Task 1: Provider preferences and credentials

Files: `internal/prefs/prefs.go`, `internal/prefs/onebot.go`, `internal/prefs/onebot_test.go`, `internal/app/onebotproviders.go`.

Interfaces: persistent `AIConnection` and `AIModel`; `AI.Validate()` validation; keyed credentials in the existing secret file; application connection selection returns immutable `agent.Settings`.

- [ ] Test fresh configuration, multiple connections, checked models, duplicate IDs, absent selected model and key isolation.
- [ ] Implement versioned preferences, validation and a fresh keyed credential store; ignore legacy AI fields.
- [ ] Run `go test ./internal/prefs` and credential integration tests; require all selected assertions to pass.

## Task 2: Protocols and capability discovery

Files: `internal/agent/presets.go`, `chat.go`, `agent.go`, new `models.go`, `protocols.go`, `protocols_test.go`, `models_test.go`.

Interfaces: `Settings.NewProvider() Provider`, `Settings.DiscoverModels(context.Context) ([]ModelInfo, error)`, `Response.FinishReason`, provider-neutral `ContentPart`, `Capabilities` and inference parameters.

- [ ] Add HTTP fixtures for Chat Completions, Responses, Anthropic, Gemini and Ollama; assert payloads, auth, tool round trips, streaming text/reasoning, normal and truncated endings.
- [ ] Implement protocol adapters, paginated discovery, bounded reads, cancellation, model capability provenance and catalog merging.
- [ ] Test malformed and duplicate model entries, pagination, unknown capabilities and stale-request behavior.
- [ ] Run `go test ./internal/agent`.

## Task 3: Native OneBot provider/model UI

Files: `internal/app/qasettings.go`, `qa.go`, `qaview.go`, `onebotproviders.go`, provider UI tests, `internal/i18n/en.go`, user-facing command and settings labels.

Interfaces: connection editor, searchable checkbox model list, capability editor, in-conversation connection/model picker using Task 1/2 data.

- [ ] Test adding two connections, listing/checking/manual models, switching during a request, failed fetch preserving enabled models, removal and key deletion errors.
- [ ] Implement the native settings and picker; replace obsolete single-provider tests with equivalent multi-provider behavior tests.
- [ ] Rename user-facing AI Q&A labels to OneBot and supply English translations.
- [ ] Run focused application tests with `ui.NewTester`.

## Task 4: Guarded document changes

Files: `internal/document/aiedit.go`, `aiedit_test.go`, editor fragment bridge.

Interfaces: captured `EditTarget`, validated `EditOperation`, OpenCode file snapshots; validate/apply/invert operations using stable block identity and expected content; one transaction per accepted group.

- [ ] Test text, rich fragment, insertion/deletion, table and metadata operations, non-target preservation, duplicate text, Unicode and stale targets.
- [ ] Implement exact target binding, structural validation, local block IDs and reversible transactions.
- [ ] Test partial acceptance and inverse application after independent manual changes.
- [ ] Run `go test ./internal/document ./internal/editor` with focused filters during development.

## Task 5: Editing tasks and persistence

Files: `internal/app/onebotedit.go`, `onebotstore.go`, tests, `internal/agent/edit.go`, tests; reuse `history` and `document.go`.

Interfaces: app-owned task state captures request, scope, candidates and target documents; OpenCode file-tool results feed Task 4; checkpoint/draft records contain no credentials.

- [ ] Test single-turn rewrite, tool-driven multi-edit, 2–5 candidates, follow-up, cancellation, stale completions and retry idempotence.
- [ ] Implement read-only discovery plus OpenCode read/edit/write/patch tools, serial main-thread apply, validated new documents, pre-edit snapshots and per-file save outcomes.
- [ ] Test restart draft recovery, saved snapshot recovery, disk conflicts, external files and partial multi-document failure.
- [ ] Run focused agent/app/history tests.

## Task 6: Editing and review UI

Files: `internal/app/oneboteditview.go`, editor context/action wiring, `qaview.go`, palette, i18n, headless regression tests.

- [ ] Test selection capture before focus change, current section/document and explicit document scopes, direct versus review mode, candidate selection and follow-up.
- [ ] Add native review display with computed diffs, per-change/all accept/reject, original navigation, cancel, retry, undo and recovery controls.
- [ ] Test split panes, composition, read-only documents, keyboard actions and reduced motion.
- [ ] Verify UI snapshots when available; do not operate the user's running instance.

## Task 7: Whole-project validation and delivery

- [ ] Review implementation against every acceptance row in the approved design; fix gaps and failures.
- [ ] Run formatting, vet, full tests, Windows/Linux builds, repository checker and its behavioral tests.
- [ ] Build the Windows x64 test installer through `tools/build_test_installer.py`; verify installer, checksum and build-info.
- [ ] Review staged diff and tracked boundaries, commit only this task, verify remote heads/tags and push main without force.
- [ ] Recycle owned temporary task artifacts after preserving the final verification report in the ignored output folder.

## Final user correction: direct OpenCode reuse

- Port the actual OpenCode edit/patch algorithms and tool schemas at pinned
  revision `85951cb08614dfe6443905552cae49d59d2d6615`; remove custom model-facing
  edit schemas. Keep native transactions solely as the editor integration.
- Preserve the original Q&A timeline. Model/attachment controls stay in the
  composer; advanced editing and provider settings stay in menus. Attach a
  compact change summary and expandable review to each answer.
- Compatibility evidence: 85 upstream patch invocations and 36 edit fixtures
  recorded against the original implementation; `internal/fileedit` tests
  compare the Go port to those results.
- Build coordination: OneBot and whiteboard share the checkout. First finisher
  performs focused checks and hands off; last finisher runs unified checks,
  Windows/Linux builds and `tools/build_test_installer.py`. Do not package twice
  concurrently. Runtime UI acceptance is separate from headless tests.
