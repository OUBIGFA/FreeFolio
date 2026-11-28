package app

// The PDF reader of spaces and the file browser, ported from ArcMD fcb7cd3
// and 14e92c1. A PDF opens read-only: its document carries a PDFViewer and
// an empty editor, and is never saved.

import (
	"fmt"
	"os"
	"slices"
	"strings"

	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/pdfdoc"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/spacefs"
	"github.com/egoist/mygo/ui"
)

func (d *document) isPDF() bool {
	return d != nil && d.pdf != nil
}

func openPDFDocument(sp *space.Space, rel, diskStamp string) *document {
	d := &document{
		space:      sp,
		rel:        rel,
		diskText:   diskStamp,
		ed:         editor.FromOwnedDocument(model.New()),
		historyOff: true,
	}
	d.initLazyBaseline()
	full, err := resolveFile(sp, rel)
	if err != nil {
		d.loadErr = err
		d.pdf = editor.NewPDFViewer(nil, err)
		return d
	}
	if info, stErr := os.Stat(full); stErr == nil {
		d.pdfModNano = info.ModTime().UnixNano()
		d.pdfSize = info.Size()
		if d.diskText == "" {
			d.diskText = fmt.Sprintf("pdf:%d:%d", d.pdfModNano, d.pdfSize)
		}
	}
	pdfDoc, openErr := pdfdoc.Open(full)
	if openErr != nil {
		// Encrypted or malformed PDFs render a friendly, actionable card inside PDFView
		// rather than a raw red text error.
		d.pdf = editor.NewPDFViewer(nil, openErr)
		return d
	}
	d.pdf = editor.NewPDFViewer(pdfDoc, nil)
	return d
}

func (d *document) reopenPDF() {
	if !d.isPDF() || d.space == nil || d.rel == "" {
		return
	}
	oldPage := d.pdf.CurrentPage
	oldSidebar := d.pdf.SidebarOpen
	d.pdf.Close()
	full, err := resolveFile(d.space, d.rel)
	if err != nil {
		d.loadErr = err
		d.pdf = editor.NewPDFViewer(nil, err)
		d.pdf.SidebarOpen = oldSidebar
		return
	}
	if info, stErr := os.Stat(full); stErr == nil {
		d.pdfModNano = info.ModTime().UnixNano()
		d.pdfSize = info.Size()
		d.diskText = fmt.Sprintf("pdf:%d:%d", d.pdfModNano, d.pdfSize)
	}
	pdfDoc, openErr := pdfdoc.Open(full)
	d.pdf = editor.NewPDFViewer(pdfDoc, openErr)
	d.pdf.SidebarOpen = oldSidebar
	if openErr == nil && oldPage > 0 {
		d.pdf.GoToPage(oldPage, true)
	}
}

// releasePDFHandles closes cached, previewed, and resident PDF handles matching
// rel (or inside folder rel; "" matches the whole Space) before rename, move,
// or trash so Windows never fails with ERROR_SHARING_VIOLATION.
func (a *App) releasePDFHandles(sp *space.Space, rel string) {
	if sp == nil {
		return
	}
	if p := &a.preview; len(p.thumbs) > 0 {
		for k, th := range p.thumbs {
			trimmed := strings.TrimPrefix(k, "open/")
			entrySpaceID, entryRel, ok := strings.Cut(trimmed, "/")
			if !ok || entrySpaceID != sp.ID {
				continue
			}
			if rel == "" || within(entryRel, rel) {
				if th.doc != nil {
					th.doc.close()
				}
				delete(p.thumbs, k)
				if idx := slices.Index(p.thumbOrder, k); idx >= 0 {
					p.thumbOrder = slices.Delete(p.thumbOrder, idx, idx+1)
				}
			}
		}
		if p.shown != nil && p.shown.space == sp && (rel == "" || within(p.shown.rel, rel)) {
			p.shown, p.closing = nil, false
		}
	}
	for _, d := range a.residentDocuments() {
		if d != nil && d.space != nil && d.space.ID == sp.ID && (rel == "" || within(d.rel, rel)) && d.isPDF() {
			d.pdf.Close()
		}
	}
}

func (a *App) reopenResidentPDFs(sp *space.Space, rel string) {
	if sp == nil {
		return
	}
	for _, d := range a.residentDocuments() {
		if d != nil && d.space != nil && d.space.ID == sp.ID && (rel == "" || within(d.rel, rel)) && d.isPDF() {
			d.reopenPDF()
		}
	}
}

func (a *App) pdfOptionsFor(c *ui.Context, d *document) editor.PDFOptions {
	var revealFn func()
	if d != nil && d.space != nil {
		sp, rel := d.space, d.rel
		revealFn = func() { a.revealIn(sp, rel) }
	}
	return editor.PDFOptions{
		Theme:      c.Theme(),
		Page:       pageStyle(a.paperDark),
		Title:      d.title(),
		Async:      a.win != nil,
		RevealFile: revealFn,
	}
}

func (a *App) pdfCorner(c *ui.Context, d *document) {
	if d == nil || !d.isPDF() || d.pdf == nil {
		return
	}
	t := c.Theme()
	ui.Row(c.Key("pdf-corner")).Gap(2).AlignItems(ui.Center).Children(func() {
		if cnt := d.pdf.PageCount(); cnt > 0 {
			ui.Row(c.Key("pdf-pages")).Height(28).PaddingX(8).Radius(7).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("共 %d 页"), cnt)).FontSize(12.5).TextColor(t.TextMuted).NoWrap()
			})
		}
		side := iconButton(c, "pdf-outline", iconSidebar, i18n.T("缩略图与目录"), a.withKey(i18n.T("缩略图与目录"), "outline"), d.pdf.PageCount() == 0)
		if d.pdf.SidebarOpen {
			side.Background(glass(t))
		}
		if side.Clicked() {
			d.pdf.ToggleOutline()
		}
		if iconButton(c, "pdf-pages-edit", iconPencil, i18n.T("编辑 PDF 页面"), i18n.T("旋转、删除、提取、拆分、合并或加密页面"), d.pdf.PageCount() == 0).Clicked() {
			a.pdfPages.openPDFPages(d)
		}
		find := iconButton(c, "pdf-find", iconSearch, i18n.T("在 PDF 中查找"), i18n.T("在 PDF 中查找")+"  ⌘F", d.pdf.PageCount() == 0)
		if d.pdf.FindOpen {
			find.Background(glass(t))
		}
		if find.Clicked() {
			if d.pdf.FindOpen {
				d.pdf.FindOpen = false
			} else {
				d.pdf.OpenFind()
			}
		}
	})
}

// pdfActions are the command palette's commands for the PDF in front
// (ArcMD 14e92c1).
func (a *App) pdfActions(cmd func(string, string, func())) {
	v := a.doc.pdf
	cmd(i18n.T("在 PDF 中查找"), a.keyLabel("find"), func() { v.OpenFind() })
	cmd(i18n.T("PDF：适应宽度"), "⌘0", func() {
		v.FitMode = editor.PDFFitWidth
		v.Zoom = 1.0
	})
	cmd(i18n.T("PDF：适应整页"), "", func() { v.FitMode = editor.PDFFitPage })
	spread := i18n.T("PDF：双页并排视图")
	if v.TwoPage {
		spread = i18n.T("PDF：单页连续视图")
	}
	cmd(spread, "", func() {
		v.TwoPage = !v.TwoPage
		v.GoToPage(v.CurrentPage, true)
	})
	cmd(i18n.T("PDF：顺时针旋转 90°"), "", func() { v.RotateCW() })
	if d := a.doc; d.loadErr == nil && v.PageCount() > 0 {
		cmd(i18n.T("编辑 PDF 页面"), "", func() { a.pdfPages.openPDFPages(d) })
	}
}

// pdfTextPages bounds the pages of a PDF that search and Q&A read.
const pdfTextPages = 200

// pdfQAPage is a PDF as the agent reads it: a section per page, each its
// own part, so a citation names the page (ArcMD 14e92c1).
func pdfQAPage(name, content string) (*spacefs.Page, error) {
	doc, err := pdfdoc.OpenBytes([]byte(content))
	if err != nil {
		return nil, err
	}
	defer doc.Close()
	var parts []spacefs.Part
	for i := range min(doc.PageCount(), pdfTextPages) {
		head := fmt.Sprintf("## Page %d", i+1)
		if txt := strings.TrimSpace(doc.PageText(i)); txt != "" {
			head += "\n" + txt
		}
		parts = append(parts, spacefs.Part{Block: fmt.Sprintf("page:%d", i+1), Text: head})
	}
	return spacefs.PageOf(name, parts), nil
}
