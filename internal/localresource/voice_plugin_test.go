package localresource

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestVoicePluginArchiveAndTraversal(t *testing.T) {
	for _, bad := range []string{"", "../escape.py", "a/../../escape.py", "C:/escape.py", `a\escape.py`} {
		t.Run(bad, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "plugin.zip")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			for _, name := range []string{"__init__.py", "AILab_QwenTTS.py", "requirements.txt", "LICENSE"} {
				w, _ := z.Create("ComfyUI-QwenTTS-" + voicePluginRevision + "/" + name)
				_, _ = w.Write([]byte("test"))
			}
			if bad != "" {
				w, _ := z.Create("ComfyUI-QwenTTS-" + voicePluginRevision + "/" + bad)
				_, _ = w.Write([]byte("bad"))
			}
			_ = z.Close()
			_ = f.Close()
			dst := filepath.Join(root, "plugin")
			err = extractVoicePlugin(archive, dst)
			if bad != "" {
				if err == nil {
					t.Fatal("unsafe archive accepted")
				}
				if _, err := os.Stat(dst); !os.IsNotExist(err) {
					t.Fatal("archive written before validation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !isRegularFile(filepath.Join(dst, "LICENSE")) {
				t.Fatal("license missing")
			}
		})
	}
}

func TestVoicePluginSetupReceiptRequiresSuccessfulImport(t *testing.T) {
	for _, failAt := range []int{0, 1, 2, 3} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			dst := t.TempDir()
			layout := ComfyLayout{Python: "comfy-python"}
			calls := 0
			err := configureVoicePlugin(context.Background(), layout, dst, func(_ context.Context, _ ComfyLayout, args ...string) error {
				calls++
				if calls == 2 && (!slices.Contains(args, "-c") || !slices.Contains(args, "--no-input") || slices.Contains(args, "--upgrade")) {
					t.Error("pip may change existing versions")
				}
				if calls == 3 && !strings.Contains(strings.Join(args, " "), "_load_qwen3_model") {
					t.Error("model import was not checked")
				}
				if calls == failAt {
					return errors.New("setup failed")
				}
				return nil
			})
			data, _ := os.ReadFile(filepath.Join(dst, ".freefolio-ready"))
			if failAt == 0 {
				if err != nil || string(data) != layout.Python {
					t.Fatal("success not recorded")
				}
			} else if err == nil || len(data) > 0 || calls != failAt {
				t.Fatal("failed setup marked ready or continued")
			}
		})
	}
}

func TestVoicePluginPinnedArchiveIntegration(t *testing.T) {
	archive := os.Getenv("FREEFOLIO_VOICE_ARCHIVE")
	if archive == "" {
		t.Skip("set FREEFOLIO_VOICE_ARCHIVE to verify downloaded upstream archive")
	}
	if err := extractVoicePlugin(archive, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// Exercise the real pinned archive through resource move and recycle paths.
	from, to := t.TempDir(), t.TempDir()
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) != voicePluginArchive.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != voicePluginArchive.SHA {
		t.Fatal("pinned archive hash mismatch")
	}
	target := filepath.Join(to, voicePluginArchive.RelPath)
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(voicePluginDir(from), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(voicePluginDir(from), ".freefolio-ready"), []byte("test-python"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreMigratedVoicePlugin(from, to); err != nil {
		t.Fatal(err)
	}
	receipt, _ := os.ReadFile(filepath.Join(voicePluginDir(to), ".freefolio-ready"))
	if string(receipt) != "test-python" {
		t.Fatal("migration lost ready receipt")
	}
	untouched := filepath.Join(voicePluginDir(to), "user-notes.txt")
	_ = os.WriteFile(untouched, []byte("keep"), 0600)
	var recycled []string
	if err := RemoveVoicePlugin(to, func(p string) error { recycled = append(recycled, p); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(recycled) < 10 || slices.Contains(recycled, untouched) {
		t.Fatal("recycle boundary incorrect")
	}
	modified := filepath.Join(voicePluginDir(to), "AILab_QwenTTS.py")
	_ = os.WriteFile(modified, []byte("user edit"), 0600)
	if err := extractVoicePlugin(target, voicePluginDir(to)); err == nil {
		t.Fatal("modified plugin overwritten")
	}
	if err := RemoveVoicePlugin(to, func(string) error { t.Fatal("recycled before verification"); return nil }); err == nil {
		t.Fatal("modified plugin removed")
	}
}
