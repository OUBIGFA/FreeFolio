package app

import (
	"testing"
	"time"
)

// A burst of image decodes hands the freed memory back once, after the
// decodes rest; hiding the window hands it back too.
func TestMemTrimAfterDecodesAndOnHide(t *testing.T) {
	a, tt := newTestApp(t)
	freed := make(chan struct{}, 8)
	a.memTrim.free = func() { freed <- struct{}{} }
	waitFree := func(what string) {
		t.Helper()
		select {
		case <-freed:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: memory not returned", what)
		}
	}
	noFree := func(what string) {
		t.Helper()
		select {
		case <-freed:
			t.Fatalf("%s: memory returned too early", what)
		default:
		}
	}
	// Startup's own decodes settle first.
	tt.Frame()
	tt.Advance(2 * memTrimDelay)
	tt.Frame()
	for len(freed) > 0 || a.memTrim.running.Load() {
		select {
		case <-freed:
		case <-time.After(5 * time.Second):
			t.Fatal("startup trim never finished")
		}
	}

	imageDecodesDone.Add(1)
	tt.Frame()
	tt.Advance(memTrimDelay / 2)
	tt.Frame()
	imageDecodesDone.Add(1) // the burst goes on
	tt.Frame()
	tt.Advance(memTrimDelay * 3 / 4)
	tt.Frame()
	noFree("during the burst")
	tt.Advance(memTrimDelay / 2)
	tt.Frame()
	waitFree("after the burst")
	tt.Advance(2 * memTrimDelay)
	tt.Frame()
	noFree("once trimmed")

	tt.SetWindowHidden(true)
	tt.Frame()
	waitFree("on hiding")
}
