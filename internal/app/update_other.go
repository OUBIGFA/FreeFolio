//go:build !windows

package app

import (
	"context"

	"github.com/egoist/mygo"
)

// Other platforms keep MyGo's signed bundle updates when configured.
type mygoUpdates struct{}

func newUpdateService(string) updateService { return mygoUpdates{} }
func (mygoUpdates) Enabled() bool           { return mygo.Updater.Enabled() }

func (mygoUpdates) Check(ctx context.Context) (updateRelease, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return nil, err
	}
	return mygoRelease{up}, nil
}

type mygoRelease struct{ up *mygo.Update }

func (r mygoRelease) Version() string { return r.up.Version }
func (r mygoRelease) Notes() string   { return r.up.Notes }
func (r mygoRelease) Download(ctx context.Context, progress func(int64, int64)) error {
	return r.up.Install(ctx, progress)
}
func (mygoRelease) Apply() error { return nil }

func restartAfterUpdate() { mygo.App.Relaunch() }
