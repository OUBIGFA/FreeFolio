package gitsync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The subprocess implements the external credential protocol, so tests cover
// actual process IO, cancellation and profile isolation without a real account.
func TestCredentialManagerProcess(t *testing.T) {
	if os.Getenv("FREEFOLIO_TEST_GCM_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	args = args[1:]
	dir := os.Getenv("FREEFOLIO_TEST_GCM_DIR")
	namespace := os.Getenv("GCM_NAMESPACE")
	if !strings.HasPrefix(namespace, "freefolio-") {
		os.Exit(3)
	}
	os.MkdirAll(dir, 0700)
	f, _ := os.OpenFile(filepath.Join(dir, "calls"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	fmt.Fprintln(f, strings.Join(args, " ")+";interactive="+os.Getenv("GCM_INTERACTIVE")+";namespace="+namespace)
	f.Close()
	path := filepath.Join(dir, namespace)
	if len(args) > 1 && args[0] == "github" {
		switch args[1] {
		case "login":
			if !strings.Contains(strings.Join(args, " "), "--browser") || os.Getenv("GCM_INTERACTIVE") != "true" {
				os.Exit(4)
			}
			os.WriteFile(path, []byte("octo"), 0600)
		case "list":
			if _, e := os.Stat(path); e == nil {
				fmt.Println("octo")
			}
		case "logout":
			os.Remove(path)
		}
	} else if len(args) > 0 && args[0] == "get" {
		input, _ := io.ReadAll(os.Stdin)
		if os.Getenv("GCM_INTERACTIVE") != "false" || !strings.Contains(string(input), "username=octo") {
			os.Exit(5)
		}
		if _, e := os.Stat(path); e != nil {
			fmt.Fprintln(os.Stderr, "credential unavailable ghp_must_not_leak")
			os.Exit(6)
		}
		fmt.Print("protocol=https\nhost=github.com\nusername=octo\npassword=fixture-token\n\n")
	}
	os.Exit(0)
}

func fixtureManager(t *testing.T, dir string) *credentialManager {
	t.Helper()
	m := &credentialManager{Path: os.Args[0], GitPath: "git", Dir: dir}
	m.command = func(ctx context.Context, args ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCredentialManagerProcess$", "--"}, args...)...)
		c.Env = append(os.Environ(), "FREEFOLIO_TEST_GCM_PROCESS=1", "FREEFOLIO_TEST_GCM_DIR="+dir)
		return c
	}
	return m
}

func TestGitHubBrowserLoginNeedsNoRegistration(t *testing.T) {
	dir := t.TempDir()
	m := fixtureManager(t, filepath.Join(dir, "manager"))
	c := NewGitHub(filepath.Join(dir, "credentials"))
	c.ensureManager = func(context.Context, func(RuntimeProgress)) (*credentialManager, error) { return m, nil }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error(r.URL, r.Header.Get("Authorization"))
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"id":42,"login":"octo","name":"Octo"}`)
	}))
	defer server.Close()
	c.api = server.URL
	c.client = server.Client()
	a, e := c.Login(context.Background(), nil)
	if e != nil || a.ID != "42" || a.Login != "octo" {
		t.Fatal(a, e)
	}
	token, e := c.Token(context.Background(), a.ID)
	if e != nil || token != "fixture-token" {
		t.Fatal(e)
	}
	accounts, e := c.Accounts()
	if e != nil || len(accounts) != 1 {
		t.Fatal(accounts, e)
	}
	var profile githubToken
	c.withStore(context.Background(), func(s *accountStore) error { profile = s.Accounts[a.ID]; return nil })
	if profile.Namespace == "" {
		t.Fatal("manager namespace not saved")
	}
	// Tokens belong to the manager and never enter the app profile JSON.
	data, _ := os.ReadFile(filepath.Join(c.dir, "github-accounts"))
	plain, e := protect(c.dir, data, false)
	if e != nil || strings.Contains(string(plain), "fixture-token") {
		t.Fatal("profile contains manager token", e)
	}
	if e = c.Disconnect(context.Background(), a.ID); e != nil {
		t.Fatal(e)
	}
	accounts, e = c.Accounts()
	if e != nil || len(accounts) != 0 {
		t.Fatal(accounts, e)
	}
	calls, _ := os.ReadFile(filepath.Join(m.Dir, "calls"))
	if !strings.Contains(string(calls), "github login --url https://github.com --browser --force") || !strings.Contains(string(calls), "interactive=false") || strings.Contains(string(calls), "fixture-token") {
		t.Fatal(string(calls))
	}
}

func TestCredentialManagerEnvironmentRejectsInheritedTracingAndPlaintext(t *testing.T) {
	t.Setenv("GCM_TRACE", "1")
	t.Setenv("GCM_TRACE_SECRETS", "1")
	t.Setenv("GCM_CREDENTIAL_STORE", "plaintext")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "leaky")
	m := fixtureManager(t, t.TempDir())
	env := m.filterEnvironment(os.Environ(), "freefolio-test", false)
	for _, entry := range env {
		if strings.HasPrefix(entry, "GCM_TRACE") || strings.HasPrefix(entry, "GIT_CONFIG_KEY") || entry == "GCM_CREDENTIAL_STORE=plaintext" {
			t.Fatal("unsafe inherited configuration", entry)
		}
	}
	if !strings.Contains(strings.Join(env, "\n"), "GCM_INTERACTIVE=false") {
		t.Fatal("background authentication could open browser")
	}
}

func TestCredentialManagerDoesNotLeakFailedCommandOutput(t *testing.T) {
	m := fixtureManager(t, t.TempDir())
	_, e := m.Token(context.Background(), "freefolio-missing", "octo")
	if e == nil || strings.Contains(e.Error(), "ghp_") {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = m.run(ctx, "freefolio-test", true, "", "github", "login", "--browser"); e != context.Canceled {
		t.Fatal(e)
	}
}

func TestOfficialGCMIsolatedCredentialLifecycle(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses Windows DPAPI")
	}
	archive := os.Getenv("FREEFOLIO_TEST_GCM_ARCHIVE")
	if archive == "" {
		t.Skip("set official GCM archive for integration test")
	}
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "runtime")
	os.Mkdir(runtimeDir, 0700)
	spec := managerArtifacts["windows/amd64"]
	if e := extractRuntime(archive, runtimeDir, spec.Digest); e != nil {
		t.Fatal(e)
	}
	gitPath, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	m := &credentialManager{Path: filepath.Join(runtimeDir, managerName()), GitPath: gitPath, Dir: filepath.Join(dir, "credentials")}
	ctx := context.Background()
	if !supportedManager(ctx, m.Path) {
		t.Fatal("official runtime did not start")
	}
	ns1, ns2 := "freefolio-fixture-first", "freefolio-fixture-second"
	input := "protocol=https\nhost=github.com\nusername=freefolio-fixture\npassword=fixture-not-a-real-token\n\n"
	for _, ns := range []string{ns1, ns2} {
		if _, e = m.run(ctx, ns, false, input, "store"); e != nil {
			t.Fatal(e)
		}
	}
	token, e := m.Token(ctx, ns1, "freefolio-fixture")
	if e != nil || token != "fixture-not-a-real-token" {
		t.Fatal("isolated token retrieval failed", e)
	}
	var count int
	e = filepath.WalkDir(filepath.Join(m.Dir, "gcm"), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		count++
		data, e := os.ReadFile(path)
		if e == nil && bytes.Contains(data, []byte("fixture-not-a-real-token")) {
			t.Error("GCM stored plaintext")
		}
		return e
	})
	if e != nil || count == 0 {
		t.Fatal("encrypted credential files missing", e)
	}
	if e = m.Logout(ctx, ns1, "freefolio-fixture"); e != nil {
		t.Fatal(e)
	}
	first, e := m.run(ctx, ns1, false, "", "github", "list")
	if e != nil || len(bytes.TrimSpace(first)) != 0 {
		t.Fatal("account remains after logout", e)
	}
	token, e = m.Token(ctx, ns2, "freefolio-fixture")
	if e != nil || token != "fixture-not-a-real-token" {
		t.Fatal("logout affected another namespace", e)
	}
	if e = m.Logout(ctx, ns2, "freefolio-fixture"); e != nil {
		t.Fatal(e)
	}
}

func TestGCMArchiveRejectsUnixLinks(t *testing.T) {
	for _, link := range []bool{false, true} {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		h := &tar.Header{Name: "git-credential-manager", Mode: 0700, Size: 4, Typeflag: tar.TypeReg}
		if link {
			h.Typeflag = tar.TypeSymlink
			h.Linkname = "../../outside"
			h.Size = 0
		}
		if e := tw.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if !link {
			tw.Write([]byte("test"))
		}
		tw.Close()
		gz.Close()
		p := filepath.Join(t.TempDir(), "gcm.tar.gz.partial")
		os.WriteFile(p, b.Bytes(), 0600)
		dest := t.TempDir()
		e := extractRuntime(p, dest, fmt.Sprintf("%x", sha256.Sum256(b.Bytes())))
		if link && e == nil || !link && e != nil {
			t.Fatal("unexpected tar result", e)
		}
		if !link {
			info, e := os.Stat(filepath.Join(dest, "git-credential-manager"))
			if e != nil || info.Size() != 4 {
				t.Fatal(e)
			}
		}
	}
}

func TestRejectedGitHubIdentityDoesNotPublishAccount(t *testing.T) {
	dir := t.TempDir()
	m := fixtureManager(t, filepath.Join(dir, "manager"))
	c := NewGitHub(filepath.Join(dir, "credentials"))
	c.ensureManager = func(context.Context, func(RuntimeProgress)) (*credentialManager, error) { return m, nil }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":42,"login":"different","name":"Wrong account"}`)
	}))
	defer srv.Close()
	c.api = srv.URL
	c.client = srv.Client()
	if _, e := c.Login(context.Background(), nil); e == nil {
		t.Fatal("mismatched account accepted")
	}
	accounts, e := c.Accounts()
	if e != nil || len(accounts) != 0 {
		t.Fatal(accounts, e)
	}
	files, _ := filepath.Glob(filepath.Join(m.Dir, "freefolio-*"))
	if len(files) != 0 {
		t.Fatal("failed login credentials retained", files)
	}
}

func TestLegacyAccountRequiresReconnectWithoutLeakingOldToken(t *testing.T) {
	c := NewGitHub(t.TempDir())
	old := map[string]any{"Version": 1, "Accounts": map[string]any{"42": map[string]any{"Account": Account{ID: "42", Login: "octo"}, "Access": "legacy-secret", "Refresh": "legacy-refresh"}}}
	plain, _ := json.Marshal(old)
	sealed, e := protect(c.dir, plain, true)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(c.dir, "github-accounts"), sealed, 0600)
	a, e := c.Accounts()
	if e != nil || len(a) != 1 {
		t.Fatal(a, e)
	}
	token, e := c.Token(context.Background(), "42")
	if e == nil || token != "" || strings.Contains(e.Error(), "legacy-secret") {
		t.Fatal("legacy auth reused", e)
	}
	if e = c.Disconnect(context.Background(), "42"); e != nil {
		t.Fatal(e)
	}
}
