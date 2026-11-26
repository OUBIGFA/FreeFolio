package editor

import (
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func (b *Board) libraryPanel(c *ui.Context, o BoardOptions) {
	ui.TextInput(c.Key("board-library-search"), &b.libraryQuery).Placeholder(i18n.T("搜索组件")).WidthPercent(100)
	var user []whiteboard.LibraryItem
	if o.UserLibrary != nil {
		user = o.UserLibrary()
	}
	ui.Row(c).Gap(6).Children(func() {
		if o.ImportLibrary != nil && ui.Button(c, i18n.T("导入素材库")).Clicked() {
			o.ImportLibrary(func(items []whiteboard.LibraryItem, err error) {
				if err != nil {
					b.err = err
					return
				}
				if o.SaveLibrary != nil {
					o.SaveLibrary(append(user, items...))
				}
			})
		}
		if o.ExportLibrary != nil && ui.Button(c, i18n.T("导出素材库")).Clicked() {
			o.ExportLibrary(user)
		}
	})
	if len(b.SelectedIDs()) > 0 && o.SaveLibrary != nil {
		ui.TextInput(c.Key("board-library-name"), &b.libraryName).Placeholder(i18n.T("组件名称")).WidthPercent(100)
		if ui.Button(c, i18n.T("保存选中图形")).Disabled(strings.TrimSpace(b.libraryName) == "").Clicked() {
			item, err := b.Scene.LibrarySelection(b.SelectedIDs(), strings.TrimSpace(b.libraryName), "自定义")
			if err != nil {
				b.err = err
			} else {
				o.SaveLibrary(append(user, item))
				b.libraryName = ""
			}
		}
	}
	items := append(whiteboard.BuiltinLibrary(), user...)
	for _, item := range items {
		if b.libraryQuery != "" && !strings.Contains(strings.ToLower(item.Name+" "+item.Category), strings.ToLower(b.libraryQuery)) {
			continue
		}
		row := ui.ButtonBase(c.Key("board-lib-"+item.ID)).WidthPercent(100).Padding(8).Gap(10).AlignItems(ui.Center).Radius(8).Border(1, o.Theme.Border).Focusable().Label(i18n.T(item.Name))
		if row.Hovered() {
			row.Background(o.Theme.SurfaceHover)
		}
		row.Children(func() {
			if b.libraryPreviews == nil {
				b.libraryPreviews = map[string]*Board{}
			}
			preview := b.libraryPreviews[item.ID]
			if preview == nil {
				s := whiteboard.NewScene()
				s.Elements = item.Elements
				s.Files = item.Files
				preview = NewBoard(s)
				preview.prepareImages(o)
				b.libraryPreviews[item.ID] = preview
			}
			ui.Box(c.Key("preview:"+item.ID)).Size(70, 48).Radius(6).Clip().Border(1, o.Theme.Border.Alpha(0.5)).Draw(func(p *ui.Painter, r ui.Rect) {
				preview.lastRect = r
				preview.fit(false)
				preview.paint(p, r, o.Theme.Dark, false)
			})
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, i18n.T(item.Name)).FontSize(13).FontWeight(500)
				ui.Text(c, i18n.T(item.Category)).FontSize(11.5).TextColor(o.Theme.TextMuted)
			})
		})
		if row.Clicked() {
			b.edit(o, func() {
				ids, err := b.Scene.InsertLibrary(item, b.scenePoint(b.lastRect.W/2, b.lastRect.H/2))
				if err != nil {
					b.err = err
				} else {
					b.selectIDs(ids)
				}
			})
			b.RequestFocus = true
		}
		if o.SaveLibrary != nil {
			row.ContextMenu(func(m *ui.Menu) {
				if m.Item(i18n.T("移除自定义组件")).Chosen() {
					next := make([]whiteboard.LibraryItem, 0, len(user))
					for _, it := range user {
						if it.ID != item.ID {
							next = append(next, it)
						}
					}
					o.SaveLibrary(next)
				}
			})
		}
	}
}
func (b *Board) mermaidPanel(c *ui.Context, o BoardOptions) {
	if b.mermaidInput == "" {
		b.mermaidInput = "flowchart LR\n  A[Client] --> B{Gateway}\n  B --> C[(Database)]"
	}
	ui.Text(c, i18n.T("将流程图或时序图转成可编辑图形")).FontSize(13)
	ui.TextArea(c.Key("board-mermaid-input"), &b.mermaidInput).Height(230).WidthPercent(100).Font("monospace").FontSize(13)
	if ui.Button(c, i18n.T("插入图形")).Clicked() {
		s, err := MermaidBoard(b.mermaidInput, whiteboard.Point{})
		if err != nil {
			b.err = err
			return
		}
		if b.pasteScene(s, o) {
			b.panel = ""
			b.RequestFocus = true
		}
	}
}
