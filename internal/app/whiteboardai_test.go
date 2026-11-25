package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

func TestGeminiCatalogFeedsWhiteboardAndOneBotImages(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			fmt.Fprint(w, `{"models":[{"name":"models/gemini-3-pro-image","displayName":"Nano Banana Pro","supportedGenerationMethods":["generateContent"]}]}`)
			return
		}
		requests++
		if r.URL.Path != "/v1beta/models/gemini-3-pro-image:generateContent" {
			t.Errorf("wrong route %s", r.URL.Path)
		}
		var body struct {
			Config struct {
				Image map[string]string `json:"imageConfig"`
			} `json:"generationConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Config.Image["imageSize"] != "2K" || body.Config.Image["aspectRatio"] != "1:1" {
			t.Errorf("native image settings not sent: %+v %v", body, err)
		}
		fmt.Fprintf(w, `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"inlineData":{"mimeType":"image/png","data":%q}}]}}]}`, base64.StdEncoding.EncodeToString(buf.Bytes()))
	}))
	defer srv.Close()
	s := agent.Settings{Protocol: agent.Gemini, BaseURL: srv.URL + "/v1beta"}
	models, err := s.DiscoverModels(context.Background())
	if err != nil || len(models) != 1 {
		t.Fatalf("discover: %v %v", models, err)
	}
	catalog, err := agent.ParseCatalog([]byte(`{"google":{"models":{"gemini-3-pro-image":{"type":"image","modalities":{"input":["text","image"],"output":["text","image"]}}}}}`), "google")
	if err != nil || len(catalog) != 1 {
		t.Fatalf("catalog: %v %v", catalog, err)
	}
	models[0] = agent.MergeModelInfo(models[0], catalog[0])
	models[0].Enabled = true
	a, tt := newTestApp(t)
	a.prefs.AI.Connections = []prefs.AIConnection{{ID: "gemini", Name: "Gemini", Protocol: agent.Gemini, Vendor: "google", BaseURL: s.BaseURL, Models: models}}
	a.prefs.AI.Active = "gemini"
	a.newWhiteboard("")
	tt.Frame()
	d := a.doc
	o := a.boardOptionsWith(nil, d)
	choices := o.AI.Models(whiteboard.AIImage)
	if len(choices) != 1 || strings.Join(choices[0].ImageSizes, ",") != "1K,2K,4K" {
		t.Fatalf("image choices: %+v", choices)
	}
	id := d.board.AddAINode(whiteboard.AIImage, whiteboard.Point{0, 0}, o)
	el := d.board.Scene.ElementByID(id)
	n := whiteboard.AINodeOf(el)
	n.Instruction = "a cat"
	n.Params.Size, n.Params.AspectRatio = "2K", "1:1"
	if err := whiteboard.SetAINode(el, n); err != nil {
		t.Fatal(err)
	}
	d.board.RunAI([]string{id}, false, false, o)
	out := whiteboard.AINodeOf(el).Output()
	if out == nil || out.Kind != "image" {
		t.Fatalf("board generation: %+v %v", out, d.board.LastError())
	}
	if data, err := o.AI.ReadFile(out.Path); err != nil || !bytes.Equal(data, buf.Bytes()) {
		t.Fatalf("board attachment: %v", err)
	}
	tool := a.chatImageTool(d)
	if tool == nil {
		t.Fatal("Gemini image tool missing")
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"prompt":"cat","size":"2K","aspect_ratio":"1:1"}`))
	if err != nil || !strings.HasPrefix(result, "saved: ![](") || requests != 2 {
		t.Fatalf("OneBot image: %s %v (%d requests)", result, err, requests)
	}
}

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
			if !strings.Contains(req.Messages[len(req.Messages)-1].Text, "slot text.0") || !strings.Contains(req.System, "whiteboard") {
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
