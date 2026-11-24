package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/agent"
)

func TestFileModeOpensOneBotOnOpenFiles(t *testing.T) {
	fake := &scripted{}
	a, tt := qaApp(t, fake)
	if !a.openSystemFile(externalFile(t, "Loose.md", "# Loose\n\nfile-mode secret\n")) {
		t.Fatal("open")
	}
	tt.Frame()
	if err := tt.Click("OneBot"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()
	if !a.qa.open || a.qa.shown < 1 || !tt.HasText(a.files.locations.Name) {
		t.Fatalf("OneBot panel missing in file mode: open=%v shown=%v texts=%q", a.qa.open, a.qa.shown, tt.Texts())
	}
	args, _ := json.Marshal(map[string]any{"path": a.doc.rel, "offset": 1, "limit": 2000})
	fake.steps = []func(agent.Request) agent.Message{call("read", string(args)), reply("读到了。")}
	a.qa.input = "这个文件写了什么？"
	a.askQA()
	tt.Frame()
	s := a.qaSession(a.files.locations.ID)
	if len(s.turns) != 1 {
		t.Fatalf("turns %d", len(s.turns))
	}
	turn := s.turns[0]
	qaSettle(t, tt, turn)
	if turn.err != nil || len(turn.steps) != 1 || turn.steps[0].failed || !strings.Contains(turn.steps[0].result, "file-mode secret") {
		t.Fatalf("read in file mode: err=%v steps=%+v", turn.err, turn.steps)
	}
	if !tt.HasText("读到了。") {
		t.Fatalf("answer missing: %q", tt.Texts())
	}
	a.leaveFileMode()
	tt.Frame()
	if a.qaActiveSpace() != a.state.ActiveSpace() {
		t.Fatal("OneBot kept the file browser's space after leaving")
	}
}

func TestSnapshotFileModeOneBot(t *testing.T) {
	dir := snapshotDir(t, "set FREEMD_SNAPSHOT to render OneBot in file mode")
	a, tt := qaApp(t, &scripted{steps: []func(agent.Request) agent.Message{reply("这个文件讲的是留白。")}})
	tt.SetSize(1280, 820)
	if !a.openSystemFile(externalFile(t, "Loose.md", writingSample)) {
		t.Fatal("open")
	}
	tt.Frame()
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()
	a.qa.input = "这个文件写了什么？"
	a.askQA()
	tt.Frame()
	qaSettle(t, tt, a.qaSession(a.files.locations.ID).turns[0])
	savePNG(t, filepath.Join(dir, "file-mode-onebot.png"), tt)
}
