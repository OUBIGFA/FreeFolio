package localresource

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type downloadGate struct {
	token    chan struct{}
	users    int
	received atomic.Int64
}

var downloadGates = struct {
	sync.Mutex
	items map[string]*downloadGate
}{items: map[string]*downloadGate{}}

// Catalog packs often share their encoder or base. Serialize only that file;
// unrelated files download in parallel. A cancelled waiter leaves the owner
// running, and a cancelled owner leaves its partial for the next waiter.
func lockDownload(ctx context.Context, target string, progress func(int64)) (*downloadGate, func(), error) {
	key := filepath.Clean(target)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	downloadGates.Lock()
	g := downloadGates.items[key]
	if g == nil {
		g = &downloadGate{token: make(chan struct{}, 1)}
		downloadGates.items[key] = g
	}
	g.users++
	downloadGates.Unlock()
	releaseUser := func() {
		downloadGates.Lock()
		g.users--
		if g.users == 0 {
			delete(downloadGates.items, key)
		}
		downloadGates.Unlock()
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case g.token <- struct{}{}:
			return g, func() { <-g.token; releaseUser() }, nil
		case <-ctx.Done():
			releaseUser()
			return nil, nil, ctx.Err()
		case <-tick.C:
			if progress != nil {
				progress(g.received.Load())
			}
		}
	}
}
