package editor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// The whiteboard's AI workflow is a node graph on the Excalidraw scene:
// nodes are rectangles with customData, edited in place on their cards;
// typed slots and links follow LiteGraph, ComfyUI's graph core (see
// board_ai_graph.go), and are drawn apart from the board's own lines. Any
// shape, note, picture or frame on the board can feed a node. The node bar
// and per-node result history follow Vibe-Workflow (MIT); no code of it is
// used.

// BoardAI is what the host lends the board to run AI nodes. A nil BoardAI
// hides the AI tools.
type BoardAI struct {
	// OpenImage opens an isolated viewer or annotation window for a node image.
	OpenImage func(nodeID string, edit bool)
	// Models are the models that can run a node of the role.
	Models func(role whiteboard.AIRole) []BoardAIModel
	// Default is the model a new node of the role starts with.
	Default func(role whiteboard.AIRole) BoardAIModel
	// Remember records the model last chosen for the role.
	Remember func(role whiteboard.AIRole, m BoardAIModel)
	// Prepare returns the runner for one run, on the main thread, where the
	// host may read its settings and credentials.
	Prepare func() (*BoardAIRunner, error)
	// ReadFile reads a result file by its path relative to the board. It
	// runs off the main thread.
	ReadFile func(rel string) ([]byte, error)
	// OpenFile shows a result file with the system's application.
	OpenFile func(rel string)
	// OpenVideo plays a result video on the board.
	OpenVideo func(rel string) (*ui.VideoPlayer, error)
	// ImportCanvas asks for a JSON Canvas file and hands its bytes back on
	// the main thread.
	ImportCanvas func(func([]byte, error))
}

// BoardAIModel is a model of a connection.
type BoardAIModel struct {
	Connection, ConnectionName, Model, Name string
}

func (m BoardAIModel) label() string {
	name := m.Name
	if name == "" {
		name = m.Model
	}
	if m.ConnectionName == "" {
		return name
	}
	return m.ConnectionName + " · " + name
}

// BoardAIRunner runs a node's model calls. Its functions run off the main
// thread and must not touch the board.
type BoardAIRunner struct {
	Text  func(ctx context.Context, job BoardAIJob, delta func(string)) (string, error)
	Media func(ctx context.Context, job BoardAIJob, progress func(string)) ([]BoardAIMedia, error)
	Note  func(name string) (string, error)
}

// BoardAIJob is one node's request: its settings, the composed prompt and
// its pictures.
type BoardAIJob struct {
	NodeID string
	Node   whiteboard.AINode
	Prompt string
	Images []BoardAIImage
	Mask   *BoardAIImage
}

// BoardAIImage is a picture input.
type BoardAIImage struct {
	Label string
	Data  []byte
	MIME  string
}

// BoardAIMedia is a generated file the host saved: its output entry and
// its bytes, which downstream nodes of the same run read.
type BoardAIMedia struct {
	Output whiteboard.AIOutput
	Data   []byte
}

// boardAIRun is a node's state during a run. It is never saved.
type boardAIRun struct {
	status, stream, progress string
	err                      error
}

type boardAINodeCache struct {
	version, nonce int64
	node           *whiteboard.AINode
	layout         *aiCardLayout
}

type boardAINodeBuffer struct {
	version, nonce int64
	text           string
}

type boardAIState struct {
	bar, barFocus bool
	query         string
	runs          map[string]*boardAIRun
	cancel        context.CancelFunc
	nodes         map[string]boardAINodeCache
	media         map[string]*ui.Bitmap
	mediaErr      map[string]error
	mediaPending  map[string]bool
	videos        map[string]*ui.VideoPlayer
	texts         map[string]boardAICardText
	// edit is the node the properties panel edits; before is the scene
	// before its first change, so a typing session is one undo step.
	edit, extra, extraErr string
	before                *whiteboard.Scene
	// drag is the link being dragged, link the selected link, and picker
	// the node menu a link dropped on empty canvas opens.
	drag   *aiDrag
	link   *aiLinkRef
	picker *aiPicker
	// panel shows the selected node's full settings beside the board.
	panel bool
	// linkedOut marks elements whose output is linked, for the frame
	// being painted.
	linkedOut     map[string]bool
	linkScene     *whiteboard.Scene
	linkSignature uint64
	links         []aiLink
	// buf holds the panel's text fields while a node is edited; bufs holds
	// per-node inline text buffers for direct canvas editing.
	buf              aiEditBuffers
	bufs             map[string]*boardAINodeBuffer
	inlineFocused    string
	focusInstruction bool
}

type boardAICardText struct {
	text   string
	width  float32
	font   ui.Font
	layout *ui.TextLayout
}

// aiRoleNames are the roles' names in the interface.
var aiRoleNames = map[whiteboard.AIRole]string{whiteboard.AIPrompt: "文字节点", whiteboard.AIImageInput: "图片输入", whiteboard.AIText: "文本生成", whiteboard.AIImage: "图像生成", whiteboard.AIVideo: "视频生成", whiteboard.AIConcat: "提示词拼接", whiteboard.AINote: "笔记引用"}

// aiTextPresets are the text node's common instructions.
var aiTextPresets = []struct{ label, instruction string }{
	{"总结", "用简洁的要点总结输入内容，保留关键事实。"},
	{"扩写", "在不改变原意的前提下扩写输入内容，补充细节和例子。"},
	{"润色", "润色输入内容，使表达清楚、通顺，保持原意和语气。"},
	{"翻译为英文", "把输入内容翻译成自然、准确的英文。"},
	{"翻译为中文", "把输入内容翻译成自然、准确的简体中文。"},
	{"结构化改写", "把输入内容改写为有标题和条目的结构化文本。"},
	{"生成绘图提示词", "根据输入内容写一段用于图像生成的英文提示词：主体、构图、风格、光线、色彩，只输出提示词。"},
}

func (b *Board) aiState() *boardAIState {
	s := &b.ai
	if s.runs == nil {
		s.runs = map[string]*boardAIRun{}
		s.nodes = map[string]boardAINodeCache{}
		s.media = map[string]*ui.Bitmap{}
		s.mediaErr = map[string]error{}
		s.mediaPending = map[string]bool{}
		s.videos = map[string]*ui.VideoPlayer{}
		s.texts = map[string]boardAICardText{}
		s.bufs = map[string]*boardAINodeBuffer{}
		s.linkedOut = map[string]bool{}
	}
	if s.bufs == nil {
		s.bufs = map[string]*boardAINodeBuffer{}
	}
	return s
}

// aiNode is an element's node, parsed once per element revision: the card
// is painted every frame.
func (b *Board) aiNode(e *whiteboard.Element) *whiteboard.AINode {
	if e == nil || (e.Kind != whiteboard.KindRectangle && e.Kind != whiteboard.KindImage) || len(e.CustomData) == 0 {
		return nil
	}
	s := b.aiState()
	version, nonce := e.ShapeRevision()
	if c, ok := s.nodes[e.ID]; ok && c.version == version && c.nonce == nonce {
		return c.node
	}
	n := whiteboard.AINodeOf(e)
	s.nodes[e.ID] = boardAINodeCache{version: version, nonce: nonce, node: n}
	return n
}

// AIRunning reports whether a run is in progress.
func (b *Board) AIRunning() bool { return b.ai.cancel != nil }

// StopAI cancels the run in progress.
func (b *Board) StopAI() {
	if b.ai.cancel != nil {
		b.ai.cancel()
	}
}

// updateAINode changes a node as one undoable edit.
func (b *Board) updateAINode(o BoardOptions, id string, fn func(*whiteboard.AINode)) {
	e := b.Scene.ElementByID(id)
	n := b.aiNode(e)
	if n == nil {
		return
	}
	next := *n
	next.Outputs = slices.Clone(n.Outputs)
	b.edit(o, func() {
		fn(&next)
		b.err = whiteboard.SetAINode(e, &next)
	})
}

// AddAINode adds a node of the role at a scene point (its center), with
// the role's remembered model, and selects it.
func (b *Board) AddAINode(role whiteboard.AIRole, at whiteboard.Point, o BoardOptions) string {
	if b.ReadOnly {
		return ""
	}
	var id string
	b.edit(o, func() {
		id = b.newAINode(role, at, o).ID
		b.selectIDs([]string{id})
	})
	return id
}

// newAINode appends a node of the role centered at a scene point, with the
// role's remembered model, inside the caller's edit.
func (b *Board) newAINode(role whiteboard.AIRole, at whiteboard.Point, o BoardOptions) *whiteboard.Element {
	w, h := whiteboard.AINodeSize(role)
	e := whiteboard.NewAINode(role, at[0]-w/2, at[1]-h/2)
	n := whiteboard.AINodeOf(e)
	if o.AI != nil && o.AI.Default != nil && (role == whiteboard.AIText || role == whiteboard.AIImage || role == whiteboard.AIVideo) {
		m := o.AI.Default(role)
		n.Connection, n.Model = m.Connection, m.Model
	}
	if role == whiteboard.AIImage || role == whiteboard.AIVideo {
		n.Params.Count = 1
	}
	e.H = max(e.H, aiMinHeight(n))
	_ = whiteboard.SetAINode(e, n)
	b.Scene.Elements = append(b.Scene.Elements, e)
	return e
}

// selectedImagesForNodes keeps conversion available only for editable,
// ordinary pictures. Reusing their IDs preserves existing reference links.
func (b *Board) selectedImagesForNodes() []*whiteboard.Element {
	if b.ReadOnly {
		return nil
	}
	var images []*whiteboard.Element
	for _, id := range b.SelectedIDs() {
		e := b.Scene.ElementByID(id)
		if e != nil && !e.Locked && e.Kind == whiteboard.KindImage && b.aiNode(e) == nil {
			images = append(images, e)
		}
	}
	return images
}

// convertImagesToNodes adds picture outputs in one undo step without copying,
// deleting or rewriting the images. Node cards keep their original centers.
func (b *Board) convertImagesToNodes(o BoardOptions) {
	images := b.selectedImagesForNodes()
	if len(images) == 0 {
		return
	}
	b.edit(o, func() {
		for _, e := range images {
			n := &whiteboard.AINode{Role: whiteboard.AIImageInput}
			if err := whiteboard.SetAINode(e, n); err != nil {
				b.err = err
				continue
			}
			cx, cy := e.X+e.W/2, e.Y+e.H/2
			w, h := whiteboard.AINodeSize(n.Role)
			e.W, e.H = max(w, e.W), max(h, e.H)
			e.X, e.Y, e.Angle = cx-e.W/2, cy-e.H/2, 0
		}
	})
}

// viewCenter is the scene point at the canvas's middle.
func (b *Board) viewCenter() whiteboard.Point {
	return b.scenePoint(b.lastRect.W/2, b.lastRect.H/2)
}

// aiSelectionInputs are the selected elements a new node reads: whole
// shapes and frames, without labels, lines or elements inside a selected
// frame.
func (b *Board) aiSelectionInputs() []*whiteboard.Element {
	ids := b.SelectedIDs()
	frames := map[string]bool{}
	for _, id := range ids {
		if e := b.Scene.ElementByID(id); e != nil && e.IsFrame() {
			frames[id] = true
		}
	}
	var out []*whiteboard.Element
	for _, id := range ids {
		e := b.Scene.ElementByID(id)
		if e == nil || e.IsLinear() || e.Kind == whiteboard.KindFreedraw || e.Kind == whiteboard.KindText && e.ContainerID != "" || frames[e.FrameID] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// AIFromSelection makes a node to the right of the selection, fed by each
// selected element, as "框选即组合" asks: select, then choose what to make.
func (b *Board) AIFromSelection(role whiteboard.AIRole, o BoardOptions) string {
	inputs := b.aiSelectionInputs()
	if len(inputs) == 0 || b.ReadOnly {
		return ""
	}
	var ids []string
	for _, e := range inputs {
		ids = append(ids, e.ID)
	}
	box := whiteboard.SelectionBounds(b.Scene, ids)
	w, h := whiteboard.AINodeSize(role)
	id := b.AddAINode(role, whiteboard.Point{box.X + box.W + 120 + w/2, box.Y + h/2}, o)
	node := b.Scene.ElementByID(id)
	if node == nil {
		return ""
	}
	b.edit(o, func() {
		for _, e := range inputs {
			if err := b.connectAI(e, node, true); err != nil {
				b.err = err
			}
		}
	})
	return id
}

// aiTemplates insert small ready-made flows.
var aiTemplates = []struct {
	key, label string
	steps      []struct {
		role        whiteboard.AIRole
		text, instr string
	}
}{
	{"brainstorm", "头脑风暴", []struct {
		role        whiteboard.AIRole
		text, instr string
	}{{"", "主题：", ""}, {whiteboard.AIText, "", "围绕主题提出 10 个方向不同的想法，每条一句，附一句理由。"}}},
	{"storyboard", "分镜拆解", []struct {
		role        whiteboard.AIRole
		text, instr string
	}{{"", "故事梗概：", ""}, {whiteboard.AIText, "", "把故事拆成 4 个镜头。每个镜头写一段英文画面描述，包括景别、主体、动作、光线，只输出第 1 个镜头。"}, {whiteboard.AIImage, "", "Cinematic storyboard frame, consistent characters."}}},
	{"redesign", "设计改稿", []struct {
		role        whiteboard.AIRole
		text, instr string
	}{{"frame", "", ""}, {whiteboard.AIImage, "", "按画框中的红圈、箭头和便签修改图片，保留其余部分，并去掉标注本身。"}}},
	{"prompt-image", "提示词增强生图", []struct {
		role        whiteboard.AIRole
		text, instr string
	}{{"", "想画的内容：", ""}, {whiteboard.AIText, "", aiTextPresets[6].instruction}, {whiteboard.AIImage, "", ""}}},
}

// insertAITemplate lays a template's steps left to right, each connected to
// the next, as one undo step.
func (b *Board) insertAITemplate(key string, o BoardOptions) {
	if b.ReadOnly {
		return
	}
	at := b.viewCenter()
	for _, t := range aiTemplates {
		if t.key != key {
			continue
		}
		b.History.Begin(b.Scene)
		x := at[0] - float64(len(t.steps))*200
		var prev *whiteboard.Element
		var last string
		for _, step := range t.steps {
			var e *whiteboard.Element
			switch step.role {
			case "":
				e = whiteboard.NewElement(whiteboard.KindNote, x, at[1]-100, 220, 160)
				text := b.newElement(whiteboard.KindText, whiteboard.Point{x, at[1] - 100}, 0, 0)
				text.Text, text.OriginalText = i18n.T(step.text), i18n.T(step.text)
				b.Scene.Elements = append(b.Scene.Elements, e, text)
				b.err = b.Scene.BindText(text.ID, e.ID, boardMeasure)
				x += 300
			case "frame":
				e = whiteboard.NewElement(whiteboard.KindFrame, x, at[1]-160, 340, 320)
				e.Name = i18n.T("标注区：放入底图并圈画")
				b.Scene.Elements = append([]*whiteboard.Element{e}, b.Scene.Elements...)
				x += 420
			default:
				w, _ := whiteboard.AINodeSize(step.role)
				e = b.newAINode(step.role, whiteboard.Point{x + w/2, at[1]}, o)
				n := whiteboard.AINodeOf(e)
				n.Instruction = i18n.T(step.instr)
				_ = whiteboard.SetAINode(e, n)
				x += w + 120
				last = e.ID
			}
			if prev != nil {
				if err := b.connectAI(prev, e, true); err != nil {
					b.err = err
				}
			}
			prev = e
		}
		if last != "" {
			b.selectIDs([]string{last})
		}
		if b.History.Commit(b.Scene) {
			b.changed(o)
		}
	}
}

// RunAI runs nodes: the given ones (every node when ids is empty), with
// their upstream nodes when upstream is set. Unchanged nodes reuse their
// result unless force is set. Results are written as undoable edits as
// each node finishes.
func (b *Board) RunAI(ids []string, upstream, force bool, o BoardOptions) {
	if b.ReadOnly || o.AI == nil || o.AI.Prepare == nil {
		return
	}
	if b.AIRunning() {
		b.err = errors.New(i18n.T("AI 节点正在运行，请等待完成或先停止。"))
		return
	}
	b.flushAIEdit()
	order, deps, err := b.Scene.AIPlan(ids, upstream)
	if err != nil {
		b.err = errors.New(i18n.T("AI 节点之间存在循环连线，无法确定运行顺序。"))
		return
	}
	if len(order) == 0 {
		return
	}
	runner, err := o.AI.Prepare()
	if err != nil {
		b.err = err
		return
	}
	s := b.aiState()
	for _, id := range order {
		s.runs[id] = &boardAIRun{status: "queued"}
	}
	snap := b.Scene.Clone()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	var mu sync.Mutex
	results := map[string]*boardAIResult{}
	// Without a dispatcher (tests, exports) the run is synchronous and its
	// callbacks are queued, then applied in order on this thread.
	var queue []func()
	post := o.Dispatch
	if post == nil {
		post = func(fn func()) { mu.Lock(); queue = append(queue, fn); mu.Unlock() }
	}
	run := func(ctx context.Context, id string) error {
		post(func() {
			if r := s.runs[id]; r != nil {
				r.status = "running"
			}
		})
		res, err := b.runAINode(ctx, snap, id, force, runner, o, func(id string) *boardAIResult {
			mu.Lock()
			defer mu.Unlock()
			return results[id]
		}, func(stream, progress string) {
			post(func() {
				if r := s.runs[id]; r != nil {
					r.stream += stream
					if progress != "" {
						r.progress = progress
					}
				}
			})
		})
		mu.Lock()
		results[id] = res
		mu.Unlock()
		return err
	}
	pending := len(order)
	done := func(id string, err error) {
		mu.Lock()
		res := results[id]
		mu.Unlock()
		post(func() {
			b.applyAIResult(id, res, err, o)
			pending--
			if pending == 0 {
				cancel()
				s.cancel = nil
			}
		})
	}
	flow := func() { whiteboard.RunAIFlow(ctx, order, deps, 4, run, done) }
	if o.Dispatch == nil {
		flow()
		for _, fn := range queue {
			fn()
		}
		return
	}
	go flow()
}

// boardAIResult is what a node produced in a run.
type boardAIResult struct {
	hash   string
	cached bool
	media  []BoardAIMedia
	text   string
}

// aiInputPicture renders a frame's region as a plain PNG: the picture
// with the hand-drawn marks a "标注改图" input sends.
func aiFramePNG(snap *whiteboard.Scene, frameID string, o BoardOptions) ([]byte, error) {
	frame := snap.ElementByID(frameID)
	s := whiteboard.NewScene()
	s.Files = snap.Files
	s.AppState = snap.AppState
	for _, e := range snap.Elements {
		if !e.IsDeleted && e.FrameID == frameID && !whiteboard.IsAINode(e) {
			c := e.Clone()
			c.FrameID = ""
			s.Elements = append(s.Elements, c)
		}
	}
	if len(s.Elements) == 0 {
		return nil, errors.New(i18n.T("画框中没有内容"))
	}
	b := NewBoard(s)
	o.Dispatch = nil
	b.prepareImages(o)
	box := whiteboard.Box{X: frame.X, Y: frame.Y, W: frame.W, H: frame.H}
	scale := math.Min(2, 2048/math.Max(box.W, box.H))
	w, h := max(1, int(math.Ceil(box.W*scale))), max(1, int(math.Ceil(box.H*scale)))
	b.Zoom, b.PanX, b.PanY = scale, -box.X*scale, -box.Y*scale
	img := ui.Render(func(c *ui.Context) {
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { b.paint(p, r, false, false) })
	}, w, h, 1)
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sniffImage(data []byte) (string, error) {
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return "", errors.New(i18n.T("输入的图片无法读取"))
	}
	return http.DetectContentType(data), nil
}

// runAINode gathers a node's inputs from the run's snapshot and earlier
// results, and runs it unless its inputs are unchanged. It runs off the
// main thread and reads nothing but its arguments.
func (b *Board) runAINode(ctx context.Context, snap *whiteboard.Scene, id string, force bool, runner *BoardAIRunner, o BoardOptions, upstream func(string) *boardAIResult, report func(stream, progress string)) (*boardAIResult, error) {
	e := snap.ElementByID(id)
	n := whiteboard.AINodeOf(e)
	if n == nil {
		return nil, errors.New(i18n.T("节点已不存在"))
	}
	if n.Role == whiteboard.AIImageInput {
		// A source validates its existing asset during an upstream run;
		// generation nodes read the same file directly through AIInputs.
		data, err := boardFileBytes(snap.Files[e.FileID], o)
		if err != nil {
			return nil, err
		}
		if _, err := sniffImage(data); err != nil {
			return nil, err
		}
		return &boardAIResult{}, nil
	}
	var texts, labels, hashes []string
	var images []BoardAIImage
	var mask *BoardAIImage
	annotated := false
	// A linked prompt slot replaces the node's instruction, as a widget
	// input does in ComfyUI.
	instruction, group := n.Instruction, ""
	addText := func(label, text string) {
		hashes = append(hashes, "t:"+group+"\x00"+label+"\x00"+text)
		if group == "prompt" {
			instruction = text
			return
		}
		texts = append(texts, text)
		labels = append(labels, label)
	}
	addImage := func(label string, data []byte, isMask bool) error {
		mime, err := sniffImage(data)
		if err != nil {
			return err
		}
		img := BoardAIImage{Label: label, Data: data, MIME: mime}
		hashes = append(hashes, "i:"+group+"\x00"+label+"\x00"+digest(data))
		if isMask {
			mask = &img
		} else {
			images = append(images, img)
		}
		return nil
	}
	for _, in := range snap.AIInputs(id) {
		group = in.Group
		switch in.Kind {
		case "text":
			addText("", in.Text)
		case "note":
			if runner.Note == nil {
				return nil, errors.New(i18n.T("无法读取笔记"))
			}
			text, err := runner.Note(in.Note)
			if err != nil {
				return nil, fmt.Errorf(i18n.T("无法读取笔记 [[%s]]：%v"), in.Note, err)
			}
			addText(in.Note, text)
		case "image":
			data, err := boardFileBytes(snap.Files[in.FileID], o)
			if err != nil {
				return nil, err
			}
			if err := addImage("", data, in.Mask); err != nil {
				return nil, err
			}
		case "frame":
			data, err := aiFramePNG(snap, in.From, o)
			if err != nil {
				return nil, err
			}
			if err := addImage("", data, false); err != nil {
				return nil, err
			}
			annotated = true
			if strings.TrimSpace(in.Text) != "" {
				addText(i18n.T("画框中的标注文字"), in.Text)
			}
		case "node":
			if r := upstream(in.From); r != nil {
				if r.text != "" {
					addText("", r.text)
				}
				for _, m := range r.media {
					if strings.HasPrefix(m.Output.MIME, "image/") {
						if err := addImage("", m.Data, in.Mask); err != nil {
							return nil, err
						}
					}
				}
				continue
			}
			up := whiteboard.AINodeOf(snap.ElementByID(in.From))
			if up != nil && up.Role == whiteboard.AIPrompt {
				if strings.TrimSpace(up.Instruction) == "" {
					return nil, errors.New(i18n.T("上游文字节点内容为空"))
				}
				addText("", up.Instruction)
				continue
			}
			out := up.Output()
			if out == nil {
				return nil, errors.New(i18n.T("上游节点还没有结果"))
			}
			switch out.Kind {
			case "text":
				addText("", out.Text)
			case "image":
				if o.AI.ReadFile == nil {
					return nil, errors.New(i18n.T("无法读取上游图片"))
				}
				data, err := o.AI.ReadFile(out.Path)
				if err != nil {
					return nil, err
				}
				if err := addImage("", data, in.Mask); err != nil {
					return nil, err
				}
			}
		}
	}
	res := &boardAIResult{hash: whiteboard.AIHash(n, hashes)}
	if !force && res.hash == n.InputHash && n.Output() != nil && n.Error == "" {
		res.cached = true
		if out := n.Output(); out.Kind == "text" {
			res.text = out.Text
		} else if out.Kind == "image" && o.AI.ReadFile != nil {
			if data, err := o.AI.ReadFile(out.Path); err == nil {
				res.media = []BoardAIMedia{{Output: *out, Data: data}}
			}
		}
		return res, nil
	}
	job := BoardAIJob{NodeID: id, Node: *n, Images: images, Mask: mask}
	job.Node.Instruction = instruction
	switch n.Role {
	case whiteboard.AIPrompt:
		res.text = n.Instruction
		return res, nil
	case whiteboard.AIConcat:
		res.text = whiteboard.ConcatTemplate(n.Params.Template, texts)
		return res, nil
	case whiteboard.AINote:
		if strings.TrimSpace(n.Note) == "" {
			return nil, errors.New(i18n.T("请填写要引用的笔记名称"))
		}
		text, err := runner.Note(whiteboard.WikiTarget("[[" + strings.Trim(n.Note, "[]") + "]]"))
		if err != nil {
			return nil, err
		}
		res.text = text
		return res, nil
	case whiteboard.AIText:
		if n.Model == "" {
			return nil, errors.New(i18n.T("请先为节点选择模型"))
		}
		var sb strings.Builder
		sb.WriteString(cmpOr(strings.TrimSpace(instruction), i18n.T("根据输入内容完成任务。")))
		for i, t := range texts {
			fmt.Fprintf(&sb, "\n\n### %s %d", i18n.T("输入"), i+1)
			if labels[i] != "" {
				sb.WriteString("（" + labels[i] + "）")
			}
			sb.WriteString("\n" + t)
		}
		job.Prompt = sb.String()
		text, err := runner.Text(ctx, job, func(d string) { report(d, "") })
		if err != nil {
			return nil, err
		}
		res.text = strings.TrimSpace(text)
		return res, nil
	}
	if n.Model == "" {
		return nil, errors.New(i18n.T("请先为节点选择模型"))
	}
	var parts []string
	if s := strings.TrimSpace(instruction); s != "" {
		parts = append(parts, s)
	}
	for i, t := range texts {
		if labels[i] != "" {
			t = labels[i] + ": " + t
		}
		parts = append(parts, t)
	}
	if annotated {
		parts = append(parts, i18n.T("参考图中的红圈、箭头和文字是修改标注：按标注修改对应区域，保留其余内容，结果中不要出现标注。"))
	}
	job.Prompt = strings.Join(parts, "\n")
	if strings.TrimSpace(job.Prompt) == "" && len(images) == 0 {
		return nil, errors.New(i18n.T("请填写提示词或连接输入"))
	}
	media, err := runner.Media(ctx, job, func(p string) { report("", p) })
	if err != nil {
		return nil, err
	}
	if len(media) == 0 {
		return nil, errors.New(i18n.T("服务没有返回结果"))
	}
	res.media = media
	return res, nil
}

func cmpOr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// applyAIResult writes a node's result to the scene on the main thread.
func (b *Board) applyAIResult(id string, res *boardAIResult, err error, o BoardOptions) {
	s := b.aiState()
	r := s.runs[id]
	if r == nil {
		r = &boardAIRun{}
		s.runs[id] = r
	}
	r.stream, r.progress = "", ""
	if b.Scene.ElementByID(id) == nil || b.aiNode(b.Scene.ElementByID(id)) == nil {
		delete(s.runs, id)
		return
	}
	switch {
	case errors.Is(err, context.Canceled):
		r.status, r.err = "stopped", nil
		return
	case errors.Is(err, whiteboard.ErrAIUpstream):
		r.status, r.err = "blocked", nil
		return
	case err != nil:
		r.status, r.err = "error", err
		b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Error = err.Error() })
		return
	case b.aiNode(b.Scene.ElementByID(id)).Role == whiteboard.AIImageInput:
		r.status, r.err = "done", nil
		if n := b.aiNode(b.Scene.ElementByID(id)); n.Error != "" {
			b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Error = "" })
		}
		return
	case res.cached:
		r.status = "cached"
		if n := b.aiNode(b.Scene.ElementByID(id)); n != nil && n.Error != "" {
			b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Error = "" })
		}
		return
	}
	r.status, r.err = "done", nil
	var outs []whiteboard.AIOutput
	if res.text != "" || len(res.media) == 0 {
		outs = append(outs, whiteboard.AIOutput{Kind: "text", Text: res.text})
	}
	for _, m := range res.media {
		outs = append(outs, m.Output)
	}
	e := b.Scene.ElementByID(id)
	n := *b.aiNode(e)
	n.Outputs = slices.Clone(n.Outputs)
	n.AddOutputs(outs)
	n.InputHash, n.Error = res.hash, ""
	b.edit(o, func() {
		b.err = whiteboard.SetAINode(e, &n)
		if n.Role == whiteboard.AIText && res.text != "" {
			b.writeAIText(e, &n, res.text)
		}
	})
}

// writeAIText also puts a text node's result where its settings say: over
// its first text input, or in a new sticky note beside it.
func (b *Board) writeAIText(node *whiteboard.Element, n *whiteboard.AINode, text string) {
	switch n.Params.Write {
	case "replace":
		for _, in := range b.Scene.AIInputs(node.ID) {
			if in.Kind != "text" {
				continue
			}
			from := b.Scene.ElementByID(in.From)
			target := from
			if from.Kind != whiteboard.KindText {
				target = b.Scene.ElementByID(from.BoundTextID())
			}
			if target != nil && !target.Locked {
				b.Scene.SetText(target.ID, text, boardMeasure)
				if target.ContainerID != "" {
					b.Scene.UpdateBindings(map[string]bool{target.ContainerID: true})
				}
			}
			return
		}
	case "note":
		b.placeAIText(node, text)
	}
}

// placeAIText adds a sticky note with the text beside the node.
func (b *Board) placeAIText(node *whiteboard.Element, text string) *whiteboard.Element {
	note := whiteboard.NewElement(whiteboard.KindNote, node.X+node.W+100, node.Y, 260, 220)
	label := b.newElement(whiteboard.KindText, whiteboard.Point{note.X, note.Y}, 0, 0)
	label.FontSize = 16
	label.Text, label.OriginalText = text, text
	b.Scene.Elements = append(b.Scene.Elements, note, label)
	b.err = b.Scene.BindText(label.ID, note.ID, boardMeasure)
	return note
}

// placeAIOutput puts a node's current result beside the node as an
// ordinary element: a picture embedded in the scene, so the board stays
// portable, or a sticky note of text.
func (b *Board) placeAIOutput(id string, o BoardOptions) {
	node := b.Scene.ElementByID(id)
	n := b.aiNode(node)
	out := n.Output()
	if out == nil || b.ReadOnly {
		return
	}
	if out.Kind == "text" {
		b.edit(o, func() { b.selectIDs([]string{b.placeAIText(node, out.Text).ID}) })
		return
	}
	if out.Kind != "image" || o.AI == nil || o.AI.ReadFile == nil {
		return
	}
	data, err := o.AI.ReadFile(out.Path)
	if err != nil {
		b.err = err
		return
	}
	at := whiteboard.Point{node.X + node.W + 100 + 150, node.Y + node.H/2}
	if err := b.InsertImage(data, at); err != nil {
		b.err = err
		return
	}
	if ids := b.SelectedIDs(); len(ids) == 1 {
		b.edit(o, func() {
			img := b.Scene.ElementByID(ids[0])
			img.X = node.X + node.W + 100
			whiteboard.Touch(img)
		})
	}
}

// flushAIEdit ends the properties panel's typing session as one undo step.
func (b *Board) flushAIEdit() {
	s := &b.ai
	if s.before != nil {
		b.History.Record(s.before, b.Scene)
		s.before = nil
	}
}

// aiNodeBuf returns the per-node inline text buffer, syncing from the node
// whenever the element's revision changed externally.
func (b *Board) aiNodeBuf(e *whiteboard.Element, n *whiteboard.AINode) *boardAINodeBuffer {
	s := b.aiState()
	buf := s.bufs[e.ID]
	current := n.Instruction
	switch n.Role {
	case whiteboard.AINote:
		current = n.Note
	case whiteboard.AIConcat:
		current = n.Params.Template
	}
	if buf == nil {
		buf = &boardAINodeBuffer{version: e.Version, nonce: e.VersionNonce, text: current}
		s.bufs[e.ID] = buf
	} else if buf.version != e.Version || buf.nonce != e.VersionNonce {
		buf.version, buf.nonce, buf.text = e.Version, e.VersionNonce, current
	}
	return buf
}

// setAINodeText updates the node's primary inline text field as part of an
// undoable typing session and keeps both inline and properties buffers synced.
func (b *Board) setAINodeText(o BoardOptions, id, text string) {
	b.editAINode(o, id, func(n *whiteboard.AINode) {
		switch n.Role {
		case whiteboard.AINote:
			n.Note = strings.Trim(strings.TrimSpace(text), "[]")
			if b.ai.edit == id {
				b.ai.buf.note = text
			}
		case whiteboard.AIConcat:
			n.Params.Template = text
			if b.ai.edit == id {
				b.ai.buf.template = text
			}
		default:
			n.Instruction = text
			if b.ai.edit == id {
				b.ai.buf.instruction = text
			}
		}
	})
	if e := b.Scene.ElementByID(id); e != nil {
		if buf := b.aiState().bufs[id]; buf != nil {
			buf.version, buf.nonce, buf.text = e.Version, e.VersionNonce, text
		}
	}
}

// editAINode changes the edited node without an undo step per keystroke;
// flushAIEdit records the session.
func (b *Board) editAINode(o BoardOptions, id string, fn func(*whiteboard.AINode)) {
	e := b.Scene.ElementByID(id)
	n := b.aiNode(e)
	if n == nil || b.ReadOnly {
		return
	}
	s := b.aiState()
	if s.before == nil {
		s.before = b.Scene.Clone()
	}
	next := *n
	next.Outputs = slices.Clone(n.Outputs)
	fn(&next)
	if err := whiteboard.SetAINode(e, &next); err != nil {
		b.err = err
		return
	}
	b.changed(o)
}

// ImportCanvas adds a JSON Canvas file's nodes and edges to the board.
func (b *Board) ImportCanvas(data []byte, o BoardOptions) error {
	s, err := whiteboard.ImportCanvas(data, boardMeasure)
	if err != nil {
		return err
	}
	var ids []string
	for _, e := range s.Elements {
		ids = append(ids, e.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	clip, err := s.Clipboard(ids)
	if err != nil {
		return err
	}
	b.edit(o, func() {
		var next []string
		next, err = b.Scene.Paste(clip, b.viewCenter())
		b.selectIDs(next)
	})
	return err
}

// Edit applies fn to the scene as one undoable change, for hosts that edit
// the board outside its view, as OneBot's whiteboard tools do.
func (b *Board) Edit(o BoardOptions, fn func()) { b.edit(o, fn) }

// BoardMeasure measures whiteboard text with the board's fonts.
func BoardMeasure(text string, font whiteboard.Font) float64 { return boardMeasure(text, font) }

// Connect links an element into an AI node as one undoable change: into
// the slot the label names ("prompt", "mask", or a slot's name or label),
// else as material into the first free slot of its type that is not the
// instruction's. Between other elements it draws a
// bound arrow, a drawing on the board.
func (b *Board) Connect(o BoardOptions, fromID, toID, label string) error {
	from, to := b.Scene.ElementByID(fromID), b.Scene.ElementByID(toID)
	if from == nil || to == nil || from.IsDeleted || to.IsDeleted || from.ID == to.ID || !from.IsBindable() || !to.IsBindable() {
		return errors.New("both ends must be existing shapes, notes, pictures, frames or nodes")
	}
	var err error
	if n := b.aiNode(to); n != nil {
		b.edit(o, func() {
			for _, sl := range whiteboard.AISlots(n) {
				named := strings.EqualFold(label, sl.Group) || label == sl.Name || label == sl.Label || label == i18n.T(sl.Label)
				if label != "" && named && whiteboard.AIValidConnection(b.Scene.AISourceType(from), sl.Type) && !b.Scene.AICreatesCycle(from.ID, to.ID) {
					b.setAILink(to.ID, sl.Name, from.ID)
					return
				}
			}
			err = b.connectAI(from, to, true)
		})
		return err
	}
	b.edit(o, func() {
		fb, tb := whiteboard.ElementBounds(from), whiteboard.ElementBounds(to)
		start, end := whiteboard.Point{fb.X + fb.W, fb.Y + fb.H/2}, whiteboard.Point{tb.X, tb.Y + tb.H/2}
		a := whiteboard.NewElement(whiteboard.KindArrow, start[0], start[1], math.Abs(end[0]-start[0]), math.Abs(end[1]-start[1]))
		a.Points = []whiteboard.Point{{0, 0}, whiteboard.Sub(end, start)}
		b.Scene.Elements = append(b.Scene.Elements, a)
		b.Scene.Bind(a.ID, false, from.ID, start)
		b.Scene.Bind(a.ID, true, to.ID, end)
		if label != "" {
			t := b.newElement(whiteboard.KindText, whiteboard.Point{a.X, a.Y}, 0, 0)
			t.Text, t.OriginalText = label, label
			b.Scene.Elements = append(b.Scene.Elements, t)
			err = b.Scene.BindText(t.ID, a.ID, boardMeasure)
		}
	})
	return err
}

// ViewCenter is the scene point at the middle of the canvas.
func (b *Board) ViewCenter() whiteboard.Point { return b.viewCenter() }

// LastError is the board's last reported error, or nil.
func (b *Board) LastError() error { return b.err }
