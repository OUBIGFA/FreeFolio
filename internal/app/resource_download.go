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
func (rs *resourceState) setPackDownloadProgress(s *resourceTransfer, pack, download localresource.ResourcePack) {
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
func (rs *resourceState) pauseResourceDownload(s *resourceTransfer) {
	if s.cancel == nil || s.pausing || s.stopping {
		return
	}
	s.pausing = true
	s.cancel()
}

func (rs *resourceState) stopResourceDownload(s *resourceTransfer) {
	if s.paused {
		s.paused = false
		s.busy = ""
		s.resume = nil
		rs.dir = ""
		return
	}
	if s.cancel != nil {
		s.pausing = false
		s.stopping = true
		s.cancel()
	}
}

func (rs *resourceState) finishResourceDownload(s *resourceTransfer, err error) {
	s.cancel = nil
	s.installing = false
	if s.pausing && errors.Is(err, context.Canceled) {
		s.pausing = false
		s.paused = true
		return
	}
	s.busy, rs.dir = "", ""
	s.resume = nil
	s.paused, s.pausing, s.stopping = false, false, false
	if !errors.Is(err, context.Canceled) {
		s.err = err
	}
}

func (rs *resourceState) resourceDownloadControls(c *ui.Context, s *resourceTransfer) {
	t := c.Theme()
	if s.installing {
		ui.Row(c.Key("install-progress")).WidthPercent(100).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("正在安装语音插件")).FontSize(12).TextColor(t.TextMuted).Grow(1)
			if ui.Button(c.Key("stop"), i18n.T("停止")).Disabled(s.stopping).Clicked() {
				rs.stopResourceDownload(s)
			}
		})
		return
	}
	if s.resume == nil {
		ui.Text(c, i18n.T("正在移除资源")).FontSize(12).TextColor(t.TextMuted)
		return
	}
	received := s.received.Load()
	ownDone := max(int64(0), min(s.total, received-s.ownStart))
	value := float64(0)
	if s.total > 0 {
		value = float64(ownDone) / float64(s.total)
	}
	ui.Column(c.Key("download-progress")).WidthPercent(100).Gap(6).Padding(8, 10).Radius(controlRadius).
		Background(t.Text.Alpha(0.025)).Border(1, separator(t)).Children(func() {
		label := fmt.Sprintf("%.0f%%", value*100)
		switch {
		case s.stopping:
			label = i18n.T("正在停止")
		case s.pausing:
			label = i18n.T("正在暂停")
		case s.paused:
			label = i18n.T("已暂停") + " · " + label
		}
		ui.Row(c.Key("main-bar-row")).WidthPercent(100).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Column(c.Key("main-bar-col")).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Row(c).WidthPercent(100).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(t.Text).Grow(1)
					ui.Text(c, fmt.Sprintf("%s / %s", formatResourceBytes(ownDone), formatResourceBytes(s.total))).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
				})
				ui.Progress(c.Key("bar"), value).WidthPercent(100).Label(i18n.T("下载进度"))
			})
			ui.Row(c.Key("dl-buttons")).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
				if s.paused {
					if ui.PrimaryButton(c.Key("resume"), i18n.T("继续下载")).Clicked() {
						s.resume()
					}
				} else if ui.Button(c.Key("pause"), i18n.T("暂停")).Disabled(s.pausing || s.stopping).Clicked() {
					rs.pauseResourceDownload(s)
				}
				if ui.Button(c.Key("stop"), i18n.T("停止")).Disabled(s.stopping).Clicked() {
					rs.stopResourceDownload(s)
				}
			})
		})
		if s.dependencyTotal > 0 {
			done := received
			if s.ownStart == 0 {
				done -= s.total
			}
			done = max(int64(0), min(s.dependencyTotal, done))
			ui.Column(c.Key("dep-bar-col")).WidthPercent(100).Gap(4).Children(func() {
				ui.Row(c).WidthPercent(100).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, i18n.T(s.dependencyLabel)).FontSize(11.5).TextColor(t.TextMuted).Grow(1)
					ui.Text(c, fmt.Sprintf("%s / %s", formatResourceBytes(done), formatResourceBytes(s.dependencyTotal))).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
				})
				ui.Progress(c.Key("dependency-bar"), float64(done)/float64(s.dependencyTotal)).WidthPercent(100).Label(i18n.T(s.dependencyLabel))
			})
		}
	})
	if !s.paused {
		c.After(150 * time.Millisecond)
	}
}

func (rs *resourceState) resourceTransferFor(id string) *resourceTransfer {
	if rs.downloads == nil {
		rs.downloads = map[string]*resourceTransfer{}
	}
	job := rs.downloads[id]
	if job == nil {
		job = &resourceTransfer{}
		rs.downloads[id] = job
	}
	return job
}
func (rs *resourceState) resourcesDownloading() bool {
	for _, job := range rs.downloads {
		if job.busy != "" {
			return true
		}
	}
	return false
}

func (rs *resourceState) cancelResourceDownloads() {
	if rs.cancel != nil {
		rs.cancel()
	}
	for _, job := range rs.downloads {
		if job.cancel != nil {
			job.cancel()
		}
	}
}
