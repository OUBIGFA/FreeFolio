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
	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type boardImageWindow struct {
	exportPath                                     string
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
		rel = d.board.DisplayedAIImage(id)
		if rel == "" {
			rel = out.Path
		}
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
	if n.Role == whiteboard.AIImage || n.Role == whiteboard.AICutout || n.Role == whiteboard.AIUpscale {
		w.exportPath = rel
	}
	a.boardImages = append(a.boardImages, w)
	w.win = a.makeWindow(i18n.T("图片查看与标注"), func(c *ui.Context) { a.boardImageView(c, w) }, func() bool { return a.closeBoardImage(w) })
	watchBoardImageOutsidePress(w.win, func() { a.dismissBoardImageOutside(w) })
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

func saveBoardImageCopy(rel string, read func(string) ([]byte, error), choose func(string) (string, error)) error {
	dest, err := choose(path.Base(unescapeImagePath(rel)))
	if err != nil || dest == "" {
		return err
	}
	data, err := read(rel)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(dest, data, 0644)
}

func (a *App) saveBoardImageAs(parent *mygo.Window, rel string, read func(string) ([]byte, error), done func(error)) {
	title := i18n.T("图片另存为")
	a.backgroundResult(func() func() {
		err := saveBoardImageCopy(rel, read, func(name string) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: parent, Title: title, DefaultPath: name})
		})
		return func() { done(err) }
	})
}

func (a *App) dismissBoardImageOutside(w *boardImageWindow) {
	// Only passive viewing dismisses automatically. Annotation and save
	// sessions keep their window and the existing unsaved-changes protection.
	if !w.closed && !w.editing && !w.saving && !w.confirmClose && w.win != nil {
		w.win.Close()
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
				w.err = i18n.T("白板状态已变化，标注副本已保留在附件中") + " " + rel
				a.redrawBoardImage(w)
				return
			}
			_, _, err = d.board.AddAnnotatedReference(w.nodeID, rel, cfg.Width, cfg.Height, generate, a.boardOptionsWith(nil, d))
			if err != nil {
				w.err = err.Error()
			} else {
				w.savedRevision = revision
				w.message = i18n.T("标注参考图已加入白板；原图保留")
				if generate {
					w.message = i18n.T("标注参考图已连接到新生图节点；填写指令后点击运行节点")
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

	pill := func(key, text string, bg, fg, border ui.Color) {
		b := ui.Row(c.Key(key)).Padding(3, 8).Radius(5).Background(bg).AlignItems(ui.Center).Shrink(0)
		if border != ui.Transparent {
			b.Border(1, border)
		}
		b.Children(func() {
			ui.Text(c, text).FontSize(11.5).FontWeight(500).TextColor(fg).SingleLine()
		})
	}

	ui.Column(c.Key("board-image-window")).Fill().Background(t.Background).Children(func() {
		ui.Row(c.Key("board-image-header")).Height(max(52, bar.Height)).Background(t.Surface).
			Padding(0, bar.Right+16, 0, bar.Left+16).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Row(c.Key("board-image-title-group")).Grow(1).MinWidth(0).Gap(10).AlignItems(ui.Center).Children(func() {
				badgeIcon := iconImage
				title := i18n.T("图片查看")
				if w.editing {
					badgeIcon = iconPencil
					title = i18n.T("图片标注")
				}
				ui.Box(c.Key("board-image-badge")).Size(28, 28).Radius(7).Shrink(0).Center().
					Background(t.Accent.Alpha(0.10)).Children(func() {
					ui.Icon(c, badgeIcon).FontSize(15).TextColor(t.Accent)
				})
				ui.Text(c, title).FontSize(14).FontWeight(600).SingleLine()
				if w.image != nil {
					pill("board-image-dims", fmt.Sprintf("%d × %d", w.image.Width, w.image.Height), t.Background, t.TextMuted, t.Border.Alpha(0.75))
				}
				if !canEdit {
					ui.Row(c.Key("board-image-readonly")).Padding(3, 8).Gap(4).Radius(5).Background(t.Background).Border(1, t.Border.Alpha(0.75)).AlignItems(ui.Center).Shrink(0).Children(func() {
						ui.Icon(c, iconLocked).FontSize(11).TextColor(t.TextMuted)
						ui.Text(c, i18n.T("只读")).FontSize(11.5).FontWeight(500).TextColor(t.TextMuted).SingleLine()
					})
				}
				if w.editing && w.dirty() {
					pill("board-image-dirty", i18n.T("未保存"), t.Accent.Alpha(0.12), t.Accent, ui.Transparent)
				}
			})
			ui.Row(c.Key("board-image-actions")).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
				if !w.editing {
					if w.exportPath != "" && ui.Button(c.Key("save-image-as"), i18n.T("另存为")).Label(i18n.T("图片另存为")).Disabled(w.saving || w.loading).Clicked() {
						w.saving, w.err = true, ""
						parent, _ := w.win.(*mygo.Window)
						a.saveBoardImageAs(parent, w.exportPath, a.boardAI(w.doc).ReadFile, func(err error) {
							w.saving = false
							if err != nil {
								w.err = fmt.Sprintf(i18n.T("另存为失败；%v"), err)
							}
							a.redrawBoardImage(w)
						})
					}
					if ui.PrimaryButton(c.Key("start-image-annotation"), i18n.T("标注图片")).Disabled(!canEdit || w.loading || w.image == nil).Clicked() {
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
		})
		ui.Box(c).Height(1).Shrink(0).Background(t.Border)

		ui.Box(c.Key("board-image-stage")).Grow(1).MinHeight(0).Background(imageStageBg(t)).Clip().Children(func() {
			if w.loading {
				ui.Column(c.Key("board-image-loading")).Fill().Center().Gap(10).Children(func() {
					ui.Spinner(c.Key("board-image-spinner")).TextColor(t.Accent).Label(i18n.T("正在加载图片"))
					ui.Text(c, i18n.T("正在加载图片")).FontSize(13).TextColor(t.TextMuted)
				})
				c.After(50 * time.Millisecond)
				return
			}
			if w.image == nil {
				ui.Column(c.Key("board-image-empty")).Fill().Center().Padding(24).Children(func() {
					ui.Column(c).MaxWidth(360).Padding(20).Gap(12).AlignItems(ui.Center).Background(t.Surface).Border(1, t.Border).Radius(panelRadius).Children(func() {
						ui.Box(c).Size(36, 36).Radius(18).Center().Background(t.Danger.Alpha(0.12)).Children(func() {
							ui.Icon(c, iconAlert).FontSize(18).TextColor(t.Danger)
						})
						errText := w.err
						if errText == "" {
							errText = i18n.T("无法显示图片")
						}
						ui.Text(c, errText).FontSize(13).TextColor(t.Danger).LineHeight(1.45)
						if ui.Button(c.Key("board-image-close-err"), i18n.T("关闭")).Clicked() && w.win != nil {
							w.win.Close()
						}
					})
				})
				return
			}

			var dismiss func()
			if !w.editing && !w.saving && w.win != nil {
				dismiss = func() { w.win.Close() }
			}
			editor.ImageAnnotationView(c, w.image, !w.editing || !canEdit || w.saving, dismiss)

			if w.saving || w.err != "" || w.message != "" {
				borderCol := t.Border
				switch {
				case w.err != "":
					borderCol = t.Danger.Alpha(0.55)
				case w.message != "":
					borderCol = t.Success.Alpha(0.5)
				}
				ui.Row(c.Key("board-image-status")).Absolute().Top(12).Right(14).MaxWidth(380).Padding(8, 12).Gap(8).
					AlignItems(ui.Center).Background(t.Surface).Border(1, borderCol).Radius(8).
					Shadow(0, 4, 14, 0, black.Alpha(0.12)).Children(func() {
					switch {
					case w.saving:
						ui.Spinner(c.Key("board-image-save-spinner")).TextColor(t.Accent).Label(i18n.T("正在保存标注图片"))
						ui.Text(c, i18n.T("正在保存标注图片")).FontSize(12.5).Grow(1).MinWidth(0)
						c.After(50 * time.Millisecond)
					case w.err != "":
						ui.Icon(c, iconAlert).FontSize(14).TextColor(t.Danger).Shrink(0)
						ui.Text(c, w.err).FontSize(12.5).TextColor(t.Danger).Grow(1).MinWidth(0).LineHeight(1.4)
					default:
						ui.Icon(c, iconCheck).FontSize(14).TextColor(t.Success).Shrink(0)
						ui.Text(c, w.message).FontSize(12.5).TextColor(t.Text).Grow(1).MinWidth(0).LineHeight(1.4)
					}
					if !w.saving {
						if iconButton(c, "board-image-status-close", iconX, i18n.T("关闭"), i18n.T("关闭"), false).Size(22, 22).Clicked() {
							w.err = ""
							w.message = ""
						}
					}
				})
			}

			hint := i18n.T("滚轮缩放 · 拖动查看 · 按 Esc 关闭")
			if boardImageOutsidePressSupported {
				hint = i18n.T("滚轮缩放 · 拖动查看 · 点击窗口外或按 Esc 关闭")
			}
			if w.editing {
				hint = i18n.T("滚轮缩放 · 拖动查看 · 标注后保存副本，原图不变")
			}
			if max(w.image.Width, w.image.Height) > 4096 {
				hint += " · " + i18n.T("导出上限 4096 像素")
			}
			ui.Row(c.Key("board-image-hint")).Absolute().Bottom(12).Right(12).Padding(6, 10).Radius(8).
				Background(t.Surface.Alpha(0.92)).Border(1, t.Border).AlignItems(ui.Center).Children(func() {
				ui.Text(c, hint).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
			})
		})
	})
	if win, ok := w.win.(*mygo.Window); ok && hasAppWindowControls() {
		windowButtons(c, win)
	}
	if w.confirmClose {
		ui.Modal(c.Key("annotation-unsaved"), &w.confirmClose, func() {
			ui.Column(c).Width(380).Gap(14).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(32, 32).Radius(8).Center().Shrink(0).Background(t.Warning.Alpha(0.14)).Children(func() {
						ui.Icon(c, iconAlert).FontSize(17).TextColor(t.Warning)
					})
					ui.Text(c, i18n.T("标注尚未保存")).FontSize(16).FontWeight(600)
				})
				ui.Text(c, i18n.T("保存为参考图后关闭，或放弃这次标注")).FontSize(13).TextColor(t.TextMuted).LineHeight(1.5)
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
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
