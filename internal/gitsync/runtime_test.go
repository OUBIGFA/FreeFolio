package gitsync

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeDownloadResumesAndVerifies(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("cmd/git.exe")
	w.Write([]byte("fixture"))
	z.Close()
	data := b.Bytes()
	parent := t.TempDir()
	os.WriteFile(filepath.Join(parent, "fixture.zip.partial"), data[:10], 0600)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=10-" {
			t.Error(r.Header)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 10-%d/%d", len(data)-1, len(data)))
		w.WriteHeader(206)
		w.Write(data[10:])
	}))
	defer server.Close()
	stage, e := runtimeArchive(context.Background(), parent, "fixture.zip", server.URL, fmt.Sprintf("%x", sha256.Sum256(data)), int64(len(data)), nil)
	if e != nil {
		t.Fatal(e)
	}
	content, e := os.ReadFile(filepath.Join(stage, "cmd/git.exe"))
	if e != nil || string(content) != "fixture" {
		t.Fatal(e, string(content))
	}
}

func TestOfficialMinGitArchive(t *testing.T) {
	p := os.Getenv("FREEFOLIO_TEST_MINGIT_ARCHIVE")
	if p == "" {
		t.Skip("set archive path for publisher package validation")
	}
	dest := t.TempDir()
	if e := extractRuntime(p, dest, "4a92850b6e917da4fd010669b614b55dccf5b0ec333a534cc4be4700dfefdafd"); e != nil {
		t.Fatal(e)
	}
	g := &Git{Path: filepath.Join(dest, "cmd/git.exe")}
	b, e := g.run(context.Background(), dest, nil, nil, "--version")
	if e != nil || !strings.Contains(string(b), "2.56.1") {
		t.Fatal(string(b), e)
	}
}

func TestRuntimeArchiveRejectsTraversalAndWrongChecksum(t *testing.T) {
	for _, name := range []string{"../outside", "C:/outside", "safe/../../outside"} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		w, _ := z.Create(name)
		w.Write([]byte("data"))
		z.Close()
		p := filepath.Join(t.TempDir(), "runtime.zip")
		os.WriteFile(p, b.Bytes(), 0600)
		if extractRuntime(p, t.TempDir(), fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))) == nil {
			t.Fatal("unsafe archive accepted", name)
		}
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("cmd/git.exe")
	w.Write([]byte("git"))
	z.Close()
	p := filepath.Join(t.TempDir(), "runtime.zip")
	os.WriteFile(p, b.Bytes(), 0600)
	if extractRuntime(p, t.TempDir(), "wrong") == nil {
		t.Fatal("wrong digest accepted")
	}
	dest := t.TempDir()
	if e := extractRuntime(p, dest, fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dest, "cmd", "git.exe")); e != nil {
		t.Fatal(e)
	}
}
