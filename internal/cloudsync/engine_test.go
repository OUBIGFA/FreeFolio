package cloudsync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Both tests exercise real HTTP transport, signing, local files and checkpoints.
func TestTwoDevicesConvergeAndPreserveConflicts(t *testing.T) {
	for _, provider := range []string{"webdav", "s3"} {
		t.Run(provider, func(t *testing.T) {
			c := DefaultConfig()
			c.Enabled = true
			c.Provider = provider
			var handler http.Handler
			if provider == "webdav" {
				dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true}, files: map[string][]byte{}}
				handler = dav
			} else {
				handler = &fakeS3{t: t, objects: map[string][]byte{}}
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
			c.S3Endpoint, c.S3Region, c.S3Bucket, c.S3AccessKey, c.S3SecretKey = server.URL, "auto", "bucket", "key", "secret"
			a := Space{ID: "a", Root: t.TempDir(), Remote: "我的笔记"}
			b := Space{ID: "b", Root: t.TempDir(), Remote: a.Remote}
			astate, bstate := t.TempDir(), t.TempDir()
			write := func(s Space, text string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(s.Root, "note.md"), []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			run := func(s Space, state string) Result {
				t.Helper()
				r, e := Sync(context.Background(), c, s, state, Hooks{Trash: os.Remove})
				if e != nil {
					t.Fatal(e)
				}
				return r
			}
			write(a, "base")
			if r := run(a, astate); r.Uploaded != 1 {
				t.Fatalf("upload: %+v", r)
			}
			if r := run(b, bstate); r.Downloaded != 1 {
				t.Fatalf("download: %+v", r)
			}
			if data, _ := os.ReadFile(filepath.Join(b.Root, "note.md")); string(data) != "base" {
				t.Fatalf("downloaded %q", data)
			}
			write(b, "updated")
			if r := run(b, bstate); r.Uploaded != 1 {
				t.Fatalf("update: %+v", r)
			}
			if r := run(a, astate); r.Downloaded != 1 {
				t.Fatalf("receive: %+v", r)
			}
			write(a, "local A")
			write(b, "local B")
			run(a, astate)
			if r := run(b, bstate); len(r.Conflicts) != 1 {
				t.Fatalf("conflict: %+v", r)
			}
			if data, _ := os.ReadFile(filepath.Join(b.Root, "note.md")); string(data) != "local B" {
				t.Fatalf("local overwritten: %q", data)
			}
			copies, _ := filepath.Glob(filepath.Join(b.Root, "note (同步冲突 *).md"))
			if len(copies) != 1 {
				t.Fatalf("conflict copies: %v", copies)
			}
			if data, _ := os.ReadFile(copies[0]); string(data) != "local A" {
				t.Fatalf("remote conflict missing: %q", data)
			}
			// An explicit decision is bound to the inspected remote digest.
			resolved, e := Sync(context.Background(), c, b, bstate, Hooks{Trash: os.Remove, Resolve: map[string]Resolution{"note.md": {UseLocal: false, RemoteHash: hashBytes([]byte("local A"))}}})
			if e != nil || resolved.Downloaded != 1 {
				t.Fatalf("resolution: %+v %v", resolved, e)
			}
			if data, _ := os.ReadFile(filepath.Join(b.Root, "note.md")); string(data) != "local A" {
				t.Fatalf("resolution content %q", data)
			}
			if err := os.Remove(filepath.Join(a.Root, "note.md")); err != nil {
				t.Fatal(err)
			}
			if r := run(a, astate); r.Deleted != 1 {
				t.Fatalf("remote delete: %+v", r)
			}
			if r := run(b, bstate); r.Deleted != 1 {
				t.Fatalf("local trash: %+v", r)
			}
			if _, err := os.Stat(filepath.Join(b.Root, "note.md")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("deleted note remains")
			}
			backups, _ := filepath.Glob(filepath.Join(bstate, "backups", "*", "*", "*"))
			if len(backups) == 0 {
				t.Fatal("missing recovery backup")
			}
		})
	}
}

func TestSyncProtectsEditsAndCredentialBoundaries(t *testing.T) {
	c := DefaultConfig()
	c.Enabled = true
	dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true}, files: map[string][]byte{}}
	server := httptest.NewServer(dav)
	defer server.Close()
	c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = server.URL+"/dav", "me", "secret"
	a := Space{ID: "a", Root: t.TempDir(), Remote: "Notes"}
	b := Space{ID: "b", Root: t.TempDir(), Remote: "Notes"}
	if err := os.WriteFile(filepath.Join(a.Root, "note.md"), []byte("remote"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), c, a, t.TempDir(), Hooks{}); err != nil {
		t.Fatal(err)
	}
	r, err := Sync(context.Background(), c, b, t.TempDir(), Hooks{BeforeApply: func(string) error { return errors.New("active edit") }})
	if err != nil || len(r.Skipped) != 1 {
		t.Fatalf("guard: %+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(b.Root, "note.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("guarded file was written")
	}
	c.WebDAVURL = "http://example.com/dav"
	if _, err := openStore(context.Background(), c); err == nil {
		t.Fatal("remote plaintext connection accepted")
	}
	for _, p := range []string{"../outside", "a/../../secret", "a\\secret", "/absolute", "a:b", "a/./b"} {
		if safeRelative(p) {
			t.Fatalf("unsafe path %q", p)
		}
	}
	for _, p := range []string{".freefolio/history/a", "credentials/password", "prefs.json", "_temp/a"} {
		if !excluded(p) {
			t.Fatalf("private path not excluded: %q", p)
		}
	}
}

func TestConfigAndEncryptedContentRoundTrip(t *testing.T) {
	c := DefaultConfig()
	c.WebDAVPassword = "test-webdav-password"
	c.S3SecretKey = "test-s3-secret"
	c.EncryptPassword = "test-content-password"
	c.Encrypt = true
	dir := t.TempDir()
	if err := SaveConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "sync.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{c.WebDAVPassword, c.S3SecretKey, c.EncryptPassword} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("credential leaked into config")
		}
	}
	back, err := LoadConfig(dir)
	if err != nil || back.WebDAVPassword != c.WebDAVPassword || back.EncryptPassword != c.EncryptPassword {
		t.Fatalf("credential roundtrip: %v", err)
	}
	sealed, err := sealBackup([]byte("private note"), c.EncryptPassword)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "private note") {
		t.Fatal("unencrypted note")
	}
	plain, err := openBackup(sealed, c.EncryptPassword)
	if err != nil || string(plain) != "private note" {
		t.Fatalf("decrypt: %q %v", plain, err)
	}
	if _, err := openBackup(sealed, "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
	now := time.Date(2026, 11, 20, 22, 0, 0, 0, time.FixedZone("local", 8*3600))
	c.Schedule = "daily"
	c.DailyTime = "21:00"
	if next := c.Next(now); next.Day() != 21 || next.Hour() != 21 {
		t.Fatalf("daily schedule: %v", next)
	}
}
