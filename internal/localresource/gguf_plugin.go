package localresource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const ggufPluginRevision = "6ea2651e7df66d7585f6ffee804b20e92fb38b8a"

var ggufPluginArchive = PackFile{RelPath: "comfyui/plugins/gguf-" + ggufPluginRevision + ".zip", URL: "https://github.com/city96/ComfyUI-GGUF/archive/" + ggufPluginRevision + ".zip", Size: 38051, SHA: "aad273a0b774684285b4496f6edff7e2293bd29d71ef89926d6861bfdf4947ac"}

func ggufPluginDir(root string) string {
	return filepath.Join(root, "comfyui", "custom_nodes", "FreeFolio-GGUF-"+ggufPluginRevision)
}
func extractGGUFPlugin(archive, dst string) error {
	return extractComfyPlugin(archive, dst, "ComfyUI-GGUF-"+ggufPluginRevision+"/", []string{"__init__.py", "nodes.py", "requirements.txt", "LICENSE"})
}

// GGUFPluginInstalled requires a successful import in this engine's environment.
func GGUFPluginInstalled(root, engineDir string) bool {
	layout, err := DetectComfyLayout(engineDir)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(ggufPluginDir(root), ".freefolio-ready"))
	return err == nil && string(b) == layout.Python && isRegularFile(filepath.Join(ggufPluginDir(root), "nodes.py"))
}

// InstallGGUFPlugin is called only by the optional model download action.
func InstallGGUFPlugin(ctx context.Context, root, engineDir string) error {
	layout, err := DetectComfyLayout(engineDir)
	if err != nil {
		return err
	}
	if !layout.IsolatedPython {
		return errors.New("GGUF requires ComfyUI's own Python environment")
	}
	archive, err := verifyPackFile(root, ggufPluginArchive)
	if err != nil {
		return err
	}
	dst := ggufPluginDir(root)
	if err = extractGGUFPlugin(archive, dst); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	return configureGGUFPlugin(ctx, layout, dst, runVoicePython)
}

func configureGGUFPlugin(ctx context.Context, layout ComfyLayout, dst string, run voicePythonRunner) error {
	const check = `import importlib.util, pathlib, sys
sys.path.insert(0, sys.argv[1])
p = pathlib.Path(sys.argv[2])
spec = importlib.util.spec_from_file_location('freefolio_gguf_check', p/'__init__.py', submodule_search_locations=[str(p)])
m = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = m
spec.loader.exec_module(m)
assert 'UnetLoaderGGUFAdvanced' in m.NODE_CLASS_MAPPINGS`
	return configureComfyPlugin(ctx, layout, dst, run, check, layout.Root, dst)
}

// RemoveGGUFPlugin retains modified files and shared Python dependencies.
func RemoveGGUFPlugin(root string, trash func(string) error) error {
	return removeComfyPlugin(root, ggufPluginArchive, ggufPluginDir(root), "ComfyUI-GGUF-"+ggufPluginRevision+"/", trash)
}

func restoreMigratedGGUFPlugin(from, to string) error {
	archive, err := verifyPackFile(to, ggufPluginArchive)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = extractGGUFPlugin(archive, ggufPluginDir(to)); err != nil {
		return err
	}
	receipt, err := os.ReadFile(filepath.Join(ggufPluginDir(from), ".freefolio-ready"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(receipt) > 32768 {
		return errors.New("invalid GGUF plugin receipt")
	}
	return os.WriteFile(filepath.Join(ggufPluginDir(to), ".freefolio-ready"), receipt, 0600)
}
