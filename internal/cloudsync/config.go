// Package cloudsync synchronizes explicitly selected document spaces over S3 or WebDAV.
package cloudsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
)

// Config contains local connection and scheduling choices. Secrets are excluded
// from JSON and are stored under credentials/, never in a document space.
type Config struct {
	Version         int    `json:"version"`
	Enabled         bool   `json:"enabled"`
	Provider        string `json:"provider"`
	WebDAVURL       string `json:"webdavURL,omitempty"`
	WebDAVUser      string `json:"webdavUser,omitempty"`
	WebDAVPassword  string `json:"-"`
	S3Endpoint      string `json:"s3Endpoint,omitempty"`
	S3Region        string `json:"s3Region,omitempty"`
	S3Bucket        string `json:"s3Bucket,omitempty"`
	S3Prefix        string `json:"s3Prefix,omitempty"`
	S3AccessKey     string `json:"s3AccessKey,omitempty"`
	S3SecretKey     string `json:"-"`
	S3PathStyle     bool   `json:"s3PathStyle"`
	Automatic       bool   `json:"automatic"`
	Schedule        string `json:"schedule"`
	IntervalMinutes int    `json:"intervalMinutes"`
	DailyTime       string `json:"dailyTime"`
	SyncOnStart     bool   `json:"syncOnStart"`
	DailyLimit      int    `json:"dailyLimit"`
	RetryLimit      int    `json:"retryLimit"`
	KeepSnapshots   int    `json:"keepSnapshots"`
	Encrypt         bool   `json:"encrypt"`
	EncryptPassword string `json:"-"`
	// Spaces maps local space IDs to user-selected remote folder names. The
	// mapping allows independent installations to share the same remote space.
	Spaces map[string]string `json:"spaces,omitempty"`
}

func DefaultConfig() Config {
	return Config{Version: 1, Provider: "webdav", S3PathStyle: true, Schedule: "interval", IntervalMinutes: 30, DailyTime: "21:00", SyncOnStart: true, RetryLimit: 3, KeepSnapshots: 5, Spaces: map[string]string{}}
}

func LoadConfig(dir string) (Config, error) {
	c := DefaultConfig()
	b, err := os.ReadFile(filepath.Join(dir, "sync.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Version != 1 {
		return c, fmt.Errorf("unsupported sync configuration version %d", c.Version)
	}
	secrets := prefs.Secrets{Dir: filepath.Join(dir, "credentials")}
	for name, target := range c.secrets() {
		b, err := secrets.Get(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return c, err
		}
		*target, err = unprotect(string(b))
		if err != nil {
			return c, fmt.Errorf("read sync credential: %w", err)
		}
	}
	if c.Spaces == nil {
		c.Spaces = map[string]string{}
	}
	return c, nil
}

func (c *Config) secrets() map[string]*string {
	return map[string]*string{"sync-webdav-password": &c.WebDAVPassword, "sync-s3-secret": &c.S3SecretKey, "sync-encryption-password": &c.EncryptPassword}
}

func SaveConfig(dir string, c Config) error {
	if err := c.ValidatePlan(); err != nil {
		return err
	}
	c.Version = 1
	secrets := prefs.Secrets{Dir: filepath.Join(dir, "credentials")}
	for name, target := range c.secrets() {
		sealed, err := protect(*target)
		if err != nil {
			return err
		}
		if err = secrets.Set(name, []byte(sealed)); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, "sync.json"), append(b, '\n'), 0o600)
}

func (c Config) ValidatePlan() error {
	if c.Provider != "webdav" && c.Provider != "s3" {
		return errors.New("请选择 WebDAV 或 S3")
	}
	if c.Schedule != "interval" && c.Schedule != "daily" {
		return errors.New("请选择按间隔或每天定时同步")
	}
	if c.IntervalMinutes < 1 || c.IntervalMinutes > 1440 {
		return errors.New("同步间隔应为 1 至 1440 分钟")
	}
	if _, err := time.Parse("15:04", c.DailyTime); err != nil {
		return errors.New("每天同步时间应为 HH:MM")
	}
	if c.RetryLimit < 0 || c.RetryLimit > 10 || c.DailyLimit < 0 || c.DailyLimit > 1440 || c.KeepSnapshots < 1 || c.KeepSnapshots > 100 {
		return errors.New("同步次数、重试次数或备份保留数无效")
	}
	if c.Encrypt && c.EncryptPassword == "" {
		return errNoPassword
	}
	for id, folder := range c.Spaces {
		if id == "" || !safeRelative(folder) {
			return errors.New("同步空间的远端目录无效")
		}
		for otherID, other := range c.Spaces {
			if id == otherID {
				continue
			}
			a, b := strings.ToLower(folder), strings.ToLower(other)
			if a == b || strings.HasPrefix(a, b+"/") {
				return errors.New("不同空间的远端目录不能相同或互相包含")
			}
		}
	}
	return nil
}

func (c Config) Next(now time.Time) time.Time {
	if c.Schedule == "daily" {
		clock, _ := time.Parse("15:04", c.DailyTime)
		next := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		return next
	}
	return now.Add(time.Duration(c.IntervalMinutes) * time.Minute)
}

// TargetID separates baselines when the account, destination or encryption
// mode changes. Password rotations do not discard an existing baseline.
func (c Config) TargetID() string {
	parts := []string{c.Provider, c.WebDAVURL, c.WebDAVUser, c.S3Endpoint, c.S3Region, c.S3Bucket, c.S3Prefix, c.S3AccessKey, fmt.Sprint(c.S3PathStyle), fmt.Sprint(c.Encrypt)}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func validEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("服务地址无效，不能包含账号、查询参数或片段")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && (strings.EqualFold(u.Hostname(), "localhost") || ip != nil && ip.IsLoopback()) {
		return nil
	}
	return errors.New("远程同步服务必须使用 HTTPS；HTTP 仅用于本机服务")
}

// openStore keeps FreeFolio under its own namespace. No existing FreeType data is read.
func openStore(ctx context.Context, c Config) (remoteStore, error) {
	if err := c.ValidatePlan(); err != nil {
		return nil, err
	}
	if c.Provider == "s3" {
		if !strings.Contains(c.S3Endpoint, "://") {
			c.S3Endpoint = "https://" + c.S3Endpoint
		}
		if err := validEndpoint(c.S3Endpoint); err != nil {
			return nil, err
		}
		if c.S3Prefix != "" && !safeRelative(strings.Trim(c.S3Prefix, "/")) {
			return nil, errors.New("S3 目录无效")
		}
		s, err := newS3Store(c)
		if err != nil {
			return nil, err
		}
		s.ctx = ctx
		if len(s.prefix) == 0 || s.prefix[len(s.prefix)-1] != "FreeFolio" {
			s.prefix = append(s.prefix, "FreeFolio")
		}
		return s, nil
	}
	if err := validEndpoint(c.WebDAVURL); err != nil {
		return nil, err
	}
	w, err := newWebDAVStore(c.WebDAVURL, c.WebDAVUser, c.WebDAVPassword)
	if err != nil {
		return nil, err
	}
	w.ctx = ctx
	if !strings.HasSuffix(w.base.Path, "/FreeFolio") {
		w.base.Path += "/FreeFolio"
	}
	return w, nil
}

func TestConnection(ctx context.Context, c Config) error {
	s, err := openStore(ctx, c)
	if err != nil {
		return err
	}
	return s.Probe()
}
