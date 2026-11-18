<div align="center">

<img src="assets/FreeMD-logo.png" width="160" alt="FreeMD logo">

# FreeMD

**A customized, local-first native Markdown editor built in Go + MyGo, derived from ArcMD.**

[简体中文](README.md) · English

<a href="mygo.json"><img src="https://img.shields.io/badge/base-ArcMD%20v0.2.1-F21158" alt="Based on ArcMD v0.2.1"></a>
<a href="https://github.com/egoist/mygo"><img src="https://img.shields.io/badge/built%20with-Go%20%2B%20MyGo-00ADD8?logo=go&amp;logoColor=white" alt="Built natively with Go and MyGo"></a>
<a href=".github/workflows/repository-check.yml"><img src="https://img.shields.io/badge/CI-Repository%20Check-2088ff?logo=githubactions&amp;logoColor=white" alt="Read-only repository check workflow"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-C912D6" alt="MIT License"></a>

</div>

---

## Overview

**FreeMD** is an active fork and deep customization of [Ciao1019/ArcMD](https://github.com/Ciao1019/ArcMD) (MIT License, baseline commit `27322d387268751932624ef83a7c580cb0b068d5` / `v0.2.1`), focused on native performance tuning, tailored workflows, and iterative feature improvements.

It builds on the pure Go + [MyGo](https://github.com/egoist/mygo) native UI stack with in-place `.md` storage and zero HTML, WebView, Electron, or cgo dependencies.

## Running and Verification

### Repository Hygiene Checks

```sh
python -X utf8 tools/check_repo.py
python -X utf8 -m unittest discover -s tests/repository -v
```

### Build and Test

```sh
CGO_ENABLED=0 go run ./cmd/arcmd
CGO_ENABLED=0 go test ./internal/document
```

Use an isolated development home directory so personal spaces are untouched:

```sh
ARCMD_HOME=/tmp/freemd-dev CGO_ENABLED=0 go run ./cmd/arcmd
```

## Documentation and Boundaries

- [Architecture Guide](docs/architecture.md)
- [Plugins and Marketplace Draft](docs/plugins.md)
- [Repository Boundaries](references/repository-boundaries.md)
- [Third-Party Attribution and Manifests](third_party/README.md)

## License

FreeMD follows the upstream [MIT License](LICENSE). Third-party attributions, upstream source records, and Go dependency inventories live in [third_party](third_party/README.md) and [go-mod.json](third_party/manifests/go-mod.json).
