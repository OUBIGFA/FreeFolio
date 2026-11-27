package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// A pause cancels the request and keeps the row reserved until its writer has
// closed. Resume starts a fresh ranged request from the persisted partial.
func (a *App) pauseResourceDownload() {
	s := &a.resources
	if s.cancel == nil || s.pausing || s.stopping {
		return
	}
	s.pausing = true
	s.cancel()
}

func (a *App) stopResourceDownload() {
	s := &a.resources
	if s.paused {
		s.paused = false
		s.busy = ""
		s.resume = nil
		s.dir = ""
		return
	}
	if s.cancel != nil {
		s.pausing = false
		s.stopping = true
		s.cancel()
	}
}

func (a *App) finishResourceDownload(err error) {
	s := &a.resources
	s.cancel = nil
	if s.pausing && errors.Is(err, context.Canceled) {
		s.pausing = false
		s.paused = true
		return
	}
	s.busy, s.dir = "", ""
	s.resume = nil
	s.paused, s.pausing, s.stopping = false, false, false
	if !errors.Is(err, context.Canceled) {
		s.err = err
	}
}

func (a *App) resourceDownloadControls(c *ui.Context) {
	s := &a.resources
	if s.resume == nil {
		ui.Text(c, i18n.T("正在移除资源")).FontSize(12)
		return
	}
	value := float64(0)
	if s.total > 0 {
		value = min(1, float64(s.received.Load())/float64(s.total))
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
		ui.Text(c, fmt.Sprintf("%s / %s", formatResourceBytes(s.received.Load()), formatResourceBytes(s.total))).FontSize(11.5).SingleLine()
		ui.Progress(c.Key("bar"), value).WidthPercent(100).Label(i18n.T("下载进度"))
	})
	if s.paused {
		if ui.Button(c.Key("resume"), i18n.T("继续下载")).Clicked() {
			s.resume()
		}
	} else if ui.Button(c.Key("pause"), i18n.T("暂停")).Disabled(s.pausing || s.stopping).Clicked() {
		a.pauseResourceDownload()
	}
	if ui.Button(c.Key("stop"), i18n.T("停止")).Disabled(s.stopping).Clicked() {
		a.stopResourceDownload()
	}
	if !s.paused {
		c.After(150 * time.Millisecond)
	}
}
