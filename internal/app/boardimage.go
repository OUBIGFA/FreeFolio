package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"maps"
	"path"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type boardImageWindow struct {
	sourceKey                                      string
	win                                            windowHandle
	doc                                            *document
	nodeID                                         string
	image                                          *editor.ImageAnnotation
	editing, loading, saving, closed, confirmClose bool
	savedRevision                                  uint64
	err, message                                   string
}

func (a *App) openBoardImage(d *document, id string, edit bool) {
	if d == nil || d.board == nil {
		return
	}
	e := d.board.Scene.ElementByID(id)
	n := whiteboard.AINodeOf(e)
	if n == nil {
		return
	}
	if edit && (d.ed.ReadOnly || e.Locked || d.board.AIRunning()) {
		return
	}
	var rel, dataURL string
	var crop *whiteboard.ImageCrop
	flip := whiteboard.Point{1, 1}
	if n.Role == whiteboard.AIImageInput {
		f := d.board.Scene.Files[e.FileID]
		if f == nil {
			return
		}
		rel, dataURL = f.Path, f.DataURL
		if e.Crop != nil {
			v := *e.Crop
			crop = &v
		}
		flip = e.Scale
	} else {
		out := n.Output()
		if out == nil || out.Kind != "image" {
			return
		}
		rel = out.Path
	}
	key, _ := json.Marshal([]any{rel, dataURL, crop, flip})
	for _, w := range a.boardImages {
		if w.doc == d && w.nodeID == id && w.sourceKey == string(key) && !w.closed {
			w.editing = w.editing || edit
			if w.win != nil {
				w.win.Focus()
			}
			return
		}
	}
	w := &boardImageWindow{doc: d, nodeID: id, sourceKey: string(key), editing: edit, loading: true}
	a.boardImages = append(a.boardImages, w)
	w.win = a.makeWindow(i18n.T("图片查看与标注"), func(c *ui.Context) { a.boardImageView(c, w) }, func() bool { return a.closeBoardImage(w) })
	read := a.boardAI(d).ReadFile
	a.backgroundResult(func() func() {
		var data []byte
		var err error
		if dataURL != "" {
			_, encoded, ok := strings.Cut(dataURL, ",")
			if !ok {
				err = errors.New(i18n.T("图片数据无效"))
			} else {
				data, err = base64.StdEncoding.DecodeString(encoded)
			}
		} else {
			data, err = read(rel)
		}
		var annotation *editor.ImageAnnotation
		if err == nil {
			annotation, err = editor.NewImageAnnotation(data, crop, flip)
		}
		return func() {
			if w.closed {
				return
			}
			w.loading = false
			if err != nil {
				w.err = err.Error()
			} else {
				w.image = annotation
				if w.editing {
					w.image.Board.Tool = editor.ToolFreedraw
				}
				w.image.Board.RequestFocus = true
			}
			a.redrawBoardImage(w)
		}
	})
}

func (a *App) redrawBoardImage(w *boardImageWindow) {
	if win, ok := w.win.(*mygo.Window); ok {
		win.Update(func() {})
	}
}
func (w *boardImageWindow) dirty() bool {
	return w.image != nil && (w.image.Composing() || w.image.Board.Revision() != w.savedRevision)
}
func (a *App) closeBoardImage(w *boardImageWindow) bool {
	if w.saving {
		return false
	}
	if w.dirty() {
		w.confirmClose = true
		a.redrawBoardImage(w)
		return false
	}
	w.closed = true
	for i, x := range a.boardImages {
		if x == w {
			a.boardImages = append(a.boardImages[:i], a.boardImages[i+1:]...)
			break
		}
	}
	a.retireClosedDocuments(w.doc)
	return true
}
func (a *App) discardBoardImage(w *boardImageWindow) {
	w.image.DiscardComposition()
	w.savedRevision = w.image.Board.Revision()
	w.confirmClose = false
	if w.win != nil {
		w.win.Close()
	}
}

func (a *App) saveBoardImage(w *boardImageWindow, generate bool) {
	if w.image == nil || w.saving {
		return
	}
	d := w.doc
	source := d.board.Scene.ElementByID(w.nodeID)
	if d.closed.Load() || d.ed.ReadOnly || source == nil || source.IsDeleted || source.Locked || d.board.AIRunning() {
		w.err = i18n.T("原节点已删除、锁定或白板为只读")
		return
	}
	snap, err := w.image.Snapshot()
	if err != nil {
		w.err = err.Error()
		return
	}
	w.saving = true
	w.err = ""
	revision := w.image.Board.Revision()
	closeAfter := w.confirmClose
	spaceCopy := *d.space
	spaceCopy.ExternalFiles = maps.Clone(d.space.ExternalFiles)
	target := &document{space: &spaceCopy, rel: d.rel}
	a.backgroundResult(func() func() {
		png, err := snap.PNG()
		var rel string
		var cfg image.Config
		var written string
		if err == nil {
			cfg, _, err = image.DecodeConfig(bytes.NewReader(png))
		}
		if err == nil {
			rel, err = writeAssetForTarget(target, path.Dir(target.rel), "annotated-image.png", png, func(full string) { written = full })
		}
		return func() {
			w.saving = false
			if written != "" {
				a.markSelfWrite(written)
			}
			if w.closed {
				return
			}
			if err != nil {
				w.err = err.Error()
				a.redrawBoardImage(w)
				return
			}
			source := d.board.Scene.ElementByID(w.nodeID)
			if d.ed.ReadOnly || source == nil || source.IsDeleted || source.Locked || d.board.AIRunning() || d.rel != target.rel || d.space.Root != target.space.Root {
				w.err = i18n.T("白板状态已变化，标注副本已保留在附件中。") + rel
				a.redrawBoardImage(w)
				return
			}
			_, _, err = d.board.AddAnnotatedReference(w.nodeID, rel, cfg.Width, cfg.Height, generate, a.boardOptionsWith(nil, d))
			if err != nil {
				w.err = err.Error()
			} else {
				w.savedRevision = revision
				w.message = i18n.T("标注参考图已加入白板；原图保留。")
				if generate {
					w.message = i18n.T("标注参考图已连接到新生图节点。填写指令后点击运行节点。")
				}
			}
			w.confirmClose = false
			a.redrawBoardImage(w)
			if err == nil && closeAfter && w.win != nil {
				w.win.Close()
			}
		}
	})
}

func (a *App) boardImageView(c *ui.Context, w *boardImageWindow) {
	c.Root().Font(a.interfaceFont())
	t := c.Theme()
	bar := c.TitleBar()
	d := w.doc
	source := d.board.Scene.ElementByID(w.nodeID)
	canEdit := !d.ed.ReadOnly && source != nil && !source.IsDeleted && !source.Locked && !d.board.AIRunning()
	ui.Column(c.Key("board-image-window")).Fill().Background(t.Background).Children(func() {
		ui.Row(c.Key("board-image-header")).Height(max(48, bar.Height)).Padding(6, bar.Right+12, 6, bar.Left+12).Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, i18n.T("图片查看与标注")).FontSize(15).FontWeight(600).Grow(1)
			if w.image != nil {
				ui.Text(c, fmt.Sprintf("%d × %d", w.image.Width, w.image.Height)).FontSize(12).TextColor(t.TextMuted)
			}
			if !w.editing {
				if ui.Button(c.Key("start-image-annotation"), i18n.T("标注图片")).Disabled(!canEdit || w.loading || w.image == nil).Clicked() {
					w.editing = true
					w.image.Board.Tool = editor.ToolFreedraw
					w.image.Board.RequestFocus = true
				}
			} else {
				if ui.Button(c.Key("save-annotation-reference"), i18n.T("保存为参考图")).Disabled(!canEdit || w.saving || w.loading || w.image == nil).Clicked() {
					a.saveBoardImage(w, false)
				}
				if ui.PrimaryButton(c.Key("annotation-generate"), i18n.T("用于生图")).Disabled(!canEdit || w.saving || w.loading || w.image == nil).Clicked() {
					a.saveBoardImage(w, true)
				}
			}
		})
		if w.err != "" {
			ui.Text(c, w.err).TextColor(t.Danger).Padding(8, 12)
		}
		if w.message != "" {
			ui.Text(c, w.message).TextColor(t.TextMuted).Padding(8, 12)
		}
		if w.loading {
			ui.Column(c).Grow(1).Center().Children(func() { ui.Text(c, i18n.T("正在加载图片…")) })
			c.After(50 * time.Millisecond)
			return
		}
		if w.saving {
			ui.Text(c, i18n.T("正在保存标注图片…")).Padding(8, 12)
			c.After(50 * time.Millisecond)
		}
		if w.image != nil {
			editor.ImageAnnotationView(c, w.image, !w.editing || !canEdit || w.saving)
		}
		if w.image != nil && max(w.image.Width, w.image.Height) > 4096 {
			ui.Text(c, i18n.T("标注副本最长边为 4096 像素，原图保持原分辨率。")).FontSize(12).TextColor(t.TextMuted).Padding(4, 12)
		}
		ui.Text(c, i18n.T("滚轮缩放 · 拖动查看 · 标注后保存副本，原图不变")).FontSize(12).TextColor(t.TextMuted).Padding(6, 12)
	})
	if win, ok := w.win.(*mygo.Window); ok && hasAppWindowControls() {
		windowButtons(c, win)
	}
	if w.confirmClose {
		ui.Modal(c.Key("annotation-unsaved"), &w.confirmClose, func() {
			ui.Column(c).Width(360).Gap(12).Children(func() {
				ui.Text(c, i18n.T("标注尚未保存")).FontSize(17)
				ui.Text(c, i18n.T("保存为参考图后关闭，或放弃这次标注。"))
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, i18n.T("继续编辑")).Clicked() {
						w.confirmClose = false
					}
					if ui.Button(c, i18n.T("放弃标注")).Clicked() {
						a.discardBoardImage(w)
					}
					if ui.PrimaryButton(c, i18n.T("保存并关闭")).Disabled(!canEdit || w.saving).Clicked() {
						a.saveBoardImage(w, false)
					}
				})
			})
		})
	}
}
