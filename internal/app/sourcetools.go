package app

// The Tools menu of a source file: line operations for any text, and
// formatting and syntax checks for the formats FreeFolio can parse.

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/formats"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/solar"
	"github.com/OUBIGFA/FreeFolio/internal/texttools"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

var iconTools = solar.Must("wrench")

// sourceTool is one entry of the Tools menu: a change of the text, or a
// check that only reports.
type sourceTool struct {
	label  string
	change func(string) (string, error)
	check  func(string) (string, error)
}

// formatTools are the tools of the file rel's own format, those first in
// the menu.
func formatTools(rel string) []sourceTool {
	f, _ := formats.Lookup(rel)
	ext := strings.ToLower(path.Ext(rel))
	syntaxOK := func(check func(string) error) func(string) (string, error) {
		return func(text string) (string, error) {
			if err := check(text); err != nil {
				return "", err
			}
			return fmt.Sprintf(i18n.T("%s 语法正确。"), f.Name), nil
		}
	}
	switch f.Language {
	case "json":
		if ext == ".jsonl" || ext == ".ndjson" {
			return []sourceTool{{label: "检查 JSON Lines 语法", check: func(text string) (string, error) {
				n, err := texttools.CheckJSONLines(text)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf(i18n.T("JSON Lines 语法正确，共 %d 条记录。"), n), nil
			}}}
		}
		return []sourceTool{
			{label: "格式化 JSON", change: texttools.FormatJSON},
			{label: "压缩 JSON", change: texttools.CompactJSON},
			{label: "检查 JSON 语法", check: syntaxOK(texttools.CheckJSON)},
		}
	case "yaml":
		return []sourceTool{{label: "检查 YAML 语法", check: syntaxOK(texttools.CheckYAML)}}
	case "xml":
		return []sourceTool{{label: "检查 XML 语法", check: syntaxOK(texttools.CheckXML)}}
	case "go":
		return []sourceTool{{label: "用 gofmt 格式化", change: texttools.FormatGo}}
	case "csv":
		return []sourceTool{{label: "检查各行列数", check: func(text string) (string, error) {
			rows, err := texttools.ParseDelimited(text, texttools.Separator(rel, text))
			if err != nil {
				return "", err
			}
			if len(rows) == 0 {
				return i18n.T("文件中没有数据行。"), nil
			}
			if r := texttools.RaggedRecord(rows); r != nil {
				return "", fmt.Errorf(i18n.T("第 %d 行有 %d 列，第一行有 %d 列。"), r.Line, len(r.Fields), len(rows[0].Fields))
			}
			return fmt.Sprintf(i18n.T("共 %d 行，每行 %d 列。"), len(rows), len(rows[0].Fields)), nil
		}}}
	}
	return nil
}

// lineTools serve any text.
var lineTools = []sourceTool{
	{label: "按升序排列各行", change: func(s string) (string, error) { return texttools.SortLines(s, false), nil }},
	{label: "按降序排列各行", change: func(s string) (string, error) { return texttools.SortLines(s, true), nil }},
	{label: "删除重复行", change: func(s string) (string, error) { return texttools.UniqueLines(s), nil }},
	{label: "删除空行", change: func(s string) (string, error) { return texttools.RemoveBlankLines(s), nil }},
	{label: "删除行尾空格", change: func(s string) (string, error) { return texttools.TrimTrailingSpace(s), nil }},
	{label: "统计行数和字符数", check: func(s string) (string, error) {
		lines := strings.Count(s, "\n")
		if s != "" && !strings.HasSuffix(s, "\n") {
			lines++
		}
		return fmt.Sprintf(i18n.T("共 %d 行，%d 个字符。"), lines, utf8.RuneCountInString(s)), nil
	}},
}

// toolError says what went wrong, with the place of a syntax error.
func toolError(err error) string {
	var se *texttools.SyntaxError
	if errors.As(err, &se) {
		switch {
		case se.Column > 0:
			return fmt.Sprintf(i18n.T("第 %d 行第 %d 列：%v"), se.Line, se.Column, se.Err)
		case se.Line > 0:
			return fmt.Sprintf(i18n.T("第 %d 行：%v"), se.Line, se.Err)
		}
		return se.Err.Error()
	}
	return err.Error()
}

// runSourceTool runs tool over the text of the source document d; a
// change replaces the text as one step of the history.
func (a *App) runSourceTool(d *document, tool sourceTool) {
	if d == nil || d.srcFile == nil || d.ed.Locked || d.ed.ReadOnly {
		return
	}
	text := sourceText(d.ed.Committed())
	if tool.check != nil {
		msg, err := tool.check(text)
		if err != nil {
			a.notify("发现问题。%s", toolError(err))
			return
		}
		a.notify("%s", msg)
		return
	}
	out, err := tool.change(text)
	if err != nil {
		a.notify("无法完成此操作。%s", toolError(err))
		return
	}
	if out == text {
		a.notify("没有需要修改的内容。")
		return
	}
	d.ed.Engine.ReplaceDocument(sourceModel(d.rel, out))
	d.ed.RequestFocus = true
}

// saveTableCopy saves a delimited file as a Markdown document holding it
// as a table, beside the file, and opens it.
func (a *App) saveTableCopy(d *document) {
	if d == nil || d.srcFile == nil {
		return
	}
	text := sourceText(d.ed.Committed())
	records, err := texttools.ParseDelimited(text, texttools.Separator(d.rel, text))
	if err == nil && len(records) == 0 {
		err = errors.New(i18n.T("文件中没有数据行。"))
	}
	if err != nil {
		a.notify("存为 Markdown 表格失败。%s", toolError(err))
		return
	}
	rows := make([][]string, len(records))
	for i, r := range records {
		rows[i] = r.Fields
	}
	doc := model.New()
	doc.Blocks = []*model.Block{model.DelimitedTable(rows)}
	root, name, err := fileLocation(d.space, d.rel)
	if err != nil {
		a.notify("存为 Markdown 表格失败。%s", toolError(err))
		return
	}
	rel, err := vault.CreateFile(root, path.Dir(name), d.title(), ".md", editor.ExportMarkdown(doc))
	if err != nil {
		a.notify("存为 Markdown 表格失败。%s", toolError(err))
		return
	}
	if d.space.IsExternal() {
		a.openSystemFile(filepath.Join(root, filepath.FromSlash(rel)))
		return
	}
	a.refreshTree()
	a.openIn(d.space, rel)
	a.notify("已存为 %s", rel)
}

// sourceToolsButton is the Tools menu of an editable source document.
func (a *App) sourceToolsButton(c *ui.Context, d *document) {
	if d.srcFile == nil || d.ed.Locked {
		return
	}
	iconButton(c, "format-tools", iconTools, i18n.T("文本工具"), i18n.T("文本工具"), false).Role(ui.RoleMenuButton).Menu(func(m *ui.Menu) {
		for _, tool := range formatTools(d.rel) {
			if m.Item(i18n.T(tool.label)).Chosen() {
				a.runSourceTool(d, tool)
			}
		}
		if f, _ := formats.Lookup(d.rel); f.Language == "csv" {
			if m.Item(i18n.T("存为 Markdown 表格副本")).Chosen() {
				a.saveTableCopy(d)
			}
		}
		if len(formatTools(d.rel)) > 0 {
			m.Separator()
		}
		for _, tool := range lineTools {
			if m.Item(i18n.T(tool.label)).Chosen() {
				a.runSourceTool(d, tool)
			}
		}
	})
}
