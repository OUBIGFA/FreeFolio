package cloudsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Count real HTTP content responses, so a zero-change result cannot hide a
// complete download of every unchanged file. Both transports use this path.
func TestUnchangedSyncAvoidsContentDownloads(t *testing.T) {
	for _, provider := range []string{"webdav", "s3"} {
		for _, omitTags := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/omit-list-etag=%v", provider, omitTags), func(t *testing.T) {
				var handler http.Handler
				if provider == "webdav" {
					handler = &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true}, files: map[string][]byte{}, omitListETag: omitTags}
				} else {
					handler = &fakeS3{t: t, objects: map[string][]byte{}, omitListETag: omitTags}
				}
				var gets, bodies, validated atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, r)
					if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "" {
						gets.Add(1)
						if rec.Code == http.StatusOK {
							bodies.Add(1)
						} else if rec.Code == http.StatusNotModified {
							validated.Add(1)
						}
					}
					for k, v := range rec.Header() {
						w.Header()[k] = v
					}
					w.WriteHeader(rec.Code)
					_, _ = w.Write(rec.Body.Bytes())
				}))
				defer server.Close()
				c := DefaultConfig()
				c.Enabled, c.Provider = true, provider
				c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
				c.S3Endpoint, c.S3Region, c.S3Bucket, c.S3AccessKey, c.S3SecretKey = server.URL, "auto", "bucket", "key", "secret"
				a := Space{ID: "a", Root: t.TempDir(), Remote: "Notes"}
				b := Space{ID: "b", Root: t.TempDir(), Remote: "Notes"}
				astate, bstate := t.TempDir(), t.TempDir()
				run := func(sp Space, state string) Result {
					t.Helper()
					r, err := Sync(context.Background(), c, sp, state, Hooks{})
					if err != nil {
						t.Fatal(err)
					}
					return r
				}
				file := filepath.Join(a.Root, "note.md")
				if err := os.WriteFile(file, []byte("first"), 0600); err != nil {
					t.Fatal(err)
				}
				if r := run(a, astate); r.Uploaded != 1 {
					t.Fatalf("initial upload: %+v", r)
				}
				for range 2 {
					r := run(a, astate)
					if r.Uploaded != 0 || r.Downloaded != 0 || r.Deleted != 0 || len(r.Conflicts) != 0 {
						t.Fatalf("unchanged: %+v", r)
					}
				}
				if bodies.Load() != 0 {
					t.Fatalf("unchanged content downloaded %d times", bodies.Load())
				}
				if !omitTags && gets.Load() != 0 {
					t.Fatalf("stable listing still needed %d GETs", gets.Load())
				}
				if omitTags && validated.Load() != 2 {
					t.Fatalf("conditional validations=%d, want 2", validated.Load())
				}
				// The same optimization must not hide a same-size remote change.
				run(b, bstate)
				if err := os.WriteFile(filepath.Join(b.Root, "note.md"), []byte("other"), 0600); err != nil {
					t.Fatal(err)
				}
				run(b, bstate)
				if r := run(a, astate); r.Downloaded != 1 {
					t.Fatalf("remote change missed: %+v", r)
				}
				if data, err := os.ReadFile(file); err != nil || string(data) != "other" {
					t.Fatalf("download=%q %v", data, err)
				}
			})
		}
	}
}

func TestWebDAVKeepsETagFromSuccessfulPropertyGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/Notes/note.md</d:href><d:propstat><d:prop><d:getetag>"v1"</d:getetag><d:getcontentlength>5</d:getcontentlength></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat><d:propstat><d:prop><d:getlastmodified/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat></d:response></d:multistatus>`)
	}))
	defer server.Close()
	s, err := newWebDAVStore(server.URL+"/dav", "", "")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.List("Notes")
	if err != nil || len(entries) != 1 || entries[0].ETag != `"v1"` || entries[0].Size != 5 {
		t.Fatalf("listing=%+v %v", entries, err)
	}
}

func TestCanceledScanRetainsConfirmedFiles(t *testing.T) {
	const remoteRoot = "/dav/FreeMD/Notes"
	dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true, "/dav/FreeMD": true, remoteRoot: true}, files: map[string][]byte{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var gets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && gets.Add(1) == 4 {
			cancel()
			<-r.Context().Done()
			return
		}
		dav.ServeHTTP(w, r)
	}))
	defer server.Close()
	c := DefaultConfig()
	c.Enabled = true
	c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
	sp := Space{ID: "a", Root: t.TempDir(), Remote: "Notes"}
	state := t.TempDir()
	for i := range 8 {
		name := fmt.Sprintf("file-%02d.js", i)
		if err := os.WriteFile(filepath.Join(sp.Root, name), []byte("same"), 0600); err != nil {
			t.Fatal(err)
		}
		dav.files[remoteRoot+"/"+name] = []byte("same")
	}
	if _, err := Sync(ctx, c, sp, state, Hooks{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	gets.Store(100)
	if _, err := Sync(context.Background(), c, sp, state, Hooks{}); err != nil {
		t.Fatal(err)
	}
	if got := gets.Load() - 100; got != 5 {
		t.Fatalf("retry downloaded %d files; want only 5 unconfirmed files", got)
	}
}

func TestFailedWebDAVPropertiesAreNotTreatedAsDeletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/Notes/note.md</d:href><d:propstat><d:prop><d:getetag/></d:prop><d:status>HTTP/1.1 403 Forbidden</d:status></d:propstat></d:response></d:multistatus>`)
	}))
	defer server.Close()
	s, err := newWebDAVStore(server.URL+"/dav", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.List("Notes"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("unreadable properties accepted: %v", err)
	}
}

func TestResolveRemoteWithCachedValidatorKeepsContent(t *testing.T) {
	dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true}, files: map[string][]byte{}, omitListETag: true}
	server := httptest.NewServer(dav)
	defer server.Close()
	c := DefaultConfig()
	c.Enabled = true
	c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
	sp := Space{ID: "a", Root: t.TempDir(), Remote: "Notes"}
	state, file := t.TempDir(), filepath.Join(sp.Root, "note.md")
	if err := os.WriteFile(file, []byte("remote"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), c, sp, state, Hooks{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Sync(context.Background(), c, sp, state, Hooks{Resolve: map[string]Resolution{"note.md": {RemoteHash: hashBytes([]byte("remote"))}}})
	if err != nil || r.Downloaded != 1 {
		t.Fatalf("resolution=%+v %v", r, err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "remote" {
		t.Fatalf("resolved content=%q %v", data, err)
	}
}

func TestCancellationBeforeLastLocalWriteIsNotReportedAsSkip(t *testing.T) {
	dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true, "/dav/FreeMD": true, "/dav/FreeMD/Notes": true}, files: map[string][]byte{"/dav/FreeMD/Notes/note.md": []byte("remote")}}
	server := httptest.NewServer(dav)
	defer server.Close()
	c := DefaultConfig()
	c.Enabled = true
	c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
	sp := Space{ID: "a", Root: t.TempDir(), Remote: "Notes"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := Sync(ctx, c, sp, t.TempDir(), Hooks{BeforeApply: func(string) error { cancel(); return ctx.Err() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost at last file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sp.Root, "note.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled download wrote a file: %v", err)
	}
}
