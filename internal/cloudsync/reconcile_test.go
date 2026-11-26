package cloudsync

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A gateway can return an ETag which it refuses on PUT forever. A fresh
// read is insufficient; recovery requires the user's explicit direction.
func TestReconcileRecoversPersistentPreconditionFailure(t *testing.T) {
	for _, side := range []string{"local", "remote"} {
		t.Run(side, func(t *testing.T) {
			dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true}, files: map[string][]byte{}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec := httptest.NewRecorder()
				dav.ServeHTTP(rec, r)
				if r.Method == http.MethodGet && rec.Code == http.StatusOK {
					rec.Header().Set("ETag", `"unusable-download-tag"`)
				}
				for k, v := range rec.Header() {
					w.Header()[k] = v
				}
				w.WriteHeader(rec.Code)
				_, _ = w.Write(rec.Body.Bytes())
			}))
			defer server.Close()
			c := DefaultConfig()
			c.Enabled = true
			c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
			sp := Space{ID: "one", Root: t.TempDir(), Remote: "Notes"}
			state := t.TempDir()
			file := filepath.Join(sp.Root, "playwright.config.js")
			if err := os.WriteFile(file, []byte("remote version"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Sync(context.Background(), c, sp, state, Hooks{}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("local version"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, recheck := range []bool{false, true} {
				if _, err := Sync(context.Background(), c, sp, state, Hooks{Recheck: recheck}); !errors.Is(err, ErrRemoteChanged) {
					t.Fatalf("expected persistent precondition failure: %v", err)
				}
			}
			if err := os.WriteFile(filepath.Join(sp.Root, "local-only.txt"), []byte("local only"), 0600); err != nil {
				t.Fatal(err)
			}
			dav.mu.Lock()
			dav.files["/dav/FreeFolio/Notes/remote-only.txt"] = []byte("remote only")
			dav.mu.Unlock()
			r, err := Sync(context.Background(), c, sp, state, Hooks{Reconcile: side})
			if err != nil || len(r.Conflicts) != 0 || r.Deleted != 0 {
				t.Fatalf("reconcile: %+v %v", r, err)
			}
			want := side + " version"
			if data, _ := os.ReadFile(file); string(data) != want {
				t.Fatalf("local = %q", data)
			}
			dav.mu.Lock()
			got := string(dav.files["/dav/FreeFolio/Notes/playwright.config.js"])
			localOnly := string(dav.files["/dav/FreeFolio/Notes/local-only.txt"])
			dav.mu.Unlock()
			if got != want || localOnly != "local only" {
				t.Fatalf("remote=%q one-sided=%q", got, localOnly)
			}
			if data, _ := os.ReadFile(filepath.Join(sp.Root, "remote-only.txt")); string(data) != "remote only" {
				t.Fatal("lost remote-only file")
			}
			backups := 0
			_ = filepath.WalkDir(filepath.Join(state, "backups"), func(p string, d os.DirEntry, e error) error {
				if e == nil && !d.IsDir() && !strings.HasSuffix(p, "source.json") {
					backups++
				}
				return e
			})
			if backups == 0 {
				t.Fatal("no local recovery copy")
			}
			if r, err = Sync(context.Background(), c, sp, state, Hooks{}); err != nil || r.Uploaded != 0 || r.Downloaded != 0 {
				t.Fatalf("baseline did not converge: %+v %v", r, err)
			}
		})
	}
}

func TestOverwriteRequiresReadBackAndOrdinaryWritesStayConditional(t *testing.T) {
	for _, provider := range []string{"webdav", "s3"} {
		t.Run(provider, func(t *testing.T) {
			var storeHandler http.Handler
			if provider == "webdav" {
				storeHandler = &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true}, files: map[string][]byte{}}
			} else {
				storeHandler = &fakeS3{t: t, objects: map[string][]byte{}}
			}
			corrupt := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if corrupt && r.Method == http.MethodGet {
					_, _ = w.Write([]byte("different remote bytes"))
					return
				}
				storeHandler.ServeHTTP(w, r)
			}))
			defer server.Close()
			c := DefaultConfig()
			c.Provider = provider
			c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
			c.S3Endpoint, c.S3Region, c.S3Bucket, c.S3AccessKey, c.S3SecretKey = server.URL, "auto", "bucket", "key", "secret"
			s, err := openStore(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = overwriteRemote(s, "Notes/a.txt", []byte("first")); err != nil {
				t.Fatal(err)
			}
			if _, err = writeRemote(s, "Notes/a.txt", []byte("ordinary"), `"stale"`, true); !errors.Is(err, ErrRemoteChanged) {
				t.Fatalf("ordinary write stopped protecting remote: %v", err)
			}
			got, _, err := readRemote(s, "Notes/a.txt", "")
			if err != nil || !bytes.Equal(got, []byte("first")) {
				t.Fatal("ordinary write overwrote remote")
			}
			corrupt = true
			if _, err = overwriteRemote(s, "Notes/a.txt", []byte("second")); !errors.Is(err, ErrRemoteChanged) {
				t.Fatalf("unverified overwrite accepted: %v", err)
			}
		})
	}
}

func TestReconcilePreservesLocalEditsAndStopsOnBackupFailure(t *testing.T) {
	for _, side := range []string{"local", "remote"} {
		t.Run(side, func(t *testing.T) {
			dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true, "/dav/FreeFolio": true, "/dav/FreeFolio/Notes": true}, files: map[string][]byte{"/dav/FreeFolio/Notes/a.txt": []byte("remote")}}
			server := httptest.NewServer(dav)
			defer server.Close()
			c := DefaultConfig()
			c.Enabled = true
			c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
			sp := Space{ID: "one", Root: t.TempDir(), Remote: "Notes"}
			state := t.TempDir()
			file := filepath.Join(sp.Root, "a.txt")
			if err := os.WriteFile(file, []byte("local"), 0600); err != nil {
				t.Fatal(err)
			}
			r, err := Sync(context.Background(), c, sp, state, Hooks{Reconcile: side, BeforeApply: func(string) error { return os.WriteFile(file, []byte("active edit"), 0600) }})
			if err != nil || len(r.Skipped) != 1 {
				t.Fatalf("concurrent edit not skipped: %+v %v", r, err)
			}
			if err := os.WriteFile(filepath.Join(state, "backups"), []byte("block directory"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = Sync(context.Background(), c, sp, state, Hooks{Reconcile: side}); err == nil {
				t.Fatal("backup failure swallowed")
			}
			if data, _ := os.ReadFile(file); string(data) != "active edit" {
				t.Fatal("local edited despite backup failure")
			}
			dav.mu.Lock()
			remote := string(dav.files["/dav/FreeFolio/Notes/a.txt"])
			dav.mu.Unlock()
			if remote != "remote" {
				t.Fatal("remote edited despite backup failure")
			}
		})
	}
}

func TestReconcileRemoteDoesNotMarkPlaintextAsEncrypted(t *testing.T) {
	dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true, "/dav/FreeFolio": true, "/dav/FreeFolio/Notes": true}, files: map[string][]byte{"/dav/FreeFolio/Notes/a.txt": []byte("remote")}}
	server := httptest.NewServer(dav)
	defer server.Close()
	c := DefaultConfig()
	c.Enabled, c.Encrypt, c.EncryptPassword = true, true, "test-password"
	c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
	sp := Space{ID: "one", Root: t.TempDir(), Remote: "Notes"}
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(sp.Root, "a.txt"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), c, sp, state, Hooks{Reconcile: "remote"}); err != nil {
		t.Fatal(err)
	}
	if r, err := Sync(context.Background(), c, sp, state, Hooks{}); err != nil || r.Uploaded != 1 {
		t.Fatalf("encryption was silently bypassed: %+v %v", r, err)
	}
	dav.mu.Lock()
	raw := bytes.Clone(dav.files["/dav/FreeFolio/Notes/a.txt"])
	dav.mu.Unlock()
	plain, err := openBackup(raw, c.EncryptPassword)
	if err != nil || string(plain) != "remote" || bytes.Equal(raw, plain) {
		t.Fatal("remote content was not preserved and encrypted")
	}
}
