# Optional Git tools

FreeFolio runs Git as a separate process. It does not link Git, embed a web UI,
or bundle these optional executables in its default installer.

| Tool | Version | Source | License |
| --- | --- | --- | --- |
| Git for Windows / MinGit | 2.56.1.windows.1 | https://github.com/git-for-windows/git/releases/tag/v2.56.1.windows.1 | Git core GPL-2.0; the unchanged official archive includes additional component notices |
| Git LFS | 3.8.1 | https://github.com/git-lfs/git-lfs/releases/tag/v3.8.1 | MIT |

Downloads come directly from the publishers' releases, retaining archive license
files. Windows x64 and ARM64 are pinned by SHA-256 in
`internal/gitsync/runtime.go` and `internal/gitsync/lfs.go`. The release API's
asset digest and byte size were checked on 2026-11-27. Linux and macOS use the
system Git and, when required, system Git LFS.

The GitHub login follows the public device authorization protocol and uses
Contents read/write access for selected repositories. FreeFolio must register
its own GitHub App; no credentials, Client Secret, or app private key are shipped.

The workflow was compared with Obsidian Git 2.44.0 (MIT). No Obsidian Git source
code, assets, JavaScript dependencies, or executable is incorporated.
