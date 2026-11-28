package gitsync

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func CredentialAllowed(query, repository string) bool {
	fields := map[string]string{}
	for _, line := range strings.Split(query, "\n") {
		k, v, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if ok {
			// Git's capability[], wwwauth[] and state[] attributes are multi-valued.
			// They do not select the credential scope, so ignore them while keeping
			// duplicate scalar fields (especially protocol, host and path) invalid.
			if strings.HasSuffix(k, "[]") {
				continue
			}
			if _, exists := fields[k]; exists {
				return false
			}
			fields[k] = v
		}
	}
	path := strings.TrimPrefix(fields["path"], "/")
	allowed := strings.TrimSuffix(path, ".git") == repository || path == repository+".git/info/lfs" || path == repository+"/info/lfs"
	return fields["protocol"] == "https" && fields["host"] == "github.com" && allowed
}

// CredentialSession exposes the token only to a short-lived loopback endpoint,
// protected by a random capability. Git receives no token in its arguments or
// environment; the helper additionally verifies host and repository scope.
func CredentialSession(ctx context.Context, repository, token string) ([]string, func(), error) {
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return nil, nil, e
	}
	var nonce [32]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		ln.Close()
		return nil, nil, e
	}
	capability := hex.EncodeToString(nonce[:])
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 8192}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/credential" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(capability)) != 1 {
			http.Error(w, "Denied", 403)
			return
		}
		b, e := io.ReadAll(io.LimitReader(r.Body, 8192))
		if e != nil || !CredentialAllowed(string(b), repository) {
			http.Error(w, "Denied", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, "username=x-access-token\npassword=%s\n\n", token)
	})
	go server.Serve(ln)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			server.Close()
		case <-done:
		}
	}()
	exe, e := os.Executable()
	if e != nil {
		server.Close()
		close(done)
		return nil, nil, e
	}
	exe = strings.ReplaceAll(exe, "\\", "/")
	quote := "'" + strings.ReplaceAll(exe, "'", "'\\''") + "'"
	env := []string{"FREEFOLIO_GIT_CREDENTIAL_ENDPOINT=http://" + ln.Addr().String() + "/credential", "FREEFOLIO_GIT_CREDENTIAL_CAPABILITY=" + capability, "GIT_CONFIG_COUNT=3", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_KEY_1=credential.helper", "GIT_CONFIG_VALUE_1=!" + quote + " git-credential", "GIT_CONFIG_KEY_2=credential.useHttpPath", "GIT_CONFIG_VALUE_2=true"}
	return env, func() { server.Close(); close(done) }, nil
}
func CredentialHelper(action string, in io.Reader, out io.Writer) error {
	if action != "get" {
		return nil
	}
	address := os.Getenv("FREEFOLIO_GIT_CREDENTIAL_ENDPOINT")
	if !strings.HasPrefix(address, "http://127.0.0.1:") {
		return fmt.Errorf("credential endpoint unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "POST", address, io.LimitReader(in, 8192))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", os.Getenv("FREEFOLIO_GIT_CREDENTIAL_CAPABILITY"))
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("credential request rejected")
	}
	_, e = io.Copy(out, io.LimitReader(res.Body, 8192))
	return e
}
