package gitsync

import (
	"context"
	"strings"
	"testing"
)

func TestCredentialSessionScopesTokenAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env, close, e := CredentialSession(ctx, "owner/repo", "private-token")
	if e != nil {
		t.Fatal(e)
	}
	defer close()
	for _, entry := range env {
		if strings.Contains(entry, "private-token") {
			t.Fatal("token in environment")
		}
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "FREEFOLIO_") {
			t.Setenv(key, value)
		}
	}
	var out strings.Builder
	if e = CredentialHelper("get", strings.NewReader("protocol=https\nhost=github.com\npath=owner/repo.git\n\n"), &out); e != nil || !strings.Contains(out.String(), "password=private-token") {
		t.Fatal(out.String(), e)
	}
	out.Reset()
	if e = CredentialHelper("get", strings.NewReader("protocol=https\nhost=evil.test\npath=owner/repo.git\n\n"), &out); e == nil || out.Len() > 0 {
		t.Fatal("out-of-scope token disclosed")
	}
}
