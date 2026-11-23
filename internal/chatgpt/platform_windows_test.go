package chatgpt

import (
	"bytes"
	"context"
	"testing"
)

func TestWindowsDPAPIPersistsAcrossClients(t *testing.T) {
	c := New(t.TempDir())
	err := c.withState(context.Background(), func(s *savedState) error {
		s.Profiles["account"] = profile{ClientID: "oaiapp_test", Status: Disconnected, Email: "synthetic@example.invalid"}
		return c.save(s)
	})
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(c.dir)
	s, err := restarted.Session(context.Background(), "account")
	if err != nil || s.Email != "synthetic@example.invalid" {
		t.Fatal("DPAPI restart", err)
	}
	data := []byte("synthetic credential")
	sealed, err := protect(c.dir, data, true)
	if err != nil || bytes.Contains(sealed, data) {
		t.Fatal("DPAPI encryption", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err = protect(c.dir, sealed, false); err == nil {
		t.Fatal("accepted altered ciphertext")
	}
}
