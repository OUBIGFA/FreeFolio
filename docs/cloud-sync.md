# Cloud sync

Open Settings → Cloud sync. Select WebDAV or S3, enter your endpoint and account,
and choose **Save sync settings** followed by **Test connection**. Remote services
require HTTPS. HTTP is accepted only for a loopback service on the same computer.
Certificate verification stays enabled and credential-bearing redirects are not
followed.

Enable the spaces you want to synchronize. Give each a remote folder name.
On another computer, select its corresponding local space and use the same
remote folder. Data lives below the connection's `FreeMD` directory. Neither
computer has to share the other's local space ID or filesystem path.

**Sync now** transfers notes and attachments in both directions. Files larger
than 64 MiB are refused with an error. Hidden folders, FreeMD internal history,
configuration files, credentials, temporary content and generated conflict copies
are excluded. Symbolic links are refused. A remote folder should be dedicated to
one logical space; do not map unrelated local spaces to the same folder.

Automatic sync is off by default. When enabled, choose an interval or a daily
time, an optional daily limit, retry count, and whether to sync on startup. Saves
schedule an additional sync after 15 seconds without another save. These choices
belong to the current computer. The app must remain running for automatic sync.
Closing to the tray keeps the app running; quitting cancels an active transfer.

The engine compares files with the last successful shared baseline. It skips
unchanged remote content when the service supplies a stable ETag. Replacements
and deletions use conditional requests against that ETag. A service that cannot
provide a strong ETag can receive new files, but existing files are not silently
overwritten or deleted.

The status shows the current scan, remote directory or file, and the number of
files already checked. A completed check with no transfers, conflicts or skipped
files shows **Up to date**. Sync still checks remote directory listings: it cannot
assume another computer has made no changes. Upload response ETags are saved
immediately. When a listing omits ETags, conditional GETs can validate cached
content with a 304 response without downloading it again. A service without
usable validators can still require full content reads.

Canceling a run shows **Sync canceled** and does not schedule a failure retry.
Confirmed files retain their baselines, so canceling a later check does not
discard earlier verification work. Scheduled sync remains enabled. Connections
time out after 30 seconds without traffic; the overall run is limited to ten
minutes. These timeouts report a failure instead of leaving a stalled transfer
indefinitely busy. Completed transfers and recovery copies remain in place.

When both copies changed, the local original stays intact and the remote content
is saved beside it as `name (同步冲突 <digest>).md` (or the attachment's extension).
The settings page lists conflicts and offers **Keep local version** and
**Use remote version**. A remote deletion is labeled separately. A decision is
bound to the remote version shown; a later remote change requires another sync.
Conflict copies remain available for inspection and manual merging.

Before replacing or deleting a local file, FreeMD writes a recovery copy under
the application's `sync-state/backups` directory. **Open recovery backups** opens
that directory. Local recovery copies are not automatically deleted. Local
deletions also create a FreeMD trash copy and use the system recycle bin; a
recycle failure leaves the original file in place. Remote overwrites and deletes
keep the previous bytes in `.freemd-history`, subject to the configured retention
count. A file being edited, composed with an input method, conflicted, or marked
read-only is skipped before an incoming write.

Optional content encryption uses PBKDF2-HMAC-SHA256 and AES-256-GCM. Enter the same
content password on each computer. File names and folder structure remain
visible. Enabling encryption also encrypts existing unchanged files on a subsequent
sync; previous recovery backups can retain their earlier plaintext form. A lost
content password cannot be recovered.

Connection settings live in the application's versioned `sync.json`. Passwords
and secret keys are stored separately under `credentials/`, with owner-only
permissions where supported. Windows protects those credential values with
DPAPI for the current user. On other platforms they use the same owner-only
credential files as the existing AI key store. Credentials and local settings
never enter a document space or remote sync payload.

The automated tests use local HTTP WebDAV/S3 servers, documented AWS signature
vectors, two independent document directories, and encrypted fixtures. They do
not certify every hosted service or replace testing an actual account's endpoint,
permissions and ETag support.
