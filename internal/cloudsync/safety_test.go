package cloudsync

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConditionalUploadDoesNotOverwriteAConcurrentRemoteEdit(t *testing.T) {
	data := []byte("new remote version")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "MKCOL" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodPut {
			if r.Header.Get("If-Match") != `"current"` {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			data, _ = io.ReadAll(r.Body)
		}
	}))
	defer server.Close()
	s, err := newWebDAVStore(server.URL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = writeRemote(s, "note.md", []byte("local overwrite"), `"stale"`, true); !errors.Is(err, errRemoteChanged) {
		t.Fatalf("stale overwrite: %v", err)
	}
	if string(data) != "new remote version" {
		t.Fatal("concurrent content overwritten")
	}
	if err = writeRemote(s, "note.md", nil, "", true); err == nil {
		t.Fatal("missing validator accepted")
	}
}

func TestEnablingEncryptionConvertsExistingContent(t *testing.T) {
	c := DefaultConfig()
	c.Enabled = true
	c.Provider = "s3"
	fake := &fakeS3{t: t, objects: map[string][]byte{}}
	server := httptest.NewServer(fake)
	defer server.Close()
	c.S3Endpoint, c.S3Region, c.S3Bucket, c.S3AccessKey, c.S3SecretKey = server.URL, "auto", "bucket", "key", "secret"
	sp := Space{ID: "one", Root: t.TempDir(), Remote: "Notes"}
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(sp.Root, "note.md"), []byte("content to encrypt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), c, sp, state, Hooks{}); err != nil {
		t.Fatal(err)
	}
	c.Encrypt, c.EncryptPassword = true, "fixture-encryption-password"
	if r, err := Sync(context.Background(), c, sp, state, Hooks{}); err != nil || r.Uploaded != 1 {
		t.Fatalf("encryption conversion: %+v %v", r, err)
	}
	fake.mu.Lock()
	encrypted := bytes.Clone(fake.objects["FreeMD/Notes/note.md"])
	fake.mu.Unlock()
	if bytes.Contains(encrypted, []byte("content to encrypt")) {
		t.Fatal("remote content stayed plaintext")
	}
	plain, err := openBackup(encrypted, c.EncryptPassword)
	if err != nil || string(plain) != "content to encrypt" {
		t.Fatalf("encrypted content: %q %v", plain, err)
	}
	if _, err := Sync(context.Background(), c, sp, state, Hooks{}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteNamesAndMappingsCannotEscapeTheirSpace(t *testing.T) {
	c := DefaultConfig()
	c.Spaces = map[string]string{"a": "Notes", "b": "Notes/Other"}
	if err := c.ValidatePlan(); err == nil {
		t.Fatal("overlapping mapping accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		io.WriteString(w, `<multistatus xmlns="DAV:"><response><href>/dav/Notes/..</href><propstat><prop><getcontentlength>1</getcontentlength></prop></propstat></response></multistatus>`)
	}))
	defer server.Close()
	s, err := newWebDAVStore(server.URL+"/dav", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = listRemote(s, "Notes"); err == nil {
		t.Fatal("remote traversal accepted")
	}
	for _, endpoint := range []string{"https://user:password@example.com", "https://example.com/?token=secret", "http://example.com"} {
		if err := validEndpoint(endpoint); err == nil {
			t.Fatalf("invalid endpoint accepted: %s", endpoint)
		}
	}
}

func TestCancellationAndFailedTrashKeepLocalFiles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := DefaultConfig()
	c.Enabled = true
	c.WebDAVURL = "https://example.invalid/dav"
	sp := Space{ID: "one", Root: t.TempDir(), Remote: "Notes"}
	if _, err := Sync(ctx, c, sp, t.TempDir(), Hooks{}); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("cancelled operation: %v", err)
	}
	// A missing recycle-bin implementation must never be replaced by hard delete.
	data := []byte("kept")
	if err := os.WriteFile(filepath.Join(sp.Root, "keep.md"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := backupLocal(t.TempDir(), "test", "keep.md", data, 5); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(sp.Root, "keep.md")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("backup removed source")
	}
}
