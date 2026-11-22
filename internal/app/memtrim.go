package app

import (
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// memTrimDelay lets a burst of image decodes, as a document full of photos
// opening, end before the memory goes back.
const memTrimDelay = time.Second

// imageDecodesDone counts the pictures decoded off the main thread, local
// and from the web, for memTrim to notice a burst of decodes.
var imageDecodesDone atomic.Uint64

// memTrim hands the Go heap's free pages back to the system once image
// decoding rests, and as the window hides. Decoding large photos leaves
// hundreds of MB of garbage: the bitmaps drop their pixels once shown, but
// the runtime keeps the freed pages for minutes, and the process shows
// them as its own. Main thread only, but for running, which free's
// goroutine clears.
type memTrim struct {
	decodes uint64
	at      time.Time
	hidden  bool
	running atomic.Bool
	// free returns the memory; nil is debug.FreeOSMemory (tests replace it).
	free func()
}

// tick runs each frame: it only reads counters, and frees on a goroutine,
// as a full collection is no work for a frame.
func (m *memTrim) tick(c *ui.Context) {
	now := c.Now()
	hidden := c.WindowHidden()
	trim := hidden && !m.hidden
	m.hidden = hidden
	if n := imageDecodesDone.Load(); n != m.decodes {
		m.decodes, m.at = n, now.Add(memTrimDelay)
	}
	if !m.at.IsZero() {
		// Decodes still running keep the burst going: wait for them.
		if wait := m.at.Sub(now); wait > 0 || len(imageDecodeSem) > 0 {
			c.After(max(wait, memTrimDelay/4))
		} else {
			trim = true
		}
	}
	if !trim {
		return
	}
	if !m.running.CompareAndSwap(false, true) {
		// The last one still runs: try again shortly.
		c.After(memTrimDelay / 4)
		return
	}
	m.at = time.Time{}
	free := m.free
	if free == nil {
		free = debug.FreeOSMemory
	}
	go func() {
		defer m.running.Store(false)
		free()
	}()
}
