package cloudsync

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The local HTTP server exercises the merge's checkpoint and guarded writes;
// document parsing and paragraph alignment have separate editor/domain tests.
func TestSyncMergeUsesCommonBytesAndGuardsActiveEdits(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "encrypted"}[encrypted], func(t *testing.T) {
			c := DefaultConfig()
			c.Enabled = true
			c.Encrypt = encrypted
			c.EncryptPassword = "content-password"
			dav := &fakeDAV{t: t, dirs: map[string]bool{"": true, "/dav": true}, files: map[string][]byte{}}
			srv := httptest.NewServer(dav)
			defer srv.Close()
			c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword = srv.URL+"/dav", "me", "secret"
			a, b := Space{"a", t.TempDir(), "Notes"}, Space{"b", t.TempDir(), "Notes"}
			as, bs := t.TempDir(), t.TempDir()
			const base = "one\n\ntwo\n"
			const local = "local one\n\ntwo\n"
			const remote = "one\n\nremote two\n"
			const merged = "local one\n\nremote two\n"
			write := func(sp Space, s string) {
				t.Helper()
				if e := os.WriteFile(filepath.Join(sp.Root, "note.md"), []byte(s), 0600); e != nil {
					t.Fatal(e)
				}
			}
			calls := 0
			h := Hooks{Merge: func(p string, ancestor, ours, theirs []byte) ([]byte, bool) {
				calls++
				if p != "note.md" || string(ancestor) != base || string(ours) != local || string(theirs) != remote {
					t.Fatalf("wrong merge inputs: %s %q %q %q", p, ancestor, ours, theirs)
				}
				return []byte(merged), true
			}}
			run := func(sp Space, dir string, h Hooks) Result {
				t.Helper()
				r, e := Sync(context.Background(), c, sp, dir, h)
				if e != nil {
					t.Fatal(e)
				}
				return r
			}
			write(a, base)
			run(a, as, h)
			run(b, bs, h)
			write(a, remote)
			run(a, as, h)
			write(b, local)
			blocked := h
			blocked.BeforeApply = func(string) error { return os.ErrPermission }
			if r := run(b, bs, blocked); len(r.Skipped) != 1 || r.Uploaded != 0 {
				t.Fatalf("guard: %+v", r)
			}
			if got, _ := os.ReadFile(filepath.Join(b.Root, "note.md")); string(got) != local {
				t.Fatal("active edit replaced")
			}
			if r := run(b, bs, h); r.Merged != 1 || len(r.Conflicts) != 0 {
				t.Fatalf("merge result: %+v", r)
			}
			run(a, as, h)
			for _, sp := range []Space{a, b} {
				got, _ := os.ReadFile(filepath.Join(sp.Root, "note.md"))
				if string(got) != merged {
					t.Fatalf("not converged: %q", got)
				}
			}
			if calls == 0 {
				t.Fatal("merge hook not called")
			}
			if encrypted {
				filepath.WalkDir(bs, func(p string, e os.DirEntry, err error) error {
					if err == nil && !e.IsDir() && strings.Contains(p, "ancestors") {
						raw, _ := os.ReadFile(p)
						if strings.Contains(string(raw), base) {
							t.Error("unencrypted ancestor")
						}
					}
					return err
				})
			}
		})
	}
}
