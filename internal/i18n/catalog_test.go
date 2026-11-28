package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// sourceStrings parses the non-test Go sources outside this package and
// returns the literal messages passed to i18n.T, directly or through a helper
// that hands one of its parameters to i18n.T (notify, labels), plus every
// string literal. The translation table is the contract here, so reading
// sources is the behavior under test.
func sourceStrings(t *testing.T) (translated map[string]string, literals map[string]bool) {
	t.Helper()
	translated, literals = map[string]string{}, map[string]bool{}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, dir := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" || filepath.Base(path) == "i18n" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err == nil {
				files = append(files, f)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	isT := func(call *ast.CallExpr) bool {
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "T" || len(call.Args) != 1 {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Name == "i18n"
	}
	// translators maps a function name to the index of the parameter it
	// translates; i18n.T itself is index 0.
	translators := map[string]int{}
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			params := map[string]int{}
			i := 0
			for _, field := range fn.Type.Params.List {
				for _, name := range field.Names {
					params[name.Name] = i
					i++
				}
				if len(field.Names) == 0 {
					i++
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && isT(call) {
					if id, ok := call.Args[0].(*ast.Ident); ok {
						if index, ok := params[id.Name]; ok {
							translators[fn.Name.Name] = index
						}
					}
				}
				return true
			})
		}
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if s, err := strconv.Unquote(n.Value); err == nil {
						literals[s] = true
					}
				}
			case *ast.CallExpr:
				index, name := -1, ""
				switch fun := n.Fun.(type) {
				case *ast.Ident:
					name = fun.Name
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				}
				if isT(n) {
					index = 0
				} else if i, ok := translators[name]; ok {
					index = i
				}
				if index < 0 || index >= len(n.Args) {
					return true
				}
				if lit, ok := n.Args[index].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						translated[s] = fset.Position(lit.Pos()).String()
					}
				}
			}
			return true
		})
	}
	return translated, literals
}

func hasHan(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0
}

var formatVerb = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?([a-zA-Z%])`)

func verbs(s string) []string {
	var out []string
	for _, m := range formatVerb.FindAllStringSubmatch(s, -1) {
		if m[1] != "%" {
			out = append(out, m[1])
		}
	}
	slices.Sort(out)
	return out
}

func TestCatalogsHaveNoDuplicates(t *testing.T) {
	seen := map[string]int{}
	for i, c := range enCatalogs {
		for zh := range c {
			if j, ok := seen[zh]; ok {
				t.Errorf("%q is in catalogs %d and %d; keep one copy, in enCommon when shared", zh, j, i)
			}
			seen[zh] = i
		}
	}
}

func TestCatalogsMatchSources(t *testing.T) {
	translated, literals := sourceStrings(t)
	for zh, at := range translated {
		if _, ok := en[zh]; !ok && hasHan(zh) {
			t.Errorf("%s: no English for %q; add it to the feature's en_*.go", at, zh)
		}
	}
	for zh := range en {
		if !literals[zh] {
			t.Errorf("no code shows %q any more; remove it from its en_*.go", zh)
		}
	}
}

// UI messages have no sentence-final period in either language, and the
// translation keeps the same format verbs.
func TestCatalogEntriesKeepUIConventions(t *testing.T) {
	for zh, s := range en {
		if strings.HasSuffix(zh, "。") || strings.HasSuffix(s, ".") {
			t.Errorf("drop the final period: %q -> %q", zh, s)
		}
		if !slices.Equal(verbs(zh), verbs(s)) {
			t.Errorf("format verbs differ: %q -> %q", zh, s)
		}
	}
}
