package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/egoist/mygo/ui"
)

func TestResourceTransfersHaveIndependentControls(t *testing.T) {
	a := &App{}
	first := a.resources.resourceTransferFor("one")
	second := a.resources.resourceTransferFor("two")
	one, cancelOne := context.WithCancel(context.Background())
	defer cancelOne()
	two, cancelTwo := context.WithCancel(context.Background())
	defer cancelTwo()
	first.busy, first.cancel, first.total = "one", cancelOne, 100
	second.busy, second.cancel, second.total = "two", cancelTwo, 200
	first.received.Store(25)
	second.received.Store(160)
	first.resume = func() {}
	second.resume = func() {}
	a.resources.pauseResourceDownload(first)
	a.resources.finishResourceDownload(first, context.Canceled)
	if !first.paused || one.Err() == nil || two.Err() != nil || second.received.Load() != 160 {
		t.Fatal("pause affected another download")
	}
	a.resources.stopResourceDownload(first)
	if first.busy != "" || !a.resources.resourcesDownloading() {
		t.Fatal("stop removed other active task")
	}
	a.resources.finishResourceDownload(second, errors.New("network failed"))
	if second.err == nil || first.err != nil || a.resources.resourcesDownloading() {
		t.Fatal("failure state leaked across rows")
	}
}

func TestActiveDownloadKeepsOtherResourcesAvailable(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabResources)
	a.resources.tab = 3
	tt.Frame()
	job := a.resources.resourceTransferFor("z-detail")
	job.busy = "z-detail"
	job.total = 100
	job.received.Store(40)
	job.cancel = func() {}
	job.resume = func() {}
	tt.Frame()
	found := false
	for _, n := range tt.AccessAll() {
		if n.Name == "下载 V1 Realism" {
			found = true
			if n.Disabled {
				t.Fatal("other model download disabled")
			}
		}
		if n.Role == ui.RoleTab && n.Disabled {
			t.Fatal("download locks resource tabs")
		}
	}
	if !found {
		t.Fatal("other download missing")
	}
	pack, _ := localresource.ComfyPack("z-detail")
	a.resources.confirm = pack.ID
	a.resources.packAction(pack, true)
	if job.busy != "z-detail" {
		t.Fatal("resource removed while downloading")
	}
	before := a.resources.resourceDirectory()
	a.resources.setResourceDirectory(t.TempDir())
	if a.resources.resourceDirectory() != before {
		t.Fatal("moved directory during download")
	}
}

func TestParallelResourceProgressSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect parallel downloads")
	a, tt := newTestApp(t)
	tt.SetSize(1440, 1000)
	a.openSettings(tabResources)
	a.resources.tab = 4
	tt.Frame()
	for i, id := range []string{"ace15-turbo", "stable-audio3"} {
		pack, _ := localresource.ComfyPack(id)
		plan, _ := localresource.PackDownload(pack)
		job := a.resources.resourceTransferFor(id)
		job.busy = id
		job.cancel = func() {}
		job.resume = func() {}
		a.resources.setPackDownloadProgress(job, pack, plan)
		job.received.Store(job.total * int64(i+1) / 4)
	}
	tt.Frame()
	savePNG(t, filepath.Join(dir, "parallel-downloads.png"), tt)
}
