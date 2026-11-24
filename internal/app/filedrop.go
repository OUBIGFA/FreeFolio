package app

// Importing documents dragged in from the system's file manager, ported
// from ArcMD fcb7cd3 and adapted to FreeMD: files dropped on a space's
// window, sidebar or folder row are copied into it, never moved, and an
// undo moves the copies to the trash the way deleting does (trashbin
// backup, then the system trash). The file browser opens dropped files
// in place instead (filemode.go).

import (
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/OUBIGFA/FreeMD/internal/space"
	"github.com/OUBIGFA/FreeMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

const fileDropFadeDur = 180 * time.Millisecond

type fileDropKind uint8

const (
	fileDropNone fileDropKind = iota
	fileDropWindow
	fileDropSidebar
	fileDropFolder
)

// fileDropState is the drop target under files dragged over the window
// this frame, and the one the overlay last showed, which it fades out.
type fileDropState struct {
	kind      fileDropKind
	name      string
	lastKind  fileDropKind
	lastName  string
	overSince time.Time
}

// setFileDropOver records the target under the dragged files; a later,
// more specific target in the same frame (sidebar, folder) wins.
func (a *App) setFileDropOver(kind fileDropKind, name string) {
	a.fileDrop.kind, a.fileDrop.name = kind, name
}

// existingSpaceRel reports the path of src in sp when it already is in
// the folder dir, so dropping a file on its own folder opens it instead of
// copying it beside itself.
func existingSpaceRel(sp *space.Space, dir, src string) (string, bool) {
	dst := sp.Root
	if dir != "" {
		full, err := vault.Resolve(sp.Root, dir)
		if err != nil {
			return "", false
		}
		dst = full
	}
	srcInfo, err1 := os.Stat(filepath.Dir(src))
	dstInfo, err2 := os.Stat(dst)
	if err1 != nil || err2 != nil || !os.SameFile(srcInfo, dstInfo) {
		return "", false
	}
	return path.Join(dir, filepath.Base(src)), true
}

// importDroppedFiles copies the documents among paths into the folder dir
// of sp, telling which files it skipped, and opens the first.
func (a *App) importDroppedFiles(sp *space.Space, dir string, paths []string) {
	if sp == nil || sp.IsExternal() || len(paths) == 0 || !a.flushForTask() {
		return
	}
	var created, existing, skipped []string
	for _, src := range paths {
		info, err := os.Stat(src)
		if err != nil || !info.Mode().IsRegular() || !vault.IsDocument(src) {
			skipped = append(skipped, filepath.Base(src))
			continue
		}
		if rel, ok := existingSpaceRel(sp, dir, src); ok {
			existing = append(existing, rel)
			continue
		}
		rel, err := vault.ImportFile(src, sp.Root, dir)
		if err != nil {
			a.notify("导入 %s 失败。%v", filepath.Base(src), err)
			continue
		}
		created = append(created, rel)
	}
	if len(skipped) > 0 {
		a.notify("只能导入 FreeMD、Markdown 或 PDF 文档：%s 未导入。", strings.Join(skipped, "、"))
	}
	if dir != "" && len(created)+len(existing) > 0 {
		for cur := dir; cur != "." && cur != ""; cur = path.Dir(cur) {
			sp.SetExpanded(cur, true)
		}
	}
	if len(created) > 0 {
		a.saveState()
		a.refreshTree()
		a.refreshSearch()
		a.offerUndo(fmt.Sprintf(i18n.T("已导入到「%s」"), folderName(sp, dir)), countLabel(created), func() {
			// trashGroup acts on the active space, which the undo
			// switches back to if the user has left it.
			if a.state.ActiveSpace() != sp {
				a.switchSpace(sp.ID)
			}
			if a.state.ActiveSpace() == sp {
				a.trashGroup(created)
			}
		})
		a.openIn(sp, created[0])
		return
	}
	if len(existing) > 0 {
		a.openIn(sp, existing[0])
	}
}

// editorFileDrop routes files dropped on a document: pictures go into it
// where they were dropped, and documents without pictures are imported
// into the document's folder.
func (a *App) editorFileDrop(d *document, paths []string, at int) {
	docs := false
	for _, p := range paths {
		if isImage(p) {
			a.insertImages(paths, at)
			return
		}
		docs = docs || vault.IsDocument(p)
	}
	if docs && a.files != nil {
		// The file browser opens documents where they are, as tabs.
		for _, p := range paths {
			if vault.IsDocument(p) {
				a.openSystemFile(p)
			}
		}
		return
	}
	if !docs || d == nil || d.space == nil || d.space.IsExternal() {
		a.insertImages(paths, at)
		return
	}
	dir := ""
	if parent := path.Dir(d.rel); parent != "." {
		dir = parent
	}
	a.importDroppedFiles(d.space, dir, paths)
}

// fileDropOverlay shows where dragged files would land: a card over the
// window, or a pill at the sidebar's foot naming the folder. It takes no
// input, so the drop reaches the element under it.
func (a *App) fileDropOverlay(c *ui.Context) {
	active := a.fileDrop.kind != fileDropNone
	if !active && a.fileDrop.lastKind == fileDropNone {
		return
	}
	first := active && a.fileDrop.lastKind == fileDropNone
	if active {
		if first {
			a.fileDrop.overSince = c.Now()
		}
		a.fileDrop.lastKind, a.fileDrop.lastName = a.fileDrop.kind, a.fileDrop.name
	}
	kind, name := a.fileDrop.lastKind, a.fileDrop.lastName
	ui.Overlay(c, func() {
		root := ui.Box(c.Key("file-drop-overlay")).Absolute().Left(0).Top(0).Right(0).Bottom(0).IgnoreInput(true)
		if first && !a.reduceMotion {
			root.Jump("in", 0)
		}
		target := float32(0)
		if active {
			target = 1
		}
		v := root.Animate("in", target, fileDropFadeDur)
		if a.reduceMotion {
			v = target
		}
		if v < 0.01 && !active {
			root.Hidden(true)
			a.fileDrop.lastKind, a.fileDrop.lastName, a.fileDrop.overSince = fileDropNone, "", time.Time{}
			return
		}
		t := c.Theme()
		cols := previewColors(t, a.look)
		// The dashed rim breathes while files hover, unless motion is
		// reduced.
		breathe := float32(0)
		if active && !a.reduceMotion {
			breathe = float32(0.5 + 0.5*math.Sin(c.Now().Sub(a.fileDrop.overSince).Seconds()*3.2))
			c.AnimationFrame()
		}
		rim := t.Accent.Alpha((0.6 + 0.25*breathe) * v)
		if kind == fileDropWindow {
			scrim := float32(0.14)
			if t.Dark {
				scrim = 0.32
			}
			root.Background(black.Alpha(scrim * v)).Center()
			root.Children(func() {
				card := ui.Column(c.Key("file-drop-card")).Width(340).Padding(28, 22).Center().Gap(8).Radius(18).
					Background(cols.fill).Shadow(0, 14, 36, 0, scaleAlpha(cols.shadow, v)).
					Opacity(v).Scale(0.94+0.06*v, 0.94+0.06*v)
				card.Draw(func(p *ui.Painter, r ui.Rect) {
					p.Stroke(r, scaleAlpha(cols.rim, v), 18, 1)
					inset := ui.Rect{X: r.X + 7, Y: r.Y + 7, W: max(0, r.W-14), H: max(0, r.H-14)}
					p.Fill(inset, t.Accent.Alpha(0.045*v), 12)
					p.StrokeDashed(inset, rim, 12, 1.5)
				})
				card.Children(func() {
					ui.Icon(c, iconFileImport).FontSize(36).TextColor(t.Accent.Alpha(v))
					ui.Text(c, fmt.Sprintf(i18n.T("松手导入到「%s」"), name)).FontSize(15).FontWeight(600).TextColor(cols.title).SingleLine()
					ui.Text(c, i18n.T("支持 FreeMD、Markdown 与 PDF 文档")).FontSize(12).TextColor(cols.muted).SingleLine()
				})
			})
			return
		}
		root.Children(func() {
			pill := ui.Row(c.Key("file-drop-pill")).Absolute().Bottom(56).Width(a.sidebarW()-20).
				Padding(10, 12).Radius(12).Gap(10).AlignItems(ui.Center).Background(cols.fill).
				Shadow(0, 8, 22, 0, scaleAlpha(cols.shadow, v)).Opacity(v).Translate(0, 8*(1-v))
			if !a.prefs.SidebarLeft {
				pill.Left(ui.Auto).Right(10)
			}
			pill.Draw(func(p *ui.Painter, r ui.Rect) {
				p.Stroke(r, scaleAlpha(cols.rim, v), 12, 1)
				p.StrokeDashed(ui.Rect{X: r.X + 2.5, Y: r.Y + 2.5, W: max(0, r.W-5), H: max(0, r.H-5)}, rim, 10, 1.25)
			})
			pill.Children(func() {
				ui.Icon(c, iconFileImport).FontSize(20).TextColor(t.Accent.Alpha(v))
				ui.Column(c.Key("file-drop-pill-text")).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, fmt.Sprintf(i18n.T("导入到「%s」"), name)).FontSize(12.5).FontWeight(600).TextColor(cols.title).SingleLine()
					ui.Text(c, i18n.T("支持 FreeMD、Markdown 与 PDF 文档")).FontSize(11).TextColor(cols.muted).SingleLine()
				})
			})
		})
	})
}
