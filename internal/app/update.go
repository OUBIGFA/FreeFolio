package app

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/update"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Software updates, as Arc's and Cra's: FreeFolio checks its GitHub releases
// in the background, ten seconds after launch and every eight hours,
// downloads a new version on its own (unless the user turned that off) and
// says so in a card above the spaces at the foot of the sidebar, or in a
// toast while the sidebar is hidden. Clicking it restarts into the new
// version, whose first launch says "FreeFolio 已更新！". "检查更新…" shows the
// result in Settings > Updates. Windows downloads a verified full installer; other platforms
// use MyGo's signed bundle updates. The views are in updateview.go.

// updateService finds new versions for the current platform.
type updateService interface {
	// Enabled reports whether this build can update itself.
	Enabled() bool
	// Check returns the newer release, or nil when FreeFolio is up to date.
	Check(ctx context.Context) (updateRelease, error)
}

// updateRelease is a newer version of FreeFolio.
type updateRelease interface {
	Version() string
	// Notes are the release notes, in Markdown.
	Notes() string
	// Download stages and verifies the release without interrupting the app.
	Download(ctx context.Context, progress func(done, total int64)) error
	// Apply rechecks and launches the installer after documents are saved.
	Apply() error
}

// updatePhase is the status shown inline in Settings > Updates.
type updatePhase int

const (
	phaseIdle updatePhase = iota
	phaseChecking
	phaseUpToDate
	phaseFound
	phaseDownloading
	phaseReady
	phaseFailed
	phaseDisabled
	phaseNoRelease
	phaseInstalling
)

// updateState is the updater's state.
// updateHost is what self-update needs from the window.
type updateHost interface {
	appConfig() Config
	clock() time.Time
	currentLook() spaceLook
	focusMode() bool
	folded() bool
	leaveDocs(docs []*document, proceed func()) bool
	libraryOpen() bool
	motionReduced() bool
	openSettings(tab string)
	preferences() *prefs.Prefs
	residentDocuments() []*document
	savePrefs()
	savePrefsAfterLaunch()
	titleBar(c *ui.Context) ui.TitleBar
	window() *mygo.Window
}

type updateState struct {
	host updateHost
	svc  updateService
	// async runs checks and downloads on goroutines, as the running app
	// does; tests run them at once.
	async bool
	// enabled is svc.Enabled, asked once: some platforms probe the disk.
	enabled, asked bool
	// next is when the next background check is due.
	next time.Time
	// checking and downloading are the work under way; cancel stops it.
	checking, downloading bool
	cancel                context.CancelFunc
	// manual keeps a requested check distinct from a background check even
	// when the user leaves the settings page while the request is running.
	manual bool
	// generation ignores callbacks from a canceled download after a retry.
	generation uint64
	// installAfterDownload is explicit consent from Download and Install.
	installAfterDownload, installing bool
	// release is the newer version found, downloading or downloaded;
	// received and total its download's progress.
	release         updateRelease
	received, total int64
	// ready is the version downloaded and waiting for a restart; found the
	// one a background check found while automatic updates are off.
	ready, found string
	// updatedTo is the version this launch was updated to, for the
	// "updated" card, until its ✕.
	updatedTo string
	// dismissed hides the ready banner until the next launch.
	dismissed bool
	// phase and failure describe the latest operation in the settings page.
	phase   updatePhase
	failure string
	// told is the last notice toasted, toast the one showing.
	told, toast *update.Notice
	toastFrames int
	// shown is the banner shown last frame and since when, for its
	// entrance animation.
	shown      string
	shownSince time.Time
	// lastCheck is when the last check answered.
	lastCheck time.Time
	// relaunch restarts FreeFolio and openURL opens a page in the browser;
	// tests replace them.
	relaunch func()
	openURL  func(url string)
}

// startUpdates notes, at launch, whether the restart before installed the
// version this FreeFolio is.
func (up *updateState) startUpdates() {
	if v := up.host.preferences().PendingUpdate; v != "" {
		if v == Version {
			up.updatedTo = Version
		}
		up.host.preferences().PendingUpdate = ""
		up.host.savePrefsAfterLaunch()
	}
}

// updatesEnabled reports whether this build can update itself.
func (up *updateState) updatesEnabled() bool {
	u := up
	if u.svc == nil {
		return false
	}
	if !u.asked {
		u.enabled, u.asked = u.svc.Enabled(), true
	}
	return u.enabled
}

// updateTick runs the background checks.
func (up *updateState) updateTick(c *ui.Context) {
	u := up
	if !up.updatesEnabled() {
		return
	}
	now := c.Now()
	if u.next.IsZero() {
		u.next = now.Add(update.FirstCheck)
	}
	if update.Due(u.next, now) {
		u.next = now.Add(update.Interval)
		if !u.checking && !u.downloading && u.ready == "" {
			up.checkUpdates(false)
		}
	}
	c.After(u.next.Sub(now))
}

// run does work off the main thread in the running app, and done back on
// it; tests do both at once.
func (up *updateState) runUpdate(work func() func()) {
	if !up.async || up.host.window() == nil {
		work()()
		return
	}
	go func() {
		done := work()
		up.host.window().Update(done)
	}()
}

// checkForUpdates opens the updates settings page and checks at once.
func (up *updateState) checkForUpdates() {
	u := up
	up.host.openSettings(tabUpdates)
	switch {
	case u.installing:
		u.phase = phaseInstalling
	case u.ready != "":
		u.phase = phaseReady
	case !up.updatesEnabled():
		u.phase = phaseDisabled
	case u.downloading:
		u.phase = phaseDownloading
	case u.checking:
		u.manual = true
	default:
		up.checkUpdates(true)
	}
}

// checkUpdates asks for a newer release; a manual check offers the download
// in settings without starting it automatically.
func (up *updateState) checkUpdates(manual bool) {
	u := up
	u.checking, u.manual = true, manual
	u.phase, u.failure = phaseChecking, ""
	u.release, u.found = nil, ""
	svc := u.svc
	up.runUpdate(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		rel, err := svc.Check(ctx)
		return func() { up.checked(rel, err) }
	})
}

// checked takes a check's answer.
func (up *updateState) checked(rel updateRelease, err error) {
	u := up
	u.checking = false
	u.lastCheck = up.host.clock()
	manual := u.manual
	u.manual = false
	switch {
	case errors.Is(err, update.ErrNoInstaller):
		u.phase = phaseNoRelease
	case err != nil:
		u.phase, u.failure = phaseFailed, i18n.T("无法检查更新：")+err.Error()
	case rel == nil:
		u.phase = phaseUpToDate
		u.release, u.found = nil, ""
	default:
		u.release, u.found = rel, rel.Version()
		u.phase = phaseFound
		if !manual && !up.host.preferences().NoAutoUpdate {
			up.downloadUpdate()
		}
	}
}

// downloadUpdate downloads the release found, which a restart installs.
func (up *updateState) downloadUpdate() {
	u := up
	if u.release == nil || u.downloading {
		return
	}
	rel := u.release
	u.downloading = true
	u.found = ""
	u.generation++
	generation := u.generation
	u.received, u.total = 0, 0
	u.phase, u.failure = phaseDownloading, ""
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	var shown atomic.Int64
	shown.Store(-1)
	up.runUpdate(func() func() {
		err := rel.Download(ctx, func(done, total int64) {
			// Update the progress as the percentage changes, not every block.
			pct := int64(0)
			if total > 0 {
				pct = done * 100 / total
			}
			if !up.async || up.host.window() == nil || shown.Swap(pct) == pct {
				if !up.async || up.host.window() == nil {
					u.received, u.total = done, total
				}
				return
			}
			up.host.window().Update(func() {
				if generation == u.generation {
					u.received, u.total = done, total
				}
			})
		})
		return func() {
			if generation == u.generation {
				up.downloaded(rel, err)
			}
		}
	})
}

// downloaded takes the end of a download.
func (up *updateState) downloaded(rel updateRelease, err error) {
	u := up
	u.downloading = false
	if u.cancel != nil {
		u.cancel()
		u.cancel = nil
	}
	if err != nil {
		u.installAfterDownload = false
		if errors.Is(err, context.Canceled) {
			return
		}
		u.phase, u.failure = phaseFailed, i18n.T("下载更新失败：")+err.Error()
		return
	}
	u.ready, u.found = rel.Version(), ""
	u.dismissed = false
	u.phase = phaseReady
	if u.installAfterDownload {
		u.installAfterDownload = false
		up.installUpdate()
	}
}

// installUpdate restarts into the downloaded version, or downloads the one
// found first. Documents are saved as on quitting: a document that cannot
// be saved keeps FreeFolio open.
func (up *updateState) installUpdate() {
	u := up
	if u.installing {
		return
	}
	if u.release == nil {
		u.ready = ""
		up.checkForUpdates()
		return
	}
	if u.ready == "" {
		u.installAfterDownload = true
		up.downloadUpdate()
		return
	}
	if !up.host.leaveDocs(up.host.residentDocuments(), up.installUpdate) {
		return
	}
	up.host.preferences().PendingUpdate = u.ready
	up.host.savePrefs()
	// Keep the settings page in front while the installer is being launched,
	// so edits cannot resume after the document-save gate has completed.
	up.host.openSettings(tabUpdates)
	u.installing, u.phase, u.failure = true, phaseInstalling, ""
	release := u.release
	up.runUpdate(func() func() {
		err := release.Apply()
		return func() {
			if err != nil {
				u.installing = false
				u.phase, u.failure = phaseFailed, i18n.T("安装更新失败：")+err.Error()
				up.host.preferences().PendingUpdate = ""
				up.host.savePrefs()
				up.host.openSettings(tabUpdates)
				return
			}
			if u.relaunch != nil {
				u.relaunch()
			} else {
				restartAfterUpdate()
			}
		}
	})
}

// cancelUpdateDownload stops only the download. Leaving settings does not
// cancel work, and a canceled download remains available for an explicit retry.
func (up *updateState) cancelUpdateDownload() {
	u := up
	if u.installing {
		return
	}
	u.installAfterDownload = false
	if u.downloading && u.cancel != nil {
		u.cancel()
		u.cancel = nil
		u.downloading = false
		u.generation++
	}
	if u.release != nil {
		u.found, u.phase = u.release.Version(), phaseFound
	}
}

// updateNotice is what the update banner says now.
func (up *updateState) updateNotice() *update.Notice {
	u := up
	return update.Current(u.ready, u.found, u.dismissed)
}

// actOnNotice is a click on the banner or the toast's button.
func (up *updateState) actOnNotice(n update.Notice) {
	u := up
	u.toast = nil
	if n.Kind == update.Ready {
		up.installUpdate()
		return
	}
	up.host.openSettings(tabUpdates)
}

// dismissBanner is a banner's ✕: the ready card hides until the next
// launch, a found update is put off, the updated card goes.
func (up *updateState) dismissBanner() {
	u := up
	switch {
	case u.ready != "":
		u.dismissed = true
	case u.found != "":
		u.found, u.release = "", nil
		u.phase = phaseIdle
	default:
		u.updatedTo = ""
	}
}

// openUpdatePage opens a page about FreeFolio's releases in the browser.
func (up *updateState) openUpdatePage(url string) {
	if up.openURL != nil {
		up.openURL(url)
		return
	}
	mygo.Shell.OpenExternal(url)
}

// updateToastTick toasts the banner's news while the sidebar is hidden.
func (up *updateState) updateToastTick() {
	u := up
	n := up.updateNotice()
	if n == nil {
		u.toast = nil
		return
	}
	visible := !up.host.focusMode() && (!up.host.folded() || up.host.libraryOpen())
	if t := update.Toast(n, u.told, visible); t != nil {
		u.toast, u.told, u.toastFrames = t, t, 0
	}
	if u.toast != nil && *u.toast != *n {
		u.toast = nil
	}
}

// toggleAutoUpdate turns the automatic download of new versions on or off.
func (up *updateState) toggleAutoUpdate() {
	up.host.preferences().NoAutoUpdate = !up.host.preferences().NoAutoUpdate
	up.host.savePrefs()
	u := up
	if !up.host.preferences().NoAutoUpdate && u.found != "" && u.release != nil {
		// Turned on with an update found: download it now.
		u.found = ""
		up.downloadUpdate()
	}
}

// updateSummary says where the updates stand, for the settings.
func (up *updateState) updateSummary() string {
	u := up
	switch {
	case !up.updatesEnabled():
		return i18n.T("此平台暂不支持应用内更新，请从下载页获取新版本")
	case u.installing:
		return i18n.T("正在启动更新安装器")
	case u.downloading:
		return i18n.T("正在下载新版本")
	case u.checking:
		return i18n.T("正在检查更新")
	case u.failure != "":
		return u.failure
	case u.ready != "":
		return "FreeFolio " + u.ready + i18n.T(" 已下载，可以安装")
	case u.found != "":
		return "FreeFolio " + u.found + i18n.T(" 可以更新")
	case u.phase == phaseNoRelease:
		return i18n.T("暂未发布适用于此平台的安装包")
	case u.phase == phaseUpToDate:
		return i18n.T("FreeFolio 已是最新版本")
	}
	return i18n.T("从 GitHub Releases 获取新版本")
}
