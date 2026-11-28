package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/vault"
)

// Opt-in fixtures must be disposable copies: navigation saves only the test
// app's isolated configuration, but document actions retain ordinary behavior.
func TestSpaceSwitchFixturePerformance(t *testing.T) {
	from, to := os.Getenv("FREEFOLIO_SWITCH_FROM"), os.Getenv("FREEFOLIO_BOARD_FIXTURE")
	if from == "" || to == "" {
		t.Skip("set isolated switch and board fixture paths")
	}
	a, tt := newTestApp(t)
	blog := a.state.Add("Many files", from)
	board := a.state.Add("Whiteboard", filepath.Dir(to))
	board.LastOpen = filepath.Base(to)
	nodes, err := readTree(blog)
	if err != nil {
		t.Fatal(err)
	}
	var expand func([]*vault.Node)
	expand = func(nodes []*vault.Node) {
		for _, n := range nodes {
			if n.Dir {
				blog.Expanded = append(blog.Expanded, n.Path)
				expand(n.Children)
			}
		}
	}
	expand(nodes)
	updates := make(chan func(), 64)
	a.runAsync = func(fn func()) { updates <- fn }
	a.readTreesAsync = true
	drain := func() {
		for {
			select {
			case fn := <-updates:
				fn()
			default:
				return
			}
		}
	}
	for trial := range 3 {
		a.switchSpace(blog.ID)
		tt.Advance(time.Second)
		drain()
		start := time.Now()
		a.switchSpace(board.ID)
		t.Logf("trial %d switch handler: %v", trial, time.Since(start))
		deadline := time.After(30 * time.Second)
		for a.doc != nil && a.doc.boardLoading {
			select {
			case fn := <-updates:
				fn()
				tt.Frame()
			case <-deadline:
				t.Fatal("whiteboard loading timed out")
			}
		}
		t.Logf("trial %d board ready: %v", trial, time.Since(start))
		durations := make([]time.Duration, 60)
		for i := range durations {
			drain()
			start = time.Now()
			tt.Advance(time.Second / 60)
			tt.Frame()
			durations[i] = time.Since(start)
		}
		slices.Sort(durations)
		t.Logf("trial %d frames p50 %v p95 %v max %v", trial, durations[30], durations[57], durations[59])
		if a.doc == nil || a.doc.board == nil {
			t.Fatalf("trial %d did not display the fixture whiteboard", trial)
		}
	}
}
