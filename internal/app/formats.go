package app

// Documents of the formats package: source files (code, configuration,
// data), edited as the text they are and written back as typed, and
// converted files (Office, OFD, e-books, mail…), read through a converter
// and shown locked. FreeFolio never writes a converted file: it can save
// its Markdown as a new, editable copy.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/formats"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/plugin"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// errFormatReadOnly guards converted files, which FreeFolio only reads.
// Its text is translated where errors are shown (errText).
var errFormatReadOnly = errors.New("此格式只读，FreeFolio 不会修改原文件。")

// sourceFile remembers how a source file stores its text, so that saving
// writes it back the same way.
type sourceFile struct {
	bom  bool // starts with a UTF-8 byte order mark
	crlf bool // every line ends with CR LF
	// foreign is set when the file is not UTF-8: it is shown decoded and
	// locked, since writing it back would change its encoding.
	foreign bool
}

// encode turns the edited text back into the file's bytes.
func (s *sourceFile) encode(text string) string {
	if s.crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if s.bom {
		text = "\uFEFF" + text
	}
	return text
}

// splitSource takes a source file's text apart into what the editor shows
// and how the file stores it.
func splitSource(text string) (string, *sourceFile) {
	sf := &sourceFile{}
	if strings.HasPrefix(text, "\uFEFF") {
		sf.bom, text = true, text[len("\uFEFF"):]
	}
	if !utf8.ValidString(text) {
		sf.foreign = true
		text = formats.DecodeText([]byte(text))
		return strings.ReplaceAll(text, "\r\n", "\n"), sf
	}
	if n := strings.Count(text, "\r\n"); n > 0 && n == strings.Count(text, "\n") {
		sf.crlf, text = true, strings.ReplaceAll(text, "\r\n", "\n")
	}
	return text, sf
}

// sourceHead is the start of text, at most n bytes, ending at a line
// break where there is one so that no character is cut.
func sourceHead(text string, n int) string {
	if len(text) <= n {
		return text
	}
	head := text[:n]
	if i := strings.LastIndexByte(head, '\n'); i > 0 {
		return head[:i]
	}
	for len(head) > 0 && !utf8.ValidString(head) {
		head = head[:len(head)-1]
	}
	return head
}

// sourceModel is a source file as a document: one code block holding it
// all, highlighted as its format's language.
func sourceModel(rel, body string) *model.Document {
	doc := model.New()
	b := model.NewBlock("code", body)
	if f, ok := formats.Lookup(rel); ok {
		b.Language = f.Language
	}
	doc.Blocks = []*model.Block{b}
	return doc
}

// sourceText is the text a source document saves: its blocks' text, a
// line between two, should the writer have split the code block.
func sourceText(doc *model.Document) string {
	if len(doc.Blocks) == 1 {
		return doc.Blocks[0].Text()
	}
	parts := make([]string, 0, len(doc.Blocks))
	for _, b := range doc.Blocks {
		parts = append(parts, b.Text())
	}
	return strings.Join(parts, "\n")
}

// errSourceTooLarge refuses source files the editor would crawl through.
var errSourceTooLarge = errors.New("文件太大，FreeFolio 最多编辑 4 MB 的文本文件。")

func openSourceDocument(sp *space.Space, rel, text string) *document {
	d := &document{space: sp, rel: rel, historyOff: sp.IsExternal() && sp.Root == "", diskText: text}
	if len(text) > formats.MaxSourceSize {
		d.loadErr = errSourceTooLarge
		d.ed = editor.FromOwnedDocument(model.New())
		d.initLazyBaseline()
		return d
	}
	body, sf := splitSource(text)
	d.srcFile = sf
	d.ed = editor.FromOwnedDocument(sourceModel(rel, body))
	if sf.foreign {
		d.ed.Locked = true
		d.ed.SetReadOnly(true)
	}
	d.initLazyBaseline()
	return d
}

// openConvertedDocument reads a converted file into a locked document;
// stamp stands for the file's content, as a PDF's does.
func openConvertedDocument(sp *space.Space, rel, stamp string) *document {
	d := newConvertedDocument(sp, rel, stamp)
	md, err := convertFile(sp, rel)
	if err != nil {
		d.loadErr = err
		return d
	}
	d.ed = editor.FromOwnedDocument(editor.ImportMarkdown(md))
	d.ed.Locked = true
	d.ed.SetReadOnly(true)
	d.initLazyBaseline()
	return d
}

func newConvertedDocument(sp *space.Space, rel, stamp string) *document {
	d := &document{space: sp, rel: rel, diskText: stamp, historyOff: true, converted: true, ed: editor.FromOwnedDocument(model.New())}
	d.ed.Locked = true
	d.ed.SetReadOnly(true)
	d.initLazyBaseline()
	return d
}

// openConvertedAsync shows a placeholder while the file converts off the
// main thread, then puts the converted document in its place.
func (a *App) openConvertedAsync(sp *space.Space, rel, stamp string) *document {
	d := newConvertedDocument(sp, rel, stamp)
	d.boardLoading = true
	win := a.win
	go func() {
		loaded := openConvertedDocument(sp, rel, stamp)
		win.Update(func() {
			if !d.closed.Load() && a.isResidentDocument(d) {
				a.replaceReferences(d, loaded)
			}
		})
	}()
	return d
}

// convertFile reads the file at rel of sp into Markdown.
func convertFile(sp *space.Space, rel string) (string, error) {
	full, err := resolveFile(sp, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if info.Size() > formats.MaxConvertSize {
		return "", convertError(formats.ErrTooLarge)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	md, err := formats.Convert(full, data)
	if err != nil {
		return "", convertError(err)
	}
	return md, nil
}

// convertError says why a file could not be read, in the interface's
// language.
func convertError(err error) error {
	switch {
	case errors.Is(err, formats.ErrEncrypted):
		return errors.New(i18n.T("文件已加密。请先在创建它的程序中去掉密码。"))
	case errors.Is(err, formats.ErrTooLarge):
		return errors.New(i18n.T("文件太大，无法打开。"))
	case errors.Is(err, formats.ErrUnsupported):
		return errors.New(i18n.T("暂不支持此文件的格式版本。"))
	}
	return fmt.Errorf(i18n.T("无法读取此文件。%v"), err)
}

// saveEditableCopy saves a converted document's Markdown beside its file
// and opens the copy.
func (a *App) saveEditableCopy(d *document) {
	if d == nil || !d.converted || d.loadErr != nil || d.boardLoading {
		return
	}
	root, name, err := fileLocation(d.space, d.rel)
	if err != nil {
		a.notify("存为 Markdown 副本失败。%v", err)
		return
	}
	rel, err := vault.CreateFile(root, path.Dir(name), d.title(), ".md", editor.ExportMarkdown(d.ed.Committed()))
	if err != nil {
		a.notify("存为 Markdown 副本失败。%v", err)
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

// formatCorner is the top-right corner of a source or converted document:
// its format and, for a converted one, the way to an editable copy.
func (a *App) formatCorner(c *ui.Context, d *document) {
	f, _ := formats.Lookup(d.rel)
	t := c.Theme()
	ui.Row(c.Key("format-corner")).Gap(2).AlignItems(ui.Center).Children(func() {
		if f.Name != "" {
			ui.Row(c.Key("format-name")).Height(28).PaddingX(8).Radius(7).AlignItems(ui.Center).Children(func() {
				label := f.Name
				if d.ed.Locked {
					label += " · " + i18n.T("只读")
				}
				ui.Text(c, label).FontSize(12.5).TextColor(t.TextMuted).NoWrap()
			})
		}
		a.sourceToolsButton(c, d)
		if d.converted && iconButton(c, "format-copy", iconFileImport, i18n.T("存为 Markdown 副本"), i18n.T("存为可编辑的 Markdown 副本"), d.boardLoading).Clicked() {
			a.saveEditableCopy(d)
		}
	})
}

// formatActions are the command palette's commands for a source or
// converted document in front.
func (a *App) formatActions(cmd func(string, string, func())) {
	d := a.doc
	if d.converted {
		cmd(i18n.T("存为可编辑的 Markdown 副本"), "", func() { a.saveEditableCopy(d) })
		return
	}
	cmd(i18n.T("版本历史…"), a.keyLabel("history"), a.openVersions)
}

// syncFormatPlugins hands the formats of the enabled wasm plugins to the
// formats package, and refreshes the spaces' lists when they change.
func (a *App) syncFormatPlugins() {
	var list []formats.Format
	var exts []string
	for _, p := range a.installed {
		if !p.Enabled || p.Manifest.Runtime != plugin.RuntimeWASM {
			continue
		}
		for _, f := range p.Manifest.Contributes.Formats {
			for _, e := range f.Extensions {
				list = append(list, formats.Format{Ext: e, Name: f.Name, Mode: formats.Converted, Plugin: p.Manifest.ID})
				exts = append(exts, p.Manifest.ID+e)
			}
		}
	}
	if a.converters != nil {
		a.converters.Set(a.installed)
	}
	formats.SetPluginFormats(list)
	key := strings.Join(exts, "|")
	if key != a.pluginFormats {
		a.pluginFormats = key
		if a.state != nil {
			a.refreshTree()
		}
	}
}

// openFilters are the open dialog's filters: every document FreeFolio
// opens, then Markdown and PDF on their own.
func openFilters() []mygo.FileFilter {
	all := append([]string{"md", "markdown", "mdx", "arcmd", "excalidraw", "pdf"}, formats.Extensions()...)
	return []mygo.FileFilter{
		{Name: i18n.T("所有支持的文件"), Extensions: all},
		{Name: "Markdown", Extensions: []string{"md", "markdown", "mdx"}},
		{Name: "PDF", Extensions: []string{"pdf"}},
	}
}

// convertWithPlugin is the formats package's converter for plugin formats.
func (a *App) convertWithPlugin(_ formats.Format, name string, data []byte) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return a.converters.Convert(ctx, name, data)
}
