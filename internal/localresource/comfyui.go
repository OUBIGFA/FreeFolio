package localresource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ComfyConfig specifies how FreeFolio locates or connects to ComfyUI and where
// FreeFolio mounts its own optional model folders via --extra-model-paths-config.
type ComfyConfig struct {
	Dir         string
	URL         string
	ResourceDir string
}

// ComfyLayout describes a detected local ComfyUI installation.
type ComfyLayout struct {
	Root   string
	MainPy string
	Python string
}

// ComfyCapabilities records the GPU device and models exposed by ComfyUI.
type ComfyCapabilities struct {
	BaseURL       string
	Device        string
	UpscaleModels []string
	CutoutModels  []string
	CutoutNode    string
}

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max <= 0 {
		b.max = 8192
	}
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}

// ComfyManager manages an optional on-demand local ComfyUI process and
// executes workflows over ComfyUI's native HTTP API.
type ComfyManager struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	waitCh      chan error
	baseURL     string
	dir         string
	resourceDir string
	client      *http.Client
	caps        *ComfyCapabilities
}

// NewComfyManager constructs a ComfyManager with sensible HTTP timeouts.
func NewComfyManager() *ComfyManager {
	return &ComfyManager{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *ComfyManager) httpClient() *http.Client {
	if m.client != nil {
		return m.client
	}
	return http.DefaultClient
}

// DetectComfyLayout validates a ComfyUI root directory and locates main.py and Python.
func DetectComfyLayout(dir string) (ComfyLayout, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ComfyLayout{}, errors.New("ComfyUI directory is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ComfyLayout{}, err
	}
	var mainPy string
	for _, cand := range []string{
		filepath.Join(abs, "main.py"),
		filepath.Join(abs, "ComfyUI", "main.py"),
	} {
		if info, err := os.Stat(cand); err == nil && info.Mode().IsRegular() {
			mainPy = cand
			break
		}
	}
	if mainPy == "" {
		return ComfyLayout{}, fmt.Errorf("main.py not found in %s", abs)
	}
	mainDir := filepath.Dir(mainPy)
	parentDir := filepath.Dir(abs)
	var pythonPath string
	candidates := []string{
		filepath.Join(abs, "python_embeded", "python.exe"),
		filepath.Join(parentDir, "python_embeded", "python.exe"),
		filepath.Join(abs, ".venv", "Scripts", "python.exe"),
		filepath.Join(abs, "venv", "Scripts", "python.exe"),
		filepath.Join(mainDir, ".venv", "Scripts", "python.exe"),
		filepath.Join(mainDir, "venv", "Scripts", "python.exe"),
		filepath.Join(abs, ".venv", "bin", "python"),
		filepath.Join(abs, ".venv", "bin", "python3"),
		filepath.Join(abs, "venv", "bin", "python"),
		filepath.Join(abs, "venv", "bin", "python3"),
		filepath.Join(mainDir, ".venv", "bin", "python"),
		filepath.Join(mainDir, ".venv", "bin", "python3"),
	}
	for _, cand := range candidates {
		if info, err := os.Stat(cand); err == nil && info.Mode().IsRegular() {
			pythonPath = cand
			break
		}
	}
	if pythonPath == "" {
		for _, bin := range []string{"python3", "python"} {
			if found, err := exec.LookPath(bin); err == nil {
				pythonPath = found
				break
			}
		}
	}
	if pythonPath == "" {
		return ComfyLayout{}, fmt.Errorf("python executable not found for %s", abs)
	}
	return ComfyLayout{Root: mainDir, MainPy: mainPy, Python: pythonPath}, nil
}

// WriteComfyExtraModelPaths creates FreeFolio's ComfyUI model folders under
// resourceDir and writes an extra_model_paths.yaml mounting them readably.
func WriteComfyExtraModelPaths(resourceDir string) (string, error) {
	if !filepath.IsAbs(resourceDir) {
		return "", errors.New("resource directory must be an absolute path")
	}
	base := filepath.Join(resourceDir, "comfyui")
	folders := []string{"diffusion_models", "text_encoders", "vae", "upscale_models", "checkpoints", "rmbg"}
	for _, sub := range folders {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o700); err != nil {
			return "", err
		}
	}
	yamlPath := filepath.Join(base, "extra_model_paths.yaml")
	var sb strings.Builder
	sb.WriteString("freefolio:\n")
	fmt.Fprintf(&sb, "    base_path: %s\n", filepath.ToSlash(base))
	for _, sub := range folders {
		fmt.Fprintf(&sb, "    %s: %s\n", sub, sub)
	}
	if err := os.WriteFile(yamlPath, []byte(sb.String()), 0o600); err != nil {
		return "", err
	}
	return yamlPath, nil
}

func normalizeComfyURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("ComfyUI URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid ComfyUI URL: %s", raw)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// Running reports whether a FreeFolio-managed ComfyUI child process is alive.
func (m *ComfyManager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil || m.waitCh == nil {
		return false
	}
	select {
	case <-m.waitCh:
		m.cmd = nil
		m.waitCh = nil
		m.baseURL = ""
		return false
	default:
		return true
	}
}

// Stop terminates any FreeFolio-managed ComfyUI child process.
func (m *ComfyManager) Stop() {
	m.mu.Lock()
	cmd := m.cmd
	waitCh := m.waitCh
	m.cmd = nil
	m.waitCh = nil
	m.baseURL = ""
	m.dir = ""
	m.caps = nil
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	if waitCh != nil {
		select {
		case <-waitCh:
		case <-time.After(3 * time.Second):
		}
	}
}

// EnsureRunning verifies a configured ComfyUI URL or silently starts ComfyUI
// from cfg.Dir with FreeFolio's resource folder mounted.
func (m *ComfyManager) EnsureRunning(ctx context.Context, cfg ComfyConfig) (string, error) {
	if raw := strings.TrimSpace(cfg.URL); raw != "" {
		base, err := normalizeComfyURL(raw)
		if err != nil {
			return "", err
		}
		if err := m.ping(ctx, base); err != nil {
			return "", fmt.Errorf("cannot reach ComfyUI at %s: %w", base, err)
		}
		return base, nil
	}
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		return "", errors.New("ComfyUI path or URL is not configured")
	}

	m.mu.Lock()
	if m.cmd != nil && m.waitCh != nil && m.dir == dir && m.resourceDir == cfg.ResourceDir && m.baseURL != "" {
		select {
		case <-m.waitCh:
			m.cmd = nil
			m.waitCh = nil
			m.baseURL = ""
		default:
			existing := m.baseURL
			m.mu.Unlock()
			if err := m.ping(ctx, existing); err == nil {
				return existing, nil
			}
			m.Stop()
			m.mu.Lock()
		}
	}
	m.mu.Unlock()

	m.Stop()

	layout, err := DetectComfyLayout(dir)
	if err != nil {
		return "", err
	}
	args := []string{layout.MainPy, "--listen", "127.0.0.1"}
	port, err := freeLoopbackPort()
	if err != nil {
		return "", err
	}
	args = append(args, "--port", strconv.Itoa(port), "--disable-auto-launch")
	if strings.TrimSpace(cfg.ResourceDir) != "" {
		yamlPath, err := WriteComfyExtraModelPaths(cfg.ResourceDir)
		if err != nil {
			return "", err
		}
		args = append(args, "--extra-model-paths-config", yamlPath)
	}

	cmd := exec.Command(layout.Python, args...)
	cmd.Dir = layout.Root
	hideConsole(cmd)
	logs := &tailBuffer{max: 8192}
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start ComfyUI: %w", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
		close(waitCh)
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	m.mu.Lock()
	m.cmd = cmd
	m.waitCh = waitCh
	m.baseURL = base
	m.dir = dir
	m.resourceDir = cfg.ResourceDir
	m.mu.Unlock()

	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := m.ping(ctx, base); err == nil {
			return base, nil
		}
		select {
		case <-ctx.Done():
			m.Stop()
			return "", ctx.Err()
		case waitErr := <-waitCh:
			m.Stop()
			detail := logs.String()
			if detail != "" {
				return "", fmt.Errorf("ComfyUI exited early (%v): %s", waitErr, detail)
			}
			return "", fmt.Errorf("ComfyUI exited early: %v", waitErr)
		case <-deadline.C:
			m.Stop()
			detail := logs.String()
			if detail != "" {
				return "", fmt.Errorf("ComfyUI startup timed out: %s", detail)
			}
			return "", errors.New("ComfyUI startup timed out")
		case <-ticker.C:
		}
	}
}

func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("unexpected loopback listener address")
	}
	return addr.Port, nil
}

func (m *ComfyManager) ping(ctx context.Context, base string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+"/system_stats", nil)
	if err != nil {
		return err
	}
	res, err := m.httpClient().Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return nil
}

// Probe ensures ComfyUI is reachable and inspects available GPU and models.
func (m *ComfyManager) Probe(ctx context.Context, cfg ComfyConfig) (*ComfyCapabilities, error) {
	base, err := m.EnsureRunning(ctx, cfg)
	if err != nil {
		return nil, err
	}
	caps := &ComfyCapabilities{BaseURL: base}
	if stats, err := m.getJSON(ctx, base+"/system_stats"); err == nil {
		var parsed struct {
			Devices []struct {
				Name string `json:"name"`
			} `json:"devices"`
		}
		if json.Unmarshal(stats, &parsed) == nil && len(parsed.Devices) > 0 {
			caps.Device = parsed.Devices[0].Name
		}
	}
	rawInfo, err := m.getJSON(ctx, base+"/object_info")
	if err != nil {
		return nil, err
	}
	var info map[string]struct {
		Input struct {
			Required map[string][]json.RawMessage `json:"required"`
		} `json:"input"`
	}
	if err := json.Unmarshal(rawInfo, &info); err != nil {
		return nil, err
	}
	if _, hasApply := info["ImageUpscaleWithModel"]; hasApply {
		if loader, ok := info["UpscaleModelLoader"]; ok {
			caps.UpscaleModels = parseComfyChoiceList(loader.Input.Required["model_name"])
		}
	}
	if rmbg, ok := info["RMBG"]; ok {
		caps.CutoutModels = parseComfyChoiceList(rmbg.Input.Required["model"])
		if len(caps.CutoutModels) > 0 {
			caps.CutoutNode = "RMBG"
		}
	}
	m.mu.Lock()
	m.caps = caps
	m.mu.Unlock()
	return caps, nil
}

func parseComfyChoiceList(spec []json.RawMessage) []string {
	if len(spec) == 0 {
		return nil
	}
	var list []string
	if json.Unmarshal(spec[0], &list) == nil {
		return list
	}
	return nil
}

func (m *ComfyManager) getJSON(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	res, err := m.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// Upscale runs an image upscale workflow via ComfyUI's UpscaleModelLoader + ImageUpscaleWithModel.
func (m *ComfyManager) Upscale(ctx context.Context, cfg ComfyConfig, model string, scale int, input []byte) ([]byte, error) {
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("upscale model is required")
	}
	if scale == 0 {
		scale = 4
	}
	if scale != 2 && scale != 4 {
		return nil, errors.New("upscale factor must be 2 or 4")
	}
	if len(input) > maxInput {
		return nil, errors.New("input image exceeds 64 MiB")
	}
	img, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	origW, origH := img.Bounds().Dx(), img.Bounds().Dy()
	if origW <= 0 || origH <= 0 {
		return nil, errors.New("empty input image")
	}
	outW, outH := origW*scale, origH*scale
	if int64(outW)*int64(outH) > maxUpscaleOutputPixels || outW > maxUpscaleOutputSide || outH > maxUpscaleOutputSide {
		return nil, errors.New("upscaled image exceeds size limit")
	}
	var pngBuf bytes.Buffer
	if err := writeNormalizedPNGTo(&pngBuf, img); err != nil {
		return nil, err
	}
	base, err := m.EnsureRunning(ctx, cfg)
	if err != nil {
		return nil, err
	}
	uploaded, err := m.uploadImage(ctx, base, pngBuf.Bytes())
	if err != nil {
		return nil, err
	}
	graph := map[string]any{
		"1": map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": uploaded}},
		"2": map[string]any{"class_type": "UpscaleModelLoader", "inputs": map[string]any{"model_name": model}},
		"3": map[string]any{"class_type": "ImageUpscaleWithModel", "inputs": map[string]any{"upscale_model": []any{"2", 0}, "image": []any{"1", 0}}},
		"4": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"3", 0}, "filename_prefix": "freefolio_upscale"}},
	}
	outBytes, err := m.runImageWorkflow(ctx, base, graph)
	if err != nil {
		return nil, err
	}
	if scale == 2 {
		if cfgOut, _, err := image.DecodeConfig(bytes.NewReader(outBytes)); err == nil && (cfgOut.Width != outW || cfgOut.Height != outH) {
			return resizePNG(outBytes, outW, outH)
		}
	}
	return outBytes, nil
}

// Cutout runs a background removal workflow via ComfyUI's RMBG node.
func (m *ComfyManager) Cutout(ctx context.Context, cfg ComfyConfig, model string, input []byte) ([]byte, error) {
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("cutout model is required")
	}
	if len(input) > maxInput {
		return nil, errors.New("input image exceeds 64 MiB")
	}
	img, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	var pngBuf bytes.Buffer
	if err := writeNormalizedPNGTo(&pngBuf, img); err != nil {
		return nil, err
	}
	base, err := m.EnsureRunning(ctx, cfg)
	if err != nil {
		return nil, err
	}
	uploaded, err := m.uploadImage(ctx, base, pngBuf.Bytes())
	if err != nil {
		return nil, err
	}
	graph := map[string]any{
		"1": map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": uploaded}},
		"2": map[string]any{"class_type": "RMBG", "inputs": map[string]any{
			"image":         []any{"1", 0},
			"model":         model,
			"sensitivity":   1.0,
			"process_res":   1024,
			"mask_blur":     0,
			"mask_offset":   0,
			"background":    "Alpha",
			"invert_output": false,
		}},
		"3": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"2", 0}, "filename_prefix": "freefolio_cutout"}},
	}
	return m.runImageWorkflow(ctx, base, graph)
}

func writeNormalizedPNGTo(w io.Writer, img image.Image) error {
	f, ok := w.(*bytes.Buffer)
	if !ok {
		return errors.New("unexpected writer")
	}
	tmp := bytes.NewBuffer(nil)
	dir, err := os.MkdirTemp("", "freefolio-comfy-png-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "in.png")
	if err := writeNormalizedPNG(path, img); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err != nil {
		return err
	}
	*f = *tmp
	return nil
}

func (m *ComfyManager) uploadImage(ctx context.Context, base string, data []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "freefolio_input.png")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	_ = mw.WriteField("type", "input")
	_ = mw.WriteField("overwrite", "true")
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/upload/image", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := m.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ComfyUI upload failed (HTTP %d): %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Name == "" {
		return "", fmt.Errorf("unexpected ComfyUI upload response: %s", strings.TrimSpace(string(raw)))
	}
	if out.Subfolder != "" {
		return strings.Trim(out.Subfolder, "/") + "/" + out.Name, nil
	}
	return out.Name, nil
}

func (m *ComfyManager) runImageWorkflow(ctx context.Context, base string, graph map[string]any) ([]byte, error) {
	payload, err := json.Marshal(map[string]any{"prompt": graph, "client_id": "freefolio"})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/prompt", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := m.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	res.Body.Close()
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ComfyUI prompt failed (HTTP %d): %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var queued struct {
		PromptID string `json:"prompt_id"`
	}
	if err := json.Unmarshal(raw, &queued); err != nil || queued.PromptID == "" {
		return nil, fmt.Errorf("unexpected ComfyUI prompt response: %s", strings.TrimSpace(string(raw)))
	}

	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.interrupt(base)
			return nil, ctx.Err()
		case <-ticker.C:
			rawHist, err := m.getJSON(ctx, base+"/history/"+url.PathEscape(queued.PromptID))
			if err != nil {
				continue
			}
			var hist map[string]struct {
				Status struct {
					StatusStr string `json:"status_str"`
					Completed bool   `json:"completed"`
				} `json:"status"`
				Outputs map[string]struct {
					Images []struct {
						Filename  string `json:"filename"`
						Subfolder string `json:"subfolder"`
						Type      string `json:"type"`
					} `json:"images"`
				} `json:"outputs"`
			}
			if json.Unmarshal(rawHist, &hist) != nil {
				continue
			}
			item, ok := hist[queued.PromptID]
			if !ok {
				continue
			}
			if item.Status.StatusStr == "error" {
				return nil, errors.New("ComfyUI workflow execution failed")
			}
			for _, out := range item.Outputs {
				if len(out.Images) == 0 {
					continue
				}
				img := out.Images[0]
				q := url.Values{}
				q.Set("filename", img.Filename)
				q.Set("subfolder", img.Subfolder)
				if img.Type == "" {
					img.Type = "output"
				}
				q.Set("type", img.Type)
				return m.getBytes(ctx, base+"/view?"+q.Encode())
			}
			if item.Status.Completed {
				return nil, errors.New("ComfyUI workflow completed with no output image")
			}
		}
	}
}

func (m *ComfyManager) getBytes(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	res, err := m.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 128<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return data, nil
}

func (m *ComfyManager) interrupt(base string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/interrupt", nil)
	if err != nil {
		return
	}
	res, err := m.httpClient().Do(req)
	if err == nil {
		res.Body.Close()
	}
}
