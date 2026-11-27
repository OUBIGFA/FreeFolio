package gitsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

var managerNamespacePattern = regexp.MustCompile(`^freefolio-[a-z0-9-]{1,100}$`)
var githubLoginPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

// GCM owns OAuth and token persistence. Each completed sign-in has its own
// namespace, so login replacement and disconnect cannot touch another app.
type credentialManager struct {
	Path, GitPath, Dir string
	command            func(context.Context, ...string) *exec.Cmd
}

func (m *credentialManager) filterEnvironment(base []string, namespace string, interactive bool) []string {
	env := []string{}
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "GCM_") || strings.HasPrefix(key, "GIT_") || key == "SSH_ASKPASS" || key == "PATH" {
			continue
		}
		env = append(env, entry)
	}
	// Per-process configuration only. Inherited debug/credential overrides are
	// excluded because they could print tokens or redirect them to plaintext.
	env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GCM_NAMESPACE="+namespace, "GCM_PROVIDER=github", "GCM_GITHUB_AUTHMODES=browser", "GCM_GUI_PROMPT=false", "GCM_INTERACTIVE="+fmt.Sprint(interactive), "GCM_GIT_PATH="+m.GitPath, "PATH="+filepath.Dir(m.GitPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	switch runtime.GOOS {
	case "windows":
		env = append(env, "GCM_CREDENTIAL_STORE=dpapi", "GCM_DPAPI_STORE_PATH="+filepath.Join(m.Dir, "gcm"))
	case "darwin":
		env = append(env, "GCM_CREDENTIAL_STORE=keychain")
	default:
		env = append(env, "GCM_CREDENTIAL_STORE=secretservice")
	}
	return env
}
func (m *credentialManager) run(ctx context.Context, namespace string, interactive bool, input string, args ...string) ([]byte, error) {
	if !managerNamespacePattern.MatchString(namespace) {
		return nil, errors.New(i18n.T("GitHub 登录记录无效，请重新连接"))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e := os.MkdirAll(m.Dir, 0700); e != nil {
		return nil, e
	}
	if runtime.GOOS == "windows" {
		if e := os.MkdirAll(filepath.Join(m.Dir, "gcm"), 0700); e != nil {
			return nil, e
		}
	}
	cmd := exec.CommandContext(ctx, m.Path, args...)
	if m.command != nil {
		cmd = m.command(ctx, args...)
	}
	hideCommand(cmd)
	cmd.Dir = m.Dir
	base := cmd.Env
	if base == nil {
		base = os.Environ()
	}
	cmd.Env = m.filterEnvironment(base, namespace, interactive)
	cmd.Stdin = strings.NewReader(input)
	cmd.WaitDelay = 3 * time.Second
	var out, diagnostic bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New(i18n.T("GitHub 登录服务未完成操作，请重试连接并检查网络或系统凭据服务"))
	}
	return out.Bytes(), nil
}
func (m *credentialManager) Login(ctx context.Context) (string, string, error) {
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return "", "", e
	}
	namespace := "freefolio-" + hex.EncodeToString(nonce[:])
	_, e := m.run(ctx, namespace, true, "", "github", "login", "--url", "https://github.com", "--browser", "--force")
	if e != nil {
		m.discard(namespace)
		return "", "", e
	}
	raw, e := m.run(ctx, namespace, false, "", "github", "list", "--url", "https://github.com")
	if e != nil {
		m.discard(namespace)
		return "", "", e
	}
	names := strings.Fields(string(raw))
	if len(names) != 1 || !githubLoginPattern.MatchString(names[0]) {
		m.discard(namespace)
		return "", "", errors.New(i18n.T("无法确认已登录的 GitHub 账号，请重新连接"))
	}
	return namespace, names[0], nil
}
func (m *credentialManager) Token(ctx context.Context, namespace, login string) (string, error) {
	if !githubLoginPattern.MatchString(login) {
		return "", errors.New(i18n.T("GitHub 登录记录无效，请重新连接"))
	}
	out, e := m.run(ctx, namespace, false, "protocol=https\nhost=github.com\nusername="+login+"\n\n", "get")
	if e != nil {
		return "", e
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if ok {
			if _, exists := fields[key]; exists {
				return "", errors.New(i18n.T("GitHub 登录服务返回了无效凭据"))
			}
			fields[key] = value
		}
	}
	if fields["password"] == "" || strings.ContainsAny(fields["password"], "\r\n\x00") || !strings.EqualFold(fields["username"], login) {
		return "", errors.New(i18n.T("GitHub 登录已过期，请重新登录"))
	}
	return fields["password"], nil
}
func (m *credentialManager) Logout(ctx context.Context, namespace, login string) error {
	if !githubLoginPattern.MatchString(login) {
		return errors.New(i18n.T("GitHub 登录记录无效，请重新连接"))
	}
	// Logout only removes this namespace's local credential, not the GitHub app
	// grant or another application's credentials. It never performs a global erase.
	accounts, e := m.run(ctx, namespace, false, "", "github", "list", "--url", "https://github.com")
	if e != nil {
		return e
	}
	found := false
	for _, name := range strings.Fields(string(accounts)) {
		if strings.EqualFold(name, login) {
			found = true
		}
	}
	if !found {
		return nil
	}
	_, e = m.run(ctx, namespace, false, "", "github", "logout", login, "--url", "https://github.com")
	return e
}
func (m *credentialManager) discard(namespace string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, e := m.run(ctx, namespace, false, "", "github", "list", "--url", "https://github.com")
	if e != nil {
		return
	}
	for _, login := range strings.Fields(string(raw)) {
		if githubLoginPattern.MatchString(login) {
			_ = m.Logout(ctx, namespace, login)
		}
	}
}
