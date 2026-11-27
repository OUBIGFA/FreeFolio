package localresource

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const voicePluginRevision = "d8122a8ba835b65fd65c113d2b273b1ad1579293"

// The optional speech integration is pinned and kept outside the application.
var voicePluginArchive = PackFile{
	RelPath: "comfyui/plugins/qwen-tts-" + voicePluginRevision + ".zip",
	URL:     "https://github.com/1038lab/ComfyUI-QwenTTS/archive/" + voicePluginRevision + ".zip",
	Size:    1293169, SHA: "8e4f81264dcfb2d6d6e3834f7bea478617105baabd95c05e19ad5d5ac210ab5a",
}

func voicePluginDir(root string) string {
	return filepath.Join(root, "comfyui", "custom_nodes", "FreeFolio-QwenTTS-"+voicePluginRevision)
}

// VoicePluginInstalled checks the completion receipt for this Python runtime.
func VoicePluginInstalled(root, engineDir string) bool {
	layout, err := DetectComfyLayout(engineDir)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(voicePluginDir(root), ".freefolio-ready"))
	return err == nil && string(b) == layout.Python && isRegularFile(filepath.Join(voicePluginDir(root), "AILab_QwenTTS.py"))
}

// InstallVoicePlugin runs only from the explicit resource download action.
// Existing Python package versions are constrained so installing speech cannot
// replace CUDA PyTorch or upgrade another ComfyUI dependency silently.
func InstallVoicePlugin(ctx context.Context, root, engineDir string) error {
	layout, err := DetectComfyLayout(engineDir)
	if err != nil {
		return fmt.Errorf("select a local ComfyUI directory before installing speech: %w", err)
	}
	if !layout.IsolatedPython {
		return errors.New("speech requires ComfyUI's own Python environment")
	}
	archive, err := verifyPackFile(root, voicePluginArchive)
	if err != nil {
		return err
	}
	dst := voicePluginDir(root)
	if err = extractVoicePlugin(archive, dst); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	return configureVoicePlugin(ctx, layout, dst, runVoicePython)
}

type voicePythonRunner func(context.Context, ComfyLayout, ...string) error

func runVoicePython(ctx context.Context, layout ComfyLayout, args ...string) error {
	cmd := exec.CommandContext(ctx, layout.Python, append([]string{"-s"}, args...)...)
	cmd.Dir = layout.Root
	cmd.Env = buildComfyEnv(os.Environ(), layout, runtime.GOOS)
	configureComfyCmd(cmd)
	logs := &tailBuffer{max: 8192}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		return err
	}
	group := attachProcessGroup(cmd)
	defer group.Close()
	finished := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			group.Kill()
		case <-finished:
		}
	}()
	err := cmd.Wait()
	close(finished)
	<-done
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("speech plugin setup failed: %w\n%s", err, logs.String())
	}
	return nil
}

// RemoveVoicePlugin recycles only files that still match the verified archive.
// Shared Python packages and generated caches are retained for other nodes.
func RemoveVoicePlugin(root string, trash func(string) error) error {
	archive, err := verifyPackFile(root, voicePluginArchive)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	prefix := "ComfyUI-QwenTTS-" + voicePluginRevision + "/"
	var targets []string
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		if rel == f.Name || path.Clean(rel) != rel || !filepath.IsLocal(filepath.FromSlash(rel)) || strings.ContainsAny(rel, "\\:") {
			return errors.New("unsafe speech plugin archive")
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, r)
		r.Close()
		if err != nil {
			return err
		}
		local, _ := filepath.Rel(root, filepath.Join(voicePluginDir(root), filepath.FromSlash(rel)))
		target, err := verifyPackFile(root, PackFile{RelPath: local, Size: int64(f.UncompressedSize64), SHA: fmt.Sprintf("%x", h.Sum(nil))})
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		targets = append(targets, target)
	}
	for _, target := range targets {
		if err := trash(target); err != nil {
			return err
		}
	}
	return nil
}

// Resource moves rebuild the pinned plugin from its migrated ZIP, retaining the
// successful environment receipt without reinstalling Python dependencies.
func restoreMigratedVoicePlugin(fromDir, toDir string) error {
	archive, err := verifyPackFile(toDir, voicePluginArchive)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := extractVoicePlugin(archive, voicePluginDir(toDir)); err != nil {
		return err
	}
	receipt, err := os.ReadFile(filepath.Join(voicePluginDir(fromDir), ".freefolio-ready"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(receipt) > 32768 {
		return errors.New("invalid speech plugin receipt")
	}
	return os.WriteFile(filepath.Join(voicePluginDir(toDir), ".freefolio-ready"), receipt, 0600)
}

func configureVoicePlugin(ctx context.Context, layout ComfyLayout, dst string, run voicePythonRunner) error {
	if err := os.WriteFile(filepath.Join(dst, ".freefolio-ready"), nil, 0600); err != nil {
		return err
	}
	constraints := filepath.Join(dst, ".freefolio-constraints.txt")
	const freeze = `import importlib.metadata as m, pathlib, sys
packages = {d.metadata['Name']: d.version for d in m.distributions() if d.metadata['Name']}
assert 'torch' in {n.lower() for n in packages}, 'Install ComfyUI CUDA PyTorch first'
pathlib.Path(sys.argv[1]).write_text('\n'.join(n+'=='+v for n,v in sorted(packages.items()))+'\n', encoding='utf-8')`
	if err := run(ctx, layout, "-c", freeze, constraints); err != nil {
		return err
	}
	if err := run(ctx, layout, "-m", "pip", "install", "--disable-pip-version-check", "--no-input", "--upgrade-strategy", "only-if-needed", "-c", constraints, "-r", filepath.Join(dst, "requirements.txt")); err != nil {
		return err
	}
	// Import the actual integration, including its bundled model loader. Its
	// outer module catches import errors, so checking exit status alone is wrong.
	const check = `import runpy, sys
sys.path.insert(0, sys.argv[1])
v = runpy.run_path(sys.argv[2])
assert v['_load_qwen3_model']() is not None, 'Qwen3-TTS model loader import failed'
assert 'AILab_Qwen3TTSCustomVoice' in v['NODE_CLASS_MAPPINGS']`
	if err := run(ctx, layout, "-c", check, layout.Root, filepath.Join(dst, "AILab_QwenTTS.py")); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, ".freefolio-ready"), []byte(layout.Python), 0600)
}

// Validate the whole ZIP before writing anything. Only the pinned repository
// root is accepted, with no links, traversal, duplicate paths or oversized files.
func extractVoicePlugin(archive, dst string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	prefix := "ComfyUI-QwenTTS-" + voicePluginRevision + "/"
	type entry struct {
		f   *zip.File
		rel string
	}
	var entries []entry
	seen := map[string]bool{}
	var total uint64
	for _, f := range z.File {
		if !strings.HasPrefix(f.Name, prefix) {
			return errors.New("unexpected speech plugin archive root")
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		if rel == "" && f.FileInfo().IsDir() {
			continue
		}
		rel = strings.TrimSuffix(rel, "/")
		if path.Clean(rel) != rel || !filepath.IsLocal(filepath.FromSlash(rel)) || strings.ContainsAny(rel, "\\:") || f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe speech plugin path: %s", f.Name)
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return fmt.Errorf("duplicate speech plugin path: %s", rel)
		}
		seen[key] = true
		total += f.UncompressedSize64
		if f.UncompressedSize64 > 8<<20 || total > 32<<20 {
			return errors.New("speech plugin archive exceeds size limit")
		}
		if !f.FileInfo().IsDir() && !f.Mode().IsRegular() {
			return errors.New("speech plugin archive contains a special file")
		}
		entries = append(entries, entry{f, rel})
	}
	for _, name := range []string{"__init__.py", "AILab_QwenTTS.py", "requirements.txt", "LICENSE"} {
		if !seen[strings.ToLower(name)] {
			return fmt.Errorf("speech plugin is missing %s", name)
		}
	}
	for _, e := range entries {
		target := filepath.Join(dst, filepath.FromSlash(e.rel))
		// Reject existing symlinks in every parent, including the destination.
		for p := target; ; p = filepath.Dir(p) {
			st, err := os.Lstat(p)
			if err == nil && st.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("speech plugin path is a link: %s", p)
			}
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if p == filepath.Dir(p) {
				break
			}
		}
		if e.f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if st, err := os.Lstat(target); err == nil {
			r, err := e.f.Open()
			if err != nil {
				return err
			}
			h := sha256.New()
			_, err = io.Copy(h, r)
			r.Close()
			if err != nil {
				return err
			}
			actual, err := fileSHA256(target, int64(e.f.UncompressedSize64))
			if !st.Mode().IsRegular() || err != nil || actual != fmt.Sprintf("%x", h.Sum(nil)) {
				return fmt.Errorf("speech plugin file was modified: %s", e.rel)
			}
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := extractZipEntry(e.f, target, 0600); err != nil {
			return err
		}
	}
	return nil
}
