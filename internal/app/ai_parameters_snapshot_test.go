package app

import (
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIParametersSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect generation parameters")
	for _, tc := range []struct {
		model, name string
		role        whiteboard.AIRole
	}{{"ace15-turbo", "music", whiteboard.AIAudio}, {"qwen3-tts", "speech", whiteboard.AIAudio}, {"wan22-5b", "video", whiteboard.AIVideo}} {
		a, tt := newTestApp(t)
		tt.SetSize(1440, 1000)
		a.newWhiteboard("")
		tt.Frame()
		b := a.doc.board
		o := a.boardOptionsWith(nil, a.doc)
		id := b.AddAINode(tc.role, whiteboard.Point{600, 360}, o)
		e := b.Scene.ElementByID(id)
		n := whiteboard.AINodeOf(e)
		n.Connection = "comfyui-media"
		n.Model = tc.model
		n.Instruction = "夏日饮料广告，清爽、明亮、有活力"
		n.Params.BPM = 86
		n.Params.KeyScale = "A minor"
		n.Params.Seconds = 15
		if tc.role == whiteboard.AIVideo {
			n.Params.Seconds = 4
			n.Params.Size = "480x832"
		}
		_ = whiteboard.SetAINode(e, n)
		tt.Frame()
		if err := tt.Click("全部设置"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		for _, node := range tt.AccessAll() {
			if node.Role == ui.RoleGroup && node.Name == "节点属性" {
				tt.Scroll(node.Bounds.X+80, node.Bounds.Y+100, 0, 420)
				break
			}
		}
		tt.Frame()
		savePNG(t, filepath.Join(dir, tc.name+"-parameters.png"), tt)
	}
}

func TestAINodeErrorSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect full node errors")
	a, tt := newTestApp(t)
	tt.SetSize(1440, 1000)
	a.newWhiteboard("")
	tt.Frame()
	b := a.doc.board
	id := b.AddAINode(whiteboard.AICutout, whiteboard.Point{600, 360}, a.boardOptionsWith(nil, a.doc))
	e := b.Scene.ElementByID(id)
	n := whiteboard.AINodeOf(e)
	n.Connection = "comfyui-rmbg3"
	n.Model = "rmbg3-fp8"
	n.Error = "ComfyUI prompt failed (HTTP 400):\n" + strings.Repeat("required_input_missing: sampling_mode.temperature\n", 25)
	_ = whiteboard.SetAINode(e, n)
	tt.Frame()
	if err := tt.Click("查看完整错误"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	savePNG(t, filepath.Join(dir, "node-full-error.png"), tt)
}
