package app

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// lockHeadlessControl serializes separate CLI/MCP processes before they load
// spaces.json and document baselines. Kernel locks release if a process exits.
func lockHeadlessControl(ctx context.Context, cfg Config) (func(), error) {
	if err := os.MkdirAll(cfg.ConfigDir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(cfg.ConfigDir, "agent-control.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		ok, err := tryControlLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if ok {
			return func() { unlockControl(f); f.Close() }, nil
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
