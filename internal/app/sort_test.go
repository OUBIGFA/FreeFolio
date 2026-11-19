package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestFolderSortByNameAndTime(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	dir := filepath.Join(root(a), "Journal")
	base := time.Now().Add(-time.Hour)
	for i, name := range []string{"b.md", "a.md", "c.md"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("# x\n"), 0o644)
		at := base.Add(time.Duration(i) * time.Minute)
		os.Chtimes(p, at, at)
	}
	idea := filepath.Join(dir, "Ideas.md")
	os.Chtimes(idea, base.Add(-time.Hour), base.Add(-time.Hour))
	a.refreshTree()
	names := func() string {
		out := ""
		for _, n := range a.childrenOf("Journal") {
			out += n.Title() + ","
		}
		return out
	}
	sp := a.state.ActiveSpace()
	if err := tt.RightClick("Journal"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("排序方式", "按修改时间（新→旧）"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	tt.Frame()
	if names() != "c,a,b,Ideas," {
		t.Fatal("newest first", names())
	}
	a.setSort(sp, "Journal", "name")
	if names() != "a,b,c,Ideas," {
		t.Fatal("by name", names())
	}
	if sp.SortOf("Journal") != "name" {
		t.Fatal("kept")
	}
}

func TestPaletteRecentCommandsAndConfirm(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.openPalette()
	a.palette.query = "开启打字机滚动"
	tt.Frame()
	a.runPaletteAction(a.results()[0])
	if !a.prefs.Typewriter || len(a.prefs.RecentCommands) != 1 || a.prefs.RecentCommands[0] != "开启打字机滚动" {
		t.Fatal("remembered", a.prefs.RecentCommands)
	}
	a.prefs.Typewriter = false
	a.openPalette()
	tt.Frame()
	idle := a.idleResults()
	first := -1
	for i, act := range idle {
		if act.kind == "命令" {
			first = i
			break
		}
	}
	if first < 0 || idle[first].title != "开启打字机滚动" {
		t.Fatal("the command used last comes first", idle)
	}
	// A dangerous command asks twice.
	ran := 0
	act := action{kind: "命令", title: "放弃修改并关闭", danger: true, run: func() { ran++ }}
	a.runPaletteAction(act)
	if ran != 0 || a.palette.confirm != act.title || !a.palette.open {
		t.Fatal("the first Enter asks")
	}
	a.runPaletteAction(act)
	if ran != 1 {
		t.Fatal("the second runs it")
	}
}

// Sorting the root newest first sorts the folders it holds, keeps folders
// first, dates a folder by what it holds, and follows a write.
func TestNewestFirstReachesSubfoldersAndFollowsWrites(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	base := time.Now().Add(-time.Hour)
	at := func(rel string, d time.Duration) {
		p := filepath.Join(root(a), filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if _, err := os.Stat(p); err != nil {
			os.WriteFile(p, []byte("# x\n"), 0o644)
		}
		os.Chtimes(p, base.Add(d), base.Add(d))
	}
	at("Welcome.md", 30*time.Minute)
	at("Journal/Ideas.md", time.Minute)
	at("Journal/new.md", 2*time.Minute)
	at("Old/a.md", -time.Hour)
	// A folder's own time moves with every save inside it; it must not
	// date the folder.
	os.Chtimes(filepath.Join(root(a), "Old"), base.Add(time.Hour), base.Add(time.Hour))
	sp := a.state.ActiveSpace()
	a.setSort(sp, "", "modified")
	names := func(dir string) string {
		out := ""
		for _, n := range a.childrenOf(dir) {
			out += n.Title() + ","
		}
		return out
	}
	if got := names(""); got != "Journal,Old,Welcome," {
		t.Fatal("folders first, newest first", got)
	}
	if got := names("Journal"); got != "new,Ideas," {
		t.Fatal("the subfolder follows the root", got)
	}
	a.setSort(sp, "Journal", "")
	if got := names("Journal"); got != "Ideas,new," || sp.SortOf("Journal") != "" {
		t.Fatal("a subfolder may keep its own order", got, sp.Sort)
	}
	a.setSort(sp, "Journal", "modified")
	at("Journal/Ideas.md", 3*time.Minute)
	a.filesChanged(sp.ID, []mygo.FileEvent{{Op: mygo.FileWrite, Path: "Journal/Ideas.md"}})
	if got := names("Journal"); got != "Ideas,new," {
		t.Fatal("a write moves the file up", got)
	}
}
