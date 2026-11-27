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
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
)

type Account struct{ ID, Login, Name, Email string }

// Existing account metadata remains readable; old OAuth credentials are not
// reused by GCM and the user reconnects once through the browser.
type githubToken struct {
	Account   Account
	Namespace string
}
type accountStore struct {
	Version  int
	Accounts map[string]githubToken
}
type GitHub struct {
	dir, api      string
	client        *http.Client
	ensureManager func(context.Context, func(RuntimeProgress)) (*credentialManager, error)
}
type RemoteRepository struct {
	Name, URL, Branch string
	Private, Writable bool
}

func NewGitHub(dir string) *GitHub {
	c := &GitHub{dir: dir, api: "https://api.github.com", client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	c.ensureManager = func(ctx context.Context, progress func(RuntimeProgress)) (*credentialManager, error) {
		return ensureCredentialManager(ctx, dir, progress)
	}
	return c
}
func failure(code string) error { return fmt.Errorf(i18n.T("GitHub 凭据不可用：%s"), code) }
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
