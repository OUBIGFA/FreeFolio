package app

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/OUBIGFA/FreeFolio/internal/update"
	"github.com/egoist/mygo"
)

type installerUpdates struct{ client *update.Client }

func newUpdateService(configDir string) updateService {
	return installerUpdates{update.NewClient(filepath.Join(configDir, "updates"), Version)}
}

func (installerUpdates) Enabled() bool { return runtime.GOARCH == "amd64" }

func (s installerUpdates) Check(ctx context.Context) (updateRelease, error) {
	release, err := s.client.Check(ctx)
	if err != nil || release == nil {
		return nil, err
	}
	return installerRelease{release}, nil
}

type installerRelease struct{ installer *update.Installer }

func (r installerRelease) Version() string { return r.installer.Version }
func (r installerRelease) Notes() string   { return r.installer.Notes }
func (r installerRelease) Download(ctx context.Context, progress func(int64, int64)) error {
	return r.installer.Download(ctx, progress)
}

func (r installerRelease) Apply() error {
	path, err := r.installer.VerifiedPath()
	if err != nil {
		return err
	}
	// NSIS reads FreeFolio's registered installation directory itself. This also
	// lets an unpackaged build update without installing into a Go temp folder.
	command := exec.Command(path, "/S", "/FREEFOLIOUPDATE")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

// The installer relaunches the new executable after it replaces the files.
func restartAfterUpdate() { mygo.App.Quit() }
