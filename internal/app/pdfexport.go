package app

import (
	"errors"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/database"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/editor/dbview"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/pdf"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// pdfExportHost is what PDF export and printing need from the window.
type pdfExportHost interface {
	activeDocument() *document
	cachedImageFor(d *document) func(string) editor.ImageResource
	clock() time.Time
	dbFeature() *databaseFeature
	notify(format string, args ...any)
	printHook() func(path string)
	refreshTree()
	remoteImage(url string) image.Image
	spaces() *space.State
	window() *mygo.Window
}

// pdfExportFeature exports and prints documents as PDF, one cancellable job at a time.
type pdfExportFeature struct {
	host pdfExportHost
	job  *pdfExport
}

type pdfExport struct {
	buildErr                        error
	board                           *whiteboard.Scene
	source                          *document
	title                           string
	ed                              *editor.Editor
	opts                            editor.Options
	tops                            []float32
	file                            *os.File
	writer                          *pdf.Writer
	page                            int
	open, printing, busy, cancelled bool
	next                            time.Time
}

func (pe *pdfExportFeature) exportPDF() { pe.startPDF(false) }
func (pe *pdfExportFeature) printDoc()  { pe.startPDF(true) }

func (pe *pdfExportFeature) startPDF(printing bool) {
	d := pe.host.activeDocument()
	if d == nil || d.loadErr != nil || d.isPDF() || d.isMDZ() || d.isDatabase() || pe.job != nil {
		return
	}
	d.ed.UnmarkText()
	root, rel, err := fileLocation(d.space, d.rel)
	if err != nil {
		pe.host.notify("导出 PDF 失败；%v", err)
		return
	}
	source := &document{space: &space.Space{Root: root}, rel: rel}
	dir := filepath.Dir(d.fullPath())
	if printing {
		dir = filepath.Join(os.TempDir(), i18n.T("FreeFolio 打印"))
		if err := os.MkdirAll(dir, 0700); err != nil {
			pe.host.notify("准备打印失败；%v", err)
			return
		}
	}
	file, err := os.CreateTemp(dir, ".freefolio-export-*.pdf")
	if err != nil {
		pe.host.notify("导出 PDF 失败；%v", err)
		return
	}
	w := pdf.NewWriter(file, d.title())
	if reg, bold, it, boldIt := editor.MathFontData(); len(reg) > 0 {
		w.RegisterFontData(pdf.FontMath, false, false, reg)
		w.RegisterFontData(pdf.FontMath, true, false, bold)
		w.RegisterFontData(pdf.FontMathItalic, false, true, it)
		w.RegisterFontData(pdf.FontMathItalic, true, true, boldIt)
	}
	pe.job = &pdfExport{source: source, title: d.title(), ed: editor.FromDocument(d.committedForRead()), file: file,
		writer: w, open: true, printing: printing, next: pe.host.clock().Add(time.Millisecond)}
	if d.board != nil {
		pe.job.board = d.board.Scene.Clone()
		editor.RegisterBoardPDFFonts(w, pe.job.board)
	} else {
		for _, leaf := range d.ed.Doc.Leaves() {
			if leaf.Block.Kind == "code" && (leaf.Block.Language == "excalidraw" || leaf.Block.Language == "whiteboard") {
				if s, err := whiteboard.Decode(leaf.Block.Text()); err == nil {
					editor.RegisterBoardPDFFonts(w, s)
				}
			}
		}
	}
}

func (pe *pdfExportFeature) cancelPDF() {
	job := pe.job
	if job == nil {
		return
	}
	job.open, job.cancelled = false, true
	if !job.busy {
		pe.discardPDF(job)
	}
}

func (pe *pdfExportFeature) discardPDF(job *pdfExport) {
	_ = job.file.Close()
	_ = os.Remove(job.file.Name())
	if pe.job == job {
		pe.job = nil
	}
}

func (pe *pdfExportFeature) pdfExportView(c *ui.Context) {
	job := pe.job
	if job == nil {
		return
	}
	if job.open {
		ui.Modal(c.Key("pdf-export"), &job.open, func() {
			ui.Column(c).Width(300).Gap(12).Children(func() {
				ui.Text(c, i18n.T("正在导出 PDF")).FontSize(18).FontWeight(600)
				ui.Text(c, job.title).SingleLine().MinWidth(0).TextColor(c.Theme().TextMuted)
				status := i18n.T("正在准备页面")
				if job.tops != nil {
					status = fmt.Sprintf(i18n.T("已完成 %d / %d 页"), job.page, len(job.tops))
				}
				ui.Text(c, status).Label(i18n.T("导出进度"))
				if ui.Button(c.Key("cancel-pdf"), i18n.T("取消导出")).Clicked() {
					job.open = false
				}
			})
		})
	}
	if !job.open {
		pe.cancelPDF()
		return
	}
	if job.busy {
		return
	}
	if wait := job.next.Sub(c.Now()); wait > 0 {
		c.After(wait)
		return
	}
	job.next = c.Now().Add(time.Millisecond)
	if job.tops == nil {
		if job.board != nil {
			job.tops = []float32{0}
			job.writer.ReservePages(1)
			c.After(time.Millisecond)
			return
		}
		job.opts = editor.Options{
			Width:     paperWidth,
			Theme:     documentTheme(false),
			Page:      pageStyle(false),
			LoadImage: pe.host.cachedImageFor(job.source),
			LoadRawImage: func(url string) image.Image {
				if isRemoteImage(url) {
					return pe.host.remoteImage(url)
				}
				return job.source.rawImage(url)
			},
			LoadDatabase: func(target string) (*database.Store, dbview.Options, uint64, error) {
				return pe.host.dbFeature().loadDatabaseFor(job.source, target)
			},
			DatabaseSnapshot: func(target, viewID string) string {
				return pe.host.dbFeature().databaseSnapshotFor(job.source, target, viewID)
			},
		}
		ui.Render(func(c *ui.Context) { job.tops = job.ed.Paginate(job.opts, paperHeight-2*paperMargin) }, 1, 1, 1)
		job.writer.ReservePages(len(job.tops))
		job.writer.SetOutlines(job.ed.ExportOutlines(job.tops, paperHeight, paperMargin, pdf.A4Height))
		c.After(time.Millisecond)
		return
	}
	if job.page >= len(job.tops) {
		pe.finishPDF(job)
		return
	}
	vp := job.buildVectorPage()
	if job.buildErr != nil {
		err := job.buildErr
		pe.discardPDF(job)
		pe.host.notify("导出 PDF 失败；%v", err)
		return
	}
	// Vector drawing stays on the UI thread for font/layout reads; zlib
	// stream compression and disk IO run between frames.
	job.busy = true
	done := func(err error) {
		job.busy = false
		if job.cancelled {
			pe.discardPDF(job)
			return
		}
		if err != nil {
			pe.discardPDF(job)
			pe.host.notify("导出 PDF 失败；%v", err)
			return
		}
		job.page++
	}
	if pe.host.window() == nil {
		done(job.writer.FinishVectorPage(vp))
		c.After(time.Millisecond)
	} else {
		go func() { err := job.writer.FinishVectorPage(vp); pe.host.window().Update(func() { done(err) }) }()
	}
}

func (job *pdfExport) buildVectorPage() *pdf.VectorPage {
	if job.board != nil {
		vp := job.writer.NewVectorPage(pdf.A4Width, pdf.A4Height)
		job.buildErr = editor.ExportBoardVector(vp, job.board, whiteboard.Box{X: 36, Y: 36, W: pdf.A4Width - 72, H: pdf.A4Height - 72}, editor.BoardOptions{})
		return vp
	}
	top := job.tops[job.page]
	body := float32(paperHeight - 2*paperMargin)
	bottom := top + body
	if job.page+1 < len(job.tops) {
		bottom = job.tops[job.page+1]
	}
	vp := job.writer.NewVectorPage(pdf.A4Width, pdf.A4Height)
	job.ed.ExportVectorPage(vp, job.opts, top, bottom, job.page, len(job.tops), job.tops, paperWidth, paperHeight, paperMargin)
	job.buildErr = job.ed.ExportError()
	return vp
}

func (pe *pdfExportFeature) finishPDF(job *pdfExport) {
	err := job.writer.Close()
	if err == nil {
		err = job.file.Sync()
	}
	if closeErr := job.file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		pe.discardPDF(job)
		pe.host.notify("导出 PDF 失败；%v", err)
		return
	}
	if job.printing {
		pe.job = nil
		if print := pe.host.printHook(); print != nil {
			print(job.file.Name())
		} else {
			err = mygo.Shell.OpenPath(job.file.Name())
		}
		if err != nil {
			_ = os.Remove(job.file.Name())
			pe.host.notify("准备打印失败；%v", err)
		} else {
			pe.host.notify("已在系统预览中打开，可从那里打印")
		}
		return
	}
	var rel string
	for i := 1; i < 1000; i++ {
		name := sanitizeName(job.title)
		if i > 1 {
			name = fmt.Sprintf("%s %d", name, i)
		}
		rel = path.Join(path.Dir(job.source.rel), name+".pdf")
		full, resolveErr := resolveFile(job.source.space, rel)
		if resolveErr != nil {
			err = resolveErr
			break
		}
		err = os.Link(job.file.Name(), full)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		break
	}
	pe.discardPDF(job)
	if err != nil {
		pe.host.notify("导出 PDF 失败；%v", err)
		return
	}
	pe.host.notify("已导出 %s（%d 页）", rel, len(job.tops))
	if full, rErr := resolveFile(job.source.space, rel); rErr == nil {
		if sp := pe.host.spaces().ActiveSpace(); sp != nil && !sp.IsExternal() && sp.Root != "" {
			if job.source.space.ID == sp.ID || isInsideDir(sp.Root, full) {
				pe.host.refreshTree()
			}
		}
		if pe.host.window() != nil {
			mygo.Shell.ShowItemInFolder(full)
		}
	}
}

func isInsideDir(root, full string) bool {
	if r, err := filepath.Rel(root, full); err == nil && filepath.IsLocal(r) {
		return true
	}
	rRoot, err1 := filepath.EvalSymlinks(root)
	rFull, err2 := filepath.EvalSymlinks(full)
	if err1 == nil && err2 == nil {
		if r, err := filepath.Rel(rRoot, rFull); err == nil && filepath.IsLocal(r) {
			return true
		}
	}
	return false
}
