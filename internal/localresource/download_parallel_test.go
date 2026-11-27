package localresource

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelResourceDownloadsAndSharedFileReuse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered := make(chan string, 4)
	release := make(chan struct{})
	var sharedCalls atomic.Int32
	payload := []byte("verified weights")
	sha := fmt.Sprintf("%x", sha256.Sum256(payload))
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shared" {
			sharedCalls.Add(1)
		}
		entered <- r.URL.Path
		select {
		case <-release:
			_, _ = w.Write(payload)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	root := t.TempDir()
	results := make(chan error, 3)
	download := func(name string) {
		results <- downloadFile(ctx, filepath.Join(root, name), srv.URL+"/"+name, int64(len(payload)), sha, srv.Client(), nil)
	}
	go download("shared")
	go download("other")
	seen := map[string]bool{}
	for range 2 {
		select {
		case path := <-entered:
			seen[path] = true
		case <-ctx.Done():
			t.Fatal("independent files were serialized")
		}
	}
	if !seen["/shared"] || !seen["/other"] {
		t.Fatal("wrong concurrent files")
	}
	go download("shared")
	close(release)
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if sharedCalls.Load() != 1 {
		t.Fatalf("shared dependency downloaded %d times", sharedCalls.Load())
	}
}

func TestCancellingSharedDownloadWaiterDoesNotStopOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	payload := []byte("test")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
			_, _ = w.Write(payload)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	target := filepath.Join(t.TempDir(), "shared")
	sha := fmt.Sprintf("%x", sha256.Sum256(payload))
	done := make(chan error, 1)
	go func() { done <- downloadFile(ctx, target, srv.URL, 4, sha, srv.Client(), nil) }()
	<-entered
	waiter, stop := context.WithCancel(ctx)
	defer stop()
	waiting := make(chan struct{})
	waitResult := make(chan error, 1)
	var once sync.Once
	go func() {
		waitResult <- downloadFile(waiter, target, srv.URL, 4, sha, srv.Client(), func(int64) { once.Do(func() { close(waiting) }) })
	}()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("waiter did not share progress")
	}
	stop()
	if err := <-waitResult; err != context.Canceled {
		t.Fatalf("waiter cancel: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal("owner was cancelled", err)
	}
}

func TestDownloadMirrorsShareTargetWithoutPublishConflict(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	payload := []byte("shared mirror bytes")
	sha := fmt.Sprintf("%x", sha256.Sum256(payload))
	var requests atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); _, _ = w.Write(payload) }))
	defer srv.Close()
	target := filepath.Join(t.TempDir(), "model")
	results := make(chan error, 2)
	for _, suffix := range []string{"/one", "/two"} {
		go func() {
			results <- downloadFile(ctx, target, srv.URL+suffix, int64(len(payload)), sha, srv.Client(), nil)
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatal("different mirrors rewrote the same installed file")
	}
}
