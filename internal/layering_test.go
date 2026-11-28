package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/OUBIGFA/FreeFolio/"

// uiTier may import MyGo: the window, the editors and the widget and icon
// packages they draw with. Every other package stays testable without a
// window and usable from the CLI and MCP server.
var uiTier = []string{"cmd/freefolio", "internal/app", "internal/editor", "internal/mindmap", "internal/solar", "resources"}

func within(pkg string, roots ...string) bool {
	for _, r := range roots {
		if pkg == r || strings.HasPrefix(pkg, r+"/") {
			return true
		}
	}
	return false
}

// TestPackageLayering keeps the dependency direction documented in
// docs/architecture.md: cmd → app → editor → domain packages, MyGo only in
// the UI tier, no cgo, and none of MyGo's rich text editors.
func TestPackageLayering(t *testing.T) {
	fset := token.NewFileSet()
	checked := 0
	for _, dir := range []string{"../internal", "../cmd", "../resources"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, err := filepath.Rel("..", filepath.Dir(path))
			if err != nil {
				return err
			}
			pkg := filepath.ToSlash(rel)
			checked++
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range f.Imports {
				imp, _ := strconv.Unquote(spec.Path.Value)
				target := strings.TrimPrefix(imp, module)
				switch {
				case imp == "C":
					t.Errorf("%s: cgo is not allowed", path)
				case strings.HasPrefix(imp, "github.com/egoist/mygo/ui/richtext"), strings.HasPrefix(imp, "github.com/egoist/mygo/ui/editor"):
					t.Errorf("%s: imports %s; FreeFolio keeps its own editor", path, imp)
				case (imp == "github.com/egoist/mygo" || strings.HasPrefix(imp, "github.com/egoist/mygo/")) && !within(pkg, uiTier...):
					t.Errorf("%s: imports %s outside the UI tier %v", path, imp, uiTier)
				case target == "internal/app" && pkg != "cmd/freefolio":
					t.Errorf("%s: only cmd/freefolio may import internal/app", path)
				case within(target, "internal/editor") && !within(pkg, "cmd/freefolio", "internal/app", "internal/editor"):
					t.Errorf("%s: imports %s; domain packages must not depend on the editor", path, imp)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("no sources found; run from the internal directory")
	}
}
