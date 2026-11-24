package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

// ErrNoInstaller distinguishes an empty release feed from an up-to-date app.
var ErrNoInstaller = errors.New("no Windows installer has been published")

const maxInstallerSize = 512 << 20

// Client checks FreeFolio's public releases. Every Windows build uses the same
// source; no build flag, signing key or writable application folder is needed.
type Client struct {
	directory, current, api string
	http                    *http.Client
}

// NewClient stores downloads in FreeFolio's configuration directory, outside the
// installed application. The installer owns replacement of the application.
func NewClient(directory, current string) *Client {
	return &Client{
		directory: directory, current: current,
		api: "https://api.github.com/repos/OUBIGFA/FreeFolio",
		http: &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 10 {
				return errors.New(i18n.T("更新下载地址无效。"))
			}
			return nil
		}},
	}
}

type githubRelease struct {
	Tag        string `json:"tag_name"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// Installer is a release whose exact asset name agrees with its stable tag.
// Its digest comes from GitHub's HTTPS API, not from the downloaded executable.
type Installer struct {
	Version, Notes string
	client         *Client
	id, size       int64
	digest, path   string
}

// Check selects the highest published version with a Windows x64 installer.
// Drafts, prereleases and unrelated assets never become executable updates.
func (c *Client) Check(ctx context.Context) (*Installer, error) {
	current, ok := stableVersion(c.current)
	if !ok {
		return nil, errors.New(i18n.T("当前版本号无效。"))
	}
	var latest *Installer
	var latestVersion versionNumber
	for page := 1; ; page++ {
		response, err := c.get(ctx, fmt.Sprintf("%s/releases?per_page=100&page=%d", c.api, page), "application/vnd.github+json")
		if err != nil {
			return nil, err
		}
		var releases []githubRelease
		err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&releases)
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, release := range releases {
			version := strings.TrimPrefix(release.Tag, "v")
			parsed, valid := stableVersion(version)
			if !valid || release.Draft || release.Prerelease || latest != nil && parsed.Compare(latestVersion) <= 0 {
				continue
			}
			name := "FreeFolio-" + version + "-Setup.exe"
			for _, asset := range release.Assets {
				if asset.Name == name && asset.ID > 0 {
					latestVersion = parsed
					latest = &Installer{Version: version, Notes: release.Body, client: c,
						id: asset.ID, size: asset.Size, digest: strings.TrimPrefix(asset.Digest, "sha256:"),
						path: filepath.Join(c.directory, name)}
					if !strings.HasPrefix(asset.Digest, "sha256:") {
						latest.digest = ""
					}
					break
				}
			}
		}
		if !strings.Contains(response.Header.Get("Link"), `rel="next"`) {
			break
		}
	}
	if latest == nil {
		return nil, ErrNoInstaller
	}
	if latestVersion.Compare(current) <= 0 {
		return nil, nil
	}
	if latest.size <= 0 || latest.size > maxInstallerSize {
		return nil, errors.New(i18n.T("更新安装包大小无效。"))
	}
	digest, err := hex.DecodeString(latest.digest)
	if err != nil || len(digest) != sha256.Size {
		return nil, errors.New(i18n.T("更新安装包缺少有效的 SHA-256 校验值。"))
	}
	return latest, nil
}

type versionNumber [3]uint64

func stableVersion(text string) (versionNumber, bool) {
	var version versionNumber
	parts := strings.Split(text, ".")
	if len(parts) != len(version) {
		return version, false
	}
	for i, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return version, false
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return version, false
			}
		}
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return version, false
		}
		version[i] = number
	}
	return version, true
}

func (v versionNumber) Compare(other versionNumber) int {
	for i, number := range v {
		if number < other[i] {
			return -1
		}
		if number > other[i] {
			return 1
		}
	}
	return 0
}

func (c *Client) get(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "FreeFolio/"+c.current)
	response, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf(i18n.T("更新服务返回 HTTP %d。"), response.StatusCode)
	}
	return response, nil
}

// Download keeps partial or corrupt downloads out of the install path. An
// already verified download can be reused after restarting FreeFolio.
func (r *Installer) Download(ctx context.Context, progress func(done, total int64)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := r.VerifiedPath(); err == nil {
		if progress != nil {
			progress(r.size, r.size)
		}
		return nil
	}
	response, err := r.client.get(ctx, fmt.Sprintf("%s/releases/assets/%d", r.client.api, r.id), "application/octet-stream")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := os.MkdirAll(r.client.directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(r.client.directory, ".download-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name()) // Only this operation's incomplete staging file.
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash, &downloadProgress{total: r.size, notify: progress}), io.LimitReader(response.Body, r.size+1))
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && (written != r.size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), r.digest)) {
		err = errors.New(i18n.T("更新安装包校验失败，请重新下载。"))
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), r.path)
}

type downloadProgress struct {
	done, total int64
	notify      func(int64, int64)
}

func (p *downloadProgress) Write(data []byte) (int, error) {
	p.done += int64(len(data))
	if p.notify != nil {
		p.notify(p.done, p.total)
	}
	return len(data), nil
}

// VerifiedPath checks the cached bytes again immediately before execution.
func (r *Installer) VerifiedPath() (string, error) {
	file, err := os.Open(r.path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, r.size+1))
	if err != nil {
		return "", err
	}
	if size != r.size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), r.digest) {
		return "", errors.New(i18n.T("更新安装包校验失败，请重新下载。"))
	}
	return r.path, nil
}
