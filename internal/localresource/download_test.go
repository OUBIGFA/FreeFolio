package localresource

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestResourceDownloadResumesCanceledFile(t *testing.T) {
	for _, packMode := range []bool{false, true} {
		t.Run(fmt.Sprint(packMode), func(t *testing.T) {
			payload := []byte("0123456789abcdef")
			var calls atomic.Int32
			var resumed atomic.Bool
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
					_, _ = w.Write(payload[:5])
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				if r.Header.Get("Range") == "bytes=5-" {
					resumed.Store(true)
					w.Header().Set("Content-Range", "bytes 5-15/16")
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(payload[5:])
					return
				}
				_, _ = w.Write(payload)
			}))
			defer srv.Close()
			root := t.TempDir()
			sha := fmt.Sprintf("%x", sha256.Sum256(payload))
			item := Resource{File: "model.onnx", URL: srv.URL, Size: int64(len(payload)), SHA: sha}
			pack := ResourcePack{Files: []PackFile{{RelPath: "model.onnx", URL: srv.URL, Size: item.Size, SHA: sha}}}
			download := func(ctx context.Context, progress func(int64)) error {
				if packMode {
					return installPack(ctx, root, pack, srv.Client(), func(done, _ int64) { progress(done) })
				}
				return install(ctx, root, item, srv.Client(), progress)
			}
			ctx, cancel := context.WithCancel(context.Background())
			if err := download(ctx, func(n int64) {
				if n >= 5 {
					cancel()
				}
			}); err == nil {
				t.Fatal("cancellation did not interrupt download")
			}
			if _, err := os.Stat(filepath.Join(root, item.File)); !os.IsNotExist(err) {
				t.Fatal("partial file published")
			}
			var progress int64
			if err := download(context.Background(), func(n int64) { progress = n }); err != nil {
				t.Fatal(err)
			}
			if !resumed.Load() || progress != 16 {
				t.Fatalf("download restarted instead of resuming: range=%v progress=%d", resumed.Load(), progress)
			}
			got, err := os.ReadFile(filepath.Join(root, item.File))
			if err != nil || string(got) != string(payload) {
				t.Fatal("resumed result differs")
			}
		})
	}
}

func TestResourceResumeRejectsBadRangeAndRestartsWhenIgnored(t *testing.T) {
	for _, mode := range []string{"ignore", "wrong-range", "bad-checksum"} {
		t.Run(mode, func(t *testing.T) {
			payload := []byte("0123456789abcdef")
			var calls atomic.Int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if n == 1 {
					w.Header().Set("Content-Length", "16")
					_, _ = w.Write(payload[:5])
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				}
				if mode == "ignore" {
					_, _ = w.Write(payload)
					return
				}
				if mode == "wrong-range" {
					w.Header().Set("Content-Range", "bytes 4-15/16")
					w.WriteHeader(206)
					_, _ = w.Write(payload[4:])
					return
				}
				if n == 2 {
					w.Header().Set("Content-Range", "bytes 5-15/16")
					w.WriteHeader(206)
					_, _ = w.Write([]byte("xxxxxxxxxxx"))
					return
				}
				if r.Header.Get("Range") != "" {
					t.Error("corrupt partial was reused")
				}
				_, _ = w.Write(payload)
			}))
			defer srv.Close()
			root := t.TempDir()
			item := Resource{File: "weights", URL: srv.URL, Size: 16, SHA: fmt.Sprintf("%x", sha256.Sum256(payload))}
			ctx, cancel := context.WithCancel(context.Background())
			_ = install(ctx, root, item, srv.Client(), func(n int64) {
				if n >= 5 {
					cancel()
				}
			})
			err := install(context.Background(), root, item, srv.Client(), nil)
			if mode == "ignore" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid resumed response accepted")
			}
			if _, err := os.Stat(filepath.Join(root, item.File)); !os.IsNotExist(err) {
				t.Fatal("unverified bytes published")
			}
			if mode == "bad-checksum" {
				if err := install(context.Background(), root, item, srv.Client(), nil); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
