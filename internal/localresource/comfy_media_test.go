package localresource

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalMediaWorkflows(t *testing.T) {
	for _, id := range []string{"ace15-turbo", "stable-audio3", "qwen3-tts", "wan22-5b"} {
		t.Run(id, func(t *testing.T) {
			pack, ok := ComfyPack(id)
			if !ok {
				t.Fatal("missing pack")
			}
			m := &ComfyManager{}
			req := ComfyMediaRequest{Model: id, Prompt: "test", Lyrics: "hello", Speaker: "Serena", Language: "en"}
			g, err := m.mediaGraph(context.Background(), "", pack, req)
			if err != nil {
				t.Fatal(err)
			}
			inputs := func(key string) map[string]any { return g[key].(map[string]any)["inputs"].(map[string]any) }
			switch id {
			case "ace15-turbo":
				if inputs("text")["lyrics"] != "hello" || inputs("text")["duration"] != 30 || inputs("sample")["steps"] != 8 {
					t.Fatal("music conditioning differs")
				}
			case "stable-audio3":
				if inputs("latent")["seconds"] != 10 || inputs("sample")["sampler_name"] != "lcm" {
					t.Fatal("sound defaults differ")
				}
			case "qwen3-tts":
				if inputs("speech")["speaker"] != "Serena" || inputs("speech")["language"] != "English" || inputs("speech")["unload_models"] != true {
					t.Fatal("speech selection lost")
				}
			case "wan22-5b":
				if inputs("latent")["length"] != 97 || inputs("latent")["width"] != 832 || inputs("decode")["tile_size"] != 256 {
					t.Fatal("bounded video defaults differ")
				}
			}
			// Every graph reference must resolve before submitting a generation.
			for _, node := range g {
				for _, v := range node.(map[string]any)["inputs"].(map[string]any) {
					if ref, ok := v.([]any); ok && g[ref[0].(string)] == nil {
						t.Fatalf("dangling reference %v", ref)
					}
				}
			}
			if dir := os.Getenv("FREEFOLIO_MEDIA_GRAPH"); dir != "" {
				data, _ := json.MarshalIndent(g, "", "  ")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			req.Seconds = 999
			if id == "qwen3-tts" {
				req.Prompt = strings.Repeat("字", 1001)
			}
			if _, err := m.mediaGraph(context.Background(), "", pack, req); err == nil {
				t.Fatal("unbounded request accepted")
			}
		})
	}
}

func TestLocalMediaFirstFrameAndUnsupportedInput(t *testing.T) {
	var uploads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { uploads.Add(1); fmt.Fprint(w, `{"name":"first.png"}`) }))
	defer srv.Close()
	m := &ComfyManager{}
	pack, _ := ComfyPack("wan22-5b")
	req := ComfyMediaRequest{Prompt: "move", Image: comfyTestPNG(t, 32, 32, color.NRGBA{A: 255})}
	g, err := m.mediaGraph(context.Background(), srv.URL, pack, req)
	if err != nil {
		t.Fatal(err)
	}
	if uploads.Load() != 1 || g["image"] == nil || g["latent"].(map[string]any)["inputs"].(map[string]any)["start_image"] == nil {
		t.Fatal("first frame not connected")
	}
	pack, _ = ComfyPack("stable-audio3")
	if _, err = m.mediaGraph(context.Background(), srv.URL, pack, req); err == nil {
		t.Fatal("sound accepted image")
	}
	if uploads.Load() != 1 {
		t.Fatal("invalid request uploaded input")
	}
}

func TestComfyMediaHistoryHandlesAudioVideoAndMetadata(t *testing.T) {
	for _, key := range []string{"audio", "images", "videos", "gifs"} {
		t.Run(key, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/prompt":
					fmt.Fprint(w, `{"prompt_id":"m"}`)
				case "/history/m":
					fmt.Fprintf(w, `{"m":{"status":{"completed":true},"outputs":{"save":{"%s":[{"filename":"result.flac","subfolder":"audio"}],"animated":[false],"text":["done"]}}}}`, key)
				case "/view":
					if r.URL.Query().Get("filename") != "result.flac" || r.URL.Query().Get("subfolder") != "audio" {
						t.Error("lost output path")
					}
					fmt.Fprint(w, "fLaCtest")
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			files, err := (&ComfyManager{}).runFilesWorkflow(ctx, srv.URL, map[string]any{})
			if err != nil || len(files) != 1 || string(files[0].Data) != "fLaCtest" {
				t.Fatalf("files=%v err=%v", files, err)
			}
		})
	}
}

func TestLocalMediaMissingNodeDoesNotSubmit(t *testing.T) {
	var submitted atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/prompt" {
			submitted.Store(true)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	_, err := (&ComfyManager{}).GenerateLocalMedia(context.Background(), ComfyConfig{URL: srv.URL}, ComfyMediaRequest{Model: "qwen3-tts", Prompt: "你好"})
	if err == nil || submitted.Load() {
		t.Fatalf("missing plugin accepted: %v", err)
	}
}

func TestMediaResourceDownloadsHavePinnedIntegrity(t *testing.T) {
	for _, p := range mediaPacks {
		plan, err := PackDownload(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range plan.Files {
			if len(f.SHA) != 64 || f.Size <= 0 || !strings.HasPrefix(f.URL, "https://") || strings.Contains(f.URL, "/resolve/main/") {
				t.Fatalf("unverified media file: %+v", f)
			}
		}
	}
	p, _ := ComfyPack("qwen3-tts")
	plan, _ := PackDownload(p)
	if plan.Files[0] != voicePluginArchive || plan.TotalSize() != p.TotalSize()+voicePluginArchive.Size {
		t.Fatal("voice plugin dependency missing")
	}
}
