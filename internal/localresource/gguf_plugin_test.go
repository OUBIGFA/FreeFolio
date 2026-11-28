package localresource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGGUFPluginSetupRequiresSuccessfulImport(t *testing.T) {
	for _, fail := range []int{0, 1, 2, 3} {
		dst := t.TempDir()
		calls := 0
		err := configureGGUFPlugin(context.Background(), ComfyLayout{Python: "isolated-python"}, dst, func(_ context.Context, _ ComfyLayout, args ...string) error {
			calls++
			if calls == 2 && (!slices.Contains(args, "-c") || slices.Contains(args, "--upgrade")) {
				t.Fatal("unconstrained dependency install")
			}
			if calls == 3 && !strings.Contains(strings.Join(args, " "), "UnetLoaderGGUFAdvanced") {
				t.Fatal("missing loader import check")
			}
			if calls == fail {
				return errors.New("setup failed")
			}
			return nil
		})
		receipt, _ := os.ReadFile(filepath.Join(dst, ".freefolio-ready"))
		if fail == 0 {
			if err != nil || string(receipt) != "isolated-python" {
				t.Fatal("success receipt missing")
			}
		} else if err == nil || len(receipt) > 0 || calls != fail {
			t.Fatal("failed installation marked ready")
		}
	}
}

func TestGGUFPluginPinnedArchiveMigrationAndRemoval(t *testing.T) {
	archive := os.Getenv("FREEFOLIO_GGUF_ARCHIVE")
	if archive == "" {
		t.Skip("set FREEFOLIO_GGUF_ARCHIVE to verify the pinned upstream archive")
	}
	from, to := t.TempDir(), t.TempDir()
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(to, ggufPluginArchive.RelPath)
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = verifyPackFile(to, ggufPluginArchive); err != nil {
		t.Fatal(err)
	}
	if err = extractGGUFPlugin(archive, ggufPluginDir(from)); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(ggufPluginDir(from), ".freefolio-ready"), []byte("python"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = restoreMigratedGGUFPlugin(from, to); err != nil {
		t.Fatal(err)
	}
	receipt, _ := os.ReadFile(filepath.Join(ggufPluginDir(to), ".freefolio-ready"))
	if string(receipt) != "python" {
		t.Fatal("migration lost receipt")
	}
	count := 0
	if err = RemoveGGUFPlugin(to, func(string) error { count++; return nil }); err != nil || count == 0 {
		t.Fatalf("verified plugin was not recycled: %v", err)
	}
	if err = os.WriteFile(filepath.Join(ggufPluginDir(to), "nodes.py"), []byte("user changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = RemoveGGUFPlugin(to, func(string) error { t.Fatal("recycled before verification"); return nil }); err == nil {
		t.Fatal("modified plugin removed")
	}
	if err = extractGGUFPlugin(archive, ggufPluginDir(to)); err == nil {
		t.Fatal("modified plugin overwritten")
	}
}
