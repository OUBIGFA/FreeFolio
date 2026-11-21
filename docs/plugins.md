# Plugins and the marketplace

> Status: **draft**. The manifest, the registry index, installing, enabling
> and the `Host` contract exist (`internal/plugin`); the runtimes and the
> marketplace UI do not yet.

FreeMD's core edits Markdown, organizes it into spaces, and includes native
whiteboards and OneBot. OneBot can answer questions and propose edits using
the AI service the user configures; these are built-in features, not plugins.
The contracts below describe a future extension point. FreeMD does not ship
plugin runtimes, a marketplace UI, or an official registry. The examples are
protocol illustrations, not installable downloads.

## A plugin

A plugin is a folder (distributed as a zip) with `freemd-plugin.json` at its
root:

```json
{
  "id": "dev.freemd.ai-writer",
  "name": "AI Writer",
  "version": "0.1.0",
  "description": "Rewrite, summarize and continue your writing.",
  "author": "FreeMD",
  "license": "MIT",
  "engine": "0.1.0",
  "runtime": "wasm",
  "main": "plugin.wasm",
  "permissions": ["document:read", "document:write", "network", "secrets", "ui:panel"],
  "contributes": {
    "commands": [
      { "id": "rewrite", "title": "AI: Rewrite Selection", "when": "selection" },
      { "id": "summarize", "title": "AI: Summarize Document", "when": "document" }
    ],
    "panels": [{ "id": "chat", "title": "Chat", "icon": "✦" }]
  }
}
```

| Field | Rule |
| --- | --- |
| `id` | Reverse-DNS, lowercase, unique in the marketplace; also the plugin's folder name |
| `version`, `engine` | Semantic versions; `engine` is the lowest FreeMD version supported |
| `runtime` | `wasm` (default, sandboxed) or `process` |
| `main` | Path inside the plugin: the `.wasm` module, or the executable |
| `permissions` | Only those listed in `plugin.Permissions`; shown to the user before install |
| `contributes.commands` | Appear in the command bar; `when` is `""`, `document` or `selection` |
| `contributes.panels` | Sidebar panels whose content the plugin returns as Markdown; need `ui:panel` |

## Plugins don't draw

FreeMD's UI is native, drawn by MyGo. Plugins never get a canvas or a
webview: they **contribute** commands and panels, which FreeMD draws in its
own style, and **answer** invocations with a `Response`:

- `edits` — replacements in the open document, applied as one undo step
  (`document:write`);
- `markdown` — shown in the invoked panel, or in a sheet for a command;
- `toast` — a short message.

This keeps every plugin consistent with the Arc/Zen look, accessible, and
unable to spoof FreeMD's own UI. Streaming (an AI reply arriving token by
token) will be added to the protocol as a sequence of `markdown` updates,
which `ui/markdown`'s `Stream` already renders.

## Runtimes

- **`wasm`** — a WASI module run in FreeMD's process by
  [wazero](https://wazero.io) (pure Go, no cgo, matching MyGo's rule). The
  module gets no file system or network of its own: it calls host functions,
  which check the manifest's permissions. Network calls go through FreeMD
  (with the system proxy, via MyGo's `netproxy`), secrets through
  `mygo.Secrets`. Portable: one build runs on every platform.
- **`process`** — an executable speaking JSON-RPC 2.0 over stdin/stdout,
  as language servers do, for plugins that need a native toolchain or a
  local model. It runs with the user's rights, so the marketplace marks such
  plugins and requires a review.

Both implement `plugin.Host`; `Instance.Invoke(Request) → Response` is the
whole protocol surface for v1.

## The marketplace

A future registry can publish a static `index.json` in a public Git
repository, with contributions validated and reviewed before publishing.
No official FreeMD registry exists. The example below uses a reserved
placeholder host rather than an upstream project's download address.

```json
{
  "version": 1,
  "plugins": [
    {
      "id": "dev.freemd.ai-writer",
      "name": "AI Writer",
      "version": "0.1.0",
      "description": "Rewrite, summarize and continue your writing.",
      "tags": ["ai", "writing"],
      "permissions": ["document:read", "document:write", "network", "secrets", "ui:panel"],
      "download": "https://example.invalid/freemd-plugins/ai-writer-0.1.0.zip",
      "sha256": "…"
    }
  ]
}
```

Installing (`Manager.Install`) — all enforced and tested:

1. Download over HTTPS only, size-capped.
2. The zip's SHA-256 must equal the index's.
3. Entries with absolute paths, `..`, backslashes or symbolic links are
   refused; the unpacked size is capped.
4. The manifest must parse, validate, and match the index's `id` and
   `version`.
5. The new version replaces the old atomically (staged, then renamed);
   the user's enabled/disabled choice is kept.

Plugins live in `<config dir>/FreeMD/plugins/<id>/`; `plugins.json` there
lists the disabled ones.

## Roadmap

1. wazero host with the permission checks, and the host functions for
   documents, network and secrets.
2. Permission prompt on install; marketplace browser in the Plugins dialog.
3. Official registry repository with CI validation.
4. First-party plugins: AI Writer (provider-agnostic: Anthropic, OpenAI,
   local models), then others.
5. Signing releases (minisign/sigstore) on top of the index's digests.
