package editor

// Image inspection reuses the native Excalidraw/dim0 board engine: the same
// pointer handling, shapes, freehand strokes, text, undo and production painter.

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"image"
	"image/png"
	"math"
	"time"
)

// ImageAnnotation owns a disposable drawing session over an immutable base.
// Its scene is never inserted into the user's board; only the rendered PNG is.
type ImageAnnotation struct {
	Board         *Board
	Width, Height int
}

// Snapshot refuses active IME composition so candidate text cannot be baked
// into a generated input. Finished text uses the board's existing commit path.
func (a *ImageAnnotation) Snapshot() (*ImageAnnotation, error) {
	if a.Board.editing != nil && a.Board.editing.hasMarked {
		return nil, errors.New(i18n.T("请先完成输入法文字，再保存标注"))
	}
	a.Board.finishText(a.Board.options)
	b := NewBoard(a.Board.Scene.Clone())
	b.annotationBase = a.Board.annotationBase
	return &ImageAnnotation{Board: b, Width: a.Width, Height: a.Height}, nil
}
func (a *ImageAnnotation) Changed() bool { return a.Board.Revision() != 0 || a.Board.editing != nil }

func (a *ImageAnnotation) Composing() bool {
	return a.Board.editing != nil && a.Board.editing.hasMarked
}
func (a *ImageAnnotation) DiscardComposition() { a.Board.editing = nil }

func NewImageAnnotation(data []byte, crop *whiteboard.ImageCrop, flip whiteboard.Point) (*ImageAnnotation, error) {
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	if len(data) > 32<<20 || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 100000000 {
		return nil, errors.New(i18n.T("图片过大，无法标注"))
	}
	w, h := float64(cfg.Width), float64(cfg.Height)
	if crop != nil {
		if crop.Width <= 0 || crop.Height <= 0 || crop.X < 0 || crop.Y < 0 || crop.X+crop.Width > w || crop.Y+crop.Height > h {
			return nil, errors.New(i18n.T("图片裁剪范围无效"))
		}
		w, h = crop.Width, crop.Height
	}
	s := whiteboard.NewScene()
	s.AppState.ViewBackgroundColor = "transparent"
	id := digest(data)
	s.Files[id] = &whiteboard.FileData{ID: id, MIMEType: "image/" + format, DataURL: boardDataURL("image/"+format, data)}
	base := whiteboard.NewElement(whiteboard.KindImage, 0, 0, w, h)
	base.FileID = id
	base.Status = "saved"
	base.Locked = true
	if crop != nil {
		copy := *crop
		base.Crop = &copy
	}
	base.Scale = whiteboard.Point{1, 1}
	if flip[0] < 0 {
		base.Scale[0] = -1
	}
	if flip[1] < 0 {
		base.Scale[1] = -1
	}
	s.Elements = []*whiteboard.Element{base}
	b := NewBoard(s)
	b.annotationBase = base.ID
	b.Tool = ToolHand
	b.style.StrokeColor = "#e03131"
	b.style.StrokeWidth = 3
	b.style.Roughness = 0
	b.style.FillColor = "transparent"
	b.prepareImages(BoardOptions{})
	for _, err := range b.imageErrors {
		return nil, err
	}
	return &ImageAnnotation{Board: b, Width: int(math.Round(w)), Height: int(math.Round(h))}, nil
}

// PNG renders the exact image rectangle without canvas margins or embedded
// editable metadata. Large images retain their aspect ratio within 4096 pixels.
func (a *ImageAnnotation) PNG() ([]byte, error) {
	s := a.Board.Scene.Clone()
	base := s.ElementByID(a.Board.annotationBase)
	if base == nil || base.IsDeleted {
		return nil, errors.New(i18n.T("标注底图已不存在"))
	}
	b := NewBoard(s)
	b.prepareImages(BoardOptions{})
	for _, e := range b.imageErrors {
		return nil, e
	}
	scale := math.Min(1, 4096/math.Max(float64(a.Width), float64(a.Height)))
	w, h := max(1, int(math.Round(float64(a.Width)*scale))), max(1, int(math.Round(float64(a.Height)*scale)))
	b.Zoom = scale
	b.PanX, b.PanY = 0, 0
	img := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { b.paint(p, r, false, false) })
	}, w, h, 1)
	var out bytes.Buffer
	if e := png.Encode(&out, img); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}

func ImageAnnotationView(c *ui.Context, a *ImageAnnotation, readOnly bool) {
	BoardView(c, a.Board, BoardOptions{Theme: c.Theme(), ReadOnly: readOnly, ImageAnnotation: true})
}

func (b *Board) annotationToolbar(c *ui.Context, o BoardOptions) {
	ui.Row(c.Key("annotation-tools")).Absolute().Top(10).Left(10).Right(10).Justify(ui.Center).PassThrough().Children(func() {
		ui.Row(c).Padding(4).Gap(2).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(8).Children(func() {
			for _, tool := range boardTools {
				switch tool.tool {
				case ToolHand, ToolSelect, ToolRect, ToolEllipse, ToolArrow, ToolLine, ToolFreedraw, ToolText, ToolEraser:
				default:
					continue
				}
				if boardIconButton(c, fmt.Sprintf("annotation-tool-%d", tool.tool), boardToolIcons[tool.tool], i18n.T(tool.label)+" "+tool.key, b.Tool == tool.tool).Clicked() {
					b.finishText(o)
					b.Tool = tool.tool
					b.penSelection()
					b.RequestFocus = true
				}
			}
		})
	})
}

func (b *Board) annotationMenu(m *ui.Menu, o BoardOptions) {
	for _, a := range []struct{ id, label string }{{"undo", "撤销"}, {"redo", "重做"}, {"delete", "删除"}, {"duplicate", "复制图形"}, {"front", "置于顶层"}, {"back", "置于底层"}, {"fit", "适应窗口"}, {"reset-zoom", "实际大小"}} {
		if m.Item(i18n.T(a.label)).Chosen() {
			b.RunAction(a.id, o)
		}
	}
}

func (b *Board) annotationProperties(c *ui.Context, o BoardOptions) {
	ids := b.SelectedIDs()
	if len(ids) == 0 && (b.Tool == ToolHand || b.Tool == ToolSelect || b.Tool == ToolEraser) {
		return
	}
	e := b.style
	if len(ids) > 0 {
		e = b.Scene.ElementByID(ids[0])
		if e == nil || e.ID == b.annotationBase {
			return
		}
	}
	ui.Column(c.Key("annotation-properties")).Absolute().Top(66).Left(12).Width(204).Padding(12).Gap(10).Background(o.Theme.Background).Border(1, o.Theme.Border).Radius(8).Children(func() {
		for _, which := range []struct{ key, label, value string }{{"stroke", "颜色", e.StrokeColor}, {"fill", "填充", e.FillColor}} {
			if which.key == "fill" && b.Tool != ToolRect && b.Tool != ToolEllipse && e.Kind != whiteboard.KindRectangle && e.Kind != whiteboard.KindEllipse {
				continue
			}
			ui.Text(c, i18n.T(which.label)).FontSize(13)
			ui.Row(c.Key(which.key)).Gap(5).Children(func() {
				for _, color := range []string{"#e03131", "#1971c2", "#2f9e44", "#f08c00", "#1e1e1e", "#ffffff", "transparent"} {
					button := ui.ButtonBase(c.Key(color)).Size(21, 21).Radius(3).Background(boardColor(color, false)).Border(1, o.Theme.Border).Focusable().Label(i18n.T(which.label) + color)
					if color == which.value {
						button.Border(2, o.Theme.Accent)
					}
					if button.Clicked() {
						b.ChangeProperty(which.key, color, o)
					}
				}
			})
		}
		for _, field := range []struct {
			key, label    string
			value, lo, hi float64
		}{{"width", "线宽", e.StrokeWidth, 1, 16}, {"opacity", "不透明度", e.Opacity, 10, 100}} {
			ui.Text(c, fmt.Sprintf("%s %.0f", i18n.T(field.label), field.value)).FontSize(13)
			value := field.value
			ui.Slider(c.Key(field.key), &value, field.lo, field.hi)
			if value != field.value {
				b.ChangeProperty(field.key, value, o)
			}
		}
		if b.Tool == ToolText || e.Kind == whiteboard.KindText {
			ui.Text(c, i18n.T("字号")).FontSize(13)
			size := e.FontSize
			if size == 0 {
				size = 20
			}
			ui.NumberInput(c.Key("annotation-font-size"), &size, 12, 96, 1)
			if size != e.FontSize {
				b.ChangeProperty("font-size", size, o)
			}
		}
	})
}

// AddAnnotatedReference creates a reusable image input and optionally a linked
// generator. Existing source pixels, result history and links stay untouched.
func (b *Board) AddAnnotatedReference(sourceID, rel string, width, height int, generate bool, o BoardOptions) (string, string, error) {
	b.err = nil
	source := b.Scene.ElementByID(sourceID)
	if b.ReadOnly || source == nil || source.IsDeleted || source.Locked {
		return "", "", errors.New(i18n.T("原节点已删除、锁定或白板为只读"))
	}
	if rel == "" || width <= 0 || height <= 0 {
		return "", "", errors.New(i18n.T("标注图片无效"))
	}
	var inputID, generatorID string
	b.edit(o, func() {
		w, h := whiteboard.AINodeSize(whiteboard.AIImageInput)
		at := whiteboard.Point{source.X + source.W + 100 + w/2, source.Y + h/2}
		input := b.newAINode(whiteboard.AIImageInput, at, o)
		inputID = input.ID
		fileID := digest([]byte(rel))
		input.FileID = fileID
		input.Status = "saved"
		input.Scale = whiteboard.Point{1, 1}
		b.Scene.Files[fileID] = &whiteboard.FileData{ID: fileID, Path: rel, MIMEType: "image/png", Created: time.Now().UnixMilli()}
		n := whiteboard.AINodeOf(input)
		n.Title = i18n.T("标注参考图")
		_ = whiteboard.SetAINode(input, n)
		if generate {
			gen := b.newAINode(whiteboard.AIImage, whiteboard.Point{input.X + input.W + 100 + 180, input.Y + 220}, o)
			generatorID = gen.ID
			gn := whiteboard.AINodeOf(gen)
			gn.Instruction = i18n.T("按照参考图中的标注修改图片，保留未标注的部分，并移除标注本身。")
			_ = whiteboard.SetAINode(gen, gn)
			b.err = b.connectAI(input, gen, true)
		}
		selected := inputID
		if generatorID != "" {
			selected = generatorID
		}
		b.selectIDs([]string{selected})
	})
	return inputID, generatorID, b.err
}

// OpenAIImage forwards the selected input or generated picture to its host.
func (b *Board) OpenAIImage(id string, edit bool, o BoardOptions) {
	e := b.Scene.ElementByID(id)
	n := b.aiNode(e)
	if n == nil || o.AI == nil || o.AI.OpenImage == nil {
		return
	}
	if edit && (b.ReadOnly || e.Locked || b.AIRunning()) {
		return
	}
	if n.Role == whiteboard.AIImageInput || n.Output() != nil && n.Output().Kind == "image" {
		o.AI.OpenImage(id, edit)
	}
}

func (b *Board) aiImageButtons(c *ui.Context, o BoardOptions, id string) {
	if o.AI == nil || o.AI.OpenImage == nil {
		return
	}
	e := b.Scene.ElementByID(id)
	if ui.Button(c.Key("view-node-image"), i18n.T("查看图片")).Height(28).Clicked() {
		b.OpenAIImage(id, false, o)
	}
	if ui.Button(c.Key("annotate-node-image"), i18n.T("标注图片")).Height(28).Disabled(b.ReadOnly || e == nil || e.Locked || b.AIRunning()).Clicked() {
		b.OpenAIImage(id, true, o)
	}
}
