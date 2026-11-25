# Optional document fonts

Fonts are downloaded only when selected. This feature does not add font binaries
to the repository or installer. Existing board resources remain separately listed.

| Font | Fixed source | License | Verification |
| --- | --- | --- | --- |
| LXGW WenKai Regular and Medium | [v1.530](https://github.com/lxgw/LxgwWenKai/releases/tag/v1.530) | [OFL-1.1 and notices](LXGW-WenKai-OFL.txt) | Size and SHA-256 in `internal/fontlibrary/download.go`; both release files verified locally. |
| Zhuque Fangsong | [v0.212](https://github.com/TrionesType/zhuque/releases/tag/v0.212) | [OFL-1.1](Zhuque-Fangsong-OFL.txt) | Selected TTF inside the ZIP; no arbitrary archive extraction. |
| Xiaolai | Quiet.md `dfdf6da369eb0432a7f6c9024ca4f5c7605de4da`, Excalidraw `3a317b5` | [OFL-1.1 and notices](../whiteboard/OFL.txt) | Compressed-byte hash and bounded expansion; existing board provenance applies. |

Files stay unmodified. Family names remain those supplied by their authors.
Publisher licenses accompany FreeFolio under `third_party/`.
