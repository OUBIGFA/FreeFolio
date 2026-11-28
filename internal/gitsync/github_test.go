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
	base := "protocol=https\nhost=github.com\npath=owner/repo.git\n"
	// HTTPS Git requests announce both capabilities before the repository fields.
	// Array attributes may repeat; identity fields must remain unambiguous.
	capabilities := "capability[]=authtype\ncapability[]=state\n"
	for _, test := range []struct {
		name, query string
		allowed     bool
	}{
		{"basic", base, true},
		{"https capabilities", capabilities + base + "wwwauth[]=Basic realm=\"GitHub\"\n", true},
		{"multiple challenges", capabilities + base + "wwwauth[]=Basic realm=\"GitHub\"\nwwwauth[]=Bearer\n", true},
		{"multiple states", capabilities + base + "state[]=first\nstate[]=second\n", true},
		{"unknown array", base + "extension[]=first\nextension[]=second\n", true},
		{"crlf", strings.ReplaceAll(capabilities+base, "\n", "\r\n"), true},
		{"lfs", capabilities + "protocol=https\nhost=github.com\npath=owner/repo.git/info/lfs\n", true},
		{"other host", capabilities + "protocol=https\nhost=evil.test\npath=owner/repo.git\n", false},
		{"other repository", capabilities + "protocol=https\nhost=github.com\npath=owner/other.git\n", false},
		{"duplicate protocol", capabilities + base + "protocol=https\n", false},
		{"conflicting protocol", capabilities + "protocol=http\n" + base, false},
		{"duplicate host", capabilities + base + "host=github.com\n", false},
		{"conflicting host", capabilities + "host=evil.test\n" + base, false},
		{"duplicate path", capabilities + base + "path=owner/repo.git\n", false},
		{"conflicting path", capabilities + "path=owner/other.git\n" + base, false},
		{"array host is not identity", capabilities + "protocol=https\nhost[]=github.com\npath=owner/repo.git\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out strings.Builder
			err := CredentialHelper("get", strings.NewReader(test.query+"\n"), &out)
			if test.allowed {
				if err != nil || out.String() != "username=x-access-token\npassword=private-token\n\n" {
					t.Fatalf("credential response missing or invalid: %v", err)
				}
			} else if err == nil || out.Len() > 0 {
				t.Fatal("invalid credential request was not rejected without output")
			}
		})
	}
}
