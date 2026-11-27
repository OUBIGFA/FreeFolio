package editor

import (
	"context"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestAudioLyricsUseSeparateInputAndOutput(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	lyricsID := b.AddAINode(whiteboard.AIPrompt, whiteboard.Point{}, o)
	b.updateAINode(o, lyricsID, func(n *whiteboard.AINode) { n.Instruction = "歌词内容" })
	id := b.AddAINode(whiteboard.AIAudio, whiteboard.Point{400, 0}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) {
		n.Connection = "comfyui-media"
		n.Model = "ace15-turbo"
		n.Instruction = "温柔的钢琴"
		n.Params.Lyrics = "旧歌词"
	})
	b.setAILink(id, "lyrics", lyricsID)
	var got BoardAIJob
	r := &BoardAIRunner{Media: func(_ context.Context, j BoardAIJob, _ func(string)) ([]BoardAIMedia, error) {
		got = j
		return []BoardAIMedia{{Output: whiteboard.AIOutput{Kind: "audio", Path: "a.flac", MIME: "audio/flac"}, Data: []byte("fLaC")}}, nil
	}}
	res, err := b.runAINode(context.Background(), b.Scene, id, true, r, o, func(string) *boardAIResult { return nil }, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "温柔的钢琴" || got.Node.Params.Lyrics != "歌词内容" || res.media[0].Output.Kind != "audio" {
		t.Fatalf("mixed lyrics and prompt: %+v", got)
	}
	n := whiteboard.AINodeOf(b.Scene.ElementByID(id))
	if n.Params.Lyrics != "旧歌词" || whiteboard.AIOutputType(n.Role) != whiteboard.AITypeAudio {
		t.Fatal("stored lyrics or output type changed")
	}
	n.Model = "qwen3-tts"
	_ = whiteboard.SetAINode(b.Scene.ElementByID(id), n)
	for _, in := range b.Scene.AIInputs(id) {
		if in.Group == "lyrics" {
			t.Fatal("speech scheduled unused lyrics input")
		}
	}
}

func TestLocalMediaControlsAndPlayback(t *testing.T) {
	for _, tc := range []struct {
		model   string
		role    whiteboard.AIRole
		control string
	}{{"ace15-turbo", whiteboard.AIAudio, "时长"}, {"qwen3-tts", whiteboard.AIAudio, "音色"}, {"wan22-5b", whiteboard.AIVideo, "时长"}} {
		t.Run(tc.model, func(t *testing.T) {
			b := NewBoard(nil)
			opened := ""
			o := BoardOptions{AI: &BoardAI{OpenFile: func(p string) { opened = p }}}
			tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 900)
			tt.Frame()
			id := b.AddAINode(tc.role, whiteboard.Point{450, 350}, o)
			b.updateAINode(o, id, func(n *whiteboard.AINode) {
				n.Connection = "comfyui-media"
				n.Model = tc.model
				if tc.role == whiteboard.AIAudio {
					n.Outputs = []whiteboard.AIOutput{{Kind: "audio", Path: "song.flac", MIME: "audio/flac"}}
				}
			})
			tt.Frame()
			if _, ok := tt.Access(tc.control); !ok {
				t.Fatalf("missing %s", tc.control)
			}
			if _, ok := tt.Access("分辨率"); ok {
				t.Fatal("local model offered unsupported size")
			}
			if tc.role == whiteboard.AIAudio {
				if err := tt.Click("播放音频"); err != nil {
					t.Fatal("missing playback")
				}
				tt.Frame()
				if opened != "song.flac" {
					t.Fatal("playback did not open result")
				}
			}
		})
	}
}

func TestSwitchingLocalMediaResetsOnlyIncompatibleSettings(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	id := b.AddAINode(whiteboard.AIAudio, whiteboard.Point{}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) {
		n.Connection = "comfyui-media"
		n.Model = "qwen3-tts"
		n.Params.Language = "Chinese"
		n.Params.Seconds = 120
		n.Params.Lyrics = "保留歌词"
	})
	b.chooseAIModel(o, id, whiteboard.AINodeOf(b.Scene.ElementByID(id)), BoardAIModel{Connection: "comfyui-media", Model: "stable-audio3"})
	n := whiteboard.AINodeOf(b.Scene.ElementByID(id))
	if n.Params.Seconds != 0 || n.Params.Language != "" || n.Params.Lyrics != "保留歌词" {
		t.Fatalf("incompatible state survived switch: %+v", n.Params)
	}
}

func TestLockedAudioSettingsAreDisabled(t *testing.T) {
	b := NewBoard(nil)
	n := &whiteboard.AINode{Role: whiteboard.AIAudio, Connection: "comfyui-media", Model: "qwen3-tts"}
	tt := ui.NewTester(func(c *ui.Context) { b.aiLocalMediaParams(c, BoardOptions{Theme: c.Theme()}, "locked", n, false, true) }, 400, 500)
	tt.Frame()
	for _, label := range []string{"音色", "语言"} {
		found := false
		for _, field := range tt.AccessAll() {
			if field.Name == label && field.Role == ui.RolePopUpButton {
				found = true
				if !field.Disabled {
					t.Fatalf("locked %s is editable", label)
				}
			}
		}
		if !found {
			t.Fatalf("missing %s control", label)
		}
	}
}
