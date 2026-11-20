package cloudsync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A flat S3 inventory must not spend one request per virtual folder.
func TestS3InventoryListsNestedFilesInOneRequest(t *testing.T) {
	fake := &fakeS3{t: t, objects: map[string][]byte{
		"FreeMD/Other/elsewhere.md":           []byte("not this space"),
		"FreeMD/Notes/.freemd-history/old.md": []byte("excluded"),
		"FreeMD/Notes/empty/":                 {},
	}}
	for i := range 64 {
		fake.objects[fmt.Sprintf("FreeMD/Notes/folder-%d/note.md", i)] = []byte("note")
	}
	var lists atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			lists.Add(1)
		}
		fake.ServeHTTP(w, r)
	}))
	defer server.Close()
	c := DefaultConfig()
	c.Provider = "s3"
	c.S3Endpoint, c.S3Region, c.S3Bucket, c.S3AccessKey, c.S3SecretKey = server.URL, "auto", "bucket", "key", "secret"
	s, err := openStore(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	files, err := listRemote(s, "Notes", nil)
	if err != nil || len(files) != 64 {
		t.Fatalf("inventory: %d files, %v", len(files), err)
	}
	if lists.Load() != 1 {
		t.Fatalf("64 folders required %d listing requests, want 1", lists.Load())
	}
	if files["folder-63/note.md"].ETag == "" {
		t.Fatal("recursive listing lost the file validator")
	}
}

func TestS3InventoryFollowsPagesAndRejectsIncompleteResults(t *testing.T) {
	for _, mode := range []string{"complete", "missing-token", "repeated-token", "outside-prefix", "duplicate", "failed-page", "unexpected-root"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				if r.URL.Query().Get("prefix") != "FreeMD/Notes/" {
					t.Errorf("wrong space prefix: %s", r.URL.RawQuery)
				}
				if mode == "unexpected-root" {
					io.WriteString(w, `<Error><Code>InternalError</Code></Error>`)
					return
				}
				if call == 1 {
					token := `<NextContinuationToken>page2</NextContinuationToken>`
					if mode == "missing-token" {
						token = ""
					}
					fmt.Fprintf(w, `<ListBucketResult><Contents><Key>FreeMD/Notes/a.md</Key><Size>1</Size><ETag>"a"</ETag></Contents><IsTruncated>true</IsTruncated>%s</ListBucketResult>`, token)
					return
				}
				if r.URL.Query().Get("continuation-token") != "page2" {
					t.Errorf("missing continuation token: %s", r.URL.RawQuery)
				}
				if mode == "failed-page" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				key := "FreeMD/Notes/folder/b.md"
				if mode == "outside-prefix" {
					key = "FreeMD/Other/b.md"
				} else if mode == "duplicate" {
					key = "FreeMD/Notes/a.md"
				}
				extra := ""
				if mode == "repeated-token" && call == 2 {
					extra = `<IsTruncated>true</IsTruncated><NextContinuationToken>page2</NextContinuationToken>`
				}
				fmt.Fprintf(w, `<ListBucketResult><Contents><Key>%s</Key><Size>1</Size><ETag>"b"</ETag></Contents>%s</ListBucketResult>`, key, extra)
			}))
			defer server.Close()
			c := DefaultConfig()
			c.Provider = "s3"
			c.S3Endpoint, c.S3Region, c.S3Bucket, c.S3AccessKey, c.S3SecretKey = server.URL, "auto", "bucket", "key", "secret"
			s, err := openStore(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			files, err := listRemote(s, "Notes", nil)
			if mode == "complete" {
				if err != nil || len(files) != 2 || files["folder/b.md"].ETag != `"b"` || calls.Load() != 2 {
					t.Fatalf("paginated inventory: %+v, calls=%d, err=%v", files, calls.Load(), err)
				}
			} else if err == nil {
				t.Fatalf("unsafe inventory accepted: %+v", files)
			}
		})
	}
}

// Hold the first four directory responses until all four are in flight. This
// verifies overlap without relying on elapsed time or sleeps.
func TestWebDAVInventoryUsesBoundedConcurrentRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimRight(r.URL.Path, "/")
		w.Header().Set("Content-Type", "application/xml")
		if p == "/dav/Notes" {
			w.WriteHeader(http.StatusMultiStatus)
			io.WriteString(w, `<multistatus xmlns="DAV:">`)
			for i := range 8 {
				fmt.Fprintf(w, `<response><href>/dav/Notes/folder-%d/</href><propstat><prop><resourcetype><collection/></resourcetype></prop><status>HTTP/1.1 200 OK</status></propstat></response>`, i)
			}
			io.WriteString(w, `</multistatus>`)
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); n > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprintf(w, `<multistatus xmlns="DAV:"><response><href>%s/note.md</href><propstat><prop><getcontentlength>1</getcontentlength><getetag>"v1"</getetag></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`, p)
	}))
	defer server.Close()
	s, err := newWebDAVStore(server.URL+"/dav", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s.ctx = ctx
	done := make(chan error, 1)
	go func() {
		files, err := listRemote(s, "Notes", nil)
		if err == nil && len(files) != 8 {
			err = fmt.Errorf("got %d files, want 8", len(files))
		}
		done <- err
	}()
	for range 4 {
		select {
		case <-started:
		case <-ctx.Done():
			close(release)
			<-done
			t.Fatal("directory listing stayed serial")
		}
	}
	if peak.Load() > 4 {
		t.Errorf("too many parallel requests: %d", peak.Load())
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 4 {
		t.Fatalf("too many parallel requests: %d", peak.Load())
	}
}

func TestWebDAVInventoryFailureCancelsPeerRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started, stopped := make(chan struct{}), make(chan struct{})
	shutdown := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// net/http watches for disconnects only after the request body is read.
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		switch strings.TrimRight(r.URL.Path, "/") {
		case "/dav/Notes":
			w.WriteHeader(http.StatusMultiStatus)
			io.WriteString(w, `<multistatus xmlns="DAV:"><response><href>/dav/Notes/blocked/</href><propstat><prop><resourcetype><collection/></resourcetype></prop></propstat></response><response><href>/dav/Notes/failed/</href><propstat><prop><resourcetype><collection/></resourcetype></prop></propstat></response></multistatus>`)
		case "/dav/Notes/blocked":
			close(started)
			select {
			case <-r.Context().Done():
			case <-shutdown:
			}
			close(stopped)
		case "/dav/Notes/failed":
			select {
			case <-started:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	defer close(shutdown)
	s, err := newWebDAVStore(server.URL+"/dav", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s.ctx = ctx
	files, err := listRemote(s, "Notes", nil)
	if err == nil || !strings.Contains(err.Error(), "503") || files != nil || ctx.Err() != nil {
		t.Fatalf("failure did not stop the inventory safely: files=%v, err=%v, parent=%v", files, err, ctx.Err())
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("failed scan left a peer request running")
	}
}
