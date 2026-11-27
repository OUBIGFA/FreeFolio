package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestMediaResourcesTabsAndPresets(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabResources)
	tt.Frame()
	for _, tc := range []struct {
		label, visible, hidden string
		tab                    int
	}{{"音频模型", "ACE-Step 1.5 Turbo", "Wan 2.2 TI2V 5B", 4}, {"视频模型", "Wan 2.2 TI2V 5B", "ACE-Step 1.5 Turbo", 5}} {
		for _, n := range tt.AccessAll() {
			if n.Role == ui.RoleTab && n.Name == tc.label {
				tt.ClickAt(n.Bounds.X+n.Bounds.W/2, n.Bounds.Y+n.Bounds.H/2)
				break
			}
		}
		tt.Frame()
		if a.resources.tab != tc.tab || !tt.HasText(tc.visible) || tt.HasText(tc.hidden) {
			t.Fatalf("wrong media tab: %s", tc.label)
		}
	}
	if len(a.boardAIModels(whiteboard.AIAudio)) != 3 {
		t.Fatal("music, sound and speech models missing")
	}
	for _, m := range a.boardAIModels(whiteboard.AIAudio) {
		if m.Connection != "comfyui-media" || !m.NeedsDownload {
			t.Fatal("media source or download state incorrect")
		}
	}
}

func TestLocalAudioRunnerSavesAttachment(t *testing.T) {
	a, tt := newTestApp(t)
	a.newWhiteboard("")
	tt.Frame()
	root := t.TempDir()
	a.prefs.ResourceDir = root
	old := localresource.ComfyPacks
	localresource.ComfyPacks = append([]localresource.ResourcePack(nil), old...)
	t.Cleanup(func() { localresource.ComfyPacks = old })
	for i, p := range localresource.ComfyPacks {
		if p.ID != "ace15-turbo" {
			continue
		}
		localresource.ComfyPacks[i].Files = append([]localresource.PackFile(nil), p.Files...)
		for j, f := range p.Files {
			f.Size = 1
			f.SHA = ""
			localresource.ComfyPacks[i].Files[j] = f
			target := filepath.Join(root, f.RelPath)
			_ = os.MkdirAll(filepath.Dir(target), 0700)
			_ = os.WriteFile(target, []byte{1}, 0600)
		}
	}
	data := []byte("fLaCtest-audio")
	var submitted map[string]struct {
		Inputs map[string]any `json:"inputs"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/system_stats":
			fmt.Fprint(w, `{}`)
		case "/object_info":
			info := map[string]any{}
			for _, name := range []string{"UNETLoader", "ModelSamplingAuraFlow", "DualCLIPLoader", "VAELoader", "TextEncodeAceStepAudio1.5", "ConditioningZeroOut", "EmptyAceStep1.5LatentAudio", "KSampler", "VAEDecodeAudio", "SaveAudioAdvanced"} {
				info[name] = map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(info)
		case "/prompt":
			var body struct {
				Prompt json.RawMessage `json:"prompt"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.Unmarshal(body.Prompt, &submitted)
			fmt.Fprint(w, `{"prompt_id":"a"}`)
		case "/history/a":
			fmt.Fprint(w, `{"a":{"status":{"completed":true},"outputs":{"save":{"audio":[{"filename":"song.flac"}],"animated":[false]}}}}`)
		case "/view":
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a.prefs.ComfyUI.URL = srv.URL
	runner, err := a.boardAIRunner(a.doc)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runner.Media(context.Background(), editor.BoardAIJob{Node: whiteboard.AINode{Role: whiteboard.AIAudio, Connection: "comfyui-media", Model: "ace15-turbo", Params: whiteboard.AIParams{BPM: 86, KeyScale: "A minor", TimeSignature: "3", Seconds: 15}}, Prompt: "piano"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Output.Kind != "audio" || out[0].Output.MIME != "audio/flac" || !bytes.Equal(out[0].Data, data) {
		t.Fatalf("invalid audio output: %+v", out)
	}
	if out[0].Output.Path == "" {
		t.Fatal("audio attachment path missing")
	}
	if submitted["text"].Inputs["bpm"] != float64(86) || submitted["text"].Inputs["keyscale"] != "A minor" || submitted["text"].Inputs["timesignature"] != "3" || submitted["latent"].Inputs["seconds"] != float64(15) {
		t.Fatal("saved music parameters not forwarded to ComfyUI")
	}

}

func TestMediaResourcesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect media tabs")
	a, tt := newTestApp(t)
	tt.SetSize(1400, 1000)
	a.openSettings(tabResources)
	for _, tab := range []int{4, 5} {
		a.resources.tab = tab
		for range 3 {
			tt.Frame()
		}
		savePNG(t, filepath.Join(dir, fmt.Sprintf("media-tab-%d.png", tab)), tt)
	}
}
