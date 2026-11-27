package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestResourceDownloadControlsPauseResumeAndStop(t *testing.T) {
	a := &App{}
	s := &a.resources
	ctx, cancel := context.WithCancel(context.Background())
	s.busy = "model"
	s.total = 100
	s.received.Store(40)
	s.cancel = cancel
	resumed := 0
	s.resume = func() { resumed++; s.paused = false; s.cancel = func() {} }
	tt := ui.NewTester(func(c *ui.Context) { ui.Row(c).Gap(6).Children(func() { a.resourceDownloadControls(c) }) }, 600, 150)
	tt.Frame()
	progress, ok := tt.Access("下载进度")
	if !ok || progress.Now != .4 {
		t.Fatalf("download progress missing: %+v", progress)
	}
	if err := tt.Click("暂停"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil || !s.pausing || s.paused {
		t.Fatal("pause did not wait for writer cancellation")
	}
	a.finishResourceDownload(context.Canceled)
	tt.Frame()
	if !s.paused || s.busy != "model" || s.received.Load() != 40 {
		t.Fatal("pause lost progress or active task")
	}
	if err := tt.Click("继续下载"); err != nil {
		t.Fatal(err)
	}
	if resumed != 1 || s.paused {
		t.Fatal("resume did not restart the paused task")
	}
	tt.Frame()
	if err := tt.Click("停止"); err != nil {
		t.Fatal(err)
	}
	if !s.stopping || s.busy == "" {
		t.Fatal("stop did not wait for writer completion")
	}
	a.finishResourceDownload(context.Canceled)
	if s.busy != "" || s.resume != nil || s.paused || s.stopping || s.err != nil {
		t.Fatal("stop left an active or failed download")
	}
}

func TestStoppingPausedResourceClearsTask(t *testing.T) {
	a := &App{}
	s := &a.resources
	s.busy = "pack"
	s.paused = true
	s.resume = func() { t.Fatal("stop restarted download") }
	s.received.Store(50)
	a.stopResourceDownload()
	if s.busy != "" || s.paused || s.resume != nil || s.received.Load() != 50 {
		t.Fatal("stopping a paused task lost progress or kept the reservation")
	}
}
