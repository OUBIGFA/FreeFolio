package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture serves GitHub's public API shape and disposable bytes, never an
// executable installer. Selection, download and hash validation stay real.
func installerFeed(t *testing.T, payload []byte, releases []map[string]any) (*Client, *int) {
	t.Helper()
	downloads := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Error("release request must use GitHub's JSON API")
			}
			json.NewEncoder(w).Encode(releases)
		case "/releases/assets/1":
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Error("asset request must download bytes")
			}
			*downloads++
			w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(filepath.Join(t.TempDir(), "updates"), "0.1.0")
	client.api, client.http = server.URL, server.Client()
	return client, downloads
}

func releaseFixture(version string, payload []byte) map[string]any {
	sum := sha256.Sum256(payload)
	return map[string]any{
		"tag_name": "v" + version, "body": "Release notes",
		"assets": []map[string]any{{"id": 1, "name": "FreeMD-" + version + "-Setup.exe",
			"size": len(payload), "digest": "sha256:" + hex.EncodeToString(sum[:])}},
	}
}

func TestInstallerDownloadVerifiesAndReusesCache(t *testing.T) {
	payload := []byte("disposable installer fixture, never executed")
	client, downloads := installerFeed(t, payload, []map[string]any{releaseFixture("0.2.0", payload)})
	release, err := client.Check(context.Background())
	if err != nil || release == nil || release.Version != "0.2.0" || release.Notes != "Release notes" {
		t.Fatalf("release: %+v, %v", release, err)
	}
	var done, total int64
	if err := release.Download(context.Background(), func(d, n int64) { done, total = d, n }); err != nil {
		t.Fatal(err)
	}
	path, err := release.VerifiedPath()
	if err != nil || done != int64(len(payload)) || total != done {
		t.Fatalf("verified download and progress: %q %d/%d %v", path, done, total, err)
	}
	if bytes, err := os.ReadFile(path); err != nil || string(bytes) != string(payload) {
		t.Fatalf("downloaded bytes: %q, %v", bytes, err)
	}
	// A fresh check after restarting can reuse only bytes matching that release.
	release, err = client.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := release.Download(context.Background(), nil); err != nil || *downloads != 1 {
		t.Fatalf("verified cache should avoid another download: %d, %v", *downloads, err)
	}
	if err := os.WriteFile(path, []byte("changed after downloading"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := release.VerifiedPath(); err == nil {
		t.Fatal("a modified installer must not be launched")
	}
	if err := release.Download(context.Background(), nil); err != nil || *downloads != 2 {
		t.Fatalf("retry must replace the damaged cache: %d, %v", *downloads, err)
	}
}

func TestInstallerSelectsOnlyMatchingStableFreeMDAssets(t *testing.T) {
	payload := []byte("fixture")
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"draft", func(r map[string]any) { r["draft"] = true }},
		{"prerelease", func(r map[string]any) { r["prerelease"] = true }},
		{"beta tag", func(r map[string]any) { r["tag_name"] = "v0.2.0-beta.1" }},
		{"unrelated tag", func(r map[string]any) { r["tag_name"] = "freetype-0.2.0" }},
		{"short tag", func(r map[string]any) { r["tag_name"] = "v0.2" }},
		{"noncanonical tag", func(r map[string]any) { r["tag_name"] = "v00.2.0" }},
		{"mismatched version", func(r map[string]any) { r["tag_name"] = "v0.3.0" }},
		{"unrelated installer", func(r map[string]any) { r["assets"].([]map[string]any)[0]["name"] = "FreeType-0.2.0-Setup.exe" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			release := releaseFixture("0.2.0", payload)
			test.change(release)
			client, _ := installerFeed(t, payload, []map[string]any{release})
			if got, err := client.Check(context.Background()); got != nil || !errors.Is(err, ErrNoInstaller) {
				t.Fatalf("ineligible release must not be offered: %+v, %v", got, err)
			}
		})
	}
	client, _ := installerFeed(t, payload, []map[string]any{releaseFixture("0.9.0", payload), releaseFixture("0.10.0", payload), releaseFixture("0.2.0", payload)})
	release, err := client.Check(context.Background())
	if err != nil || release.Version != "0.10.0" {
		t.Fatalf("select the highest numeric version: %+v, %v", release, err)
	}
	client.current = "0.10.0"
	if release, err := client.Check(context.Background()); release != nil || err != nil {
		t.Fatalf("the installed version is current: %+v, %v", release, err)
	}
	client.current = "1.0.0"
	if release, err := client.Check(context.Background()); release != nil || err != nil {
		t.Fatalf("never offer a downgrade: %+v, %v", release, err)
	}
}

func TestInstallerRejectsMissingDigestAndInvalidSize(t *testing.T) {
	for _, test := range []struct {
		field string
		value any
	}{
		{"digest", ""}, {"digest", "sha256:1234"}, {"digest", strings.Repeat("a", 64)},
		{"size", 0}, {"size", maxInstallerSize + 1},
	} {
		t.Run(fmt.Sprint(test.field, test.value), func(t *testing.T) {
			release := releaseFixture("0.2.0", []byte("fixture"))
			release["assets"].([]map[string]any)[0][test.field] = test.value
			client, downloads := installerFeed(t, []byte("fixture"), []map[string]any{release})
			if got, err := client.Check(context.Background()); got != nil || err == nil || errors.Is(err, ErrNoInstaller) || *downloads != 0 {
				t.Fatalf("invalid metadata must fail before download: %+v %v downloads=%d", got, err, *downloads)
			}
		})
	}
}

func TestInstallerRejectsCorruptTruncatedAndCanceledDownloads(t *testing.T) {
	for _, payload := range []string{"wrong!!", "short", "excess fixture bytes"} {
		t.Run(payload, func(t *testing.T) {
			client, _ := installerFeed(t, []byte(payload), []map[string]any{releaseFixture("0.2.0", []byte("fixture"))})
			release, err := client.Check(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := release.Download(context.Background(), nil); err == nil {
				t.Fatal("invalid bytes must not be staged")
			}
			if _, err := release.VerifiedPath(); err == nil {
				t.Fatal("invalid download must not be executable")
			}
			files, err := os.ReadDir(client.directory)
			if err != nil || len(files) != 0 {
				t.Fatalf("failed download must not leave a partial file: %v, %v", files, err)
			}
		})
	}
	client, _ := installerFeed(t, []byte("fixture"), []map[string]any{releaseFixture("0.2.0", []byte("fixture"))})
	release, err := client.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := release.Download(ctx, func(int64, int64) { cancel() }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel must prevent staging the installer: %v", err)
	}
	if _, err := release.VerifiedPath(); err == nil {
		t.Fatal("canceled download must not be executable")
	}
}

func TestInstallerDistinguishesNoReleaseAndServiceFailure(t *testing.T) {
	client, _ := installerFeed(t, nil, nil)
	if _, err := client.Check(context.Background()); !errors.Is(err, ErrNoInstaller) {
		t.Fatalf("an empty feed is not up to date: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit", http.StatusForbidden)
	}))
	defer server.Close()
	client.api, client.http = server.URL, server.Client()
	if _, err := client.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "403") || errors.Is(err, ErrNoInstaller) {
		t.Fatalf("a service error is not an empty feed: %v", err)
	}
}

func TestInstallerFindsReleaseOnNextPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", `<https://api.github.com/ignored>; rel="next"`)
			w.Write([]byte("[]"))
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{releaseFixture("0.2.0", []byte("fixture"))})
	}))
	defer server.Close()
	client := NewClient(t.TempDir(), "0.1.0")
	client.api, client.http = server.URL, server.Client()
	if release, err := client.Check(context.Background()); err != nil || release == nil || release.Version != "0.2.0" {
		t.Fatalf("release on next page: %+v, %v", release, err)
	}
}

func TestInstallerRefusesInsecureRedirect(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/installer.exe", http.StatusFound)
	}))
	defer server.Close()
	client := NewClient(t.TempDir(), "0.1.0")
	client.api = server.URL
	// Trust only the local TLS certificate; keep production redirect policy.
	client.http.Transport = server.Client().Transport
	if _, err := client.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "更新下载地址无效") {
		t.Fatalf("HTTPS must not redirect to an insecure asset: %v", err)
	}
}
