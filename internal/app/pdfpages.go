package app

// Editing a PDF's pages with pdfcpu (internal/pdfedit). Rotating,
// deleting, encrypting and decrypting change the file in place, after
// checking that it did not change elsewhere, and can be undone; extracting,
// splitting and merging write new files beside it and leave it as it is.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/pdfedit"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The operations of the window, in its order.
const (
	pdfOpRotate  = "rotate"
	pdfOpRemove  = "remove"
	pdfOpExtract = "extract"
	pdfOpSplit   = "split"
	pdfOpMerge   = "merge"
	pdfOpEncrypt = "encrypt"
	pdfOpDecrypt = "decrypt"
)

var pdfOps = []struct{ id, title string }{
	{pdfOpRotate, "旋转"},
	{pdfOpRemove, "删除页"},
	{pdfOpExtract, "提取页"},
	{pdfOpSplit, "拆分"},
	{pdfOpMerge, "合并"},
	{pdfOpEncrypt, "加密"},
	{pdfOpDecrypt, "解密"},
}

// pdfPagesState is the window editing the pages of a PDF.
// pdfPagesHost is what the PDF page tools need from the window.
type pdfPagesHost interface {
	isResidentDocument(d *document) bool
	notify(format string, args ...any)
	offerUndo(title, name string, undo func())
	openIn(sp *space.Space, rel string)
	openSystemFile(file string) bool
	refreshTree()
	replaceReferences(old, replacement *document)
	window() *mygo.Window
}

type pdfPagesState struct {
	host     pdfPagesHost
	open     bool
	doc      *document
	op       string
	pages    string
	degrees  int
	span     string
	password string
	merge    []string // the other PDFs to merge, absolute paths
	busy     bool
	err      string
}

// pdfPagesResult is what an operation did, off the main thread.
type pdfPagesResult struct {
	err error
	// before is the file's content an in-place change replaced, for undo;
	// after its new stamp.
	before []byte
	after  string
	perm   os.FileMode
	// created are the new files, absolute paths.
	created []string
}

// openPDFPages opens the window for d, a PDF.
func (pp *pdfPagesState) openPDFPages(d *document) {
	if d == nil || !d.isPDF() || d.loadErr != nil {
		return
	}
	*pp = pdfPagesState{host: pp.host, open: true, doc: d, op: pdfOpRotate, degrees: 90, span: "1"}
}

// pdfStampOf is the stamp readFile gives the PDF at full.
func pdfStampOf(full string) (string, os.FileMode, error) {
	info, err := os.Stat(full)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("pdf:%d:%d", info.ModTime().UnixNano(), info.Size()), info.Mode().Perm(), nil
}

var errPDFChanged = errors.New("PDF 已在别处修改，请重新打开后再试")

// pdfEditError says what went wrong in the interface's language.
func pdfEditError(err error) string {
	switch {
	case errors.Is(err, pdfedit.ErrNoPages):
		return i18n.T("请填写要处理的页码")
	case errors.Is(err, pdfedit.ErrAllPages):
		return i18n.T("不能删除全部页面")
	case errors.Is(err, pdfedit.ErrTooLarge):
		return i18n.T("PDF 太大，无法编辑")
	case errors.Is(err, errPDFChanged):
		return i18n.T(errPDFChanged.Error())
	}
	return fmt.Sprintf(i18n.T("处理 PDF 失败；%v"), err)
}

// inPlace reports whether op changes the PDF itself.
func pdfInPlace(op string) bool {
	return op == pdfOpRotate || op == pdfOpRemove || op == pdfOpEncrypt || op == pdfOpDecrypt
}

// runPDFPages runs the window's operation, off the main thread when there
// is a window.
func (pp *pdfPagesState) runPDFPages() {
	s := pp
	d := s.doc
	if d == nil || s.busy {
		return
	}
	full, err := resolveFile(d.space, d.rel)
	if err != nil {
		s.err = pdfEditError(err)
		return
	}
	if (s.op == pdfOpEncrypt || s.op == pdfOpDecrypt) && len(s.password) == 0 {
		s.err = i18n.T("请填写密码")
		return
	}
	if s.op == pdfOpMerge && len(s.merge) == 0 {
		s.err = i18n.T("请先选择要合并的 PDF")
		return
	}
	span := 1
	if s.op == pdfOpSplit {
		span, err = strconv.Atoi(strings.TrimSpace(s.span))
		if err != nil || span < 1 {
			s.err = i18n.T("每份页数须为正整数")
			return
		}
	}
	op, sel, pw, deg, merge, stamp := s.op, s.pages, s.password, s.degrees, append([]string(nil), s.merge...), d.diskText
	title := d.title()
	s.busy, s.err = true, ""
	work := func() pdfPagesResult {
		return pdfPagesWork(full, title, op, sel, pw, deg, span, merge, stamp)
	}
	if pp.host.window() == nil {
		pp.finishPDFPages(d, full, op, work())
		return
	}
	win := pp.host.window()
	go func() {
		res := work()
		win.Update(func() { pp.finishPDFPages(d, full, op, res) })
	}()
}

// pdfPagesWork does an operation on the PDF at full, whose stamp must
// still be stamp.
func pdfPagesWork(full, title, op, sel, pw string, deg, span int, merge []string, stamp string) (res pdfPagesResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now, perm, err := pdfStampOf(full)
	if err == nil && now != stamp {
		err = errPDFChanged
	}
	if err != nil {
		return pdfPagesResult{err: err}
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return pdfPagesResult{err: err}
	}
	var pages []int
	if op == pdfOpRotate || op == pdfOpRemove || op == pdfOpExtract {
		count, err := pdfedit.PageCount(ctx, data, pw)
		if err == nil {
			pages, err = pdfedit.ParsePages(sel, count)
		}
		if err != nil {
			return pdfPagesResult{err: err}
		}
	}
	dir := filepath.Dir(full)
	var out []byte
	switch op {
	case pdfOpRotate:
		out, err = pdfedit.Rotate(ctx, data, pages, deg, pw)
	case pdfOpRemove:
		out, err = pdfedit.Remove(ctx, data, pages, pw)
	case pdfOpEncrypt:
		out, err = pdfedit.Encrypt(ctx, data, pw)
	case pdfOpDecrypt:
		out, err = pdfedit.Decrypt(ctx, data, pw)
	case pdfOpExtract:
		out, err = pdfedit.Extract(ctx, data, pages, pw)
		if err == nil {
			name := fmt.Sprintf(i18n.T("%s 节选"), title)
			var p string
			if p, err = createUniqueFile(dir, name, ".pdf", out); err == nil {
				res.created = append(res.created, p)
			}
		}
		res.err = err
		return res
	case pdfOpSplit:
		parts, err := pdfedit.Split(ctx, data, span, pw)
		for _, part := range parts {
			if err != nil {
				break
			}
			name := fmt.Sprintf(i18n.T("%s 第 %d-%d 页"), title, part.From, part.Thru)
			if part.From == part.Thru {
				name = fmt.Sprintf(i18n.T("%s 第 %d 页"), title, part.From)
			}
			var p string
			if p, err = createUniqueFile(dir, name, ".pdf", part.Data); err == nil {
				res.created = append(res.created, p)
			}
		}
		res.err = err
		return res
	case pdfOpMerge:
		docs := [][]byte{data}
		for _, m := range merge {
			b, err := os.ReadFile(m)
			if err != nil {
				return pdfPagesResult{err: err}
			}
			docs = append(docs, b)
		}
		out, err = pdfedit.Merge(ctx, docs)
		if err == nil {
			var p string
			if p, err = createUniqueFile(dir, fmt.Sprintf(i18n.T("%s 合并"), title), ".pdf", out); err == nil {
				res.created = append(res.created, p)
			}
		}
		res.err = err
		return res
	}
	if err != nil {
		return pdfPagesResult{err: err}
	}
	// Check again right before writing: reading and pdfcpu took a while.
	if again, _, err := pdfStampOf(full); err != nil || again != stamp {
		return pdfPagesResult{err: errPDFChanged}
	}
	if err := fsutil.WriteFileAtomic(full, out, perm); err != nil {
		return pdfPagesResult{err: err}
	}
	after, _, _ := pdfStampOf(full)
	return pdfPagesResult{before: data, after: after, perm: perm}
}

// createUniqueFile writes data to a new file named base in dir, adding
// " 2", " 3"… when the name is taken; it never overwrites a file.
func createUniqueFile(dir, base, ext string, data []byte) (string, error) {
	base = strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(base)
	for i := 1; i < 1000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s %d", base, i)
		}
		p := filepath.Join(dir, name+ext)
		err := fsutil.CreateFileAtomic(p, data, 0o644)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", fs.ErrExist
}

// finishPDFPages shows what the operation did, on the main thread.
func (pp *pdfPagesState) finishPDFPages(d *document, full, op string, res pdfPagesResult) {
	s := pp
	if s.doc == d {
		s.busy = false
	}
	if res.err != nil {
		if s.doc == d {
			s.err = pdfEditError(res.err)
		}
		if len(res.created) == 0 {
			return
		}
	}
	if s.doc == d {
		s.open = false
	}
	if pdfInPlace(op) {
		cur := pp.reopenPDF(d)
		before, after, perm := res.before, res.after, res.perm
		pp.host.offerUndo(i18n.T("已修改 PDF"), d.rel, func() {
			if now, _, err := pdfStampOf(full); err != nil || now != after {
				pp.host.notify("PDF 在修改后又有变化，没有撤销")
				return
			}
			if err := fsutil.WriteFileAtomic(full, before, perm); err != nil {
				pp.host.notify("撤销失败；%v", err)
				return
			}
			pp.reopenPDF(cur)
		})
		return
	}
	pp.host.refreshTree()
	if len(res.created) == 1 && op != pdfOpSplit {
		if d.space.IsExternal() {
			pp.host.openSystemFile(res.created[0])
		} else {
			pp.host.openIn(d.space, path.Join(path.Dir(d.rel), filepath.Base(res.created[0])))
		}
	}
	pp.host.notify("已生成 %d 个 PDF 文件", len(res.created))
}

// reopenPDF reads d's file again into a new document in its place and
// returns it, or d when it is no longer open.
func (pp *pdfPagesState) reopenPDF(d *document) *document {
	if d == nil || !pp.host.isResidentDocument(d) {
		return d
	}
	text, err := readFile(d.space, d.rel)
	if err != nil {
		pp.host.notify("重新载入 %s 失败；%v", d.title(), err)
		return d
	}
	fresh := openPDFDocument(d.space, d.rel, text)
	pp.host.replaceReferences(d, fresh)
	return fresh
}

// chooseMergePDFs asks for the PDFs to merge after the open one.
func (pp *pdfPagesState) chooseMergePDFs() {
	if pp.host.window() == nil {
		return
	}
	win, d := pp.host.window(), pp.doc
	go func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: i18n.T("选择要合并的 PDF"), Multiple: true,
			Filters: []mygo.FileFilter{{Name: "PDF", Extensions: []string{"pdf"}}}})
		if err != nil || len(files) == 0 {
			return
		}
		win.Update(func() {
			if pp.doc == d {
				pp.merge = append(pp.merge, files...)
			}
		})
	}()
}

// pdfPagesView is the window.
func (pp *pdfPagesState) pdfPagesView(c *ui.Context) {
	s := pp
	if !s.open {
		return
	}
	t := c.Theme()
	ui.Modal(c.Key("pdf-pages"), &s.open, func() {
		ui.Column(c.Key("pdf-pages-body")).Width(420).Gap(12).Children(func() {
			ui.Text(c, i18n.T("编辑 PDF 页面")).FontSize(16).FontWeight(600)
			if s.doc != nil {
				ui.Text(c, s.doc.title()).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
			}
			for row, ops := range [][]int{{0, 1, 2, 3}, {4, 5, 6}} {
				ui.Row(c.Key(fmt.Sprintf("pdf-ops-%d", row))).Gap(6).Children(func() {
					for _, i := range ops {
						op := pdfOps[i]
						var b ui.Element
						if s.op == op.id {
							b = ui.PrimaryButton(c.Key("pdf-op-"+op.id), i18n.T(op.title))
						} else {
							b = ui.Button(c.Key("pdf-op-"+op.id), i18n.T(op.title))
						}
						if b.Clicked() && !s.busy {
							s.op, s.err = op.id, ""
						}
					}
				})
			}
			note := i18n.T("另存为新文件，原文件保持不变")
			if pdfInPlace(s.op) {
				note = i18n.T("直接修改原文件，完成后可撤销")
			}
			ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
			switch s.op {
			case pdfOpRotate, pdfOpRemove, pdfOpExtract:
				ui.TextInput(c.Key("pdf-pages-sel"), &s.pages).Placeholder(i18n.T("页码，如 1-3, 5；留空为全部")).Label(i18n.T("页码"))
			case pdfOpSplit:
				ui.TextInput(c.Key("pdf-pages-span"), &s.span).Placeholder(i18n.T("每份页数")).Label(i18n.T("每份页数"))
			case pdfOpMerge:
				label := i18n.T("选择要合并的 PDF")
				if n := len(s.merge); n > 0 {
					label = fmt.Sprintf(i18n.T("已选 %d 个 PDF，继续添加"), n)
				}
				if ui.Button(c.Key("pdf-merge-pick"), label).Clicked() {
					pp.chooseMergePDFs()
				}
				for i, m := range s.merge {
					ui.Text(c.Key(fmt.Sprintf("pdf-merge-%d", i)), filepath.Base(m)).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
				}
			}
			if s.op == pdfOpRotate {
				ui.Row(c.Key("pdf-degrees")).Gap(6).Children(func() {
					for _, deg := range []int{90, 180, 270} {
						label := fmt.Sprintf(i18n.T("顺时针 %d°"), deg)
						key := c.Key(fmt.Sprintf("pdf-deg-%d", deg))
						var b ui.Element
						if s.degrees == deg {
							b = ui.PrimaryButton(key, label)
						} else {
							b = ui.Button(key, label)
						}
						if b.Clicked() {
							s.degrees = deg
						}
					}
				})
			}
			if s.op != pdfOpMerge {
				label := i18n.T("密码（文件已加密时填写）")
				switch s.op {
				case pdfOpEncrypt:
					label = i18n.T("新密码")
				case pdfOpDecrypt:
					label = i18n.T("当前密码")
				}
				ui.TextInput(c.Key("pdf-pages-password"), &s.password).Password().Placeholder(label).Label(label)
			}
			if s.err != "" {
				ui.Text(c, s.err).FontSize(12).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("pdf-pages-cancel"), i18n.T("取消")).Clicked() {
					s.open = false
				}
				go_ := i18n.T("执行")
				if s.busy {
					go_ = i18n.T("正在处理")
				}
				if ui.PrimaryButton(c.Key("pdf-pages-go"), go_).Disabled(s.busy).Clicked() {
					pp.runPDFPages()
				}
			})
		})
	})
	if !s.open && !s.busy {
		s.doc = nil
	}
}
