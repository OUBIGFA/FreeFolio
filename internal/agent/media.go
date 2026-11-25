package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Media is a connection to a service that only generates pictures and
// videos through OpenAI's Images and Videos APIs, as OpenAI itself and the
// gateways that copy its routes (new-api, one-api, LiteLLM, xAI…) offer
// them. It is never used for conversation.
const Media = "openai-media"

// Kinds of generated media.
const (
	MediaImage = "image"
	MediaVideo = "video"
)

// ErrMediaUnsupported is a connection or model that cannot make the kind of
// media asked for. The official ChatGPT plan channel offers no image or video
// generation, so it is refused rather than tried.
var ErrMediaUnsupported = errors.New("this connection cannot generate this kind of media")

// MediaRequest is one generation: a prompt, optional source images (for an
// edit, a reference or a video's first frame) and an optional edit mask.
type MediaRequest struct {
	Kind   string
	Prompt string
	Images []ContentPart
	Mask   *ContentPart
	// Size is OpenAI's "WxH"/"auto" or Gemini's "1K"/"2K"/"4K";
	// AspectRatio ("16:9") is what Gemini and Veo take. Empty uses defaults.
	Size, AspectRatio string
	Count             int
	Quality           string
	Seconds           int
	// Extra is sent as additional body fields, for a service's own options.
	Extra map[string]any
}

// MediaAsset is one generated file.
type MediaAsset struct {
	Data          []byte
	MIME          string
	RevisedPrompt string
}

// MediaResult is a generation's files and any text the model added.
type MediaResult struct {
	Assets []MediaAsset
	Text   string
}

// MediaProgress reports a long generation: a video's status and percent.
type MediaProgress struct {
	Status  string
	Percent int
}

// Limits of what a generation may download. Videos are a few tens of MB.
const (
	maxMediaJSON  = 160 << 20
	maxMediaAsset = 256 << 20
	maxMediaPolls = 720
)

// SupportsMedia reports whether a connection's model can be offered for the
// kind of media. Unknown output metadata only counts for the dedicated media
// protocol, whose models exist for this; elsewhere it must be declared.
func SupportsMedia(protocol, vendor string, c Capabilities, kind string) bool {
	switch protocol {
	case ChatGPT, Anthropic, Ollama:
		return false
	case Media:
		return len(c.Output) == 0 || slices.Contains(c.Output, kind)
	case Responses:
		// The image_generation tool belongs to the conversation model.
		return kind == MediaImage && c.Tools != Unsupported || slices.Contains(c.Output, kind)
	}
	return slices.Contains(c.Output, kind)
}

// GenerateMedia makes pictures or a video with the settings' service. With
// several API keys it moves to the next key on the same refusals a
// conversation does.
func (s Settings) GenerateMedia(ctx context.Context, req MediaRequest, progress func(MediaProgress)) (MediaResult, error) {
	if progress == nil {
		progress = func(MediaProgress) {}
	}
	if req.Kind != MediaImage && req.Kind != MediaVideo {
		return MediaResult{}, fmt.Errorf("unknown media kind %q", req.Kind)
	}
	if strings.TrimSpace(req.Prompt) == "" && len(req.Images) == 0 {
		return MediaResult{}, errors.New("a prompt is required")
	}
	if s.Protocol == ChatGPT || s.Protocol == Anthropic || s.Protocol == Ollama {
		return MediaResult{}, ErrMediaUnsupported
	}
	if err := s.Check(); err != nil {
		return MediaResult{}, err
	}
	req.Count = max(1, min(req.Count, 4))
	keys := []string{s.APIKey}
	if len(s.APIKeys) > 1 {
		keys = s.keyOrder()
	}
	var out MediaResult
	var err error
	for i, key := range keys {
		one := s
		one.APIKey, one.APIKeys = key, nil
		out, err = one.generate(ctx, req, progress)
		if err == nil || ctx.Err() != nil || i == len(keys)-1 || !keyRefused(err) {
			break
		}
	}
	if err != nil && ctx.Err() != nil {
		return MediaResult{}, ctx.Err()
	}
	return out, err
}

func (s Settings) generate(ctx context.Context, req MediaRequest, progress func(MediaProgress)) (MediaResult, error) {
	switch {
	case req.Kind == MediaVideo && s.Protocol == Gemini:
		return s.veo(ctx, req, progress)
	case req.Kind == MediaVideo:
		return s.openAIVideo(ctx, req, progress)
	case s.Protocol == Gemini && strings.HasPrefix(strings.TrimPrefix(s.Model, "models/"), "imagen"):
		return s.imagen(ctx, req)
	case s.Protocol == Gemini:
		return s.repeat(ctx, req, s.geminiImage)
	case s.Protocol == Responses:
		return s.repeat(ctx, req, s.responsesImage)
	case s.Protocol == Chat && s.Vendor == "openrouter":
		return s.repeat(ctx, req, s.chatImage)
	}
	return s.openAIImages(ctx, req)
}

// repeat runs a one-picture-per-call route once for each picture asked for.
func (s Settings) repeat(ctx context.Context, req MediaRequest, one func(context.Context, MediaRequest) (MediaResult, error)) (MediaResult, error) {
	var out MediaResult
	for range req.Count {
		r, err := one(ctx, req)
		if err != nil {
			return out, err
		}
		out.Assets = append(out.Assets, r.Assets...)
		out.Text = strings.TrimSpace(out.Text + "\n" + r.Text)
	}
	if len(out.Assets) == 0 {
		return out, errors.New("the service returned no image")
	}
	return out, nil
}

func (s Settings) base() string { return strings.TrimRight(strings.TrimSpace(s.BaseURL), "/") }

// send posts a body of the given type with the protocol's credentials and
// returns the successful response, or the service's refusal, redacted.
func (s Settings) send(ctx context.Context, method, endpoint, contentType string, body []byte) (*http.Response, error) {
	hr, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		hr.Header.Set("Content-Type", contentType)
	}
	s.authorize(hr.Header)
	client := *s.httpClient()
	client.Timeout = 10 * time.Minute
	client.CheckRedirect = sameHostRedirect
	res, err := client.Do(hr)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("cannot reach the model service: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		err := apiError(res, s.APIKey)
		if api, ok := err.(*APIError); ok {
			api.Protocol = s.Protocol
		}
		return nil, err
	}
	return res, nil
}

func (s Settings) authorize(h http.Header) {
	if s.Protocol == Gemini {
		h.Set("x-goog-api-key", s.APIKey)
	} else if s.APIKey != "" {
		h.Set("Authorization", "Bearer "+s.APIKey)
	}
}

// sameHostRedirect keeps credentials on the service's host: a redirect to
// another host (a storage bucket, a CDN) is followed without them.
func sameHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if req.URL.Host != via[0].URL.Host {
		req.Header.Del("Authorization")
		req.Header.Del("x-goog-api-key")
	}
	return nil
}

func (s Settings) postJSON(ctx context.Context, endpoint string, body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	res, err := s.send(ctx, http.MethodPost, endpoint, "application/json", data)
	if err != nil {
		return nil, err
	}
	return readMediaJSON(res)
}

func (s Settings) getJSON(ctx context.Context, endpoint string) (map[string]any, error) {
	res, err := s.send(ctx, http.MethodGet, endpoint, "", nil)
	if err != nil {
		return nil, err
	}
	return readMediaJSON(res)
}

func readMediaJSON(res *http.Response) (map[string]any, error) {
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxMediaJSON+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStreamCut, err)
	}
	if len(data) > maxMediaJSON {
		return nil, errors.New("the generated media is too large")
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("the service's reply is not in the expected format: %w", err)
	}
	if e := object(out["error"]); e != nil && str(e["message"]) != "" {
		return nil, &APIError{Status: 500, Message: str(e["message"]), Code: str(e["code"]), Shape: "error"}
	}
	return out, nil
}

// asset decodes base64 media, its type from the data when not given.
func asset(data, mime string) (MediaAsset, error) {
	if strings.HasPrefix(data, "data:") {
		header, value, ok := strings.Cut(strings.TrimPrefix(data, "data:"), ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return MediaAsset{}, errors.New("invalid media data")
		}
		mime, data = strings.TrimSuffix(header, ";base64"), value
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(data))
	if err != nil {
		return MediaAsset{}, errors.New("the service returned invalid media data")
	}
	if len(raw) == 0 {
		return MediaAsset{}, errors.New("the service returned empty media")
	}
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(raw)
	}
	return MediaAsset{Data: raw, MIME: mime}, nil
}

// download fetches a result the service gave as an address. Signed result
// addresses need no credentials; the service's own host gets them.
func (s Settings) download(ctx context.Context, address string) (MediaAsset, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && IsLocal(address))) {
		return MediaAsset{}, errors.New("the service returned an unsafe media address")
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return MediaAsset{}, err
	}
	if base, err := url.Parse(s.base()); err == nil && base.Host == u.Host {
		s.authorize(hr.Header)
	}
	client := *s.httpClient()
	client.Timeout = 10 * time.Minute
	client.CheckRedirect = sameHostRedirect
	res, err := client.Do(hr)
	if err != nil {
		if ctx.Err() != nil {
			return MediaAsset{}, ctx.Err()
		}
		return MediaAsset{}, fmt.Errorf("cannot download the generated media: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return MediaAsset{}, apiError(res, s.APIKey)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxMediaAsset+1))
	if err != nil {
		return MediaAsset{}, fmt.Errorf("%w: %v", ErrStreamCut, err)
	}
	if len(data) > maxMediaAsset {
		return MediaAsset{}, errors.New("the generated media is too large")
	}
	mime, _, _ := strings.Cut(res.Header.Get("Content-Type"), ";")
	if mime == "" || mime == "application/octet-stream" || mime == "binary/octet-stream" {
		mime = http.DetectContentType(data)
	}
	return MediaAsset{Data: data, MIME: strings.TrimSpace(mime)}, nil
}

// wait sleeps between polls of a long generation, unless ctx ends first.
func (s Settings) wait(ctx context.Context, attempt int) error {
	if s.Wait != nil {
		return s.Wait(ctx, attempt)
	}
	d := 2 * time.Second
	if attempt > 10 {
		d = 5 * time.Second
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func extraInto(body map[string]any, extra map[string]any) {
	for k, v := range extra {
		if _, taken := body[k]; !taken {
			body[k] = v
		}
	}
}

// openAIImages uses /images/generations, or /images/edits when the request
// has source images (several as image[], a mask as mask), per OpenAI's API.
func (s Settings) openAIImages(ctx context.Context, req MediaRequest) (MediaResult, error) {
	gptImage := strings.HasPrefix(s.Model, "gpt-image") || strings.Contains(s.Model, "/gpt-image")
	var res map[string]any
	var err error
	if len(req.Images) == 0 {
		body := map[string]any{"model": s.Model, "prompt": req.Prompt, "n": req.Count}
		if req.Size != "" {
			body["size"] = req.Size
		}
		if req.Quality != "" {
			body["quality"] = req.Quality
		}
		// gpt-image models always answer in base64 and refuse the field;
		// older models and most gateways default to short-lived addresses.
		if !gptImage {
			body["response_format"] = "b64_json"
		}
		extraInto(body, req.Extra)
		res, err = s.postJSON(ctx, s.base()+"/images/generations", body)
	} else {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fields := map[string]string{"model": s.Model, "prompt": req.Prompt, "n": strconv.Itoa(req.Count)}
		if req.Size != "" {
			fields["size"] = req.Size
		}
		if req.Quality != "" {
			fields["quality"] = req.Quality
		}
		if !gptImage {
			fields["response_format"] = "b64_json"
		}
		for k, v := range req.Extra {
			if _, taken := fields[k]; !taken {
				fields[k] = fmt.Sprint(v)
			}
		}
		for _, k := range slices.Sorted(func(yield func(string) bool) {
			for k := range fields {
				if !yield(k) {
					return
				}
			}
		}) {
			_ = w.WriteField(k, fields[k])
		}
		field := "image"
		if len(req.Images) > 1 {
			field = "image[]"
		}
		for i, part := range req.Images {
			if err := writeFilePart(w, field, fmt.Sprintf("image-%d", i+1), part); err != nil {
				return MediaResult{}, err
			}
		}
		if req.Mask != nil {
			if err := writeFilePart(w, "mask", "mask", *req.Mask); err != nil {
				return MediaResult{}, err
			}
		}
		if err := w.Close(); err != nil {
			return MediaResult{}, err
		}
		var hr *http.Response
		hr, err = s.send(ctx, http.MethodPost, s.base()+"/images/edits", w.FormDataContentType(), buf.Bytes())
		if err == nil {
			res, err = readMediaJSON(hr)
		}
	}
	if err != nil {
		return MediaResult{}, err
	}
	var out MediaResult
	for _, raw := range array(res["data"]) {
		item := object(raw)
		var a MediaAsset
		switch {
		case str(item["b64_json"]) != "":
			a, err = asset(str(item["b64_json"]), "")
		case str(item["url"]) != "":
			a, err = s.download(ctx, str(item["url"]))
		default:
			continue
		}
		if err != nil {
			return out, err
		}
		a.RevisedPrompt = str(item["revised_prompt"])
		out.Assets = append(out.Assets, a)
	}
	if len(out.Assets) == 0 {
		return out, errors.New("the service returned no image")
	}
	return out, nil
}

var extensions = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}

// writeFilePart adds a file with its media type; OpenAI rejects an image
// whose part has none (missing_file_mimetype).
func writeFilePart(w *multipart.Writer, field, name string, part ContentPart) error {
	mime, data, err := dataPart(part)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return err
	}
	if ext := extensions[mime]; ext != "" {
		name += ext
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, name))
	h.Set("Content-Type", mime)
	pw, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = pw.Write(raw)
	return err
}

// responsesImage asks a conversation model to call its built-in
// image_generation tool, with the source images in the same message.
func (s Settings) responsesImage(ctx context.Context, req MediaRequest) (MediaResult, error) {
	content := []any{map[string]any{"type": "input_text", "text": req.Prompt}}
	for _, part := range req.Images {
		mime, data, err := dataPart(part)
		if err != nil {
			return MediaResult{}, err
		}
		content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + mime + ";base64," + data})
	}
	tool := map[string]any{"type": "image_generation"}
	if req.Size != "" {
		tool["size"] = req.Size
	}
	if req.Quality != "" {
		tool["quality"] = req.Quality
	}
	if req.Mask != nil {
		mime, data, err := dataPart(*req.Mask)
		if err != nil {
			return MediaResult{}, err
		}
		tool["input_image_mask"] = map[string]any{"image_url": "data:" + mime + ";base64," + data}
	}
	extraInto(tool, req.Extra)
	body := map[string]any{"model": s.Model, "input": []any{map[string]any{"role": "user", "content": content}}, "tools": []any{tool}, "tool_choice": map[string]any{"type": "image_generation"}, "store": false}
	res, err := s.postJSON(ctx, s.base()+"/responses", body)
	if err != nil {
		return MediaResult{}, err
	}
	var out MediaResult
	for _, raw := range array(res["output"]) {
		item := object(raw)
		switch str(item["type"]) {
		case "image_generation_call":
			if str(item["result"]) == "" {
				continue
			}
			a, err := asset(str(item["result"]), "")
			if err != nil {
				return out, err
			}
			a.RevisedPrompt = str(item["revised_prompt"])
			out.Assets = append(out.Assets, a)
		case "message":
			for _, c := range array(item["content"]) {
				out.Text += str(object(c)["text"])
			}
		}
	}
	return out, nil
}

// chatImage uses Chat Completions with image output, as OpenRouter offers
// it: modalities ["image","text"], pictures in message.images.
func (s Settings) chatImage(ctx context.Context, req MediaRequest) (MediaResult, error) {
	content := []any{map[string]any{"type": "text", "text": req.Prompt}}
	for _, part := range req.Images {
		mime, data, err := dataPart(part)
		if err != nil {
			return MediaResult{}, err
		}
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + data}})
	}
	body := map[string]any{"model": s.Model, "messages": []any{map[string]any{"role": "user", "content": content}}, "modalities": []string{"image", "text"}, "stream": false}
	if req.AspectRatio != "" {
		body["image_config"] = map[string]any{"aspect_ratio": req.AspectRatio}
	}
	extraInto(body, req.Extra)
	res, err := s.postJSON(ctx, s.base()+"/chat/completions", body)
	if err != nil {
		return MediaResult{}, err
	}
	var out MediaResult
	for _, raw := range array(res["choices"]) {
		msg := object(object(raw)["message"])
		out.Text += str(msg["content"])
		for _, img := range array(msg["images"]) {
			address := str(object(object(img)["image_url"])["url"])
			var a MediaAsset
			if strings.HasPrefix(address, "data:") {
				a, err = asset(address, "")
			} else {
				a, err = s.download(ctx, address)
			}
			if err != nil {
				return out, err
			}
			out.Assets = append(out.Assets, a)
		}
	}
	return out, nil
}

func (s Settings) geminiModel() string {
	return s.base() + "/models/" + url.PathEscape(strings.TrimPrefix(s.Model, "models/"))
}

// geminiImage uses a Gemini image model's native output: text and source
// pictures in, responseModalities TEXT and IMAGE, pictures as inlineData.
func (s Settings) geminiImage(ctx context.Context, req MediaRequest) (MediaResult, error) {
	if req.Mask != nil {
		return MediaResult{}, errors.New("Gemini image generation does not accept an edit mask; use reference images and describe the edit")
	}
	parts := []any{map[string]any{"text": req.Prompt}}
	for _, part := range req.Images {
		mime, data, err := dataPart(part)
		if err != nil {
			return MediaResult{}, err
		}
		parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": data}})
	}
	config := map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}
	// Match Google Gen AI Go SDK's ImageConfig. Keep advanced image options
	// while the explicit controls take precedence, without mutating the request.
	imageConfig := map[string]any{}
	for k, v := range object(req.Extra["imageConfig"]) {
		imageConfig[k] = v
	}
	switch req.Size {
	case "", "auto":
	case "1K", "2K", "4K":
		imageConfig["imageSize"] = req.Size
	default:
		return MediaResult{}, errors.New("Gemini image size must be 1K, 2K or 4K; use aspect ratio to set the shape")
	}
	if req.AspectRatio != "" {
		imageConfig["aspectRatio"] = req.AspectRatio
	}
	if len(imageConfig) > 0 {
		config["imageConfig"] = imageConfig
	}
	extraInto(config, req.Extra)
	res, err := s.postJSON(ctx, s.geminiModel()+":generateContent", map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "generationConfig": config})
	if err != nil {
		return MediaResult{}, err
	}
	if reason := str(object(res["promptFeedback"])["blockReason"]); reason != "" {
		return MediaResult{}, fmt.Errorf("%w: %s", ErrIncomplete, reason)
	}
	var out MediaResult
	var finish string
	for _, c := range array(res["candidates"]) {
		finish = str(object(c)["finishReason"])
		for _, raw := range array(object(object(c)["content"])["parts"]) {
			p := object(raw)
			if thought, _ := p["thought"].(bool); thought {
				continue
			}
			out.Text += str(p["text"])
			if d := object(p["inlineData"]); d != nil {
				a, err := asset(str(d["data"]), str(d["mimeType"]))
				if err != nil {
					return out, err
				}
				out.Assets = append(out.Assets, a)
			}
		}
	}
	if len(out.Assets) == 0 {
		return out, fmt.Errorf("%w: Gemini returned no image (%s): %s", ErrIncomplete, finish, strings.TrimSpace(out.Text))
	}
	return out, nil
}

// imagen uses the Gemini API's Imagen predict method.
func (s Settings) imagen(ctx context.Context, req MediaRequest) (MediaResult, error) {
	params := map[string]any{"sampleCount": req.Count}
	if req.AspectRatio != "" {
		params["aspectRatio"] = req.AspectRatio
	}
	extraInto(params, req.Extra)
	res, err := s.postJSON(ctx, s.geminiModel()+":predict", map[string]any{"instances": []any{map[string]any{"prompt": req.Prompt}}, "parameters": params})
	if err != nil {
		return MediaResult{}, err
	}
	var out MediaResult
	for _, raw := range array(res["predictions"]) {
		p := object(raw)
		if str(p["bytesBase64Encoded"]) == "" {
			continue
		}
		a, err := asset(str(p["bytesBase64Encoded"]), str(p["mimeType"]))
		if err != nil {
			return out, err
		}
		out.Assets = append(out.Assets, a)
	}
	if len(out.Assets) == 0 {
		return out, errors.New("the service returned no image")
	}
	return out, nil
}

// openAIVideo uses OpenAI's Videos API: create a job (an optional first
// frame as input_reference), poll it until it ends, download its content.
func (s Settings) openAIVideo(ctx context.Context, req MediaRequest, progress func(MediaProgress)) (MediaResult, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("model", s.Model)
	_ = w.WriteField("prompt", req.Prompt)
	if req.Seconds > 0 {
		_ = w.WriteField("seconds", strconv.Itoa(req.Seconds))
	}
	if req.Size != "" && req.Size != "auto" {
		_ = w.WriteField("size", req.Size)
	}
	for k, v := range req.Extra {
		_ = w.WriteField(k, fmt.Sprint(v))
	}
	if len(req.Images) > 0 {
		if err := writeFilePart(w, "input_reference", "reference", req.Images[0]); err != nil {
			return MediaResult{}, err
		}
	}
	if err := w.Close(); err != nil {
		return MediaResult{}, err
	}
	hr, err := s.send(ctx, http.MethodPost, s.base()+"/videos", w.FormDataContentType(), buf.Bytes())
	if err != nil {
		return MediaResult{}, err
	}
	job, err := readMediaJSON(hr)
	if err != nil {
		return MediaResult{}, err
	}
	id := str(job["id"])
	if id == "" {
		return MediaResult{}, errors.New("the service did not return a video job")
	}
	for attempt := 0; ; attempt++ {
		status := str(job["status"])
		progress(MediaProgress{Status: status, Percent: number(job["progress"])})
		switch status {
		case "completed", "succeeded":
			a, err := s.download(ctx, s.base()+"/videos/"+url.PathEscape(id)+"/content")
			if err != nil {
				return MediaResult{}, err
			}
			if !strings.HasPrefix(a.MIME, "video/") {
				a.MIME = "video/mp4"
			}
			return MediaResult{Assets: []MediaAsset{a}}, nil
		case "failed", "cancelled", "expired":
			msg := str(object(job["error"])["message"])
			if msg == "" {
				msg = "video generation " + status
			}
			return MediaResult{}, &APIError{Status: 500, Message: msg, Code: str(object(job["error"])["code"]), Protocol: s.Protocol}
		}
		if attempt >= maxMediaPolls {
			return MediaResult{}, errors.New("video generation took too long")
		}
		if err := s.wait(ctx, attempt); err != nil {
			return MediaResult{}, err
		}
		if job, err = s.getJSON(ctx, s.base()+"/videos/"+url.PathEscape(id)); err != nil {
			return MediaResult{}, err
		}
	}
}

// veo uses the Gemini API's long-running Veo method and downloads the
// sample's file with the API key, which only the API's host receives.
func (s Settings) veo(ctx context.Context, req MediaRequest, progress func(MediaProgress)) (MediaResult, error) {
	instance := map[string]any{"prompt": req.Prompt}
	if len(req.Images) > 0 {
		mime, data, err := dataPart(req.Images[0])
		if err != nil {
			return MediaResult{}, err
		}
		instance["image"] = map[string]any{"bytesBase64Encoded": data, "mimeType": mime}
	}
	params := map[string]any{}
	if req.AspectRatio != "" {
		params["aspectRatio"] = req.AspectRatio
	}
	if req.Seconds > 0 {
		params["durationSeconds"] = req.Seconds
	}
	extraInto(params, req.Extra)
	op, err := s.postJSON(ctx, s.geminiModel()+":predictLongRunning", map[string]any{"instances": []any{instance}, "parameters": params})
	if err != nil {
		return MediaResult{}, err
	}
	name := str(op["name"])
	if name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return MediaResult{}, errors.New("the service did not return a video operation")
	}
	for attempt := 0; ; attempt++ {
		if done, _ := op["done"].(bool); done {
			if e := object(op["error"]); e != nil {
				return MediaResult{}, &APIError{Status: 500, Message: str(e["message"]), Protocol: s.Protocol}
			}
			samples := array(object(object(op["response"])["generateVideoResponse"])["generatedSamples"])
			if len(samples) == 0 {
				return MediaResult{}, fmt.Errorf("%w: the video was filtered", ErrIncomplete)
			}
			a, err := s.download(ctx, str(object(object(samples[0])["video"])["uri"]))
			if err != nil {
				return MediaResult{}, err
			}
			if !strings.HasPrefix(a.MIME, "video/") {
				a.MIME = "video/mp4"
			}
			return MediaResult{Assets: []MediaAsset{a}}, nil
		}
		progress(MediaProgress{Status: "in_progress"})
		if attempt >= maxMediaPolls {
			return MediaResult{}, errors.New("video generation took too long")
		}
		if err := s.wait(ctx, attempt); err != nil {
			return MediaResult{}, err
		}
		if op, err = s.getJSON(ctx, s.base()+"/"+name); err != nil {
			return MediaResult{}, err
		}
	}
}
