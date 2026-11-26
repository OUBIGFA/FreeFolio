package localresource

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallVerifyAndRemoveOnlySelectedResource(t *testing.T) {
	payload := "test model bytes"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, payload) }))
	defer srv.Close()
	file := Resource{File: "model.onnx", URL: srv.URL, Size: int64(len(payload)), SHA: fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))}
	root := t.TempDir()
	unrelated := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := file
	bad.SHA = "wrong"
	if err := install(context.Background(), root, bad, srv.Client(), nil); err == nil {
		t.Fatal("accepted wrong checksum")
	}
	if _, err := os.Stat(filepath.Join(root, file.File)); !os.IsNotExist(err) {
		t.Fatal("published incomplete file")
	}
	if err := install(context.Background(), root, file, srv.Client(), nil); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, file, func(name string) error {
		if name != filepath.Join(root, file.File) {
			t.Fatalf("wrong target: %s", name)
		}
		return os.Rename(name, filepath.Join(root, "recycled"))
	}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
		t.Fatal("unrelated file changed")
	}
	if err := os.WriteFile(filepath.Join(root, file.File), []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, file, func(string) error { t.Fatal("foreign file removed"); return nil }); err == nil {
		t.Fatal("accepted foreign file")
	}
	if err := install(context.Background(), root, file, srv.Client(), nil); err == nil {
		t.Fatal("overwrote foreign file")
	}
}

func TestPublishDownloadDoesNotReplaceExistingBytes(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "new"), filepath.Join(dir, "kept")
	for _, file := range []string{from, to} {
		if err := os.WriteFile(file, []byte(filepath.Base(file)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := publishDownload(from, to); err == nil {
		t.Fatal("replaced a file created during download")
	}
	data, err := os.ReadFile(to)
	if err != nil || string(data) != "kept" {
		t.Fatalf("existing file changed: %q %v", data, err)
	}
}
