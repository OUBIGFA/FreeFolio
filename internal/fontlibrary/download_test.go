package fontlibrary

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

func TestDownloadVerifiesBeforeReplacingCache(t *testing.T) {
	data := []byte("font fixture")
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer s.Close()
	f := File{Name: "fixture.ttf", URL: s.URL, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Bytes: int64(len(data))}
	dir := t.TempDir()
	if e := Download(context.Background(), s.Client(), dir, []File{f}, nil); e != nil {
		t.Fatal(e)
	}
	got, e := Read(dir, f)
	if e != nil || string(got) != string(data) {
		t.Fatalf("cache: %q %v", got, e)
	}
	data = []byte("wrong bytes!")
	if e := Download(context.Background(), s.Client(), dir, []File{f}, nil); e == nil {
		t.Fatal("unverified download accepted")
	}
	got, _ = os.ReadFile(filepath.Join(dir, f.Name))
	if string(got) != "font fixture" {
		t.Fatal("bad download replaced verified cache")
	}
	f.Name = "../outside.ttf"
	if e := Download(context.Background(), s.Client(), dir, []File{f}, nil); e == nil {
		t.Fatal("escaped cache")
	}
}
