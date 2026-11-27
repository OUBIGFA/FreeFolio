package localresource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestComfyAutoStartOnlyUsesLocalInstallation(t *testing.T) {
	root := t.TempDir()
	env := comfyDetectEnv{userConfigDir: root, goos: "windows"}
	for _, u := range []string{"http://127.0.0.1:8188", "http://localhost:8188", "http://[::1]:8188"} {
		dir, err := comfyAutoStartDirectory(u, root, env)
		if err != nil || dir != root {
			t.Fatalf("local startup rejected: %s %v", u, err)
		}
	}
	if _, err := comfyAutoStartDirectory("https://example.com", root, env); err == nil {
		t.Fatal("remote URL started local engine")
	}
	if _, err := comfyAutoStartDirectory("http://localhost:8188", "", env); err == nil {
		t.Fatal("missing install reported ready")
	}
	install := filepath.Join(root, "install")
	writeTestFile(t, filepath.Join(install, "ComfyUI", "main.py"), "# engine")
	writeTestFile(t, filepath.Join(install, ".venv", "Scripts", "python.exe"), "python")
	data, _ := json.Marshal([]map[string]any{{"id": "local", "status": "installed", "sourceId": "standalone", "installPath": install}})
	writeTestFile(t, filepath.Join(root, "Comfy Desktop", "installations.json"), string(data))
	dir, err := comfyAutoStartDirectory("http://localhost:8188", "", env)
	if err != nil || dir != filepath.Join(install, "ComfyUI") {
		t.Fatalf("registered desktop not detected: %s %v", dir, err)
	}
}

func TestComfyAutoStartProcessIntegration(t *testing.T) {
	dir := os.Getenv("FREEFOLIO_TEST_COMFY_DIR")
	if dir == "" {
		t.Skip("set FREEFOLIO_TEST_COMFY_DIR to an isolated test engine")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	u := srv.URL
	srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	mgr := NewComfyManager()
	defer mgr.Stop()
	cfg := ComfyConfig{Dir: dir, URL: u, ResourceDir: t.TempDir()}
	first, err := mgr.EnsureRunning(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !mgr.Running() {
		t.Fatal("engine not started")
	}
	second, err := mgr.EnsureRunning(ctx, cfg)
	if err != nil || second != first {
		t.Fatalf("started duplicate engine: %s %s %v", first, second, err)
	}
	mgr.Stop()
	if mgr.Running() {
		t.Fatal("test engine did not stop")
	}
	if _, err := mgr.EnsureRunning(ctx, cfg); err != nil {
		t.Fatal("could not restart stopped engine", err)
	}
}

func TestComfyRunningURLDoesNotRequireEngineDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) }))
	defer srv.Close()
	got, err := NewComfyManager().EnsureRunning(context.Background(), ComfyConfig{URL: srv.URL, Dir: "nonexistent"})
	if err != nil || got != srv.URL {
		t.Fatalf("working service not reused: %s %v", got, err)
	}
}

func TestComfyStoppedLocalURLAttemptsConfiguredStartup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	u := srv.URL
	srv.Close()
	_, err := NewComfyManager().EnsureRunning(context.Background(), ComfyConfig{URL: u, Dir: filepath.Join(t.TempDir(), "missing-engine")})
	if err == nil || !strings.Contains(err.Error(), "directory does not exist") {
		t.Fatalf("unreachable local URL did not attempt configured engine: %v", err)
	}
}
