# Optional Git tools

FreeFolio runs Git as a separate process. It does not link Git, embed a web UI,
or bundle these optional executables in its default installer.

| Tool | Version | Source | License |
| --- | --- | --- | --- |
| Git for Windows / MinGit | 2.56.1.windows.1 | https://github.com/git-for-windows/git/releases/tag/v2.56.1.windows.1 | Git core GPL-2.0; the unchanged official archive includes additional component notices |
| Git LFS | 3.8.1 | https://github.com/git-lfs/git-lfs/releases/tag/v3.8.1 | MIT |
| Git Credential Manager | 3.0.2 | https://github.com/git-ecosystem/git-credential-manager/releases/tag/v3.0.2 | MIT |

Downloads come directly from the publishers' releases, retaining archive license
files. Windows x64 and ARM64 are pinned by SHA-256 in
`internal/gitsync/runtime.go`, `internal/gitsync/lfs.go` and `internal/gitsync/gcm_runtime.go`. The release API's
asset digest and byte size were checked on 2026-11-27. Linux and macOS use the
system Git and, when required, system Git LFS. GCM is also available as a pinned
optional download for macOS and Linux x64/ARM64. Its upstream .NET/native build
is a separate executable, not linked into FreeFolio or bundled in the installer.

GitHub browser login is delegated to the official GCM executable and its
registered OAuth application. No GCM client identifiers or secrets are copied
into FreeFolio. The publisher's login requests repo, gist and workflow scopes;
FreeFolio executes sync only for selected spaces. GCM configuration and storage
are isolated per process and account namespace, without changing global Git
settings. Missing system credential services produce an error, never plaintext
fallback. See the upstream [usage](https://github.com/git-ecosystem/git-credential-manager/blob/v3.0.2/docs/usage.md)
and [credential stores](https://github.com/git-ecosystem/git-credential-manager/blob/v3.0.2/docs/credstores.md).

The workflow was compared with Obsidian Git 2.44.0 (MIT). No Obsidian Git source
code, assets, JavaScript dependencies, or executable is incorporated.
