package editor

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// fakeAI records the jobs a run sends and answers them like a service.
type fakeAI struct {
	mu    sync.Mutex
	jobs  []BoardAIJob
	files map[string][]byte
	fail  string
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (f *fakeAI) options(t *testing.T) BoardOptions {
	data := testPNG(t)
	f.files = map[string][]byte{}
	ai := &BoardAI{
		Models: func(role whiteboard.AIRole) []BoardAIModel {
			return []BoardAIModel{{Connection: "c", ConnectionName: "Svc", Model: "m-" + string(role)}}
		},
		Default: func(role whiteboard.AIRole) BoardAIModel {
			return BoardAIModel{Connection: "c", Model: "m-" + string(role)}
		},
		ReadFile: func(rel string) ([]byte, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if d, ok := f.files[rel]; ok {
				return d, nil
			}
			return nil, errors.New("missing")
		},
	}
	ai.Prepare = func() (*BoardAIRunner, error) {
		return &BoardAIRunner{
			Text: func(_ context.Context, job BoardAIJob, delta func(string)) (string, error) {
				f.mu.Lock()
				f.jobs = append(f.jobs, job)
				f.mu.Unlock()
				if f.fail == job.NodeID {
					return "", errors.New("service down")
				}
				delta("partial")
				return "answer: " + job.Node.Instruction, nil
			},
			Media: func(_ context.Context, job BoardAIJob, _ func(string)) ([]BoardAIMedia, error) {
				f.mu.Lock()
				defer f.mu.Unlock()
				f.jobs = append(f.jobs, job)
				rel := "assets/ai-" + job.NodeID + ".png"
				f.files[rel] = data
				return []BoardAIMedia{{Output: whiteboard.AIOutput{Kind: "image", Path: rel, MIME: "image/png"}, Data: data}}, nil
			},
			Note: func(name string) (string, error) { return "note body of " + name, nil },
		}, nil
	}
	return BoardOptions{AI: ai}
}

func note(b *Board, text string, x float64) *whiteboard.Element {
	n := whiteboard.NewElement(whiteboard.KindNote, x, 0, 200, 150)
	l := whiteboard.NewElement(whiteboard.KindText, x, 0, 0, 0)
	l.Text, l.OriginalText = text, text
	b.Scene.Elements = append(b.Scene.Elements, n, l)
	_ = b.Scene.BindText(l.ID, n.ID, nil)
	return n
}

func TestBoardAITextNodeRunsCachesAndUndoes(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	a, z := note(b, "苹果", 0), note(b, "香蕉", 0)
	b.selectIDs([]string{a.ID, z.ID})
	id := b.AIFromSelection(whiteboard.AIText, o)
	if id == "" || len(b.Scene.AIInputs(id)) != 2 {
		t.Fatalf("selection node: %q %+v", id, b.Scene.AIInputs(id))
	}
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Instruction = "总结" })
	b.RunAI([]string{id}, false, false, o)
	n := whiteboard.AINodeOf(b.Scene.ElementByID(id))
	if n.Output() == nil || n.Output().Text != "answer: 总结" || n.InputHash == "" || n.Model != "m-text" {
		t.Fatalf("result: %+v", n)
	}
	job := f.jobs[0]
	if !strings.Contains(job.Prompt, "苹果") || !strings.Contains(job.Prompt, "香蕉") || !strings.HasPrefix(job.Prompt, "总结") {
		t.Fatalf("prompt %q", job.Prompt)
	}
	b.RunAI([]string{id}, false, false, o)
	if len(f.jobs) != 1 || b.ai.runs[id].status != "cached" {
		t.Fatalf("unchanged node ran again: %d %+v", len(f.jobs), b.ai.runs[id])
	}
	b.RunAI([]string{id}, false, true, o)
	if len(f.jobs) != 2 || len(whiteboard.AINodeOf(b.Scene.ElementByID(id)).Outputs) != 2 {
		t.Fatal("forced run did not add a result")
	}
	b.Undo()
	if len(whiteboard.AINodeOf(b.Scene.ElementByID(id)).Outputs) != 1 {
		t.Fatal("undo did not remove the latest result")
	}
	b.Scene.SetText(b.Scene.ElementByID(a.BoundTextID()).ID, "梨", nil)
	b.RunAI([]string{id}, false, false, o)
	if len(f.jobs) != 3 {
		t.Fatal("changed input did not rerun")
	}
}

func TestBoardAIChainsImagesWritesBackAndBlocksDownstream(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	src := note(b, "a red cat", 0)
	gen := b.AddAINode(whiteboard.AIImage, whiteboard.Point{600, 0}, o)
	edit := b.AddAINode(whiteboard.AIImage, whiteboard.Point{1200, 0}, o)
	text := b.AddAINode(whiteboard.AIText, whiteboard.Point{1800, 0}, o)
	b.edit(o, func() {
		_ = b.connectAI(src, b.Scene.ElementByID(gen), true)
		_ = b.connectAI(b.Scene.ElementByID(gen), b.Scene.ElementByID(edit), true)
		_ = b.connectAI(src, b.Scene.ElementByID(text), true)
	})
	b.updateAINode(o, edit, func(n *whiteboard.AINode) { n.Instruction = "make it blue" })
	b.updateAINode(o, text, func(n *whiteboard.AINode) { n.Params.Write = "replace"; n.Instruction = "rewrite" })
	b.RunAI(nil, false, false, o)
	var editJob *BoardAIJob
	for i := range f.jobs {
		if f.jobs[i].NodeID == edit {
			editJob = &f.jobs[i]
		}
	}
	if editJob == nil || len(editJob.Images) != 1 || editJob.Images[0].MIME != "image/png" || editJob.Prompt != "make it blue" {
		t.Fatalf("edit job %+v", editJob)
	}
	if got := b.Scene.ElementText(src); got != "answer: rewrite" {
		t.Fatalf("write back: %q", got)
	}
	if out := whiteboard.AINodeOf(b.Scene.ElementByID(edit)).Output(); out == nil || out.Kind != "image" {
		t.Fatal("edit result missing")
	}
	// A failing upstream node leaves its downstream unrun.
	f.fail = text
	down := b.AddAINode(whiteboard.AIText, whiteboard.Point{2400, 0}, o)
	b.edit(o, func() { _ = b.connectAI(b.Scene.ElementByID(text), b.Scene.ElementByID(down), true) })
	b.RunAI([]string{down}, true, true, o)
	if b.ai.runs[text].status != "error" || b.ai.runs[down].status != "blocked" || whiteboard.AINodeOf(b.Scene.ElementByID(text)).Error != "service down" {
		t.Fatalf("%+v %+v", b.ai.runs[text], b.ai.runs[down])
	}
	b.placeAIOutput(edit, o)
	if b.Scene.ElementByID(b.SelectedIDs()[0]).Kind != whiteboard.KindImage {
		t.Fatal("placing the result did not insert a picture")
	}
}

func TestBoardAIFrameNoteAndConcatInputs(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	frame := whiteboard.NewElement(whiteboard.KindFrame, 0, 0, 300, 300)
	b.Scene.Elements = append(b.Scene.Elements, frame)
	mark := note(b, "换成陶瓷", 20)
	mark.FrameID = frame.ID
	ref := b.AddAINode(whiteboard.AINote, whiteboard.Point{500, 500}, o)
	b.updateAINode(o, ref, func(n *whiteboard.AINode) { n.Note = "说明" })
	cat := b.AddAINode(whiteboard.AIConcat, whiteboard.Point{900, 500}, o)
	b.updateAINode(o, cat, func(n *whiteboard.AINode) { n.Params.Template = "<{1}>" })
	img := b.AddAINode(whiteboard.AIImage, whiteboard.Point{900, 0}, o)
	b.edit(o, func() {
		_ = b.connectAI(b.Scene.ElementByID(ref), b.Scene.ElementByID(cat), true)
		_ = b.connectAI(frame, b.Scene.ElementByID(img), true)
		_ = b.connectAI(b.Scene.ElementByID(cat), b.Scene.ElementByID(img), true)
	})
	b.RunAI([]string{img}, true, false, o)
	if out := whiteboard.AINodeOf(b.Scene.ElementByID(cat)).Output(); out == nil || out.Text != "<note body of 说明>" {
		t.Fatalf("concat %+v", out)
	}
	job := f.jobs[len(f.jobs)-1]
	if len(job.Images) != 1 || !strings.Contains(job.Prompt, "换成陶瓷") || !strings.Contains(job.Prompt, "<note body of 说明>") {
		t.Fatalf("frame job %+v", job.Prompt)
	}
}

func TestBoardAIConvertImageToReferenceNode(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	data := testPNG(t)
	if err := b.InsertImage(data, whiteboard.Point{240, 270}); err != nil {
		t.Fatal(err)
	}
	picture := b.Scene.ElementByID(b.SelectedIDs()[0])
	picture.X, picture.Y, picture.W, picture.H = 120, 180, 240, 180
	picture.Crop = &whiteboard.ImageCrop{X: 1, Y: 0, Width: 2, Height: 3, NaturalWidth: 4, NaturalHeight: 4}
	picture.Scale = [2]float64{-1, 1}
	id, fileID := picture.ID, picture.FileID
	original := picture.Clone()
	// Imported attachments may already live on disk instead of in a data URL.
	asset := b.Scene.Files[fileID]
	asset.Path, asset.DataURL = "assets/reference.png", ""
	o.ResolveFile = func(file *whiteboard.FileData) ([]byte, error) {
		if file.Path != "assets/reference.png" {
			return nil, errors.New("unexpected asset")
		}
		return data, nil
	}
	targetID := b.AddAINode(whiteboard.AIImage, whiteboard.Point{800, 360}, o)
	if err := b.connectAI(picture, b.Scene.ElementByID(targetID), true); err != nil {
		t.Fatal(err)
	}
	b.selectIDs([]string{id})
	b.fitted = true
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
	if err := tt.Click("转为节点"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	converted := b.Scene.ElementByID(id)
	n := whiteboard.AINodeOf(converted)
	if n == nil || n.Role != "image-input" || converted.FileID != fileID || len(b.Scene.Files) != 1 || converted.Scale != original.Scale || !reflect.DeepEqual(converted.Crop, original.Crop) {
		t.Fatalf("conversion lost picture data: %+v", converted)
	}
	if len(f.jobs) != 0 || n.Model != "" || n.Connection != "" || len(n.Outputs) != 0 {
		t.Fatal("conversion must reuse the picture without generating it")
	}
	if whiteboard.AINodeOf(b.Scene.ElementByID(targetID)).LinkAt("image.0") != id {
		t.Fatal("conversion broke the existing reference link")
	}
	if !b.Undo() || whiteboard.IsAINode(b.Scene.ElementByID(id)) || b.Scene.ElementByID(id).W != original.W {
		t.Fatal("conversion could not be undone")
	}
	if !b.Redo() || !whiteboard.IsAINode(b.Scene.ElementByID(id)) {
		t.Fatal("conversion could not be redone")
	}
	tt.Frame()
	x, y := slotAt(b, id, "")
	X, Y := slotAt(b, targetID, "image.0")
	drag(tt, x, y, X, Y)
	if whiteboard.AINodeOf(b.Scene.ElementByID(targetID)).LinkAt("image.0") != id {
		t.Fatal("image output did not connect to the reference slot")
	}
	noArrows(t, b)
	for _, upstream := range []bool{false, true} {
		before := len(f.jobs)
		b.RunAI([]string{targetID}, upstream, true, o)
		if len(f.jobs) != before+1 {
			t.Fatalf("generation did not run once: %+v", b.ai.runs[targetID])
		}
		job := f.jobs[len(f.jobs)-1]
		if job.NodeID != targetID || len(job.Images) != 1 || !bytes.Equal(job.Images[0].Data, data) {
			t.Fatalf("reference picture did not reach generation: %+v", job)
		}
	}
	if len(f.jobs) != 2 || len(whiteboard.AINodeOf(b.Scene.ElementByID(id)).Outputs) != 0 {
		t.Fatal("running upstream regenerated the input picture")
	}
	delete(b.Scene.Files, fileID)
	b.RunAI([]string{targetID}, true, true, o)
	if len(f.jobs) != 2 || b.ai.runs[id].status != "error" || b.ai.runs[targetID].status != "blocked" {
		t.Fatal("a missing reference picture reached generation")
	}
}

func TestBoardAIImageConversionMenuAndPermissions(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	if err := b.InsertImage(testPNG(t), whiteboard.Point{260, 280}); err != nil {
		t.Fatal(err)
	}
	picture := b.Scene.ElementByID(b.SelectedIDs()[0])
	picture.X, picture.Y, picture.W, picture.H = 160, 180, 200, 200
	b.fitted = true
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
	picture.Locked = true
	tt.Frame()
	if tt.HasText("转为节点") {
		t.Fatal("locked picture offers conversion")
	}
	b.convertImagesToNodes(o)
	if whiteboard.IsAINode(picture) {
		t.Fatal("locked picture was converted")
	}
	picture.Locked = false
	o.ReadOnly = true
	tt.Frame()
	b.convertImagesToNodes(o)
	if tt.HasText("转为节点") || whiteboard.IsAINode(picture) {
		t.Fatal("read-only picture was converted")
	}
	o.ReadOnly = false
	tt.Frame()
	tt.RightClickAt(260, 280)
	if err := tt.ChooseMenuItem("转为节点"); err != nil {
		t.Fatal(err)
	}
	if !whiteboard.IsAINode(b.Scene.ElementByID(picture.ID)) {
		t.Fatal("context menu did not convert the picture")
	}
}

// slotAt is a node slot's point in the canvas: the output when slot is "".
func slotAt(b *Board, id, slot string) (float32, float32) {
	e := b.Scene.ElementByID(id)
	n := b.aiNode(e)
	l := b.aiLayout(e, n)
	p := aiOutputPoint(e, l)
	for i, sl := range l.slots {
		if sl.Name == slot {
			p = aiInputPoint(e, l, i)
		}
	}
	x, y, _, _ := b.canvasRect(p[0], p[1], 0, 0)
	return x, y
}

func drag(tt *ui.Tester, x, y, X, Y float32) {
	tt.Press(x, y)
	tt.Move((x+X)/2, (y+Y)/2)
	tt.Move(X, Y)
	tt.Release(X, Y)
	tt.Frame()
}

func noArrows(t *testing.T, b *Board) {
	t.Helper()
	for _, el := range b.Scene.Elements {
		if el.IsLinear() && !el.IsDeleted {
			t.Fatal("a node link became a board line")
		}
	}
}

func TestBoardAIOutputDragPickerAndPanel(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
	tt.Frame()
	b.ai.bar = true
	tt.Frame()
	if err := tt.Click("文本生成"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	e, n := b.selectedAINode()
	if e == nil || n.Role != whiteboard.AIText {
		t.Fatal("node bar did not add a node")
	}
	b.ai.bar = false
	// Settings live on the card; the full panel opens on demand.
	if _, ok := tt.Find("运行节点"); ok {
		t.Fatal("panel opened by itself")
	}
	if err := tt.Click("全部设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("运行节点"); !ok {
		t.Fatal("full settings missing")
	}
	b.ai.panel = false
	// A link dragged from the output onto empty canvas offers nodes.
	x, y := slotAt(b, e.ID, "")
	drag(tt, x, y, x+260, y+40)
	if b.ai.picker == nil {
		t.Fatal("picker did not open")
	}
	if err := tt.Click("图像生成"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	var image *whiteboard.Element
	for _, el := range b.Scene.Elements {
		if n := whiteboard.AINodeOf(el); n != nil && n.Role == whiteboard.AIImage {
			image = el
		}
	}
	if image == nil || whiteboard.AINodeOf(image).LinkAt("prompt") != e.ID {
		t.Fatal("picked node not linked into its prompt")
	}
	noArrows(t, b)
	// The new node's slot sits where the link was dropped.
	if px, py := slotAt(b, image.ID, "prompt"); px < x+250 || px > x+270 || py < y+30 || py > y+50 {
		t.Fatalf("slot at %v,%v", px, py)
	}
	// Typing in the card is one undo step.
	b.selectIDs([]string{e.ID})
	b.ai.focusInstruction = true
	tt.Frame()
	tt.Frame()
	tt.Type("写一首诗")
	tt.Frame()
	if whiteboard.AINodeOf(b.Scene.ElementByID(e.ID)).Instruction != "写一首诗" {
		t.Fatal("instruction not stored")
	}
	b.RequestFocus = true
	tt.Frame()
	b.Undo()
	if whiteboard.AINodeOf(b.Scene.ElementByID(e.ID)).Instruction != "" {
		t.Fatal("typing session not undone in one step")
	}
}

func TestBoardAILinkedPromptDisablesFieldAndLinksMove(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1280, 860)
	tt.Frame()
	promptID := b.AddAINode(whiteboard.AIPrompt, whiteboard.Point{240, 320}, o)
	imgID := b.AddAINode(whiteboard.AIImage, whiteboard.Point{760, 320}, o)
	b.Selected = map[string]bool{}
	tt.Frame()

	area, ok := tt.Access("文字内容")
	if !ok {
		t.Fatal("text node field missing")
	}
	tt.ClickAt(area.Bounds.X+area.Bounds.W/2, area.Bounds.Y+area.Bounds.H/2)
	tt.Type("a sunlit studio")
	tt.Frame()
	if got := whiteboard.AINodeOf(b.Scene.ElementByID(promptID)).Instruction; got != "a sunlit studio" {
		t.Fatalf("inline edit got %q", got)
	}
	if field, ok := tt.Access("指令"); !ok || field.Disabled {
		t.Fatal("unlinked instruction field should be editable")
	}

	// From the image node's free prompt input back onto the text node.
	sx, sy := slotAt(b, imgID, "prompt")
	p := b.Scene.ElementByID(promptID)
	ex, ey, _, _ := b.canvasRect(p.X+p.W/2, p.Y+20, 0, 0)
	drag(tt, sx, sy, ex, ey)
	if got := whiteboard.AINodeOf(b.Scene.ElementByID(imgID)).LinkAt("prompt"); got != promptID {
		t.Fatalf("backward link failed: %q", got)
	}
	noArrows(t, b)
	field, ok := tt.Access("指令")
	if !ok || !field.Disabled || field.Value != "" || !strings.Contains(field.Description, "已连接") {
		t.Fatalf("linked instruction field: %+v", field)
	}
	// Run: the linked text is the prompt, not the node's own instruction.
	b.updateAINode(o, imgID, func(n *whiteboard.AINode) { n.Instruction = "ignored" })
	b.RunAI([]string{imgID}, false, false, o)
	if len(f.jobs) != 1 || f.jobs[0].Prompt != "a sunlit studio" {
		t.Fatalf("prompt %+v", f.jobs)
	}

	// Picking the link up from its input and dropping it on nothing
	// disconnects it, as one undo step.
	sx, sy = slotAt(b, imgID, "prompt")
	drag(tt, sx, sy, sx-80, sy+260)
	if whiteboard.AINodeOf(b.Scene.ElementByID(imgID)).LinkAt("prompt") != "" || b.ai.picker != nil {
		t.Fatal("picked-up link not removed")
	}
	b.Undo()
	if whiteboard.AINodeOf(b.Scene.ElementByID(imgID)).LinkAt("prompt") != promptID {
		t.Fatal("undo did not restore the link")
	}
	tt.Frame()

	// A click on the link selects it; Delete removes it.
	links := b.aiLinks()
	if len(links) != 1 {
		t.Fatal(links)
	}
	mid := aiSplineAt(links[0].curve, 0.3)
	mx, my, _, _ := b.canvasRect(mid[0], mid[1], 0, 0)
	tt.ClickAt(mx, my)
	tt.Frame()
	if b.ai.link == nil || b.ai.link.node != imgID {
		t.Fatal("link not selected")
	}
	tt.Key(0, ui.KeyDelete)
	tt.Frame()
	if whiteboard.AINodeOf(b.Scene.ElementByID(imgID)).LinkAt("prompt") != "" || b.Scene.ElementByID(promptID).IsDeleted {
		t.Fatal("Delete did not remove just the link")
	}
	if field, ok := tt.Access("指令"); !ok || field.Disabled {
		t.Fatal("field stays disabled after unlinking")
	}
}

func TestBoardAIRejectsIncompatibleAndCyclicLinks(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1400, 900)
	tt.Frame()
	img := b.AddAINode(whiteboard.AIImage, whiteboard.Point{300, 300}, o)
	cat := b.AddAINode(whiteboard.AIConcat, whiteboard.Point{800, 300}, o)
	text := b.AddAINode(whiteboard.AIText, whiteboard.Point{1200, 300}, o)
	tt.Frame()
	// A picture cannot feed a text-only node.
	x, y := slotAt(b, img, "")
	c := b.Scene.ElementByID(cat)
	cx, cy, _, _ := b.canvasRect(c.X+c.W/2, c.Y+c.H/2, 0, 0)
	drag(tt, x, y, cx, cy)
	if len(whiteboard.AINodeOf(c).Links) != 0 || b.ai.picker != nil {
		t.Fatal("incompatible link made or picker opened over a node")
	}
	// text → concat, then concat → text would be a cycle.
	b.edit(o, func() { _ = b.connectAI(b.Scene.ElementByID(text), c, false) })
	if err := b.Connect(o, cat, text, ""); err == nil {
		t.Fatal("cycle accepted")
	}
	noArrows(t, b)
}

func TestBoardAICardControlsStayWhileDragging(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
	tt.Frame()
	b.AddAINode(whiteboard.AIPrompt, whiteboard.Point{300, 300}, o)
	b.Selected = map[string]bool{}
	tt.Frame()
	present := func(when string) {
		t.Helper()
		if _, ok := tt.Access("文字内容"); !ok {
			t.Fatalf("card field gone %s", when)
		}
	}
	// A selection box from empty canvas, then a pan: the card keeps its
	// widgets in every frame.
	tt.Press(900, 600)
	tt.Move(850, 560)
	tt.Frame()
	present("during a selection box")
	tt.Release(850, 560)
	tt.PressButton(2, 900, 600)
	tt.Move(880, 590)
	tt.Frame()
	present("during a pan")
	tt.ReleaseButton(2, 880, 590)
	tt.Frame()
	// Widgets scale with the board.
	before, _ := tt.Access("文字内容")
	b.Zoom = 0.5
	tt.Frame()
	after, ok := tt.Access("文字内容")
	if !ok || after.Bounds.W > before.Bounds.W*0.6 {
		t.Fatalf("field did not scale: %v → %v", before.Bounds, after.Bounds)
	}
}
