package localresource

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseNvidiaSMIOutputAndVRAMGiB(t *testing.T) {
	raw := "Intel UHD Graphics, 0\nNVIDIA GeForce RTX 3060, 12288\nNVIDIA GeForce RTX 3080 Ti, 12288\n"
	gpu, ok := parseNvidiaSMIOutput(raw)
	if !ok || gpu.VRAMMiB != 12288 || gpu.VRAMGiB() != 12 {
		t.Fatalf("unexpected gpu parse result: %+v ok=%v", gpu, ok)
	}
	if _, ok := parseNvidiaSMIOutput("invalid\n"); ok {
		t.Fatal("expected invalid nvidia-smi output to return false")
	}
}

func TestInstallVerifyResumeAndRemovePack(t *testing.T) {
	aBytes := []byte("alpha-weights-data")
	bBytes := []byte("beta-config-json")
	var reqCount atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		switch r.URL.Path {
		case "/a.safetensors":
			_, _ = w.Write(aBytes)
		case "/b.json":
			_, _ = w.Write(bBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pack := ResourcePack{
		ID:      "test-pack",
		Name:    "Test Pack",
		License: "Apache-2.0",
		Kind:    "sa2va",
		ModelID: "Org/TestPack",
		Files: []PackFile{
			{RelPath: "comfyui/sa2va/Org/TestPack/a.safetensors", URL: srv.URL + "/a.safetensors", Size: int64(len(aBytes)), SHA: fmt.Sprintf("%x", sha256.Sum256(aBytes))},
			{RelPath: "comfyui/sa2va/Org/TestPack/b.json", URL: srv.URL + "/b.json", Size: int64(len(bBytes))},
		},
	}

	root := t.TempDir()
	bad := pack
	bad.Files = append([]PackFile(nil), pack.Files...)
	bad.Files[0].SHA = "deadbeef"
	if err := installPack(context.Background(), root, bad, srv.Client(), nil); err == nil {
		t.Fatal("accepted mismatched SHA-256")
	}
	if PackPresent(root, pack) {
		t.Fatal("pack reported present after failed checksum")
	}

	var lastDone, lastTotal int64
	if err := installPack(context.Background(), root, pack, srv.Client(), func(done, total int64) {
		lastDone, lastTotal = done, total
	}); err != nil {
		t.Fatal(err)
	}
	if lastDone != pack.TotalSize() || lastTotal != pack.TotalSize() {
		t.Fatalf("unexpected progress: %d / %d", lastDone, lastTotal)
	}
	if !PackPresent(root, pack) {
		t.Fatal("pack not present after install")
	}

	// Re-running InstallPack skips already verified files (0 HTTP requests).
	reqCount.Store(0)
	if err := installPack(context.Background(), root, pack, srv.Client(), nil); err != nil {
		t.Fatal(err)
	}
	if reqCount.Load() != 0 {
		t.Fatalf("expected resumable skip of verified files, got %d requests", reqCount.Load())
	}

	// RemovePack verifies contents before trashing.
	var trashed []string
	if err := RemovePack(root, pack, func(p string) error {
		trashed = append(trashed, filepath.Base(p))
		return os.Remove(p)
	}); err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 2 || PackPresent(root, pack) {
		t.Fatalf("unexpected remove result: trashed=%v present=%v", trashed, PackPresent(root, pack))
	}

	// Foreign file with wrong bytes must not be overwritten or trashed.
	foreignPath := filepath.Join(root, filepath.FromSlash(pack.Files[0].RelPath))
	if err := os.MkdirAll(filepath.Dir(foreignPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreignPath, []byte("foreign-user-file!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemovePack(root, pack, func(string) error { t.Fatal("foreign file removed"); return nil }); err == nil {
		t.Fatal("expected error when removing foreign file")
	}
	if err := installPack(context.Background(), root, pack, srv.Client(), nil); err == nil {
		t.Fatal("expected error overwriting foreign file")
	}
}

func TestComfyPackCatalogIntegrity(t *testing.T) {
	if p, ok := ComfyPack("rmbg3-fp8"); !ok || !strings.Contains(p.Files[0].RelPath, "RMBG-3.0-fp8.safetensors") || len(p.Files) != 3 {
		t.Fatalf("unexpected rmbg3-fp8 pack: %+v ok=%v", p, ok)
	}
	for _, pack := range ComfyPacks {
		if pack.ID == "" || pack.Name == "" || pack.ModelID == "" || pack.TotalSize() <= 0 {
			t.Fatalf("incomplete pack metadata: %+v", pack)
		}
		for _, pf := range pack.Files {
			if !strings.HasPrefix(pf.URL, "https://") || pf.Size <= 0 || !strings.HasPrefix(pf.RelPath, "comfyui/") {
				t.Fatalf("invalid pack file in %s: %+v", pack.ID, pf)
			}
			if strings.HasSuffix(pf.RelPath, ".safetensors") && len(pf.SHA) != 64 {
				t.Fatalf("missing pinned SHA-256 for weights %s in %s", pf.RelPath, pack.ID)
			}
		}
	}
}
