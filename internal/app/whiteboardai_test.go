package app

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

func TestWhiteboardAINodesUseOneBotConnections(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("path %s", r.URL.Path)
		}
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(buf.Bytes()))
	}))
	defer srv.Close()
	model := &scripted{steps: []func(agent.Request) agent.Message{reply("A cute red cat, watercolor")}}
	a, tt := qaApp(t, model)
	a.prefs.AI.Connections = append(a.prefs.AI.Connections, prefs.AIConnection{ID: "draw", Name: "Draw", Protocol: agent.Media, BaseURL: srv.URL + "/v1", Models: []agent.ModelInfo{{ID: "gpt-image-1", Enabled: true}}})
	tt.Frame()
	a.newWhiteboard("")
	tt.Frame()
	d := a.doc
	if d == nil || d.board == nil {
		t.Fatal("no whiteboard")
	}
	o := a.boardOptionsWith(nil, d)
	if o.AI == nil {
		t.Fatal("board has no AI")
	}
	if models := o.AI.Models(whiteboard.AIImage); len(models) != 1 || models[0].Connection != "draw" {
		t.Fatalf("image models %+v", models)
	}
	if models := o.AI.Models(whiteboard.AIText); len(models) != 1 || models[0].Model != "test-model" {
		t.Fatalf("text models %+v", models)
	}
	b := d.board
	text := b.AddAINode(whiteboard.AIText, whiteboard.Point{0, 0}, o)
	img := b.AddAINode(whiteboard.AIImage, whiteboard.Point{600, 0}, o)
	if err := b.Connect(o, text, img, ""); err != nil {
		t.Fatal(err)
	}
	b.RunAI(nil, false, false, o)
	if err := b.LastError(); err != nil {
		t.Fatal(err)
	}
	if out := whiteboard.AINodeOf(b.Scene.ElementByID(text)).Output(); out == nil || out.Text != "A cute red cat, watercolor" {
		t.Fatalf("text %+v", out)
	}
	out := whiteboard.AINodeOf(b.Scene.ElementByID(img)).Output()
	if out == nil || out.Kind != "image" || out.Path == "" {
		t.Fatalf("image %+v %v", out, whiteboard.AINodeOf(b.Scene.ElementByID(img)).Error)
	}
	full := filepath.Join(root(a), filepath.FromSlash(path.Join(path.Dir(d.rel), out.Path)))
	if data, err := os.ReadFile(full); err != nil || !bytes.Equal(data, buf.Bytes()) {
		t.Fatalf("asset %s: %v", full, err)
	}
	if a.prefs.AI.Whiteboard != nil {
		t.Fatal("nothing was chosen, yet a model was remembered")
	}
	o.AI.Remember(whiteboard.AIImage, o.AI.Models(whiteboard.AIImage)[0])
	if a.boardAIDefault(whiteboard.AIImage).Model != "gpt-image-1" {
		t.Fatal("remembered model not used")
	}
}

func TestOneBotEditsTheOpenWhiteboardWithTools(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		call("board_add_card", `{"text":"目标用户"}`),
		func(req agent.Request) agent.Message {
			last := req.Messages[len(req.Messages)-1].Text
			id := strings.TrimPrefix(last, "created ")
			return agent.Message{Role: agent.Assistant, Calls: []agent.ToolCall{{ID: "n", Name: "board_add_ai_node", Args: fmt.Sprintf(`{"role":"text","instruction":"列出痛点","inputs":[%q]}`, id)}}}
		},
		call("board_read", `{}`),
		func(req agent.Request) agent.Message {
			if !strings.Contains(req.Messages[len(req.Messages)-1].Text, "connection ") || !strings.Contains(req.System, "whiteboard") {
				t.Errorf("listing: %q", req.Messages[len(req.Messages)-1].Text)
			}
			return agent.Message{Role: agent.Assistant, Text: "已添加。"}
		},
	}}
	a, tt := qaApp(t, model)
	a.prefs.AI.Connections[0].Models[0].Capabilities.Tools = agent.Supported
	tt.Frame()
	a.newWhiteboard("")
	tt.Frame()
	d := a.doc
	a.ask(d.space.ID, "帮我搭一个用户痛点分析", nil)
	var nodes, notes int
	for _, e := range d.board.Scene.Elements {
		if n := whiteboard.AINodeOf(e); n != nil && n.Instruction == "列出痛点" && len(d.board.Scene.AIInputs(e.ID)) == 1 {
			nodes++
		}
		if e.Kind == whiteboard.KindNote && d.board.Scene.ElementText(e) == "目标用户" {
			notes++
		}
	}
	if nodes != 1 || notes != 1 {
		t.Fatalf("board not edited: %d %d", nodes, notes)
	}
	if !d.board.Undo() {
		t.Fatal("tool edits are not undoable")
	}
}

func TestOneBotGeneratesPicturesBesideTheOpenDocument(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(buf.Bytes()))
	}))
	defer srv.Close()
	var saved string
	model := &scripted{steps: []func(agent.Request) agent.Message{
		call("generate_image", `{"prompt":"a lighthouse"}`),
		func(req agent.Request) agent.Message {
			saved = req.Messages[len(req.Messages)-1].Text
			return agent.Message{Role: agent.Assistant, Text: "好了"}
		},
	}}
	a, tt := qaApp(t, model)
	a.prefs.AI.Connections[0].Models[0].Capabilities.Tools = agent.Supported
	a.prefs.AI.Connections = append(a.prefs.AI.Connections, prefs.AIConnection{ID: "draw", Name: "Draw", Protocol: agent.Media, BaseURL: srv.URL + "/v1", Models: []agent.ModelInfo{{ID: "gpt-image-1", Enabled: true}}})
	os.WriteFile(filepath.Join(root(a), "灯塔.md"), []byte("# 灯塔\n"), 0o644)
	a.refreshTree()
	a.open("灯塔.md")
	tt.Frame()
	a.ask(a.doc.space.ID, "画一座灯塔", nil)
	if !strings.HasPrefix(saved, "saved: ![](") {
		t.Fatalf("tool result %q", saved)
	}
	url := strings.TrimSuffix(strings.TrimPrefix(saved, "saved: ![]("), ")")
	if data, err := os.ReadFile(filepath.Join(root(a), filepath.FromSlash(unescapeImagePath(url)))); err != nil || !bytes.Equal(data, buf.Bytes()) {
		t.Fatalf("picture %s: %v", url, err)
	}
}
