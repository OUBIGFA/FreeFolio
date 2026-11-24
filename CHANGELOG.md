# Changelog

## FreeFolio 0.1.0

FreeFolio's first public release: a local-first native Markdown writing and knowledge studio built with Go and MyGo.

- Native Markdown and legacy `.arcmd` editing with full-width page mode, drag-and-drop document import, HTML export with syntax highlighting and Mermaid diagrams, vector PDF export, and document mind maps.
- Open Markdown files outside a space in separate tabs, save edits in place or save a copy, and keep file browsing limited to the files explicitly opened.
- Native whiteboards from the home page or title bar with split-pane support, notes, shapes, and arrows.
- OneBot AI assistant with conversation history, file/image drop attachments, multi-provider & multi-key configuration, ChatGPT OAuth/OIDC connection, PDF full-text search & citations, and native Go LDRS loaders.
- Opt-in WebDAV and S3 cloud synchronization for selected spaces, with conflict copies, recovery backups, optional content encryption, and system proxy auto-detection.
- Verified Windows in-app updates from Settings → Update, and faster startup with deferred post-first-frame work and idle heap memory release.
- Distribute the locally built Windows x64 installer, `FreeFolio-0.1.0-Setup.exe`. Checksums and build records stay local; no automatic-update assets are included.

## Upstream provenance

ArcMD's historical releases are not FreeFolio releases. See the [third-party credits](third_party/README.md) and [pinned source records](third_party/manifests/upstreams.json) for the upstream baseline, selected adaptations and licenses.
