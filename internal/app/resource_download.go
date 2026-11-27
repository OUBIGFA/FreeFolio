package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/egoist/mygo/ui"
)

// Keep model bytes separate from dependency bytes in the existing sequential
// download plan: LoRA bases come first; SeedVR2's shared VAE comes last.
func (a *App) setPackDownloadProgress(s *resourceTransfer, pack, download localresource.ResourcePack) {
	s.received.Store(0)
	s.total, s.ownStart, s.dependencyTotal, s.dependencyLabel = pack.TotalSize(), 0, 0, ""
	if pack.BaseModel != "" || len(pack.Dependencies) > 0 {
		s.ownStart = download.TotalSize() - pack.TotalSize()
		s.dependencyTotal, s.dependencyLabel = s.ownStart, "底模下载"
		if len(pack.Dependencies) > 0 {
			s.dependencyLabel = "依赖下载"
		}
	} else if pack.Kind == "seedvr2" {
		s.total = pack.Files[0].Size
		s.dependencyTotal, s.dependencyLabel = download.TotalSize()-s.total, "VAE下载"
	}
}

// A pause cancels the request and keeps the row reserved until its writer has
// closed. Resume starts a fresh ranged request from the persisted partial.
func (a *App) pauseResourceDownload(s *resourceTransfer) {
	if s.cancel == nil || s.pausing || s.stopping {
		return
	}
	s.pausing = true
	s.cancel()
}

func (a *App) stopResourceDownload(s *resourceTransfer) {
	if s.paused {
		s.paused = false
		s.busy = ""
		s.resume = nil
		a.resources.dir = ""
		return
	}
	if s.cancel != nil {
		s.pausing = false
		s.stopping = true
		s.cancel()
	}
}

func (a *App) finishResourceDownload(s *resourceTransfer, err error) {
	s.cancel = nil
	s.installing = false
	if s.pausing && errors.Is(err, context.Canceled) {
		s.pausing = false
		s.paused = true
		return
	}
	s.busy, a.resources.dir = "", ""
	s.resume = nil
	s.paused, s.pausing, s.stopping = false, false, false
	if !errors.Is(err, context.Canceled) {
		s.err = err
	}
}

func (a *App) resourceDownloadControls(c *ui.Context, s *resourceTransfer) {
	if s.installing {
		ui.Text(c, i18n.T("正在安装语音插件")).FontSize(12)
		if ui.Button(c.Key("stop"), i18n.T("停止")).Disabled(s.stopping).Clicked() {
			a.stopResourceDownload(s)
		}
		return
	}
	if s.resume == nil {
		ui.Text(c, i18n.T("正在移除资源")).FontSize(12)
		return
	}
	received := s.received.Load()
	ownDone := max(int64(0), min(s.total, received-s.ownStart))
	value := float64(0)
	if s.total > 0 {
		value = float64(ownDone) / float64(s.total)
	}
	ui.Column(c.Key("download-progress")).Width(170).Gap(4).Children(func() {
		label := fmt.Sprintf("%.0f%%", value*100)
		if s.paused {
			label = i18n.T("已暂停") + " · " + label
		}
		if s.pausing {
			label = i18n.T("正在暂停")
		}
		if s.stopping {
			label = i18n.T("正在停止")
		}
		ui.Text(c, label).FontSize(11.5)
		ui.Text(c, fmt.Sprintf("%s / %s", formatResourceBytes(ownDone), formatResourceBytes(s.total))).FontSize(11.5).SingleLine()
		ui.Progress(c.Key("bar"), value).WidthPercent(100).Label(i18n.T("下载进度"))
		if s.dependencyTotal > 0 {
			done := received
			if s.ownStart == 0 {
				done -= s.total
			}
			done = max(int64(0), min(s.dependencyTotal, done))
			ui.Text(c, i18n.T(s.dependencyLabel)).FontSize(11.5)
			ui.Text(c, fmt.Sprintf("%s / %s", formatResourceBytes(done), formatResourceBytes(s.dependencyTotal))).FontSize(11.5).SingleLine()
			ui.Progress(c.Key("dependency-bar"), float64(done)/float64(s.dependencyTotal)).WidthPercent(100).Label(i18n.T(s.dependencyLabel))
		}
	})
	if s.paused {
		if ui.Button(c.Key("resume"), i18n.T("继续下载")).Clicked() {
			s.resume()
		}
	} else if ui.Button(c.Key("pause"), i18n.T("暂停")).Disabled(s.pausing || s.stopping).Clicked() {
		a.pauseResourceDownload(s)
	}
	if ui.Button(c.Key("stop"), i18n.T("停止")).Disabled(s.stopping).Clicked() {
		a.stopResourceDownload(s)
	}
	if !s.paused {
		c.After(150 * time.Millisecond)
	}
}

func (a *App) resourceTransferFor(id string) *resourceTransfer {
	if a.resources.downloads == nil {
		a.resources.downloads = map[string]*resourceTransfer{}
	}
	job := a.resources.downloads[id]
	if job == nil {
		job = &resourceTransfer{}
		a.resources.downloads[id] = job
	}
	return job
}
func (a *App) resourcesDownloading() bool {
	for _, job := range a.resources.downloads {
		if job.busy != "" {
			return true
		}
	}
	return false
}

func (a *App) cancelResourceDownloads() {
	if a.resources.cancel != nil {
		a.resources.cancel()
	}
	for _, job := range a.resources.downloads {
		if job.cancel != nil {
			job.cancel()
		}
	}
}
