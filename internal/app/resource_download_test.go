package app

import (
	"context"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/egoist/mygo/ui"
)

func TestPackProgressShowsOwnSizeSeparateFromDependencies(t *testing.T) {
	for _, tc := range []struct {
		id, ownText, dependency string
		ownBytes                int64
	}{
		{"z-detail", "162.2 MiB", "底模", 170127776},
		{"seedvr2-3b-int8", "3.22 GiB", "VAE", 3458259704},
		{"comfy-realesrgan-anime", "17.1 MiB", "", 17938799},
	} {
		t.Run(tc.id, func(t *testing.T) {
			pack, _ := localresource.ComfyPack(tc.id)
			download, err := localresource.PackDownload(pack)
			if err != nil {
				t.Fatal(err)
			}
			a := &App{}
			s := &a.resources
			a.setPackDownloadProgress(pack, download)
			if s.total != tc.ownBytes {
				t.Fatalf("display total includes dependencies: %d, want %d", s.total, tc.ownBytes)
			}
			s.resume = func() {}
			s.received.Store(s.ownStart + tc.ownBytes/2)
			tt := ui.NewTester(func(c *ui.Context) { ui.Row(c).Gap(6).Children(func() { a.resourceDownloadControls(c) }) }, 650, 180)
			tt.Frame()
			p, ok := tt.Access("下载进度")
			if !ok || p.Now < .499 || p.Now > .501 {
				t.Fatalf("progress counts dependencies as the model: %+v", p)
			}
			_, _, detail := a.packDetailAndBadge(pack)
			if !strings.Contains(detail, tc.ownText) {
				t.Fatalf("model size missing: %s", detail)
			}
			if tc.dependency != "" && !tt.HasText(tc.dependency+"下载") {
				t.Fatal("dependency progress is not identified separately")
			}
			if tc.id == "z-detail" && !strings.HasPrefix(detail, "LoRA：162.2 MiB") {
				t.Fatalf("LoRA size appears to describe the base: %s", detail)
			}
			if s.ownStart > 0 {
				s.received.Store(s.ownStart / 2)
				tt.Frame()
				for _, node := range tt.AccessAll() {
					if node.Role != ui.RoleProgress {
						continue
					}
					if node.Name == "下载进度" && node.Now != 0 {
						t.Fatal("base bytes advanced LoRA progress")
					}
					if node.Name == "底模下载" && (node.Now < .499 || node.Now > .501) {
						t.Fatal("base progress is not independent")
					}
				}
			} else if s.dependencyTotal > 0 {
				s.received.Store(s.total + s.dependencyTotal/2)
				tt.Frame()
				for _, node := range tt.AccessAll() {
					if node.Role != ui.RoleProgress {
						continue
					}
					if node.Name == "下载进度" && node.Now != 1 {
						t.Fatal("VAE bytes changed completed model progress")
					}
					if node.Name == "VAE下载" && (node.Now < .499 || node.Now > .501) {
						t.Fatal("VAE progress is not independent")
					}
				}
			}
		})
	}
}

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
