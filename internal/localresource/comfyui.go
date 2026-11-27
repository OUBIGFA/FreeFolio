package localresource

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"image"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
	Root            string
	MainPy          string
	Python          string
	BaseDir         string
	InputDir        string
	OutputDir       string
	UserDir         string
	ExtraModelPaths []string
	IsolatedPython  bool
	Source          string
}

// ComfyCapabilities records the GPU device and models exposed by ComfyUI.
type ComfyCapabilities struct {
	NodeTypes           map[string]bool
	ModelFiles          map[string][]string
	BaseURL             string
	Device              string
	VRAMMiB             int
	Version             string
	ResolvedRoot        string
	ResolvedPython      string
	UpscaleModels       []string
	CutoutModels        []string
	CutoutNode          string
	RMBG3Native         bool
	RMBG3Models         []string
	TextEncoders        []string
	VAEModels           []string
	Sa2VANode           string
	Sa2VAModels         []string
	SeedVR2Native       bool
	SeedVR2Models       []string
	ZImageNative        bool
	ZImageModels        []string
	QwenImageNative     bool
	QwenImageBaseNative bool
	QwenImageModels     []string
	LoRANative          bool
	LoRAModels          []string
}

type comfyNodeSchema struct {
	Required map[string]any
	Output   []string
}

type comfyFileRef struct {
	Filename  string `json:"filename"`
	Subfolder string `json:"subfolder"`
	Type      string `json:"type"`
}

// ComfyOutput is one saved file exposed by a workflow's output nodes.
type ComfyOutput struct {
	Filename string
	Data     []byte
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
	startMu        sync.Mutex
	mu             sync.Mutex
	cmd            *exec.Cmd
	waitCh         chan error
	procGroup      *processGroup
	baseURL        string
	dir            string
	resourceDir    string
	resolvedRoot   string
	resolvedPython string
	client         *http.Client
	caps           *ComfyCapabilities
	schemas        map[string]comfyNodeSchema
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

type comfyDetectEnv struct {
	userConfigDir string
	localAppData  string
	goos          string
	lookPath      func(string) (string, error)
}

func defaultComfyDetectEnv() comfyDetectEnv {
	cfgDir, _ := os.UserConfigDir()
	return comfyDetectEnv{
		userConfigDir: cfgDir,
		localAppData:  os.Getenv("LOCALAPPDATA"),
		goos:          runtime.GOOS,
		lookPath:      exec.LookPath,
	}
}

type comfyDesktopV2Install struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	SourceID          string `json:"sourceId"`
	RemoteURL         string `json:"remoteUrl"`
	Status            string `json:"status"`
	InstallPath       string `json:"installPath"`
	Adopted           bool   `json:"adopted"`
	AdoptedBaseDir    string `json:"adoptedBaseDir"`
	AdoptedPythonPath string `json:"adoptedPythonPath"`
	InputDir          string `json:"inputDir"`
	OutputDir         string `json:"outputDir"`
	LastLaunchedAt    int64  `json:"lastLaunchedAt"`
}

type comfyDesktopV2Settings struct {
	InstallDir          string `json:"installDir"`
	AutoLaunchOnStartup string `json:"autoLaunchOnStartup"`
}

type comfyDesktopV2Meta struct {
	cfgDir      string
	hasInstalls bool
	installs    []comfyDesktopV2Install
	settings    comfyDesktopV2Settings
}

type comfyLegacyDesktopMeta struct {
	basePath        string
	extraModelsYAML string
}

func loadComfyDesktopV2Meta(userConfigDir string) comfyDesktopV2Meta {
	if strings.TrimSpace(userConfigDir) == "" {
		return comfyDesktopV2Meta{}
	}
	cfgDir := filepath.Join(userConfigDir, "Comfy Desktop")
	meta := comfyDesktopV2Meta{cfgDir: cfgDir}
	if raw, err := os.ReadFile(filepath.Join(cfgDir, "installations.json")); err == nil {
		var installs []comfyDesktopV2Install
		if json.Unmarshal(raw, &installs) == nil {
			meta.hasInstalls = true
			meta.installs = installs
		}
	}
	if raw, err := os.ReadFile(filepath.Join(cfgDir, "settings.json")); err == nil {
		_ = json.Unmarshal(raw, &meta.settings)
	}
	return meta
}

func loadComfyLegacyDesktopMeta(userConfigDir string) comfyLegacyDesktopMeta {
	if strings.TrimSpace(userConfigDir) == "" {
		return comfyLegacyDesktopMeta{}
	}
	cfgDir := filepath.Join(userConfigDir, "ComfyUI")
	var meta comfyLegacyDesktopMeta
	if raw, err := os.ReadFile(filepath.Join(cfgDir, "config.json")); err == nil {
		var cfg struct {
			BasePath string `json:"basePath"`
		}
		if json.Unmarshal(raw, &cfg) == nil {
			meta.basePath = strings.TrimSpace(cfg.BasePath)
		}
	}
	yamlPath := filepath.Join(cfgDir, "extra_models_config.yaml")
	if isRegularFile(yamlPath) {
		meta.extraModelsYAML = yamlPath
	}
	return meta
}

func normalizeComparePath(p, goos string) string {
	p = filepath.Clean(p)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if goos == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func samePath(a, b, goos string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	return normalizeComparePath(a, goos) == normalizeComparePath(b, goos)
}

func isSubpath(child, parent, goos string) bool {
	if strings.TrimSpace(child) == "" || strings.TrimSpace(parent) == "" {
		return false
	}
	c := normalizeComparePath(child, goos)
	p := normalizeComparePath(parent, goos)
	if c == p {
		return true
	}
	rel, err := filepath.Rel(p, c)
	if err != nil || rel == "" || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isRegularFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isExistingDir(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isWindowsStorePythonStub(path, goos string) bool {
	if goos != "windows" {
		return false
	}
	low := strings.ToLower(filepath.Clean(path))
	if strings.Contains(low, `\microsoft\windowsapps\`) {
		return true
	}
	info, err := os.Lstat(path)
	if err != nil {
		return true
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return true
	}
	return false
}

func isRegularExecutable(path, goos string) bool {
	if !isRegularFile(path) {
		return false
	}
	return !isWindowsStorePythonStub(path, goos)
}

func findDedicatedPython(goos string, roots ...string) string {
	var seen []string
	relCandidates := []string{
		filepath.Join("python_embeded", "python.exe"),
		filepath.Join("python_embedded", "python.exe"),
		filepath.Join("python", "python.exe"),
		filepath.Join(".venv", "Scripts", "python.exe"),
		filepath.Join("venv", "Scripts", "python.exe"),
		filepath.Join("env", "Scripts", "python.exe"),
		filepath.Join(".venv", "bin", "python"),
		filepath.Join(".venv", "bin", "python3"),
		filepath.Join("venv", "bin", "python"),
		filepath.Join("venv", "bin", "python3"),
		filepath.Join("env", "bin", "python"),
		filepath.Join("env", "bin", "python3"),
		filepath.Join("python", "bin", "python3"),
		filepath.Join("python", "bin", "python"),
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
		dup := false
		for _, prev := range seen {
			if samePath(prev, root, goos) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		seen = append(seen, root)
		for _, rel := range relCandidates {
			cand := filepath.Join(root, rel)
			if isRegularExecutable(cand, goos) {
				return cand
			}
		}
	}
	return ""
}

func findFallbackPython(env comfyDetectEnv) string {
	look := env.lookPath
	if look == nil {
		look = exec.LookPath
	}
	for _, bin := range []string{"python3", "python"} {
		if found, err := look(bin); err == nil && isRegularExecutable(found, env.goos) {
			return found
		}
	}
	return ""
}

func findMainPyInDir(dir string) string {
	for _, cand := range []string{
		filepath.Join(dir, "main.py"),
		filepath.Join(dir, "ComfyUI", "main.py"),
		filepath.Join(dir, "ComfyUI_windows_portable", "ComfyUI", "main.py"),
	} {
		if isRegularFile(cand) {
			return cand
		}
	}
	return ""
}

func findLegacyEmbeddedMainPy(dir string) string {
	for _, cand := range []string{
		filepath.Join(dir, "resources", "ComfyUI", "main.py"),
		filepath.Join(dir, "Contents", "Resources", "ComfyUI", "main.py"),
		filepath.Join(dir, "Resources", "ComfyUI", "main.py"),
	} {
		if isRegularFile(cand) {
			return cand
		}
	}
	return ""
}

func hasComfyDesktopV2Markers(dir string) bool {
	markers := []string{
		filepath.Join(dir, "Comfy Desktop.exe"),
		filepath.Join(dir, "Comfy Desktop", "Comfy Desktop.exe"),
		filepath.Join(dir, "Contents", "MacOS", "Comfy Desktop"),
		filepath.Join(dir, "resources", "bootstrap-python"),
	}
	for _, m := range markers {
		if _, err := os.Stat(m); err == nil {
			return true
		}
	}
	return false
}

func resolveDesktopV2Install(inst comfyDesktopV2Install, cfgDir, goos string) (ComfyLayout, bool) {
	if strings.EqualFold(strings.TrimSpace(inst.SourceID), "cloud") || strings.TrimSpace(inst.RemoteURL) != "" {
		return ComfyLayout{}, false
	}
	if inst.Status != "" && !strings.EqualFold(strings.TrimSpace(inst.Status), "installed") {
		return ComfyLayout{}, false
	}
	installPath := strings.TrimSpace(inst.InstallPath)
	if installPath == "" {
		return ComfyLayout{}, false
	}
	var mainPy string
	for _, cand := range []string{
		filepath.Join(installPath, "ComfyUI", "main.py"),
		filepath.Join(installPath, "main.py"),
	} {
		if isRegularFile(cand) {
			mainPy = cand
			break
		}
	}
	if mainPy == "" {
		return ComfyLayout{}, false
	}
	mainDir := filepath.Dir(mainPy)
	py := strings.TrimSpace(inst.AdoptedPythonPath)
	if !isRegularExecutable(py, goos) {
		py = findDedicatedPython(goos, installPath, mainDir, inst.AdoptedBaseDir, filepath.Dir(installPath))
	}
	if py == "" {
		return ComfyLayout{}, false
	}
	layout := ComfyLayout{
		Root:           mainDir,
		MainPy:         mainPy,
		Python:         py,
		IsolatedPython: true,
		Source:         "comfy-desktop",
	}
	if isExistingDir(inst.AdoptedBaseDir) {
		layout.BaseDir = filepath.Clean(inst.AdoptedBaseDir)
	} else if isExistingDir(filepath.Join(installPath, "user")) {
		layout.BaseDir = filepath.Clean(installPath)
	}
	if isExistingDir(inst.InputDir) {
		layout.InputDir = filepath.Clean(inst.InputDir)
	} else if layout.BaseDir != "" && isExistingDir(filepath.Join(layout.BaseDir, "input")) {
		layout.InputDir = filepath.Join(layout.BaseDir, "input")
	}
	if isExistingDir(inst.OutputDir) {
		layout.OutputDir = filepath.Clean(inst.OutputDir)
	} else if layout.BaseDir != "" && isExistingDir(filepath.Join(layout.BaseDir, "output")) {
		layout.OutputDir = filepath.Join(layout.BaseDir, "output")
	}
	if layout.BaseDir != "" && isExistingDir(filepath.Join(layout.BaseDir, "user")) {
		layout.UserDir = filepath.Join(layout.BaseDir, "user")
	}
	if inst.ID != "" && cfgDir != "" {
		instYAML := filepath.Join(cfgDir, "instance-model-paths", inst.ID+".yaml")
		if isRegularFile(instYAML) {
			layout.ExtraModelPaths = append(layout.ExtraModelPaths, instYAML)
		}
	}
	return layout, true
}

func selectBestDesktopV2Install(meta comfyDesktopV2Meta, filter func(comfyDesktopV2Install) bool, goos string) (ComfyLayout, bool) {
	type candidate struct {
		inst   comfyDesktopV2Install
		layout ComfyLayout
		index  int
	}
	var cands []candidate
	for i, inst := range meta.installs {
		if filter != nil && !filter(inst) {
			continue
		}
		if layout, ok := resolveDesktopV2Install(inst, meta.cfgDir, goos); ok {
			cands = append(cands, candidate{inst: inst, layout: layout, index: i})
		}
	}
	if len(cands) == 0 {
		return ComfyLayout{}, false
	}
	slices.SortStableFunc(cands, func(a, b candidate) int {
		aPref := meta.settings.AutoLaunchOnStartup != "" && a.inst.ID == meta.settings.AutoLaunchOnStartup
		bPref := meta.settings.AutoLaunchOnStartup != "" && b.inst.ID == meta.settings.AutoLaunchOnStartup
		if aPref != bPref {
			if aPref {
				return -1
			}
			return 1
		}
		if a.inst.LastLaunchedAt != b.inst.LastLaunchedAt {
			if a.inst.LastLaunchedAt > b.inst.LastLaunchedAt {
				return -1
			}
			return 1
		}
		return a.index - b.index
	})
	return cands[0].layout, true
}

func applyLegacyDesktopPaths(layout *ComfyLayout, legacy comfyLegacyDesktopMeta) {
	if isExistingDir(legacy.basePath) {
		base := filepath.Clean(legacy.basePath)
		layout.BaseDir = base
		if isExistingDir(filepath.Join(base, "input")) {
			layout.InputDir = filepath.Join(base, "input")
		}
		if isExistingDir(filepath.Join(base, "output")) {
			layout.OutputDir = filepath.Join(base, "output")
		}
		if isExistingDir(filepath.Join(base, "user")) {
			layout.UserDir = filepath.Join(base, "user")
		}
	}
	if legacy.extraModelsYAML != "" {
		layout.ExtraModelPaths = append(layout.ExtraModelPaths, legacy.extraModelsYAML)
	}
}

// DetectComfyLayout validates a ComfyUI root directory (portable, venv/source,
// third-party pack, or ComfyUI Desktop) and locates main.py, Python, and data paths.
func DetectComfyLayout(dir string) (ComfyLayout, error) {
	return detectComfyLayout(dir, defaultComfyDetectEnv())
}

func detectComfyLayout(dir string, env comfyDetectEnv) (ComfyLayout, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ComfyLayout{}, errors.New("ComfyUI directory is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ComfyLayout{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return ComfyLayout{}, fmt.Errorf("ComfyUI directory does not exist: %s", abs)
		}
		return ComfyLayout{}, err
	}
	if info.Mode().IsRegular() {
		abs = filepath.Dir(abs)
	} else if !info.IsDir() {
		return ComfyLayout{}, fmt.Errorf("ComfyUI path is not a directory: %s", abs)
	}

	v2Meta := loadComfyDesktopV2Meta(env.userConfigDir)
	legacyMeta := loadComfyLegacyDesktopMeta(env.userConfigDir)

	// 1. Direct match against Comfy Desktop v2 managed or adopted installations.
	if layout, ok := selectBestDesktopV2Install(v2Meta, func(inst comfyDesktopV2Install) bool {
		if inst.InstallPath != "" {
			if isSubpath(abs, inst.InstallPath, env.goos) || samePath(abs, filepath.Dir(inst.InstallPath), env.goos) {
				return true
			}
		}
		if v2Meta.settings.InstallDir != "" && samePath(abs, v2Meta.settings.InstallDir, env.goos) {
			return true
		}
		if inst.AdoptedBaseDir != "" && samePath(abs, inst.AdoptedBaseDir, env.goos) {
			return true
		}
		return false
	}, env.goos); ok {
		return layout, nil
	}

	// 2. Comfy Desktop v2 Electron shell directory (e.g. AppData\Local\Programs\ComfyUI).
	if hasComfyDesktopV2Markers(abs) {
		if layout, ok := selectBestDesktopV2Install(v2Meta, nil, env.goos); ok {
			return layout, nil
		}
		if v2Meta.hasInstalls {
			return ComfyLayout{}, fmt.Errorf("Comfy Desktop detected at %s, but no valid local ComfyUI installation was found", abs)
		}
	}

	// 3. Legacy ComfyUI Desktop Electron directory (resources/ComfyUI/main.py).
	if mainPy := findLegacyEmbeddedMainPy(abs); mainPy != "" {
		mainDir := filepath.Dir(mainPy)
		py := findDedicatedPython(env.goos, abs, mainDir, legacyMeta.basePath)
		isolated := py != ""
		if py == "" {
			py = findFallbackPython(env)
		}
		if py == "" {
			return ComfyLayout{}, fmt.Errorf("python executable not found for ComfyUI Desktop at %s", abs)
		}
		layout := ComfyLayout{
			Root:           mainDir,
			MainPy:         mainPy,
			Python:         py,
			IsolatedPython: isolated,
			Source:         "comfyui-desktop",
		}
		applyLegacyDesktopPaths(&layout, legacyMeta)
		return layout, nil
	}

	// 4. Standard / portable / third-party integrated pack / source venv layout.
	if mainPy := findMainPyInDir(abs); mainPy != "" {
		mainDir := filepath.Dir(mainPy)
		py := findDedicatedPython(env.goos, abs, mainDir, filepath.Dir(abs), filepath.Dir(mainDir))
		isolated := py != ""
		if py == "" {
			py = findFallbackPython(env)
		}
		if py == "" {
			return ComfyLayout{}, fmt.Errorf("python executable not found for %s", abs)
		}
		source := "venv"
		if strings.Contains(strings.ToLower(py), "python_embed") {
			source = "portable"
		} else if !isolated {
			source = "system-python"
		}
		return ComfyLayout{
			Root:           mainDir,
			MainPy:         mainPy,
			Python:         py,
			IsolatedPython: isolated,
			Source:         source,
		}, nil
	}

	// 5. Legacy ComfyUI Desktop basePath (user pointed to their _data folder).
	if legacyMeta.basePath != "" && samePath(abs, legacyMeta.basePath, env.goos) {
		var appCandidates []string
		if env.goos == "windows" && env.localAppData != "" {
			appCandidates = append(appCandidates, filepath.Join(env.localAppData, "Programs", "ComfyUI"))
		} else if env.goos == "darwin" {
			appCandidates = append(appCandidates, "/Applications/ComfyUI.app")
		}
		for _, appDir := range appCandidates {
			if mainPy := findLegacyEmbeddedMainPy(appDir); mainPy != "" {
				py := findDedicatedPython(env.goos, abs, appDir)
				if py != "" {
					layout := ComfyLayout{
						Root:           filepath.Dir(mainPy),
						MainPy:         mainPy,
						Python:         py,
						IsolatedPython: true,
						Source:         "comfyui-desktop",
					}
					applyLegacyDesktopPaths(&layout, legacyMeta)
					return layout, nil
				}
			}
		}
	}

	if hasComfyDesktopV2Markers(abs) {
		return ComfyLayout{}, fmt.Errorf("Comfy Desktop detected at %s, but no local ComfyUI installation was found", abs)
	}
	return ComfyLayout{}, fmt.Errorf("main.py not found in %s", abs)
}

func yamlSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// WriteComfyExtraModelPaths creates FreeFolio's ComfyUI model folders under
// resourceDir and writes an extra_model_paths.yaml mounting them readably.
func WriteComfyExtraModelPaths(resourceDir string) (string, error) {
	if !filepath.IsAbs(resourceDir) {
		return "", errors.New("resource directory must be an absolute path")
	}
	base := filepath.Join(resourceDir, "comfyui")
	folders := []string{"diffusion_models", "text_encoders", "vae", "upscale_models", "checkpoints", "rmbg", "sa2va", "vitmatte", "loras", "tts", "custom_nodes"}
	for _, sub := range folders {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o700); err != nil {
			return "", err
		}
	}
	yamlPath := filepath.Join(base, "extra_model_paths.yaml")
	var sb strings.Builder
	sb.WriteString("freefolio:\n")
	fmt.Fprintf(&sb, "    base_path: %s\n", yamlSingleQuote(filepath.ToSlash(base)))
	for _, sub := range folders {
		fmt.Fprintf(&sb, "    %s: %s\n", sub, sub)
	}
	if err := fsutil.WriteFileAtomic(yamlPath, []byte(sb.String()), 0o600); err != nil {
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
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.stopLocked()
}

func (m *ComfyManager) stopLocked() {
	m.mu.Lock()
	cmd := m.cmd
	waitCh := m.waitCh
	pg := m.procGroup
	m.cmd = nil
	m.waitCh = nil
	m.procGroup = nil
	m.baseURL = ""
	m.dir = ""
	m.resourceDir = ""
	m.resolvedRoot = ""
	m.resolvedPython = ""
	m.caps = nil
	m.schemas = nil
	m.mu.Unlock()
	if pg != nil {
		pg.Kill()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if waitCh != nil {
		select {
		case <-waitCh:
		case <-time.After(3 * time.Second):
		}
	}
}

// Caps returns the most recently probed ComfyUI capabilities, or nil.
func (m *ComfyManager) Caps() *ComfyCapabilities {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.caps == nil {
		return nil
	}
	cp := *m.caps
	return &cp
}

// InvalidateCaps clears cached /object_info probe state so the next run or probe refreshes models.
func (m *ComfyManager) InvalidateCaps() {
	m.mu.Lock()
	m.caps = nil
	m.schemas = nil
	m.mu.Unlock()
}

func buildComfyArgs(layout ComfyLayout, port int, freefolioYAML string, goos string) []string {
	var args []string
	if layout.IsolatedPython {
		args = append(args, "-s")
	}
	args = append(args, layout.MainPy, "--listen", "127.0.0.1", "--port", strconv.Itoa(port), "--disable-auto-launch")
	if layout.BaseDir != "" {
		args = append(args, "--base-directory", layout.BaseDir)
	}
	if layout.UserDir != "" {
		args = append(args, "--user-directory", layout.UserDir)
	}
	if layout.InputDir != "" {
		args = append(args, "--input-directory", layout.InputDir)
	}
	if layout.OutputDir != "" {
		args = append(args, "--output-directory", layout.OutputDir)
	}
	var seenYAML []string
	appendYAML := func(p string) {
		p = strings.TrimSpace(p)
		if !isRegularFile(p) {
			return
		}
		for _, prev := range seenYAML {
			if samePath(prev, p, goos) {
				return
			}
		}
		seenYAML = append(seenYAML, p)
		args = append(args, "--extra-model-paths-config", p)
	}
	for _, p := range layout.ExtraModelPaths {
		appendYAML(p)
	}
	appendYAML(freefolioYAML)
	return args
}

func setEnvVar(env []string, key, value, goos string) []string {
	match := func(k string) bool {
		if goos == "windows" {
			return strings.EqualFold(k, key)
		}
		return k == key
	}
	for i, entry := range env {
		if k, _, ok := strings.Cut(entry, "="); ok && match(k) {
			env[i] = key + "=" + value
			return env
		}
	}
	return append(env, key+"="+value)
}

func buildComfyEnv(baseEnv []string, layout ComfyLayout, goos string) []string {
	env := slices.Clone(baseEnv)
	env = setEnvVar(env, "PYTHONUNBUFFERED", "1", goos)
	env = setEnvVar(env, "PYTHONUTF8", "1", goos)
	env = setEnvVar(env, "PYTHONIOENCODING", "utf-8", goos)
	if layout.IsolatedPython && layout.Python != "" {
		binDir := filepath.Dir(layout.Python)
		binName := filepath.Base(binDir)
		if (goos == "windows" && strings.EqualFold(binName, "Scripts")) || binName == "bin" {
			venvRoot := filepath.Dir(binDir)
			venvName := filepath.Base(venvRoot)
			if isRegularFile(filepath.Join(venvRoot, "pyvenv.cfg")) || venvName == ".venv" || venvName == "venv" || venvName == "env" {
				env = setEnvVar(env, "VIRTUAL_ENV", venvRoot, goos)
				curPath := ""
				for _, entry := range env {
					if k, v, ok := strings.Cut(entry, "="); ok && (k == "PATH" || (goos == "windows" && strings.EqualFold(k, "PATH"))) {
						curPath = v
						break
					}
				}
				if curPath == "" {
					env = setEnvVar(env, "PATH", binDir, goos)
				} else {
					env = setEnvVar(env, "PATH", binDir+string(os.PathListSeparator)+curPath, goos)
				}
			}
		}
	}
	return env
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
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			var requestErr *url.Error
			if errors.As(err, &requestErr) {
				dir, findErr := comfyAutoStartDirectory(base, cfg.Dir, defaultComfyDetectEnv())
				if findErr == nil {
					cfg.URL = ""
					cfg.Dir = dir
					return m.EnsureRunning(ctx, cfg)
				}
				return "", fmt.Errorf("cannot reach ComfyUI at %s: %w; %v", base, err, findErr)
			}
			return "", fmt.Errorf("cannot reach ComfyUI at %s: %w", base, err)
		}
		return base, nil
	}
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		return "", errors.New("ComfyUI path or URL is not configured")
	}

	m.startMu.Lock()
	defer m.startMu.Unlock()

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
			m.stopLocked()
			m.mu.Lock()
		}
	}
	m.mu.Unlock()

	m.stopLocked()

	layout, err := DetectComfyLayout(dir)
	if err != nil {
		return "", err
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return "", err
	}
	var yamlPath string
	if strings.TrimSpace(cfg.ResourceDir) != "" {
		yamlPath, err = WriteComfyExtraModelPaths(cfg.ResourceDir)
		if err != nil {
			return "", err
		}
	}
	args := buildComfyArgs(layout, port, yamlPath, runtime.GOOS)

	cmd := exec.Command(layout.Python, args...)
	cmd.Dir = layout.Root
	cmd.Env = buildComfyEnv(os.Environ(), layout, runtime.GOOS)
	configureComfyCmd(cmd)
	logs := &tailBuffer{max: 8192}
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start ComfyUI: %w", err)
	}
	pg := attachProcessGroup(cmd)
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
		close(waitCh)
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	m.mu.Lock()
	m.cmd = cmd
	m.waitCh = waitCh
	m.procGroup = pg
	m.baseURL = base
	m.dir = dir
	m.resourceDir = cfg.ResourceDir
	m.resolvedRoot = layout.Root
	m.resolvedPython = layout.Python
	m.mu.Unlock()

	deadline := time.NewTimer(120 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := m.ping(ctx, base); err == nil {
			return base, nil
		}
		select {
		case <-ctx.Done():
			m.stopLocked()
			return "", ctx.Err()
		case waitErr := <-waitCh:
			m.stopLocked()
			detail := logs.String()
			if detail != "" {
				return "", fmt.Errorf("ComfyUI exited early (%v): %s", waitErr, detail)
			}
			return "", fmt.Errorf("ComfyUI exited early: %v", waitErr)
		case <-deadline.C:
			m.stopLocked()
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
	m.mu.Lock()
	caps := &ComfyCapabilities{
		BaseURL:        base,
		ResolvedRoot:   m.resolvedRoot,
		ResolvedPython: m.resolvedPython,
	}
	m.mu.Unlock()
	if stats, err := m.getJSON(ctx, base+"/system_stats"); err == nil {
		var parsed struct {
			System struct {
				ComfyUIVersion string `json:"comfyui_version"`
			} `json:"system"`
			Devices []struct {
				Name      string `json:"name"`
				VRAMTotal int64  `json:"vram_total"`
			} `json:"devices"`
		}
		if json.Unmarshal(stats, &parsed) == nil {
			caps.Version = strings.TrimSpace(parsed.System.ComfyUIVersion)
			if len(parsed.Devices) > 0 {
				caps.Device = parsed.Devices[0].Name
				if parsed.Devices[0].VRAMTotal > 0 {
					caps.VRAMMiB = int(parsed.Devices[0].VRAMTotal / (1024 * 1024))
				}
			}
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
		Output []string `json:"output"`
	}
	if err := json.Unmarshal(rawInfo, &info); err != nil {
		return nil, err
	}
	caps.NodeTypes = map[string]bool{}
	caps.ModelFiles = map[string][]string{}
	for name := range info {
		caps.NodeTypes[name] = true
	}
	for _, loader := range []struct{ node, key, folder string }{{"UNETLoader", "unet_name", "diffusion_models"}, {"CLIPLoader", "clip_name", "text_encoders"}, {"VAELoader", "vae_name", "vae"}, {"LoraLoaderModelOnly", "lora_name", "loras"}, {"CheckpointLoaderSimple", "ckpt_name", "checkpoints"}, {"UpscaleModelLoader", "model_name", "upscale_models"}} {
		if node, ok := info[loader.node]; ok {
			caps.ModelFiles[loader.folder] = parseComfyChoiceList(node.Input.Required[loader.key])
		}
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
	if loader, ok := info["LoraLoaderModelOnly"]; ok {
		caps.LoRANative = true
		caps.LoRAModels = parseComfyChoiceList(loader.Input.Required["lora_name"])
	}
	hasAll := func(names ...string) bool {
		for _, name := range names {
			if _, ok := info[name]; !ok {
				return false
			}
		}
		return true
	}
	if hasAll("UNETLoader", "CLIPLoader", "VAELoader", "TextEncodeFibo2", "KSampler", "VAEDecode", "ImageToMask", "InvertMask", "JoinImageWithAlpha") {
		caps.RMBG3Native = true
	}
	if hasAll("UNETLoader", "VAELoader", "KSampler", "JoinImageWithAlpha", "ResizeImageMaskNode", "SeedVR2Preprocess", "SeedVR2Conditioning", "SeedVR2PostProcessing", "VAEEncodeTiled", "VAEDecodeTiled") {
		caps.SeedVR2Native = true
	}
	if hasAll("UNETLoader", "CLIPLoader", "VAELoader", "ModelSamplingAuraFlow", "CLIPTextEncode", "ConditioningZeroOut", "EmptySD3LatentImage", "KSampler", "VAEDecode") {
		caps.ZImageNative = true
	}
	if hasAll("UNETLoader", "CLIPLoader", "VAELoader", "TextEncodeQwenImage21", "QwenImage21Cache", "ManualSigmas", "KSamplerSelect", "RandomNoise", "CFGGuider", "SamplerCustomAdvanced", "EmptyLatentImage", "VAEDecode") {
		caps.QwenImageNative = true
	}
	if hasAll("UNETLoader", "CLIPLoader", "VAELoader", "TextEncodeQwenImage21", "QwenImage21Cache", "KSampler", "EmptyLatentImage", "VAEDecode") {
		caps.QwenImageBaseNative = true
	}
	if unet, ok := info["UNETLoader"]; ok {
		for _, name := range parseComfyChoiceList(unet.Input.Required["unet_name"]) {
			low := strings.ToLower(name)
			switch {
			case strings.Contains(low, "rmbg3") || strings.Contains(low, "rmbg-3"):
				caps.RMBG3Models = append(caps.RMBG3Models, name)
			case strings.Contains(low, "seedvr2"):
				caps.SeedVR2Models = append(caps.SeedVR2Models, name)
			case strings.Contains(low, "z_image") || strings.Contains(low, "z-image"):
				caps.ZImageModels = append(caps.ZImageModels, name)
			case strings.Contains(low, "qwen_image") || strings.Contains(low, "qwen-image"):
				caps.QwenImageModels = append(caps.QwenImageModels, name)
			}
		}
	}
	if clip, ok := info["CLIPLoader"]; ok {
		caps.TextEncoders = parseComfyChoiceList(clip.Input.Required["clip_name"])
	}
	if vae, ok := info["VAELoader"]; ok {
		caps.VAEModels = parseComfyChoiceList(vae.Input.Required["vae_name"])
	}
	schemas := map[string]comfyNodeSchema{}
	for _, key := range []string{
		"TextGenerateV2", "TextEncodeFibo2",
		"XJSa2VAImageSegmentationV2", "XJSa2VAImageSegmentation", "Sa2VANodeTpl",
		"ResizeImageMaskNode", "SeedVR2PostProcessing",
		"TextEncodeQwenImage21", "QwenImage21Cache",
	} {
		if node, ok := info[key]; ok {
			schemas[key] = comfyNodeSchema{
				Required: parseComfyRequiredDefaults(node.Input.Required),
				Output:   append([]string(nil), node.Output...),
			}
		}
	}
	for _, sa2vaKey := range []string{"XJSa2VAImageSegmentationV2", "XJSa2VAImageSegmentation", "Sa2VANodeTpl"} {
		if node, ok := info[sa2vaKey]; ok {
			caps.Sa2VANode = sa2vaKey
			caps.Sa2VAModels = parseComfyChoiceList(node.Input.Required["model_name"])
			break
		}
	}
	m.mu.Lock()
	m.caps = caps
	m.schemas = schemas
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

func parseComfyRequiredDefaults(req map[string][]json.RawMessage) map[string]any {
	out := map[string]any{}
	for k, spec := range req {
		if len(spec) == 0 {
			continue
		}
		var meta struct {
			Default any               `json:"default"`
			Options []json.RawMessage `json:"options"`
		}
		if len(spec) >= 2 && json.Unmarshal(spec[1], &meta) == nil {
			var kind string
			_ = json.Unmarshal(spec[0], &kind)
			if kind == "COMFY_DYNAMICCOMBO_V3" && len(meta.Options) > 0 {
				// V3 branch widgets are required dotted inputs in the prompt API.
				type dynamicOption struct {
					Key    string `json:"key"`
					Inputs struct {
						Required map[string][]json.RawMessage `json:"required"`
					} `json:"inputs"`
				}
				var options []dynamicOption
				for _, raw := range meta.Options {
					var option dynamicOption
					if json.Unmarshal(raw, &option) == nil {
						options = append(options, option)
					}
				}
				if len(options) > 0 {
					chosen := options[0]
					for _, option := range options {
						if option.Key == meta.Default {
							chosen = option
							break
						}
					}
					out[k] = chosen.Key
					for sub, value := range parseComfyRequiredDefaults(chosen.Inputs.Required) {
						out[k+"."+sub] = value
					}
				}
				continue
			}
			if meta.Default != nil {
				out[k] = meta.Default
				continue
			}
			if kind == "COMBO" && len(meta.Options) > 0 {
				var value any
				if json.Unmarshal(meta.Options[0], &value) == nil {
					out[k] = value
				}
				continue
			}
		}
		if choices := parseComfyChoiceList(spec); len(choices) > 0 {
			out[k] = choices[0]
		}
	}
	return out
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
func (m *ComfyManager) Cutout(ctx context.Context, cfg ComfyConfig, model string, input []byte, options ...ComfyOptions) ([]byte, error) {
	opts := firstComfyOptions(options)
	sensitivity := 1.0
	if opts.Sensitivity != nil {
		sensitivity = *opts.Sensitivity
	}
	if math.IsNaN(sensitivity) || math.IsInf(sensitivity, 0) || sensitivity < 0 || sensitivity > 1 || opts.MaskBlur < 0 || opts.MaskBlur > 64 || opts.MaskOffset < -64 || opts.MaskOffset > 64 {
		return nil, errors.New("invalid matting sensitivity or edge settings")
	}
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
			"sensitivity":   sensitivity,
			"process_res":   1024,
			"mask_blur":     opts.MaskBlur,
			"mask_offset":   opts.MaskOffset,
			"background":    "Alpha",
			"invert_output": false,
		}},
		"3": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"2", 0}, "filename_prefix": "freefolio_cutout"}},
	}
	return m.runImageWorkflow(ctx, base, graph)
}

const rmbg3SystemPrompt = `You are the prompt writer for an image matting model (RMBG 3.0). You receive a photo and, optionally, a short note from the user about what the subject is or what material it is made of.

Always answer with ONE paragraph (max 90 words) that instructs the image model to render a grayscale transparency map of the main subject:
- pure black background;
- near-white for every opaque part of the subject, naming them as they appear in this image;
- middle gray for see-through parts (glass, tulle, sheer fabric, liquid, smoke, mesh), dark gray only for the thinnest of them;
- the entire subject must stay present and opaque parts must not be turned transparent;
- keep continuous gray gradients inside the shape, and exclude the backdrop and any reflection or shadow on the ground.

Rules that override any other reading:
- A part the note calls transparent, translucent or glass maps to middle gray (dark gray where it is thinnest) - NEVER to near-white.
- Colour never implies opacity: coloured glass, coloured liquid and coloured mesh are still see-through and get gray, never white.
- Only parts that are solid in the note and in the photo (skin, fabric, petals, stems, metal, wood) get near-white.

If the note is empty, decide the opaque / see-through split from the image alone. If the note names a material or what the thing is, treat it as authoritative and let it override what you would otherwise guess.

Reply with the instruction only.`

func (m *ComfyManager) ensureCaps(ctx context.Context, cfg ComfyConfig) (*ComfyCapabilities, map[string]comfyNodeSchema, error) {
	m.mu.Lock()
	caps := m.caps
	schemas := m.schemas
	m.mu.Unlock()
	if caps != nil && schemas != nil {
		if caps.BaseURL != "" && m.ping(ctx, caps.BaseURL) == nil {
			return caps, schemas, nil
		}
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		m.InvalidateCaps()
	}
	probed, err := m.Probe(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	m.mu.Lock()
	schemas = m.schemas
	m.mu.Unlock()
	return probed, schemas, nil
}

func (m *ComfyManager) prepareCutoutUpload(ctx context.Context, cfg ComfyConfig, input []byte) (string, *ComfyCapabilities, map[string]comfyNodeSchema, string, error) {
	if len(input) > maxInput {
		return "", nil, nil, "", errors.New("input image exceeds 64 MiB")
	}
	img, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return "", nil, nil, "", err
	}
	var pngBuf bytes.Buffer
	if err := writeNormalizedPNGTo(&pngBuf, img); err != nil {
		return "", nil, nil, "", err
	}
	caps, schemas, err := m.ensureCaps(ctx, cfg)
	if err != nil {
		return "", nil, nil, "", err
	}
	base := caps.BaseURL
	if base == "" {
		base, err = m.EnsureRunning(ctx, cfg)
		if err != nil {
			return "", nil, nil, "", err
		}
	}
	uploaded, err := m.uploadImage(ctx, base, pngBuf.Bytes())
	if err != nil {
		return "", nil, nil, "", err
	}
	return base, caps, schemas, uploaded, nil
}

func pickComfyModel(preferred []string, available []string, contains ...string) string {
	for _, pref := range preferred {
		lowPref := strings.ToLower(pref)
		for _, cand := range available {
			if strings.EqualFold(filepath.Base(filepath.FromSlash(cand)), pref) || strings.ToLower(cand) == lowPref {
				return cand
			}
		}
	}
	for _, cand := range available {
		low := strings.ToLower(cand)
		ok := len(contains) > 0
		for _, sub := range contains {
			if !strings.Contains(low, sub) {
				ok = false
				break
			}
		}
		if ok {
			return cand
		}
	}
	return ""
}

func cloneInputsMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// CutoutRMBG3 runs BRIA RMBG-3.0 using ComfyUI's native Fibo2 core nodes.
func (m *ComfyManager) CutoutRMBG3(ctx context.Context, cfg ComfyConfig, subjectNote string, input []byte, options ...ComfyOptions) ([]byte, error) {
	opts := firstComfyOptions(options)
	seed, steps, guidance, optionErr := opts.sampling(42, 4, 1)
	if optionErr != nil {
		return nil, optionErr
	}
	base, caps, schemas, uploaded, err := m.prepareCutoutUpload(ctx, cfg, input)
	if err != nil {
		return nil, err
	}
	if !caps.RMBG3Native {
		return nil, errors.New("ComfyUI does not expose native RMBG-3.0 nodes (TextEncodeFibo2); update ComfyUI to 0.4.60 or newer")
	}
	unetName := pickComfyModel([]string{"RMBG-3.0-fp8.safetensors", "rmbg3_int8_convrot.safetensors", "rmbg3_bf16.safetensors"}, caps.RMBG3Models, "rmbg")
	if unetName == "" {
		return nil, errors.New("BRIA RMBG-3.0 diffusion model not found in ComfyUI; download it in Settings -> Optional Resources")
	}
	clipName := pickComfyModel([]string{"qwen3vl_4b_int8_convrot.safetensors", "qwen3vl_4b_bf16.safetensors"}, caps.TextEncoders, "qwen3vl")
	if clipName == "" {
		return nil, errors.New("Qwen3-VL text encoder for RMBG-3.0 not found in ComfyUI; download the BRIA RMBG-3.0 pack in Settings -> Optional Resources")
	}
	vaeName := pickComfyModel([]string{"flux2-vae.safetensors"}, caps.VAEModels, "flux2", "vae")
	if vaeName == "" {
		return nil, errors.New("Flux2 VAE not found in ComfyUI; download the BRIA RMBG-3.0 pack in Settings -> Optional Resources")
	}

	subjectNote = strings.TrimSpace(subjectNote)
	fiboInputs := cloneInputsMap(schemas["TextEncodeFibo2"].Required)
	fiboInputs["clip"] = []any{"3", 0}
	fiboInputs["vae"] = []any{"4", 0}
	fiboInputs["images.image_1"] = []any{"1", 0}
	fiboInputs["negative_prompt"] = ""
	fiboInputs["width"] = 0
	fiboInputs["height"] = 0
	fiboInputs["batch_size"] = 1

	graph := map[string]any{
		"1": map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": uploaded}},
		"2": map[string]any{"class_type": "UNETLoader", "inputs": map[string]any{"unet_name": unetName, "weight_dtype": "default"}},
		"3": map[string]any{"class_type": "CLIPLoader", "inputs": map[string]any{"clip_name": clipName, "type": "fibo2", "device": "default"}},
		"4": map[string]any{"class_type": "VAELoader", "inputs": map[string]any{"vae_name": vaeName}},
	}
	if genSchema, hasGen := schemas["TextGenerateV2"]; hasGen {
		graph["5"] = map[string]any{"class_type": "CLIPLoader", "inputs": map[string]any{"clip_name": clipName, "type": "stable_diffusion", "device": "default"}}
		genInputs := cloneInputsMap(genSchema.Required)
		genInputs["clip"] = []any{"5", 0}
		genInputs["images.image1"] = []any{"1", 0}
		genInputs["system_prompt"] = rmbg3SystemPrompt
		genInputs["prompt"] = subjectNote
		if _, ok := genInputs["max_length"]; !ok {
			genInputs["max_length"] = 512
		}
		if _, ok := genInputs["sampling_mode"]; !ok {
			genInputs["sampling_mode"] = "on"
		}
		if _, ok := genInputs["use_default_template"]; !ok {
			genInputs["use_default_template"] = true
		}
		if _, ok := genInputs["mtp"]; !ok {
			genInputs["mtp"] = "auto"
		}
		graph["6"] = map[string]any{"class_type": "TextGenerateV2", "inputs": genInputs}
		fiboInputs["prompt"] = []any{"6", 0}
	} else {
		directPrompt := "Render a grayscale transparency matte of the main subject on a pure black background with continuous soft alpha gradients for hair, fur and translucent areas."
		if subjectNote != "" {
			directPrompt += " Subject note: " + subjectNote
		}
		fiboInputs["prompt"] = directPrompt
	}
	graph["7"] = map[string]any{"class_type": "TextEncodeFibo2", "inputs": fiboInputs}
	graph["8"] = map[string]any{"class_type": "KSampler", "inputs": map[string]any{
		"model":        []any{"2", 0},
		"positive":     []any{"7", 0},
		"negative":     []any{"7", 1},
		"latent_image": []any{"7", 2},
		"seed":         seed,
		"steps":        steps,
		"cfg":          guidance,
		"sampler_name": "euler",
		"scheduler":    "simple",
		"denoise":      1.0,
	}}
	graph["9"] = map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"8", 0}, "vae": []any{"4", 0}}}
	graph["10"] = map[string]any{"class_type": "ImageToMask", "inputs": map[string]any{"image": []any{"9", 0}, "channel": "red"}}
	graph["11"] = map[string]any{"class_type": "InvertMask", "inputs": map[string]any{"mask": []any{"10", 0}}}
	graph["12"] = map[string]any{"class_type": "JoinImageWithAlpha", "inputs": map[string]any{"image": []any{"1", 0}, "alpha": []any{"11", 0}}}
	graph["13"] = map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"12", 0}, "filename_prefix": "freefolio_rmbg3"}}
	return m.runImageWorkflow(ctx, base, graph)
}

func sa2vaNeeds8Bit(modelName string, vramMiB int) bool {
	if vramMiB > 16384 {
		return false
	}
	low := strings.ToLower(modelName)
	return strings.Contains(low, "4b") || strings.Contains(low, "7b") || strings.Contains(low, "8b") || strings.Contains(low, "14b")
}

// CutoutSa2VA runs text-prompted referring segmentation via ComfyUI-Sa2VA-XJ or ComfyUI-Sa2VA.
func (m *ComfyManager) CutoutSa2VA(ctx context.Context, cfg ComfyConfig, modelName string, prompt string, input []byte, options ...ComfyOptions) ([]byte, error) {
	opts := firstComfyOptions(options)
	threshold := 0.5
	if opts.MaskThreshold != nil {
		threshold = *opts.MaskThreshold
	}
	if !validUnitValue(threshold) {
		return nil, errors.New("mask threshold must be between 0 and 1")
	}
	if strings.TrimSpace(modelName) == "" {
		return nil, errors.New("Sa2VA model is required")
	}
	base, caps, schemas, uploaded, err := m.prepareCutoutUpload(ctx, cfg, input)
	if err != nil {
		return nil, err
	}
	if caps.Sa2VANode == "" {
		return nil, errors.New("Sa2VA node not installed in ComfyUI; install ComfyUI-Sa2VA-XJ or ComfyUI-Sa2VA")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "Please provide segmentation masks for the main subject."
	}
	use8Bit := sa2vaNeeds8Bit(modelName, caps.VRAMMiB)
	schema := schemas[caps.Sa2VANode]
	nodeInputs := cloneInputsMap(schema.Required)
	nodeInputs["model_name"] = modelName
	nodeInputs["image"] = []any{"1", 0}
	nodeInputs["segmentation_prompt"] = prompt
	nodeInputs["use_flash_attn"] = false
	if caps.Sa2VANode == "Sa2VANodeTpl" {
		nodeInputs["mask_threshold"] = threshold
		nodeInputs["use_8bit_quantization"] = use8Bit
	} else {
		nodeInputs["threshold"] = threshold
		nodeInputs["use_8bit"] = use8Bit
		nodeInputs["unload"] = true
		if _, hasDetail := nodeInputs["process_detail"]; hasDetail {
			// Avoid unexpected Hugging Face auto-downloads when ViTMatte is not installed.
			nodeInputs["process_detail"] = false
		}
	}
	maskOutIdx := 1
	for i, typ := range schema.Output {
		if strings.EqualFold(typ, "MASK") {
			maskOutIdx = i
			break
		}
	}
	graph := map[string]any{
		"1": map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": uploaded}},
		"2": map[string]any{"class_type": caps.Sa2VANode, "inputs": nodeInputs},
		"3": map[string]any{"class_type": "InvertMask", "inputs": map[string]any{"mask": []any{"2", maskOutIdx}}},
		"4": map[string]any{"class_type": "JoinImageWithAlpha", "inputs": map[string]any{"image": []any{"1", 0}, "alpha": []any{"3", 0}}},
		"5": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"4", 0}, "filename_prefix": "freefolio_sa2va"}},
	}
	return m.runImageWorkflow(ctx, base, graph)
}

// ComfyImageRequest configures text-to-image or reference-image editing via ComfyUI.
type ComfyImageRequest struct {
	Options     ComfyOptions
	BaseModel   string
	Prompt      string
	Size        string
	AspectRatio string
	Count       int
	Images      [][]byte
	LoRA        string
	Strength    float64
	Enhance     bool
	LoRAs       []ComfyLoRA
}

// ComfyLoRA is an ordered model-only patch in a native workflow.
type ComfyLoRA struct {
	ID       string
	Strength float64
}

func randomComfySeed() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		// Keep within signed 53-bit JSON-safe integer range for ComfyUI widgets.
		return (binary.LittleEndian.Uint64(b[:]) & ((uint64(1) << 50) - 1)) | 1
	}
	return uint64(time.Now().UnixNano()&((1<<50)-1)) | 1
}

func alignDim(v, mult, minV, maxV int) int {
	if mult <= 1 {
		mult = 16
	}
	v = max(minV, min(maxV, v))
	v = ((v + mult/2) / mult) * mult
	if v < minV {
		v = minV
	}
	if v > maxV {
		v = maxV
	}
	return v
}

func resolveComfyImageSize(size, aspect string, refImg []byte, mult int) (int, int) {
	if mult <= 0 {
		mult = 16
	}
	if raw := strings.TrimSpace(strings.ToLower(size)); raw != "" {
		raw = strings.ReplaceAll(raw, "×", "x")
		if left, right, ok := strings.Cut(raw, "x"); ok {
			w, errW := strconv.Atoi(strings.TrimSpace(left))
			h, errH := strconv.Atoi(strings.TrimSpace(right))
			if errW == nil && errH == nil && w > 0 && h > 0 {
				return alignDim(w, mult, 512, 2048), alignDim(h, mult, 512, 2048)
			}
		}
	}
	aspect = strings.TrimSpace(aspect)
	switch aspect {
	case "16:9":
		return alignDim(1344, mult, 512, 2048), alignDim(768, mult, 512, 2048)
	case "9:16":
		return alignDim(768, mult, 512, 2048), alignDim(1344, mult, 512, 2048)
	case "4:3":
		return alignDim(1152, mult, 512, 2048), alignDim(896, mult, 512, 2048)
	case "3:4":
		return alignDim(896, mult, 512, 2048), alignDim(1152, mult, 512, 2048)
	case "1:1":
		return 1024, 1024
	}
	if len(refImg) > 0 {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(refImg)); err == nil && cfg.Width > 0 && cfg.Height > 0 {
			// Scale both axes with one factor before aligning to the latent grid.
			factor := math.Min(1, 2048/float64(max(cfg.Width, cfg.Height)))
			return alignDim(int(math.Round(float64(cfg.Width)*factor)), mult, mult, 2048), alignDim(int(math.Round(float64(cfg.Height)*factor)), mult, mult, 2048)
		}
	}
	return 1024, 1024
}

func (m *ComfyManager) encodeAndUploadPNG(ctx context.Context, base string, data []byte) (string, int, int, error) {
	if len(data) > maxInput {
		return "", 0, 0, errors.New("input image exceeds 64 MiB")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return "", 0, 0, errors.New("empty input image")
	}
	var pngBuf bytes.Buffer
	if err := writeNormalizedPNGTo(&pngBuf, img); err != nil {
		return "", 0, 0, err
	}
	uploaded, err := m.uploadImage(ctx, base, pngBuf.Bytes())
	if err != nil {
		return "", 0, 0, err
	}
	return uploaded, w, h, nil
}

// UpscaleSeedVR2 runs ByteDance SeedVR2 generative restoration/upscaling via ComfyUI native nodes.
func (m *ComfyManager) UpscaleSeedVR2(ctx context.Context, cfg ComfyConfig, scale int, input []byte) ([]byte, error) {
	return m.UpscaleSeedVR2Model(ctx, cfg, "seedvr2-7b-sharp-int8", scale, input)
}

// UpscaleSeedVR2Model honors the chosen variant instead of preferring 7B.
func (m *ComfyManager) UpscaleSeedVR2Model(ctx context.Context, cfg ComfyConfig, modelID string, scale int, input []byte, options ...ComfyOptions) ([]byte, error) {
	opts := firstComfyOptions(options)
	seed, steps, guidance, optionErr := opts.sampling(42, 1, 1)
	if optionErr != nil {
		return nil, optionErr
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
	caps, schemas, err := m.ensureCaps(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if !caps.SeedVR2Native {
		return nil, errors.New("ComfyUI does not expose native SeedVR2 nodes; update ComfyUI first")
	}
	pack, ok := ComfyPack(modelID)
	if !ok || pack.Kind != "seedvr2" {
		return nil, fmt.Errorf("unknown SeedVR2 model: %s", modelID)
	}
	unetName := ""
	for _, name := range caps.SeedVR2Models {
		if name == filepath.Base(pack.Files[0].RelPath) {
			unetName = name
			break
		}
	}
	if unetName == "" {
		return nil, errors.New("SeedVR2 diffusion model not found in ComfyUI; download it in Settings -> Optional Resources")
	}
	vaeName := pickComfyModel([]string{"seedvr2_ema_vae_fp16.safetensors", "ema_vae_fp16.safetensors"}, caps.VAEModels, "seedvr2", "vae")
	if vaeName == "" {
		vaeName = pickComfyModel(nil, caps.VAEModels, "ema_vae")
	}
	if vaeName == "" {
		return nil, errors.New("SeedVR2 VAE not found in ComfyUI; download the SeedVR2 pack in Settings -> Optional Resources")
	}
	base := caps.BaseURL
	if base == "" {
		base, err = m.EnsureRunning(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	uploaded, err := m.uploadImage(ctx, base, pngBuf.Bytes())
	if err != nil {
		return nil, err
	}

	resizeInputs := cloneInputsMap(schemas["ResizeImageMaskNode"].Required)
	resizeInputs["input"] = []any{"2", 0}
	resizeInputs["resize_type"] = "scale by multiplier"
	resizeInputs["resize_type.multiplier"] = float64(scale)
	resizeInputs["scale_method"] = "lanczos"

	postInputs := cloneInputsMap(schemas["SeedVR2PostProcessing"].Required)
	postInputs["images"] = []any{"10", 0}
	postInputs["original_resized_images"] = []any{"3", 0}
	colorMethod := opts.ColorCorrection
	if colorMethod == "" {
		colorMethod = "none"
	}
	if !slices.Contains([]string{"none", "lab", "wavelet", "adain"}, colorMethod) {
		return nil, errors.New("unknown color correction method")
	}
	postInputs["color_correction_method"] = colorMethod

	graph := map[string]any{
		"1": map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": uploaded}},
		"2": map[string]any{"class_type": "JoinImageWithAlpha", "inputs": map[string]any{"image": []any{"1", 0}, "alpha": []any{"1", 1}}},
		"3": map[string]any{"class_type": "ResizeImageMaskNode", "inputs": resizeInputs},
		"4": map[string]any{"class_type": "SeedVR2Preprocess", "inputs": map[string]any{"resized_images": []any{"3", 0}}},
		"5": map[string]any{"class_type": "VAELoader", "inputs": map[string]any{"vae_name": vaeName}},
		"6": map[string]any{"class_type": "UNETLoader", "inputs": map[string]any{"unet_name": unetName, "weight_dtype": "default"}},
		"7": map[string]any{"class_type": "VAEEncodeTiled", "inputs": map[string]any{
			"pixels":           []any{"4", 0},
			"vae":              []any{"5", 0},
			"tile_size":        512,
			"overlap":          128,
			"temporal_size":    4096,
			"temporal_overlap": 8,
		}},
		"8": map[string]any{"class_type": "SeedVR2Conditioning", "inputs": map[string]any{"model": []any{"6", 0}, "vae_conditioning": []any{"7", 0}}},
		"9": map[string]any{"class_type": "KSampler", "inputs": map[string]any{
			"model":        []any{"6", 0},
			"positive":     []any{"8", 0},
			"negative":     []any{"8", 1},
			"latent_image": []any{"7", 0},
			"seed":         seed,
			"steps":        steps,
			"cfg":          guidance,
			"sampler_name": "euler",
			"scheduler":    "simple",
			"denoise":      1.0,
		}},
		"10": map[string]any{"class_type": "VAEDecodeTiled", "inputs": map[string]any{
			"samples":          []any{"9", 0},
			"vae":              []any{"5", 0},
			"tile_size":        512,
			"overlap":          128,
			"temporal_size":    4096,
			"temporal_overlap": 8,
		}},
		"11": map[string]any{"class_type": "SeedVR2PostProcessing", "inputs": postInputs},
		"12": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"11", 0}, "filename_prefix": "freefolio_seedvr2"}},
	}
	return m.runImageWorkflow(ctx, base, graph)
}

// GenerateZImageTurbo generates images with Z-Image-Turbo (INT8) via ComfyUI native core nodes.
func (m *ComfyManager) GenerateZImageTurbo(ctx context.Context, cfg ComfyConfig, req ComfyImageRequest) ([][]byte, error) {
	seed, steps, guidance, optionErr := req.Options.sampling(randomComfySeed(), 8, 1)
	if optionErr != nil {
		return nil, optionErr
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}
	if len(req.Images) > 0 {
		return nil, errors.New("Z-Image-Turbo only supports text-to-image; use Qwen-Image 2.1 Turbo for reference image editing")
	}
	count := max(1, min(req.Count, 4))
	caps, _, err := m.ensureCaps(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if !caps.ZImageNative {
		return nil, errors.New("ComfyUI does not expose native Z-Image nodes; update ComfyUI first")
	}
	loras, err := prepareLoRAChain(cfg, caps, &req, "zimage")
	if err != nil {
		return nil, err
	}
	prompt = strings.TrimSpace(req.Prompt)
	unetName := pickComfyModel([]string{"z_image_turbo_int8_convrot.safetensors", "z_image_turbo_bf16.safetensors"}, caps.ZImageModels, "z_image_turbo")
	if unetName == "" {
		return nil, errors.New("Z-Image-Turbo diffusion model not found in ComfyUI; download it in Settings -> Optional Resources")
	}
	clipName := pickComfyModel([]string{"qwen_3_4b_fp8_mixed.safetensors", "qwen_3_4b.safetensors"}, caps.TextEncoders, "qwen_3_4b")
	if clipName == "" {
		return nil, errors.New("Qwen3-4B text encoder for Z-Image-Turbo not found in ComfyUI; download the Z-Image-Turbo pack in Settings -> Optional Resources")
	}
	vaeName := pickComfyModel([]string{"ae.safetensors"}, caps.VAEModels, "ae.safetensors")
	if vaeName == "" {
		return nil, errors.New("VAE (ae.safetensors) for Z-Image-Turbo not found in ComfyUI; download the Z-Image-Turbo pack in Settings -> Optional Resources")
	}
	base := caps.BaseURL
	if base == "" {
		base, err = m.EnsureRunning(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	w, h := resolveComfyImageSize(req.Size, req.AspectRatio, nil, 16)
	graph := map[string]any{
		"1": map[string]any{"class_type": "UNETLoader", "inputs": map[string]any{"unet_name": unetName, "weight_dtype": "default"}},
		"2": map[string]any{"class_type": "ModelSamplingAuraFlow", "inputs": map[string]any{"model": []any{"1", 0}, "shift": 3}},
		"3": map[string]any{"class_type": "CLIPLoader", "inputs": map[string]any{"clip_name": clipName, "type": "lumina2", "device": "default"}},
		"4": map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{"clip": []any{"3", 0}, "text": prompt}},
		"5": map[string]any{"class_type": "ConditioningZeroOut", "inputs": map[string]any{"conditioning": []any{"4", 0}}},
		"6": map[string]any{"class_type": "VAELoader", "inputs": map[string]any{"vae_name": vaeName}},
		"7": map[string]any{"class_type": "EmptySD3LatentImage", "inputs": map[string]any{"width": w, "height": h, "batch_size": count}},
		"8": map[string]any{"class_type": "KSampler", "inputs": map[string]any{
			"model":        []any{"2", 0},
			"positive":     []any{"4", 0},
			"negative":     []any{"5", 0},
			"latent_image": []any{"7", 0},
			"seed":         seed,
			"steps":        steps,
			"cfg":          guidance,
			"sampler_name": "res_multistep",
			"scheduler":    "simple",
			"denoise":      1.0,
		}},
		"9":  map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"8", 0}, "vae": []any{"6", 0}}},
		"10": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"9", 0}, "filename_prefix": "freefolio_zimage"}},
	}
	if negative := req.Options.negative(""); negative != "" {
		graph["5"] = map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{"clip": []any{"3", 0}, "text": negative}}
	}
	graph["2"].(map[string]any)["inputs"].(map[string]any)["model"] = attachLoRAChain(graph, loras)
	return m.runImagesWorkflow(ctx, base, graph)
}

// GenerateQwenImage21Turbo runs text-to-image or multi-image editing via Qwen-Image 2.1 Turbo native nodes.
func (m *ComfyManager) GenerateQwenImage21Turbo(ctx context.Context, cfg ComfyConfig, req ComfyImageRequest) ([][]byte, error) {
	req.BaseModel = "qwen-image-2.1-turbo-int8"
	return m.GenerateQwenImage21(ctx, cfg, req)
}

// GenerateQwenImage21 keeps ordinary and distilled sampling paths distinct.
func (m *ComfyManager) GenerateQwenImage21(ctx context.Context, cfg ComfyConfig, req ComfyImageRequest) ([][]byte, error) {
	seed, steps, guidance, optionErr := req.Options.sampling(randomComfySeed(), 25, 1)
	if optionErr != nil {
		return nil, optionErr
	}
	if req.BaseModel == "" {
		req.BaseModel = "qwen-image-2.1-turbo-int8"
	}
	turbo := req.BaseModel == "qwen-image-2.1-turbo-int8"
	if turbo && req.Options.Steps != 0 && req.Options.Steps != 8 {
		return nil, errors.New("Qwen Turbo uses its fixed 8-step sigma schedule")
	}
	if !turbo && req.BaseModel != "qwen-image-2.1-int8" {
		return nil, errors.New("unknown Qwen Image 2.1 base model")
	}
	if req.Enhance {
		if len(req.Images) != 1 {
			return nil, errors.New("image enhancement requires one input image")
		}
		data, err := enhancementInput(req.Images[0])
		if err != nil {
			return nil, err
		}
		req.Images = [][]byte{data}
		req.Count = 1
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}
	count := max(1, min(req.Count, 4))
	caps, schemas, err := m.ensureCaps(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if turbo && !caps.QwenImageNative || !turbo && !caps.QwenImageBaseNative {
		return nil, errors.New("ComfyUI does not expose native Qwen-Image 2.1 nodes; update ComfyUI first")
	}
	loras, err := prepareLoRAChain(cfg, caps, &req, "qwen")
	if err != nil {
		return nil, err
	}
	prompt = strings.TrimSpace(req.Prompt)
	unetName := pickComfyModel([]string{"qwen_image_2.1_turbo_int8_convrot.safetensors", "qwen_image_2.1_turbo_bf16.safetensors"}, caps.QwenImageModels, "qwen_image_2.1_turbo")
	if !turbo {
		// Do not fall back to a Turbo weight when the ordinary model is missing.
		unetName = ""
		for _, want := range []string{"qwen_image_2.1_int8_convrot.safetensors", "qwen_image_2.1_bf16.safetensors"} {
			if slices.Contains(caps.QwenImageModels, want) {
				unetName = want
				break
			}
		}
	}
	if unetName == "" {
		return nil, fmt.Errorf("selected Qwen Image 2.1 model not found: %s", req.BaseModel)
	}
	clipName := pickComfyModel([]string{"qwen3vl_8b_int8_convrot.safetensors", "qwen3vl_8b_w4a8.safetensors", "qwen3vl_8b_bf16.safetensors"}, caps.TextEncoders, "qwen3vl_8b")
	for _, adapter := range req.LoRAs {
		if adapter.Strength == 0 {
			continue
		}
		if pack, ok := ComfyPack(adapter.ID); ok && pack.TextEncoder != "" {
			clipName = pack.TextEncoder
		}
	}
	if req.LoRA != "" && req.Strength != 0 {
		if pack, ok := ComfyPack(req.LoRA); ok && pack.TextEncoder != "" {
			clipName = pack.TextEncoder
		}
	}
	if !slices.Contains(caps.TextEncoders, clipName) {
		return nil, fmt.Errorf("required Qwen text encoder is missing: %s", clipName)
	}
	if clipName == "" {
		return nil, errors.New("Qwen3-VL-8B text encoder not found in ComfyUI; download the Qwen-Image 2.1 Turbo pack in Settings -> Optional Resources")
	}
	vaeName := pickComfyModel([]string{"qwen_image_2.1_vae_bf16.safetensors"}, caps.VAEModels, "qwen_image_2.1")
	if vaeName == "" {
		return nil, errors.New("Qwen-Image 2.1 VAE not found in ComfyUI; download the Qwen-Image 2.1 Turbo pack in Settings -> Optional Resources")
	}
	base := caps.BaseURL
	if base == "" {
		base, err = m.EnsureRunning(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}

	var firstRef []byte
	if len(req.Images) > 0 {
		firstRef = req.Images[0]
	}
	w, h := resolveComfyImageSize(req.Size, req.AspectRatio, firstRef, 32)

	cacheInputs := cloneInputsMap(schemas["QwenImage21Cache"].Required)
	cacheInputs["model"] = []any{"1", 0}
	cacheInputs["device"] = "auto"
	cacheInputs["dtype"] = "default"

	encInputs := cloneInputsMap(schemas["TextEncodeQwenImage21"].Required)
	encInputs["clip"] = []any{"3", 0}
	encInputs["prompt"] = prompt
	encInputs["negative_prompt"] = req.Options.negative("")
	encInputs["resolution"] = 1024

	graph := map[string]any{
		"1": map[string]any{"class_type": "UNETLoader", "inputs": map[string]any{"unet_name": unetName, "weight_dtype": "default"}},
		"2": map[string]any{"class_type": "QwenImage21Cache", "inputs": cacheInputs},
		"3": map[string]any{"class_type": "CLIPLoader", "inputs": map[string]any{"clip_name": clipName, "type": "qwen_image", "device": "default"}},
		"4": map[string]any{"class_type": "VAELoader", "inputs": map[string]any{"vae_name": vaeName}},
	}
	cacheInputs["model"] = attachLoRAChain(graph, loras)
	followInput := len(req.Images) > 0 && (strings.TrimSpace(req.Size) == "" || req.Size == "auto") && strings.TrimSpace(req.AspectRatio) == ""
	if req.Enhance || followInput {
		encInputs["resolution"] = 0
	}
	if len(req.Images) > 0 {
		encInputs["vae"] = []any{"4", 0}
		limit := min(len(req.Images), 8)
		for i := range limit {
			uploaded, _, _, err := m.encodeAndUploadPNG(ctx, base, req.Images[i])
			if err != nil {
				return nil, err
			}
			loadID := fmt.Sprintf("img_%d", i+1)
			graph[loadID] = map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": uploaded}}
			ref := []any{loadID, 0}
			if followInput && !req.Enhance {
				rw, rh := resolveComfyImageSize("", "", req.Images[i], 32)
				cfg, _, err := image.DecodeConfig(bytes.NewReader(req.Images[i]))
				if err != nil {
					return nil, err
				}
				if float64(cfg.Width)/float64(cfg.Height) > 64 || float64(cfg.Height)/float64(cfg.Width) > 64 {
					return nil, errors.New("reference image aspect ratio exceeds the local model size range")
				}
				if rw != cfg.Width || rh != cfg.Height {
					scaleID := fmt.Sprintf("ref_size_%d", i+1)
					graph[scaleID] = map[string]any{"class_type": "ImageScale", "inputs": map[string]any{"image": ref, "upscale_method": "lanczos", "width": rw, "height": rh, "crop": "disabled"}}
					ref = []any{scaleID, 0}
				}
			}
			encInputs[fmt.Sprintf("images.image_%d", i+1)] = ref
		}
	}
	graph["5"] = map[string]any{"class_type": "TextEncodeQwenImage21", "inputs": encInputs}

	var latentRef []any
	if req.Enhance || followInput && count == 1 {
		latentRef = []any{"5", 2}
	} else {
		graph["6"] = map[string]any{"class_type": "EmptyLatentImage", "inputs": map[string]any{"width": w, "height": h, "batch_size": count}}
		latentRef = []any{"6", 0}
	}

	graph["7"] = map[string]any{"class_type": "CFGGuider", "inputs": map[string]any{"model": []any{"2", 0}, "positive": []any{"5", 0}, "negative": []any{"5", 1}, "cfg": guidance}}
	graph["8"] = map[string]any{"class_type": "ManualSigmas", "inputs": map[string]any{"sigmas": "1.0, 0.978453, 0.95418, 0.926626, 0.89508, 0.845148, 0.704534, 0.414568, 0"}}
	graph["9"] = map[string]any{"class_type": "KSamplerSelect", "inputs": map[string]any{"sampler_name": "euler"}}
	graph["10"] = map[string]any{"class_type": "RandomNoise", "inputs": map[string]any{"noise_seed": seed}}
	graph["11"] = map[string]any{"class_type": "SamplerCustomAdvanced", "inputs": map[string]any{
		"noise":        []any{"10", 0},
		"guider":       []any{"7", 0},
		"sampler":      []any{"9", 0},
		"sigmas":       []any{"8", 0},
		"latent_image": latentRef,
	}}
	if !turbo {
		for _, id := range []string{"7", "8", "9", "10"} {
			delete(graph, id)
		}
		graph["11"] = map[string]any{"class_type": "KSampler", "inputs": map[string]any{"model": []any{"2", 0}, "positive": []any{"5", 0}, "negative": []any{"5", 1}, "latent_image": latentRef, "seed": seed, "steps": steps, "cfg": guidance, "sampler_name": "euler", "scheduler": "simple", "denoise": 1.0}}
	}
	graph["12"] = map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"11", 0}, "vae": []any{"4", 0}}}
	graph["13"] = map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"12", 0}, "filename_prefix": "freefolio_qwen21"}}
	return m.runImagesWorkflow(ctx, base, graph)
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
	imgs, err := m.runImagesWorkflow(ctx, base, graph)
	if err != nil {
		return nil, err
	}
	return imgs[0], nil
}

func (m *ComfyManager) runImagesWorkflow(ctx context.Context, base string, graph map[string]any) ([][]byte, error) {
	files, err := m.runFilesWorkflow(ctx, base, graph)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, file := range files {
		out = append(out, file.Data)
	}
	return out, nil
}

func (m *ComfyManager) runFilesWorkflow(ctx context.Context, base string, graph map[string]any) ([]ComfyOutput, error) {
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
					StatusStr string              `json:"status_str"`
					Completed bool                `json:"completed"`
					Messages  [][]json.RawMessage `json:"messages"`
				} `json:"status"`
				Outputs map[string]struct {
					Images []comfyFileRef `json:"images"`
					Audio  []comfyFileRef `json:"audio"`
					Videos []comfyFileRef `json:"videos"`
					Gifs   []comfyFileRef `json:"gifs"`
				} `json:"outputs"`
			}
			if err := json.Unmarshal(rawHist, &hist); err != nil {
				return nil, fmt.Errorf("invalid ComfyUI history: %w", err)
			}
			item, ok := hist[queued.PromptID]
			if !ok {
				continue
			}
			if item.Status.StatusStr == "error" {
				return nil, formatComfyExecutionError(item.Status.Messages)
			}
			var results []ComfyOutput
			for _, out := range item.Outputs {
				for _, refs := range [][]comfyFileRef{out.Images, out.Audio, out.Videos, out.Gifs} {
					for _, img := range refs {
						q := url.Values{}
						q.Set("filename", img.Filename)
						q.Set("subfolder", img.Subfolder)
						if img.Type == "" {
							img.Type = "output"
						}
						q.Set("type", img.Type)
						data, err := m.getBytes(ctx, base+"/view?"+q.Encode())
						if err != nil {
							return nil, err
						}
						results = append(results, ComfyOutput{Filename: img.Filename, Data: data})
					}
				}
			}
			if len(results) > 0 {
				return results, nil
			}
			if item.Status.Completed {
				return nil, errors.New("ComfyUI workflow completed with no output file")
			}
		}
	}
}

func formatComfyExecutionError(messages [][]json.RawMessage) error {
	for _, entry := range messages {
		if len(entry) < 2 {
			continue
		}
		var kind string
		if json.Unmarshal(entry[0], &kind) != nil || kind != "execution_error" {
			continue
		}
		var detail struct {
			NodeType         string `json:"node_type"`
			ExceptionMessage string `json:"exception_message"`
		}
		if json.Unmarshal(entry[1], &detail) != nil {
			continue
		}
		msg := strings.TrimSpace(detail.ExceptionMessage)
		nodeType := strings.TrimSpace(detail.NodeType)
		switch {
		case nodeType != "" && msg != "":
			return fmt.Errorf("ComfyUI workflow execution failed (%s): %s", nodeType, msg)
		case msg != "":
			return fmt.Errorf("ComfyUI workflow execution failed: %s", msg)
		case nodeType != "":
			return fmt.Errorf("ComfyUI workflow execution failed (%s)", nodeType)
		}
	}
	return errors.New("ComfyUI workflow execution failed")
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
	const maxMediaBytes = 256 << 20
	data, err := io.ReadAll(io.LimitReader(res.Body, maxMediaBytes+1))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if len(data) > maxMediaBytes {
		return nil, errors.New("ComfyUI output exceeds 256 MiB")
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
