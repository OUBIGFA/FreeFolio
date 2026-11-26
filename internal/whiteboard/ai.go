package whiteboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// AI workflow nodes live on Excalidraw rectangles, or images for picture
// inputs so their file, crop and clipboard behavior stay intact. Their settings,
// input links and results are kept in customData under AINamespace, so
// other Excalidraw readers open the file unchanged and other namespaces
// survive. Links are not board elements: the board's arrows and lines stay
// drawings (see aigraph.go). Transient run state (queued, streaming text)
// is never stored here.
const AINamespace = "freefolioAI"

// AIRole is what a node does.
type AIRole string

const (
	// AIPrompt holds editable prompt or text directly on the board.
	AIPrompt AIRole = "prompt"
	// AIImageInput supplies an imported picture without generating a new one.
	AIImageInput AIRole = "image-input"
	// AIText writes text from its inputs: summaries, rewrites, prompts.
	AIText AIRole = "text"
	// AIImage generates a picture, or edits one when it has picture inputs.
	AIImage AIRole = "image"
	// AIVideo generates a video, from a first frame when given a picture.
	AIVideo AIRole = "video"
	// AIConcat joins its text inputs through a template, without a model.
	AIConcat AIRole = "concat"
	// AINote supplies a space document's text, named as [[document]].
	AINote AIRole = "note"
)

// AIRoles are the supported roles stored in a scene.
var AIRoles = []AIRole{AIPrompt, AIImageInput, AIText, AIImage, AIVideo, AIConcat, AINote}

// MaxAIOutputs bounds non-image history; image references remain available
// for selection and comparison across runs.
const MaxAIOutputs = 30

// AIParams are a node's generation options. Zero values are the service's
// defaults.
type AIParams struct {
	Size        string `json:"size,omitempty"`
	AspectRatio string `json:"aspectRatio,omitempty"`
	Quality     string `json:"quality,omitempty"`
	Count       int    `json:"count,omitempty"`
	Seconds     int    `json:"seconds,omitempty"`
	// Template joins a concat node's inputs; {1}, {2}… are inputs by order
	// and {all} is every input, one per line.
	Template string `json:"template,omitempty"`
	// Write is where a text node's result also goes: "" keeps it in the
	// node, "replace" rewrites the first text input, "note" adds a sticky
	// note beside the node.
	Write string `json:"write,omitempty"`
	// Extra holds service-specific fields, sent with the request as given.
	Extra map[string]any `json:"extra,omitempty"`
}

// AIOutput is one result. Media is a file beside the board, by a path
// relative to it, as images and videos are too large to embed.
type AIOutput struct {
	Kind    string `json:"kind"`
	Text    string `json:"text,omitempty"`
	Path    string `json:"path,omitempty"`
	MIME    string `json:"mime,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	Run     int    `json:"run,omitempty"`
	Created int64  `json:"created,omitempty"`
}

// AINode is a node's stored state.
type AINode struct {
	V           int        `json:"v"`
	Role        AIRole     `json:"role"`
	Title       string     `json:"title,omitempty"`
	Connection  string     `json:"connection,omitempty"`
	Model       string     `json:"model,omitempty"`
	Instruction string     `json:"instruction,omitempty"`
	Note        string     `json:"note,omitempty"`
	Params      AIParams   `json:"params,omitzero"`
	Links       []AILink   `json:"links,omitempty"`
	Outputs     []AIOutput `json:"outputs,omitempty"`
	Current     int        `json:"current,omitempty"`
	InputHash   string     `json:"inputHash,omitempty"`
	Error       string     `json:"error,omitempty"`
	Runs        int        `json:"runs,omitempty"`
}

// Output is the output being shown, or nil.
func (n *AINode) Output() *AIOutput {
	if n == nil || len(n.Outputs) == 0 {
		return nil
	}
	i := max(0, min(n.Current, len(n.Outputs)-1))
	return &n.Outputs[i]
}

// Kind is the data a node supplies downstream.
func (n *AINode) Kind() string {
	switch n.Role {
	case AIImage, AIImageInput:
		return "image"
	case AIVideo:
		return "video"
	}
	return "text"
}

// AddOutputs appends a run's results and shows the first of them.
func (n *AINode) AddOutputs(outs []AIOutput) {
	if len(outs) == 0 {
		return
	}
	n.Runs++
	now := time.Now().UnixMilli()
	for i := range outs {
		outs[i].Run = n.Runs
		if outs[i].Created == 0 {
			outs[i].Created = now
		}
	}
	n.Outputs = append(n.Outputs, outs...)
	if extra := len(n.Outputs) - MaxAIOutputs; n.Role != AIImage && extra > 0 {
		n.Outputs = n.Outputs[extra:]
	}
	n.Current = len(n.Outputs) - len(outs)
	if n.Current < 0 {
		n.Current = 0
	}
}

// RemoveOutput drops one history entry. Its file stays where it is.
func (n *AINode) RemoveOutput(i int) {
	if i < 0 || i >= len(n.Outputs) {
		return
	}
	n.Outputs = slices.Delete(n.Outputs, i, i+1)
	n.Current = max(0, min(n.Current, len(n.Outputs)-1))
	if len(n.Outputs) == 0 {
		n.InputHash = ""
	}
}

// AINodeOf reads an element's node, or nil for an ordinary element or a
// node this version does not understand.
func AINodeOf(e *Element) *AINode {
	if e == nil || e.IsDeleted || (e.Kind != KindRectangle && e.Kind != KindImage) || len(e.CustomData) == 0 {
		return nil
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(e.CustomData, &data) != nil || data[AINamespace] == nil {
		return nil
	}
	var n AINode
	if json.Unmarshal(data[AINamespace], &n) != nil || n.V != 1 || !slices.Contains(AIRoles, n.Role) {
		return nil
	}
	if (n.Role == AIImageInput) != (e.Kind == KindImage) {
		return nil
	}
	return &n
}

// IsAINode reports an element that is a node.
func IsAINode(e *Element) bool { return AINodeOf(e) != nil }

// SetAINode stores a node in the element, keeping other customData keys.
func SetAINode(e *Element, n *AINode) error {
	data := map[string]json.RawMessage{}
	if len(e.CustomData) > 0 {
		if err := json.Unmarshal(e.CustomData, &data); err != nil {
			return fmt.Errorf("whiteboard: element custom data is not an object: %w", err)
		}
	}
	n.V = 1
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	data[AINamespace] = raw
	out, err := json.Marshal(data)
	if err != nil {
		return err
	}
	e.CustomData = out
	Touch(e)
	return nil
}

// AINodeSize is a new node's card size by role. The editor grows a card
// that needs more room for its slots.
func AINodeSize(role AIRole) (float64, float64) {
	switch role {
	case AIPrompt:
		return 300, 220
	case AIImageInput:
		return 300, 300
	case AIImage:
		return 380, 520
	case AIVideo:
		return 380, 470
	case AIText:
		return 350, 430
	case AIConcat:
		return 310, 280
	}
	return 300, 210
}

// NewAINode makes a node card at a scene point (its top left).
func NewAINode(role AIRole, x, y float64) *Element {
	w, h := AINodeSize(role)
	e := NewElement(KindRectangle, x, y, w, h)
	if role == AIImageInput {
		e.Kind = KindImage
	}
	e.Roughness = 0
	e.StrokeWidth = 1
	e.StrokeColor = "#868e96"
	e.FillColor = "#ffffff"
	e.FillStyle = FillSolid
	n := &AINode{Role: role}
	if role == AIConcat {
		n.Params.Template = "{all}"
	}
	_ = SetAINode(e, n)
	return e
}

// AIInput is what one linked slot feeds into a node, in slot order.
type AIInput struct {
	From string
	// Slot is the input's slot name; Group is its slot group: "prompt"
	// replaces the node's instruction, "mask" is an edit's mask.
	Slot, Group string
	// Kind is "text", "image", "frame" (a region to picture), "node" (an
	// upstream node's current output) or "note" (a [[document]] to read).
	Kind   string
	Text   string
	FileID string
	Note   string
	Mask   bool
}

// ElementText is the text an element shows: its own or its bound label's.
func (s *Scene) ElementText(e *Element) string {
	if e == nil {
		return ""
	}
	if e.Kind == KindText {
		if e.OriginalText != "" {
			return e.OriginalText
		}
		return e.Text
	}
	if t := s.ElementByID(e.BoundTextID()); t != nil && !t.IsDeleted {
		return s.ElementText(t)
	}
	return ""
}

// WikiTarget is the document an element's link names, or "".
func WikiTarget(link string) string {
	link = strings.TrimSpace(link)
	if strings.HasPrefix(link, "[[") && strings.HasSuffix(link, "]]") {
		name := strings.TrimSuffix(strings.TrimPrefix(link, "[["), "]]")
		name, _, _ = strings.Cut(name, "|")
		return strings.TrimSpace(name)
	}
	return ""
}

// AIInputs are the inputs of a node, by its slots' order. Links to
// elements that are gone and text sources without text are left out.
func (s *Scene) AIInputs(id string) []AIInput {
	n := AINodeOf(s.ElementByID(id))
	if n == nil {
		return nil
	}
	var out []AIInput
	for _, sl := range AISlots(n) {
		from := s.AILinkSource(n, sl.Name)
		if from == nil || from.ID == id {
			continue
		}
		in := AIInput{From: from.ID, Slot: sl.Name, Group: sl.Group, Mask: sl.Group == "mask"}
		switch {
		case from.Kind == KindImage:
			// Imported picture nodes read the original asset even when only
			// the downstream node runs; they have no generated output history.
			in.Kind, in.FileID = "image", from.FileID
		case IsAINode(from):
			in.Kind = "node"
		case from.IsFrame():
			in.Kind, in.Text = "frame", s.frameText(from.ID)
		default:
			in.Kind, in.Text = "text", s.ElementText(from)
			if note := WikiTarget(from.Link); note != "" && strings.TrimSpace(in.Text) == "" {
				in.Kind, in.Note = "note", note
			}
		}
		if in.Kind == "text" && strings.TrimSpace(in.Text) == "" {
			continue
		}
		out = append(out, in)
	}
	return out
}

// frameText is the text inside a frame, top to bottom, as annotations for
// the picture of its region.
func (s *Scene) frameText(id string) string {
	type line struct {
		y, x float64
		text string
	}
	var lines []line
	for _, e := range s.Elements {
		if e.IsDeleted || e.FrameID != id || e.Kind == KindText && e.ContainerID != "" {
			continue
		}
		if t := strings.TrimSpace(s.ElementText(e)); t != "" {
			lines = append(lines, line{e.Y, e.X, t})
		}
	}
	slices.SortStableFunc(lines, func(a, b line) int {
		if a.y != b.y {
			if a.y < b.y {
				return -1
			}
			return 1
		}
		if a.x < b.x {
			return -1
		}
		if a.x > b.x {
			return 1
		}
		return 0
	})
	var out []string
	for _, l := range lines {
		out = append(out, l.text)
	}
	return strings.Join(out, "\n")
}

// AIDependencies are each node's upstream nodes.
func (s *Scene) AIDependencies() map[string][]string {
	deps := map[string][]string{}
	for _, e := range s.Elements {
		if !IsAINode(e) {
			continue
		}
		deps[e.ID] = nil
		n := AINodeOf(e)
		for _, sl := range AISlots(n) {
			if from := s.AILinkSource(n, sl.Name); IsAINode(from) && !slices.Contains(deps[e.ID], from.ID) {
				deps[e.ID] = append(deps[e.ID], from.ID)
			}
		}
	}
	return deps
}

// ErrAICycle is a set of nodes that feed each other.
var ErrAICycle = fmt.Errorf("whiteboard: AI nodes form a cycle")

// AIPlan orders the nodes to run: targets (all nodes when empty), with
// their upstream nodes when upstream is set, every node after the nodes it
// reads. A cycle among them is an error naming no order.
func (s *Scene) AIPlan(targets []string, upstream bool) ([]string, map[string][]string, error) {
	deps := s.AIDependencies()
	want := map[string]bool{}
	if len(targets) == 0 {
		for id := range deps {
			want[id] = true
		}
	}
	var visit func(string)
	visit = func(id string) {
		if want[id] {
			return
		}
		want[id] = true
		if upstream {
			for _, d := range deps[id] {
				visit(d)
			}
		}
	}
	for _, id := range targets {
		if _, ok := deps[id]; ok {
			visit(id)
		}
	}
	// Kahn's algorithm in scene order, so equal ranks keep a stable order.
	indegree := map[string]int{}
	for id := range want {
		for _, d := range deps[id] {
			if want[d] {
				indegree[id]++
			}
		}
	}
	var order []string
	done := map[string]bool{}
	for len(order) < len(want) {
		progressed := false
		for _, e := range s.Elements {
			id := e.ID
			if !want[id] || done[id] || indegree[id] > 0 {
				continue
			}
			done[id] = true
			order = append(order, id)
			progressed = true
			for other := range want {
				if slices.Contains(deps[other], id) {
					indegree[other]--
				}
			}
		}
		if !progressed {
			return nil, nil, ErrAICycle
		}
	}
	plan := map[string][]string{}
	for _, id := range order {
		for _, d := range deps[id] {
			if want[d] {
				plan[id] = append(plan[id], d)
			}
		}
	}
	return order, plan, nil
}

// AIHash identifies a run's inputs: the node's settings and each input's
// content digest. A node whose hash is unchanged and has an output is not
// run again unless forced.
func AIHash(n *AINode, inputs []string) string {
	h := sha256.New()
	cfg, _ := json.Marshal(struct {
		Role                           AIRole
		Connection, Model, Instruction string
		Note                           string
		Params                         AIParams
	}{n.Role, n.Connection, n.Model, n.Instruction, n.Note, n.Params})
	h.Write(cfg)
	for _, in := range inputs {
		h.Write([]byte{0})
		h.Write([]byte(in))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// ConcatTemplate fills a concat node's template with its text inputs.
func ConcatTemplate(template string, texts []string) string {
	if strings.TrimSpace(template) == "" {
		template = "{all}"
	}
	// One pass, so an input that contains "{2}" is not filled again.
	pairs := []string{"{all}", strings.Join(texts, "\n")}
	for i, t := range texts {
		pairs = append(pairs, fmt.Sprintf("{%d}", i+1), t)
	}
	out := strings.NewReplacer(pairs...).Replace(template)
	return out
}
