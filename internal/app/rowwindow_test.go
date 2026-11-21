package app

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

// longSpaceApp is an app whose space holds 300 documents in three open
// folders, one of them empty, and a fourth folder closed.
func longSpaceApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	for i := 0; i < 300; i++ {
		if err := vault.Write(sp.Root, fmt.Sprintf("F%d/Note %03d.md", i%3, i), "# x\n"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := vault.Mkdir(sp.Root, "", "Empty"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	sp.Expanded = []string{"F0", "F1", "F2", "Empty"}
	settle(tt)
	return a, tt
}

func rowRects(a *App) map[string]ui.Rect {
	out := map[string]ui.Rect{}
	for _, r := range a.rows {
		if r.rel != "" && r.r.W > 0 {
			out[r.rel] = r.r
		}
	}
	return out
}

// A long list builds only the rows near what it shows, and each of them
// stands exactly where building every row puts it, with the same scroll
// range.
func TestSidebarBuildsOnlyRowsNearView(t *testing.T) {
	a, tt := longSpaceApp(t)
	scroll := &a.session(a.state.ActiveSpace().ID).tree
	scroll.Y = 4000
	settle(tt)
	some, maxY := rowRects(a), scroll.MaxY
	if len(some) == 0 || len(some) > 80 {
		t.Fatalf("built %d of 300 rows", len(some))
	}
	if _, ok := some["F0/Note 000.md"]; ok {
		t.Fatal("built the first row while showing the middle of the list")
	}
	a.rowsAll = true
	settle(tt)
	all := rowRects(a)
	if len(all) < 300 {
		t.Fatalf("building every row gave %d", len(all))
	}
	if scroll.MaxY != maxY {
		t.Fatalf("the scroll range is %v with every row, %v without", scroll.MaxY, maxY)
	}
	for rel, r := range some {
		if all[rel] != r {
			t.Errorf("%s at %v, built with every row at %v", rel, r, all[rel])
		}
	}
}

// Scrolled at once to the end, as a drag of the scroll bar does, the list
// builds the rows there before it settles.
func TestSidebarJumpBuildsTheRowsShown(t *testing.T) {
	a, tt := longSpaceApp(t)
	scroll := &a.session(a.state.ActiveSpace().ID).tree
	scroll.Y = math.MaxFloat32
	tt.Frame()
	tt.Frame()
	if _, ok := rowRects(a)["F2/Note 299.md"]; !ok {
		t.Fatal("the last row was not built after scrolling to the end")
	}
	// "Empty" sorts first: its line is at the top, out of view.
	if _, ok := tt.Find("空文件夹"); ok {
		t.Fatal("the empty folder's line at the top was built at the end of the list")
	}
}

// The row being renamed stays built when the list scrolls away from it.
func TestSidebarKeepsTheRenamedRow(t *testing.T) {
	a, tt := longSpaceApp(t)
	a.startRename("F0/Note 000.md")
	settle(tt)
	scroll := &a.session(a.state.ActiveSpace().ID).tree
	scroll.Y = 6000
	settle(tt)
	rows := rowRects(a)
	if _, ok := rows["F0/Note 000.md"]; !ok {
		t.Fatal("the renamed row was left out")
	}
	if _, ok := rows["F0/Note 003.md"]; ok {
		t.Fatal("rows near the renamed one should still be left out")
	}
}

// Resting the pointer on a folder's row, on its edges and on the seams
// between rows, shows nothing new: there is no inline "新建文档" any more.
func TestSidebarHoverMakesNoNewDocumentRow(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil
	sp := a.state.ActiveSpace()
	if _, err := vault.Mkdir(sp.Root, "", "Empty"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	sp.Expanded = []string{"Empty"}
	settle(tt)
	names, texts := rootNames(a), tt.Texts()
	for _, r := range slices.Clone(a.rows) {
		for _, y := range []float32{r.r.Y - 1, r.r.Y, r.r.Y + 2, r.r.Y + r.r.H/2, r.r.Y + r.r.H, r.r.Y + r.r.H + 1} {
			tt.Move(r.r.X+40, y)
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			if got := tt.Texts(); slices.Contains(got, "新建文档") && !slices.Contains(texts, "新建文档") {
				t.Fatalf("hovering %s at %v showed 新建文档", r.rel, y)
			}
		}
	}
	tt.Move(10, 10)
	settle(tt)
	if got := rootNames(a); !slices.Equal(got, names) {
		t.Fatalf("hovering changed the list: %v, was %v", got, names)
	}
}
