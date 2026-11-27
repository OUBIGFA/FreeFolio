package gitsync

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

// Login delegates the browser authorization to GCM's registered GitHub client.
// A unique credential namespace identifies the just-completed login even when
// several accounts were connected earlier. Only a verified /user response is
// published to the application's account list.
func (c *GitHub) Login(ctx context.Context, progress func(RuntimeProgress)) (Account, error) {
	var account Account
	manager, e := c.ensureManager(ctx, progress)
	if e != nil {
		return account, e
	}
	if progress != nil {
		progress(RuntimeProgress{})
	}
	namespace, login, e := manager.Login(ctx)
	if e != nil {
		return account, e
	}
	keep := false
	defer func() {
		if !keep {
			manager.discard(namespace)
		}
	}()
	token, e := manager.Token(ctx, namespace, login)
	if e != nil {
		return account, e
	}
	var user struct {
		ID          int64 `json:"id"`
		Login, Name string
	}
	if e = c.request(ctx, "GET", c.api+"/user", token, nil, &user); e != nil {
		return account, e
	}
	if user.ID <= 0 || !githubLoginPattern.MatchString(user.Login) || !strings.EqualFold(user.Login, login) {
		return account, errors.New(i18n.T("GitHub 账号响应无效"))
	}
	account = Account{ID: strconv.FormatInt(user.ID, 10), Login: user.Login, Name: user.Name, Email: fmt.Sprintf("%d+%s@users.noreply.github.com", user.ID, user.Login)}
	var previous githubToken
	e = c.withStore(ctx, func(s *accountStore) error {
		previous = s.Accounts[account.ID]
		s.Accounts[account.ID] = githubToken{Account: account, Namespace: namespace}
		return c.save(s)
	})
	if e != nil {
		return Account{}, e
	}
	keep = true
	if previous.Namespace != "" && previous.Namespace != namespace {
		manager.discard(previous.Namespace)
	}
	return account, nil
}
func (c *GitHub) Token(ctx context.Context, id string) (string, error) {
	var token string
	e := c.withStore(ctx, func(s *accountStore) error {
		profile, ok := s.Accounts[id]
		if !ok {
			return errors.New(i18n.T("请先登录 GitHub"))
		}
		if profile.Namespace == "" {
			return errors.New(i18n.T("请重新连接 GitHub，完成浏览器授权"))
		}
		manager, e := c.ensureManager(ctx, nil)
		if e != nil {
			return e
		}
		token, e = manager.Token(ctx, profile.Namespace, profile.Account.Login)
		return e
	})
	return token, e
}
func (c *GitHub) Disconnect(ctx context.Context, id string) error {
	return c.withStore(ctx, func(s *accountStore) error {
		profile, ok := s.Accounts[id]
		if !ok {
			return nil
		}
		if profile.Namespace != "" {
			manager, e := c.ensureManager(ctx, nil)
			if e != nil {
				return e
			}
			if e = manager.Logout(ctx, profile.Namespace, profile.Account.Login); e != nil {
				return e
			}
		}
		delete(s.Accounts, id)
		return c.save(s)
	})
}
