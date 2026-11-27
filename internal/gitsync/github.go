package gitsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
)

// GitHubClientID is public application metadata, set by the maintainer at build
// time or through FREEFOLIO_GITHUB_CLIENT_ID. It is never an OAuth secret.
var GitHubClientID string

type Account struct{ ID, Login, Name, Email string }
type githubToken struct {
	Account                 Account
	Access, Refresh         string
	Expires, RefreshExpires time.Time
}
type accountStore struct {
	Version  int
	Accounts map[string]githubToken
}
type GitHub struct {
	dir, clientID, web, api string
	client                  *http.Client
}
type Device struct {
	Code, Secret, URL string
	Interval          time.Duration
	Expires           time.Time
}
type RemoteRepository struct {
	Name, URL, Branch string
	Private, Writable bool
}

func NewGitHub(dir, id string) *GitHub {
	if id == "" {
		id = GitHubClientID
	}
	if id == "" {
		id = os.Getenv("FREEFOLIO_GITHUB_CLIENT_ID")
	}
	return &GitHub{dir: dir, clientID: id, web: "https://github.com", api: "https://api.github.com", client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *GitHub) Configured() bool { return c.clientID != "" }
func failure(code string) error    { return fmt.Errorf(i18n.T("GitHub 凭据不可用：%s"), code) }
func (c *GitHub) withStore(ctx context.Context, fn func(*accountStore) error) error {
	if e := os.MkdirAll(c.dir, 0700); e != nil {
		return e
	}
	if e := os.Chmod(c.dir, 0700); e != nil {
		return e
	}
	lock := filepath.Join(c.dir, "github-accounts.lock")
	if i, e := os.Lstat(lock); e == nil && !i.Mode().IsRegular() {
		return failure("storage_invalid")
	}
	f, e := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		ok, e := tryLock(f)
		if e != nil {
			return e
		}
		if ok {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	defer unlock(f)
	s := accountStore{Version: 1, Accounts: map[string]githubToken{}}
	p := filepath.Join(c.dir, "github-accounts")
	if i, e := os.Lstat(p); e == nil && (!i.Mode().IsRegular() || i.Size() > 4<<20) {
		return failure("storage_invalid")
	}
	data, e := os.ReadFile(p)
	if e == nil {
		data, e = protect(c.dir, data, false)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(data, &s); e != nil {
			return failure("storage_invalid")
		}
		if s.Version != 1 || s.Accounts == nil {
			return failure("storage_invalid")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return fn(&s)
}
func (c *GitHub) save(s *accountStore) error {
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	b, e = protect(c.dir, b, true)
	if e != nil {
		return e
	}
	return fsutil.WriteFileAtomic(filepath.Join(c.dir, "github-accounts"), b, 0600)
}
func (c *GitHub) request(ctx context.Context, method, address, token string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r, e := http.NewRequestWithContext(ctx, method, address, body)
	if e != nil {
		return e
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "FreeFolio")
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	}
	res, e := c.client.Do(r)
	if e != nil {
		return errors.New(i18n.T("无法连接 GitHub，请检查网络"))
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf(i18n.T("GitHub 请求失败（HTTP %d）"), res.StatusCode)
	}
	if e = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out); e != nil {
		return errors.New(i18n.T("GitHub 返回的数据无效"))
	}
	return nil
}
func (c *GitHub) Begin(ctx context.Context) (Device, error) {
	var d Device
	if !c.Configured() {
		return d, errors.New(i18n.T("此构建尚未配置 GitHub 应用，维护者需完成一次性注册"))
	}
	var res struct {
		Device   string `json:"device_code"`
		Code     string `json:"user_code"`
		URL      string `json:"verification_uri"`
		Expires  int    `json:"expires_in"`
		Interval int    `json:"interval"`
	}
	e := c.request(ctx, "POST", c.web+"/login/device/code", "", url.Values{"client_id": {c.clientID}}, &res)
	if e != nil {
		return d, e
	}
	if res.Device == "" || res.Code == "" || res.URL != "https://github.com/login/device" || res.Expires < 1 {
		return d, errors.New(i18n.T("GitHub 设备授权响应无效"))
	}
	return Device{Code: res.Code, Secret: res.Device, URL: res.URL, Interval: time.Duration(res.Interval) * time.Second, Expires: time.Now().Add(time.Duration(res.Expires) * time.Second)}, nil
}

type tokenResponse struct {
	Access         string `json:"access_token"`
	Refresh        string `json:"refresh_token"`
	Expires        int    `json:"expires_in"`
	RefreshExpires int    `json:"refresh_token_expires_in"`
	Error          string `json:"error"`
}

func tokenTimes(t *githubToken, r tokenResponse) {
	t.Access, t.Refresh = r.Access, r.Refresh
	t.Expires = time.Time{}
	if r.Expires > 0 {
		t.Expires = time.Now().Add(time.Duration(r.Expires) * time.Second)
	}
	if r.RefreshExpires > 0 {
		t.RefreshExpires = time.Now().Add(time.Duration(r.RefreshExpires) * time.Second)
	}
}
func (c *GitHub) Complete(ctx context.Context, d Device) (Account, error) {
	var a Account
	interval := d.Interval
	for time.Now().Before(d.Expires) {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return a, ctx.Err()
		case <-timer.C:
		}
		var r tokenResponse
		e := c.request(ctx, "POST", c.web+"/login/oauth/access_token", "", url.Values{"client_id": {c.clientID}, "device_code": {d.Secret}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &r)
		if e != nil {
			return a, e
		}
		switch r.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "":
		default:
			return a, errors.New(i18n.T("GitHub 授权已取消或过期，请重新登录"))
		}
		if r.Access == "" {
			return a, errors.New(i18n.T("GitHub 未返回访问令牌"))
		}
		var user struct {
			ID          int64 `json:"id"`
			Login, Name string
		}
		if e = c.request(ctx, "GET", c.api+"/user", r.Access, nil, &user); e != nil {
			return a, e
		}
		if user.ID == 0 || user.Login == "" {
			return a, errors.New(i18n.T("GitHub 账号响应无效"))
		}
		a = Account{ID: strconv.FormatInt(user.ID, 10), Login: user.Login, Name: user.Name, Email: fmt.Sprintf("%d+%s@users.noreply.github.com", user.ID, user.Login)}
		t := githubToken{Account: a}
		tokenTimes(&t, r)
		e = c.withStore(ctx, func(s *accountStore) error { s.Accounts[a.ID] = t; return c.save(s) })
		return a, e
	}
	if ctx.Err() != nil {
		return a, ctx.Err()
	}
	return a, errors.New(i18n.T("GitHub 授权已过期，请重新登录"))
}
func (c *GitHub) Accounts() ([]Account, error) {
	var out []Account
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e := c.withStore(ctx, func(s *accountStore) error {
		for _, t := range s.Accounts {
			out = append(out, t.Account)
		}
		return nil
	})
	slices.SortFunc(out, func(a, b Account) int { return strings.Compare(a.Login, b.Login) })
	return out, e
}
func (c *GitHub) Disconnect(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.withStore(ctx, func(s *accountStore) error { delete(s.Accounts, id); return c.save(s) })
}
func (c *GitHub) Token(ctx context.Context, id string) (string, error) {
	var token string
	e := c.withStore(ctx, func(s *accountStore) error {
		t, ok := s.Accounts[id]
		if !ok {
			return errors.New(i18n.T("请先登录 GitHub"))
		}
		if !t.Expires.IsZero() && time.Now().Add(2*time.Minute).After(t.Expires) {
			if t.Refresh == "" || !t.RefreshExpires.IsZero() && time.Now().After(t.RefreshExpires) {
				return errors.New(i18n.T("GitHub 登录已过期，请重新登录"))
			}
			var r tokenResponse
			if e := c.request(ctx, "POST", c.web+"/login/oauth/access_token", "", url.Values{"client_id": {c.clientID}, "grant_type": {"refresh_token"}, "refresh_token": {t.Refresh}}, &r); e != nil {
				return e
			}
			if r.Error != "" || r.Access == "" || r.Refresh == "" {
				return errors.New(i18n.T("GitHub 登录续期失败，请重新登录"))
			}
			tokenTimes(&t, r)
			s.Accounts[id] = t
			// Save refreshed credentials before any caller can use them. A failed
			// save never falls back to the now-invalid old token.
			if e := c.save(s); e != nil {
				return e
			}
		}
		token = t.Access
		return nil
	})
	return token, e
}
func (c *GitHub) Repositories(ctx context.Context, id string) ([]RemoteRepository, error) {
	token, e := c.Token(ctx, id)
	if e != nil {
		return nil, e
	}
	var out []RemoteRepository
	for page := 1; ; page++ {
		var rows []struct {
			Name        string `json:"full_name"`
			URL         string `json:"clone_url"`
			Branch      string `json:"default_branch"`
			Private     bool
			Permissions struct{ Push bool }
		}
		if e = c.request(ctx, "GET", fmt.Sprintf("%s/user/repos?per_page=100&page=%d&sort=updated", c.api, page), token, nil, &rows); e != nil {
			return nil, e
		}
		for _, r := range rows {
			out = append(out, RemoteRepository{Name: r.Name, URL: r.URL, Branch: r.Branch, Private: r.Private, Writable: r.Permissions.Push})
		}
		if len(rows) < 100 {
			return out, nil
		}
	}
}
