package localresource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shaHex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func TestMigrateMovesKnownFilesAndCleansComfyDirs(t *testing.T) {
	fromDir := t.TempDir()
	toDir := t.TempDir()

	modelData := bytes.Repeat([]byte("M"), 64)
	comfyData := bytes.Repeat([]byte("C"), 48)
	unrelatedData := []byte("user-note-keep-me")

	write := func(root, rel string, data []byte) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	write(fromDir, "ben2-fp16.onnx", modelData)
	write(fromDir, "comfyui/diffusion_models/model.safetensors", comfyData)
	if _, err := WriteComfyExtraModelPaths(fromDir); err != nil {
		t.Fatal(err)
	}
	unrelatedPath := write(fromDir, "notes/readme.txt", unrelatedData)

	items := []MigrationItem{
		{RelPath: "ben2-fp16.onnx", Size: int64(len(modelData)), SHA: shaHex(modelData)},
		{RelPath: filepath.FromSlash("comfyui/diffusion_models/model.safetensors"), Size: int64(len(comfyData)), SHA: shaHex(comfyData), Comfy: true},
	}
	total := int64(len(modelData) + len(comfyData))

	var lastDone, lastTotal int64
	if err := migrateItems(context.Background(), fromDir, toDir, items, total, true, func(done, tot int64) {
		lastDone, lastTotal = done, tot
	}); err != nil {
		t.Fatalf("migrateItems failed: %v", err)
	}
	if lastDone != total || lastTotal != total {
		t.Fatalf("progress = %d/%d, want %d/%d", lastDone, lastTotal, total, total)
	}

	if _, err := os.Stat(filepath.Join(fromDir, "ben2-fp16.onnx")); !os.IsNotExist(err) {
		t.Fatalf("source model still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fromDir, "comfyui")); !os.IsNotExist(err) {
		t.Fatalf("empty comfyui dir was not removed: %v", err)
	}

	gotModel, err := os.ReadFile(filepath.Join(toDir, "ben2-fp16.onnx"))
	if err != nil || !bytes.Equal(gotModel, modelData) {
		t.Fatalf("moved model mismatch: err=%v", err)
	}
	gotComfy, err := os.ReadFile(filepath.Join(toDir, "comfyui", "diffusion_models", "model.safetensors"))
	if err != nil || !bytes.Equal(gotComfy, comfyData) {
		t.Fatalf("moved comfy model mismatch: err=%v", err)
	}
	yamlBytes, err := os.ReadFile(filepath.Join(toDir, "comfyui", "extra_model_paths.yaml"))
	if err != nil || !strings.Contains(string(yamlBytes), filepath.ToSlash(filepath.Join(toDir, "comfyui"))) {
		t.Fatalf("extra_model_paths.yaml did not update to new dir: err=%v\n%s", err, string(yamlBytes))
	}
	gotUnrelated, err := os.ReadFile(unrelatedPath)
	if err != nil || !bytes.Equal(gotUnrelated, unrelatedData) {
		t.Fatalf("unrelated user file was modified or removed: err=%v", err)
	}
}

func TestMoveFileCopyAndCancellation(t *testing.T) {
	fromDir := t.TempDir()
	toDir := t.TempDir()

	payload := bytes.Repeat([]byte("X"), 512*1024+17)
	src := filepath.Join(fromDir, "model.bin")
	dst := filepath.Join(toDir, "model.bin")
	if err := os.WriteFile(src, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	// Cancel mid-copy and ensure source remains intact and temp files are cleaned.
	ctx, cancel := context.WithCancel(context.Background())
	err := moveFileCopy(ctx, src, dst, int64(len(payload)), shaHex(payload), func(copied int64) {
		if copied > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if _, statErr := os.Stat(src); statErr != nil {
		t.Fatalf("source was deleted on cancel: %v", statErr)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("destination exists after cancel: %v", statErr)
	}
	entries, err := os.ReadDir(toDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("leftover temp files in destination: %v %v", entries, err)
	}

	// Complete copy succeeds and deletes source.
	var lastCopied int64
	if err := moveFileCopy(context.Background(), src, dst, int64(len(payload)), shaHex(payload), func(copied int64) {
		lastCopied = copied
	}); err != nil {
		t.Fatalf("moveFileCopy failed: %v", err)
	}
	if lastCopied != int64(len(payload)) {
		t.Fatalf("copied = %d, want %d", lastCopied, len(payload))
	}
	if _, statErr := os.Stat(src); !os.IsNotExist(statErr) {
		t.Fatalf("source still exists after copy move: %v", statErr)
	}
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("copied payload mismatch: err=%v", err)
	}
}

func TestMigrateDestinationConflictAndDuplicate(t *testing.T) {
	fromDir := t.TempDir()
	toDir := t.TempDir()

	srcData := bytes.Repeat([]byte("A"), 32)
	otherData := bytes.Repeat([]byte("B"), 32)
	src := filepath.Join(fromDir, "item.bin")
	dst := filepath.Join(toDir, "item.bin")
	if err := os.WriteFile(src, srcData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, otherData, 0o600); err != nil {
		t.Fatal(err)
	}

	items := []MigrationItem{{RelPath: "item.bin", Size: int64(len(srcData)), SHA: shaHex(srcData)}}
	if err := migrateItems(context.Background(), fromDir, toDir, items, int64(len(srcData)), false, nil); err == nil {
		t.Fatal("expected destination conflict error")
	}
	if gotDst, _ := os.ReadFile(dst); !bytes.Equal(gotDst, otherData) {
		t.Fatal("destination file was overwritten on conflict")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source file was removed on conflict")
	}

	// Identical destination removes duplicate source cleanly.
	if err := os.WriteFile(dst, srcData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateItems(context.Background(), fromDir, toDir, items, int64(len(srcData)), false, nil); err != nil {
		t.Fatalf("identical destination failed: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("duplicate source still exists: %v", err)
	}
}

func TestCollectMigratableFilesOnlyPicksExactSizeKnownFiles(t *testing.T) {
	root := t.TempDir()
	pack := ComfyPacks[1] // sa2va-internvl3-2b has small JSON files with exact sizes
	pf := pack.Files[0]   // added_tokens.json (881 bytes)
	p := filepath.Join(root, filepath.FromSlash(pf.RelPath))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, bytes.Repeat([]byte(" "), int(pf.Size)), 0o600); err != nil {
		t.Fatal(err)
	}
	// Wrong-size file with known name should be ignored.
	if err := os.WriteFile(filepath.Join(root, Models[0].File), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	items, total, hasComfy := CollectMigratableFiles(root)
	if len(items) != 1 || items[0].RelPath != filepath.FromSlash(pf.RelPath) || total != pf.Size || !hasComfy {
		t.Fatalf("unexpected collected items: items=%+v total=%d hasComfy=%v", items, total, hasComfy)
	}
}
