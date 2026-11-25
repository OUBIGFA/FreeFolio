package app

import (
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

func TestWhiteboardSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect the native whiteboard")
	for _, dark := range []bool{false, true} {
		name := "whiteboard"
		if dark {
			name += "-dark"
		}
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.Frame()
			a.newWhiteboard("")
			tt.Frame()
			s, err := editor.MermaidBoard("flowchart LR\n A[客户端 Client] -->|请求| B{API 网关}\n B --> C[(数据库)]", whiteboard.Point{500, 300})
			if err != nil {
				t.Fatal(err)
			}
			a.doc.board = editor.NewBoard(s)
			tt.SetDark(dark)
			a.boardChanged(a.doc)
			tt.Frame()
			savePNG(t, filepath.Join(dir, name+".png"), tt)
		})
	}
}

func TestWhiteboardAISnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect the whiteboard AI nodes")
	for _, dark := range []bool{false, true} {
		name := "whiteboard-ai"
		if dark {
			name += "-dark"
		}
		t.Run(name, func(t *testing.T) {
			model := &scripted{steps: []func(agent.Request) agent.Message{reply("A cozy cafe at dusk, warm light, people reading by the window, watercolor style")}}
			a, tt := qaApp(t, model)
			a.prefs.AI.Connections = append(a.prefs.AI.Connections, prefs.AIConnection{ID: "draw", Name: "Draw", Protocol: agent.Media, BaseURL: "https://api.openai.com/v1", Models: []agent.ModelInfo{{ID: "gpt-image-1", Enabled: true}}})
			tt.Frame()
			a.newWhiteboard("")
			tt.Frame()
			d := a.doc
			b := d.board
			o := a.boardOptionsWith(nil, d)
			src := whiteboard.NewElement(whiteboard.KindNote, 60, 280, 220, 160)
			label := whiteboard.NewElement(whiteboard.KindText, 60, 280, 0, 0)
			label.Text, label.OriginalText = "黄昏的咖啡馆", "黄昏的咖啡馆"
			b.Edit(o, func() {
				b.Scene.Elements = append(b.Scene.Elements, src, label)
				_ = b.Scene.BindText(label.ID, src.ID, editor.BoardMeasure)
			})
			text := b.AddAINode(whiteboard.AIText, whiteboard.Point{500, 380}, o)
			img := b.AddAINode(whiteboard.AIImage, whiteboard.Point{900, 400}, o)
			_ = b.Connect(o, src.ID, text, "")
			_ = b.Connect(o, text, img, "")
			b.Edit(o, func() {
				n := whiteboard.AINodeOf(b.Scene.ElementByID(text))
				n.Instruction = "生成绘图提示词"
				_ = whiteboard.SetAINode(b.Scene.ElementByID(text), n)
			})
			b.RunAI([]string{text}, false, false, o)
			// The light shot shows the properties panel, the dark one the cards.
			b.Selected = map[string]bool{}
			if !dark {
				b.Selected[text] = true
			}
			tt.SetDark(dark)
			a.boardChanged(d)
			tt.Frame()
			tt.Frame()
			savePNG(t, filepath.Join(dir, name+".png"), tt)
		})
	}
}
