package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// This measures pre-window preparation against real files with a warm OS
// directory cache. It is not a reboot/cold-disk or native presentation test.
func BenchmarkStartupLargeSpace(b *testing.B) {
	seed, _ := newTestApp(b)
	seed.prefs.StartAtHome = true
	seed.savePrefs()
	for i := range 5000 {
		name := filepath.Join(root(seed), fmt.Sprintf("Note %05d.md", i))
		if err := os.WriteFile(name, []byte("# Note\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	cfg := seed.cfg
	cfg.DeferStartup = true
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := New(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// Run with tools/perf's overlay to exclude the test software rasterizer.
// Synthetic trees isolate sidebar work from directory reads and document I/O.
func BenchmarkLargeSidebar(b *testing.B) {
	for _, folders := range []bool{false, true} {
		name := "10000-files"
		if folders {
			name = "2000-folders"
		}
		b.Run(name, func(b *testing.B) {
			a, _ := newTestApp(b)
			sp := a.state.ActiveSpace()
			a.state.Favorites, sp.Pins, sp.Expanded = nil, nil, nil
			a.doc = nil
			count := 10000
			if folders {
				count = 2000
			}
			nodes := make([]*vault.Node, count)
			for i := range nodes {
				name := fmt.Sprintf("Item %05d.md", i)
				nodes[i] = &vault.Node{Name: name, Path: name, Dir: folders}
			}
			a.setTree(sp, nodes, nil)
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Children(func() { a.spaceColumn(c, sp, true, true) })
			}, 280, 760)
			tt.Frame()
			tt.Advance(time.Second)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tt.Frame()
			}
		})
	}
}
