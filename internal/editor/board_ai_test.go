package editor

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
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
		b.connectAI(src, b.Scene.ElementByID(gen))
		b.connectAI(b.Scene.ElementByID(gen), b.Scene.ElementByID(edit))
		b.connectAI(src, b.Scene.ElementByID(text))
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
	b.edit(o, func() { b.connectAI(b.Scene.ElementByID(text), b.Scene.ElementByID(down)) })
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
		b.connectAI(b.Scene.ElementByID(ref), b.Scene.ElementByID(cat))
		b.connectAI(frame, b.Scene.ElementByID(img))
		b.connectAI(b.Scene.ElementByID(cat), b.Scene.ElementByID(img))
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

func TestBoardAIViewPortDragAndPanel(t *testing.T) {
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
	if _, ok := tt.Find("运行节点"); !ok {
		t.Fatal("properties panel missing")
	}
	// Drag from the output port to empty canvas: the picker offers nodes.
	x, y, _, _ := b.canvasRect(e.X+e.W, e.Y+e.H/2, 0, 0)
	tt.Press(x, y)
	tt.Move(x+120, y+40)
	tt.Move(x+200, y+60)
	tt.Release(x+200, y+60)
	tt.Frame()
	if b.ai.pickerAt == nil {
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
	if image == nil || len(b.Scene.AIInputs(image.ID)) != 1 || b.Scene.AIInputs(image.ID)[0].From != e.ID {
		t.Fatal("picked node not connected")
	}
	for _, el := range b.Scene.Elements {
		if el.Kind == whiteboard.KindArrow && !el.IsDeleted && el.EndBinding == nil {
			t.Fatal("loose line left behind")
		}
	}
	// Typing an instruction is one undo step.
	b.selectIDs([]string{e.ID})
	tt.Frame()
	b.ai.focusInstruction = true
	tt.Frame()
	tt.Type("写一首诗")
	tt.Frame()
	if whiteboard.AINodeOf(b.Scene.ElementByID(e.ID)).Instruction != "写一首诗" {
		t.Fatal("instruction not stored")
	}
	b.Selected = map[string]bool{}
	tt.Frame()
	b.Undo()
	if whiteboard.AINodeOf(b.Scene.ElementByID(e.ID)).Instruction != "" {
		t.Fatal("typing session not undone in one step")
	}
}
