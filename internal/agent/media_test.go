package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n0000IHDR")

func pngPart() ContentPart {
	return ContentPart{Type: "image", Data: "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)}
}

func noWait(context.Context, int) error { return nil }

func TestGeminiImageNativeOptionsAndReferences(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1beta/models/gemini-3-pro-image:generateContent" || r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("wrong Gemini request: %s", r.URL.Path)
		}
		body := decodeBody(t, r)
		config := object(body["generationConfig"])
		img := object(config["imageConfig"])
		if img["imageSize"] != "2K" || img["aspectRatio"] != "16:9" {
			t.Errorf("native options lost: %+v", config)
		}
		if got := texts(config["responseModalities"]); strings.Join(got, ",") != "TEXT,IMAGE" {
			t.Errorf("image output was not requested: %v", got)
		}
		parts := array(object(array(body["contents"])[0])["parts"])
		if len(parts) != 3 || object(object(parts[1])["inlineData"])["data"] != base64.StdEncoding.EncodeToString(pngBytes) || object(object(parts[2])["inlineData"])["mimeType"] != "image/png" {
			t.Errorf("reference images lost: %+v", parts)
		}
		fmt.Fprintf(w, `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"thought":true,"inlineData":{"data":%q,"mimeType":"image/png"}},{"text":"result"},{"inlineData":{"data":%q,"mimeType":"image/png"}}]}}]}`, base64.StdEncoding.EncodeToString(pngBytes), base64.StdEncoding.EncodeToString(pngBytes))
	}))
	defer srv.Close()
	s := Settings{Protocol: Gemini, BaseURL: srv.URL + "/v1beta", Model: "models/gemini-3-pro-image", APIKey: "test-key"}
	out, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "combine", Images: []ContentPart{pngPart(), pngPart()}, Size: "2K", AspectRatio: "16:9", Count: 2}, nil)
	if err != nil || calls != 2 || len(out.Assets) != 2 || out.Text != "result\nresult" {
		t.Fatalf("native generation: %+v %v, calls %d", out, err, calls)
	}
}

func TestGeminiImageReportsRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"candidates":[{"finishReason":"IMAGE_SAFETY","content":{"parts":[{"text":"Cannot generate this image."}]}}]}`)
	}))
	defer srv.Close()
	s := Settings{Protocol: Gemini, BaseURL: srv.URL + "/v1beta", Model: "gemini-3-pro-image"}
	_, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "test"}, nil)
	if !errors.Is(err, ErrIncomplete) || !strings.Contains(err.Error(), "IMAGE_SAFETY") {
		t.Fatalf("provider refusal was hidden: %v", err)
	}
}

func TestGeminiImageRejectsUnsupportedControls(t *testing.T) {
	s := Settings{Protocol: Gemini, BaseURL: "http://localhost:1/v1beta", Model: "gemini-3-pro-image"}
	mask := pngPart()
	for _, req := range []MediaRequest{
		{Kind: MediaImage, Prompt: "test", Size: "1024x1024"},
		{Kind: MediaImage, Prompt: "test", Mask: &mask},
	} {
		_, err := s.GenerateMedia(context.Background(), req, nil)
		if err == nil || !strings.Contains(err.Error(), "Gemini") {
			t.Fatalf("unsupported control was silently ignored: %v", err)
		}
	}
}

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestImageOnlyGatewayFormat(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(fmt.Sprintf("edit-%t", edit), func(t *testing.T) {
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("key sent to asset host")
				}
				w.Write(pngBytes)
			}))
			defer cdn.Close()
			var formats []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test" {
					t.Error("incorrect method or credentials")
				}
				var format string
				switch {
				case edit && r.URL.Path == "/v1/images/edits":
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						return
					}
					if len(r.MultipartForm.File["image"]) != 1 || r.FormValue("size") != "1024x1024" {
						t.Error("missing edit input")
					}
					format = r.FormValue("response_format")
				case !edit && r.URL.Path == "/v1/images/generations":
					body := decodeBody(t, r)
					if body["size"] != "1024x1024" || body["model"] != "gpt-image-custom" {
						t.Error("lost generation settings")
					}
					format, _ = body["response_format"].(string)
				default:
					http.Error(w, "站点用户 API 仅开放图片接口", http.StatusForbidden)
					return
				}
				formats = append(formats, format)
				if format != "url" {
					http.Error(w, `{"error":{"message":"站点用户 API 目前仅支持 response_format=url"}}`, http.StatusBadRequest)
					return
				}
				fmt.Fprintf(w, `{"data":[{"url":%q}]}`, cdn.URL+"/picture.png")
			}))
			defer srv.Close()
			settings := Settings{Protocol: Media, Model: "gpt-image-custom", APIKey: "test", BaseURL: srv.URL + "/v1"}
			req := MediaRequest{Kind: MediaImage, Prompt: "cat", Size: "1024x1024"}
			if edit {
				req.Images = []ContentPart{pngPart()}
			}
			out, err := settings.GenerateMedia(context.Background(), req, nil)
			if err != nil || len(out.Assets) != 1 || string(out.Assets[0].Data) != string(pngBytes) || strings.Join(formats, "|") != "|url" {
				t.Fatalf("formats=%q assets=%v err=%v", formats, out.Assets, err)
			}
			formats = nil
			req.Extra = map[string]any{"response_format": "url"}
			if _, err = settings.GenerateMedia(context.Background(), req, nil); err != nil || strings.Join(formats, "|") != "url" {
				t.Fatalf("explicit format: %q %v", formats, err)
			}
		})
	}
}

func TestImageFormatRefusalDoesNotReplayOtherRequests(t *testing.T) {
	for _, status := range []int{400, 403, 429, 500} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			http.Error(w, `{"error":{"message":"站点用户 API 仅开放图片接口，请使用 /v1/images/generations 或 /v1/images/edits"}}`, status)
		}))
		settings := Settings{Protocol: Media, Model: "alias", BaseURL: srv.URL}
		_, err := settings.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "cat"}, nil)
		srv.Close()
		var refusal *APIError
		if !errors.As(err, &refusal) || refusal.Status != status || calls != 1 {
			t.Fatalf("status=%d calls=%d err=%v", status, calls, err)
		}
	}
}

func TestOpenAIImagesGenerateAndDownload(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			if r.Header.Get("Authorization") != "Bearer k" {
				t.Fatalf("missing key: %v", r.Header)
			}
			body := decodeBody(t, r)
			if body["model"] != "dall-e-3" || body["prompt"] != "cat" || body["n"] != float64(2) || body["size"] != "1024x1024" || body["response_format"] != nil || body["style"] != "vivid" {
				t.Fatalf("body %+v", body)
			}
			fmt.Fprintf(w, `{"data":[{"b64_json":%q,"revised_prompt":"a cat"},{"url":%q}]}`, base64.StdEncoding.EncodeToString(pngBytes), srv.URL+"/file.png")
		case "/file.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngBytes)
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	s := Settings{BaseURL: srv.URL + "/v1", Model: "dall-e-3", APIKey: "k", Protocol: Media}
	out, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "cat", Count: 2, Size: "1024x1024", Extra: map[string]any{"style": "vivid"}}, nil)
	if err != nil || len(out.Assets) != 2 || out.Assets[0].RevisedPrompt != "a cat" || out.Assets[1].MIME != "image/png" || string(out.Assets[1].Data) != string(pngBytes) {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestOpenAIImageEditsSendSeveralImagesAndMask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		f := r.MultipartForm
		if f.Value["model"][0] != "gpt-image-1" || f.Value["prompt"][0] != "edit" || len(f.File["image[]"]) != 2 || len(f.File["mask"]) != 1 || f.Value["response_format"] != nil {
			t.Fatalf("form %+v %+v", f.Value, f.File)
		}
		if ct := f.File["image[]"][0].Header.Get("Content-Type"); ct != "image/png" {
			t.Fatalf("part type %q", ct)
		}
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(pngBytes))
	}))
	defer srv.Close()
	mask := pngPart()
	s := Settings{BaseURL: srv.URL + "/v1", Model: "gpt-image-1", APIKey: "k", Protocol: Media}
	out, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "edit", Images: []ContentPart{pngPart(), pngPart()}, Mask: &mask}, nil)
	if err != nil || len(out.Assets) != 1 || out.Assets[0].MIME != "image/png" {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestResponsesImageGenerationTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		tools := body["tools"].([]any)
		input := body["input"].([]any)[0].(map[string]any)["content"].([]any)
		if r.URL.Path != "/v1/responses" || tools[0].(map[string]any)["type"] != "image_generation" || len(input) != 2 {
			t.Fatalf("body %+v", body)
		}
		fmt.Fprintf(w, `{"output":[{"type":"image_generation_call","result":%q,"revised_prompt":"r"},{"type":"message","content":[{"type":"output_text","text":"done"}]}]}`, base64.StdEncoding.EncodeToString(pngBytes))
	}))
	defer srv.Close()
	s := Settings{BaseURL: srv.URL + "/v1", Model: "gpt-5", APIKey: "k", Protocol: Responses}
	out, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "p", Images: []ContentPart{pngPart()}}, nil)
	if err != nil || len(out.Assets) != 1 || out.Text != "done" || out.Assets[0].RevisedPrompt != "r" {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestGeminiImageAndImagen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "k" {
			t.Fatal("missing key")
		}
		body := decodeBody(t, r)
		data := base64.StdEncoding.EncodeToString(pngBytes)
		switch r.URL.Path {
		case "/v1beta/models/gemini-image:generateContent":
			config := body["generationConfig"].(map[string]any)
			if config["imageConfig"].(map[string]any)["aspectRatio"] != "16:9" {
				t.Fatalf("config %+v", config)
			}
			fmt.Fprintf(w, `{"candidates":[{"content":{"parts":[{"text":"here"},{"inlineData":{"mimeType":"image/png","data":%q}}]}}]}`, data)
		case "/v1beta/models/imagen-4:predict":
			fmt.Fprintf(w, `{"predictions":[{"bytesBase64Encoded":%q,"mimeType":"image/png"},{"bytesBase64Encoded":%q}]}`, data, data)
		default:
			t.Fatalf("path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	s := Settings{BaseURL: srv.URL + "/v1beta", Model: "gemini-image", APIKey: "k", Protocol: Gemini}
	out, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "p", AspectRatio: "16:9"}, nil)
	if err != nil || len(out.Assets) != 1 || out.Text != "here" {
		t.Fatalf("%+v %v", out, err)
	}
	s.Model = "imagen-4"
	out, err = s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "p", Count: 2}, nil)
	if err != nil || len(out.Assets) != 2 {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestOpenAIVideoPollsAndDownloads(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/videos":
			if err := r.ParseMultipartForm(1 << 20); err != nil || r.FormValue("seconds") != "4" || len(r.MultipartForm.File["input_reference"]) != 1 {
				t.Fatalf("form %v %+v", err, r.MultipartForm)
			}
			fmt.Fprint(w, `{"id":"v1","status":"queued"}`)
		case r.URL.Path == "/v1/videos/v1":
			polls++
			status := "in_progress"
			if polls > 1 {
				status = "completed"
			}
			fmt.Fprintf(w, `{"id":"v1","status":%q,"progress":50}`, status)
		case r.URL.Path == "/v1/videos/v1/content":
			w.Header().Set("Content-Type", "video/mp4")
			w.Write([]byte("mp4data"))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	var seen []string
	s := Settings{BaseURL: srv.URL + "/v1", Model: "sora-2", APIKey: "k", Protocol: Media, Wait: noWait}
	out, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaVideo, Prompt: "p", Seconds: 4, Images: []ContentPart{pngPart()}}, func(p MediaProgress) { seen = append(seen, p.Status) })
	if err != nil || len(out.Assets) != 1 || out.Assets[0].MIME != "video/mp4" || string(out.Assets[0].Data) != "mp4data" || len(seen) != 3 {
		t.Fatalf("%+v %v %v", out, err, seen)
	}
}

func TestOpenAIVideoFailureIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"v1","status":"failed","error":{"message":"moderation"}}`)
	}))
	defer srv.Close()
	s := Settings{BaseURL: srv.URL + "/v1", Model: "sora-2", APIKey: "k", Protocol: Media, Wait: noWait}
	_, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaVideo, Prompt: "p"}, nil)
	if err == nil || !strings.Contains(err.Error(), "moderation") {
		t.Fatal(err)
	}
}

func TestVeoOperationDownloadsWithKeyOnlyOnServiceHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "" {
			t.Fatal("key forwarded to another host")
		}
		w.Write([]byte("video"))
	}))
	defer other.Close()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1beta/models/veo-3:predictLongRunning":
			fmt.Fprint(w, `{"name":"models/veo-3/operations/op1"}`)
		case "/v1beta/models/veo-3/operations/op1":
			fmt.Fprintf(w, `{"done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":%q}}]}}}`, srv.URL+"/redirect")
		case "/redirect":
			if r.Header.Get("x-goog-api-key") != "k" {
				t.Fatal("service host lost its key")
			}
			http.Redirect(w, r, other.URL+"/file.mp4", http.StatusFound)
		default:
			t.Fatalf("path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	s := Settings{BaseURL: srv.URL + "/v1beta", Model: "veo-3", APIKey: "k", Protocol: Gemini, Wait: noWait}
	out, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaVideo, Prompt: "p"}, nil)
	if err != nil || string(out.Assets[0].Data) != "video" || out.Assets[0].MIME != "video/mp4" {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestMediaRefusalsAndKeyFailover(t *testing.T) {
	if _, err := (Settings{Protocol: ChatGPT, BaseURL: "https://api.openai.com/v1", Model: "m"}).GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "p"}, nil); !errors.Is(err, ErrMediaUnsupported) {
		t.Fatal(err)
	}
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer bad" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"message":"bad key bad"}}`)
			return
		}
		io.Copy(io.Discard, r.Body)
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(pngBytes))
	}))
	defer srv.Close()
	s := Settings{BaseURL: srv.URL + "/v1", Model: "m", APIKey: "bad", APIKeys: []string{"bad", "good"}, KeyStrategy: KeyFailover, Protocol: Media}
	if _, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "p"}, nil); err != nil || len(keys) != 2 {
		t.Fatalf("%v %v", err, keys)
	}
	s.APIKeys = nil
	_, err := s.GenerateMedia(context.Background(), MediaRequest{Kind: MediaImage, Prompt: "p"}, nil)
	if err == nil || strings.Contains(err.Error(), "bad key bad") && !strings.Contains(err.Error(), "[redacted]") {
		t.Fatal(err)
	}
	if _, err := (Settings{BaseURL: srv.URL + "/v1", Model: "m", APIKey: "k", Protocol: Media}).NewProvider().Complete(context.Background(), Request{}, nil); err == nil {
		t.Fatal("media connection answered a conversation")
	}
}

func TestSupportsMedia(t *testing.T) {
	for _, tc := range []struct {
		protocol, kind string
		caps           Capabilities
		want           bool
	}{
		{Media, MediaImage, Capabilities{}, true},
		{Media, MediaVideo, Capabilities{Output: []string{"image"}}, false},
		{Responses, MediaImage, Capabilities{}, true},
		{Responses, MediaVideo, Capabilities{}, false},
		{Chat, MediaImage, Capabilities{}, false},
		{Chat, MediaImage, Capabilities{Output: []string{"text", "image"}}, true},
		{Gemini, MediaVideo, Capabilities{Output: []string{"video"}}, true},
		{ChatGPT, MediaImage, Capabilities{Output: []string{"image"}}, false},
		{Anthropic, MediaImage, Capabilities{Output: []string{"image"}}, false},
	} {
		if got := SupportsMedia(tc.protocol, "", tc.caps, tc.kind); got != tc.want {
			t.Errorf("%s %s %+v = %v", tc.protocol, tc.kind, tc.caps, got)
		}
	}
}
