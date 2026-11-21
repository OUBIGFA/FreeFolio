package app

import (
	"testing"
	"time"
)

// The window's icon and the tray icon wait for the first frame to show,
// and are then set once.
func TestLaunchFinishesAfterTheFirstFrame(t *testing.T) {
	a, tt := newTestApp(t)
	finished := 0
	a.launch = launch{pending: true, finish: func() { finished++ }}
	tt.Frame()
	tt.Frame()
	if finished != 0 {
		t.Fatalf("the launch finished %d times before the first frame had shown", finished)
	}
	tt.Advance(launchSettle - time.Millisecond)
	if finished != 0 {
		t.Fatal("the launch finished before launchSettle had passed")
	}
	tt.Advance(time.Millisecond)
	if finished != 1 || a.launch.pending {
		t.Fatalf("after launchSettle the launch finished %d times, pending %v", finished, a.launch.pending)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if finished != 1 {
		t.Fatalf("the launch finished %d times", finished)
	}
}
