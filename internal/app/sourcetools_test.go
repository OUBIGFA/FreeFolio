package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The Tools menu of a JSON file formats it as one step of the history,
// saves it in the file's own line ends, and reports where a syntax error
// is.
func TestSourceToolsFormatJSON(t *testing.T) {
	a, tt := newTestApp(t)
	full := filepath.Join(root(a), "data.json")
	if err := os.WriteFile(full, []byte("{\"b\":1,\"a\":[2]}\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.open("data.json")
	tt.Frame()
	d := a.doc
	if err := tt.Click("文本工具"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Contains(menu, "格式化 JSON") || !slices.Contains(menu, "删除重复行") {
		t.Fatalf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("格式化 JSON"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	want := "{\n  \"b\": 1,\n  \"a\": [\n    2\n  ]\n}\n"
	if got := sourceText(d.ed.Committed()); got != want {
		t.Fatalf("formatted %q", got)
	}
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(full); string(got) != strings.ReplaceAll(want, "\n", "\r\n") {
		t.Fatalf("saved %q", got)
	}
	d.ed.Engine.Undo()
	if got := sourceText(d.ed.Committed()); got != "{\"b\":1,\"a\":[2]}\n" {
		t.Fatalf("undo left %q", got)
	}

	d.ed.Engine.ReplaceDocument(sourceModel("data.json", "{\n  \"a\": x\n}\n"))
	a.notices = nil
	a.runSourceTool(d, formatTools("data.json")[2])
	if len(a.notices) != 1 || !strings.Contains(a.notices[0], "第 2 行第 8 列") {
		t.Fatalf("notices %q", a.notices)
	}
}

// Line tools serve any text file; a CSV file also checks its columns.
func TestSourceToolsLinesAndCSV(t *testing.T) {
	a, tt := newTestApp(t)
	if err := os.WriteFile(filepath.Join(root(a), "list.csv"), []byte("b,1\na,2\nb,1\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.open("list.csv")
	tt.Frame()
	d := a.doc
	a.notices = nil
	a.runSourceTool(d, formatTools("list.csv")[0])
	if len(a.notices) != 1 || !strings.Contains(a.notices[0], "第 4 行有 1 列") {
		t.Fatalf("notices %q", a.notices)
	}
	if err := tt.Click("文本工具"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除重复行"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("文本工具"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("按升序排列各行"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := sourceText(d.ed.Committed()); got != "a,2\nb,1\nc\n" {
		t.Fatalf("lines %q", got)
	}
}
