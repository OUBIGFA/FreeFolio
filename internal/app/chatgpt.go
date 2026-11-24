package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/chatgpt"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
)

type chatGPTState struct {
	client          *chatgpt.Client
	sessions        map[string]chatgpt.Session
	loaded, loading map[string]bool
	errors          map[string]error
	busyID          string
	epoch           uint64
	cancel          context.CancelFunc
	welcome         bool
	openBrowser     func(string) error
}

// ensureChatGPT only constructs memory state. Disk, OS credentials and network
// work are confined to explicit actions or deferred startup, never View.
func (a *App) ensureChatGPT() {
	if a.chatgpt.client != nil {
		return
	}
	a.chatgpt.client = chatgpt.New(filepath.Join(a.cfg.ConfigDir, "credentials"))
	a.chatgpt.sessions = map[string]chatgpt.Session{}
	a.chatgpt.loaded = map[string]bool{}
	a.chatgpt.loading = map[string]bool{}
	a.chatgpt.errors = map[string]error{}
}

func (a *App) chatGPTSession() chatgpt.Session {
	if s, ok := a.chatgpt.sessions[a.prefs.AI.Active]; ok {
		return s
	}
	return chatgpt.Session{Status: chatgpt.Disconnected}
}

func (a *App) chatGPTSettings() agent.Settings {
	a.ensureChatGPT()
	c := a.prefs.AI.Current()
	s := agent.Settings{ConnectionID: c.ID, Protocol: agent.ChatGPT, Vendor: "chatgpt", BaseURL: "https://api.openai.com/v1", Model: c.Model, Parameters: c.Parameters}
	if session := a.chatGPTSession(); session.Sharing || session.Pending {
		s.TokenSource = a.chatgpt.client.Source(c.ID)
	}
	if m := c.CurrentModel(); m != nil && m.Enabled {
		s.Capabilities = m.Capabilities
	} else {
		s.Model = ""
	}
	return s
}

func (a *App) loadChatGPTSession(id string, force bool) {
	c := a.prefs.AI.Find(id)
	if c == nil || c.Protocol != agent.ChatGPT {
		return
	}
	a.ensureChatGPT()
	g := &a.chatgpt
	if g.loading[id] || g.busyID == id || g.loaded[id] && !force {
		return
	}
	g.loading[id] = true
	epoch := g.epoch
	work := func() (chatgpt.Session, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return g.client.Session(ctx, id)
	}
	done := func(s chatgpt.Session, err error) {
		g.loading[id] = false
		if a.prefs.AI.Find(id) == nil || g.busyID == id {
			return
		}
		if epoch != g.epoch {
			a.loadChatGPTSession(id, true)
			return
		}
		g.loaded[id] = true
		g.sessions[id] = s
		if err != nil || !force {
			g.errors[id] = err
		}
		if a.prefs.AI.Active == id {
			a.showChatGPTWelcome()
			if !force && err == nil && s.Sharing {
				a.fetchModels()
			}
		}
	}
	if a.win == nil || a.qa.sync {
		done(work())
		return
	}
	win := a.win
	go func() { s, err := work(); win.Update(func() { done(s, err) }) }()
}

func (a *App) cancelChatGPTSignIn() {
	g := &a.chatgpt
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
	g.epoch++
	g.busyID = ""
}

func (a *App) startChatGPTSignIn(reconsent bool) {
	c := a.prefs.AI.Current()
	if c == nil || c.Protocol != agent.ChatGPT {
		return
	}
	a.ensureChatGPT()
	g := &a.chatgpt
	if g.openBrowser == nil {
		g.errors[c.ID] = &chatgpt.Error{Code: "browser_unavailable"}
		return
	}
	if g.busyID != "" {
		return
	}
	id := c.ID
	a.cancelChatGPTSignIn()
	epoch := g.epoch
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.busyID = id
	g.errors[id] = nil
	client, open := g.client, g.openBrowser
	done := func(s chatgpt.Session, err error) {
		defer cancel()
		if g.epoch != epoch || a.prefs.AI.Find(id) == nil {
			if g.busyID == "" {
				a.loadChatGPTSession(id, true)
			}
			return
		}
		g.cancel = nil
		g.busyID = ""
		g.errors[id] = err
		if err != nil {
			a.loadChatGPTSession(id, true)
			g.errors[id] = err
			return
		}
		g.sessions[id] = s
		g.loaded[id] = true
		if a.prefs.AI.Active == id {
			a.cancelModelFetch()
			a.qa.modelsFor = ""
			a.showChatGPTWelcome()
			if s.Sharing {
				a.fetchModels()
			}
		}
	}
	if a.win == nil || a.qa.sync {
		done(client.SignIn(ctx, id, reconsent, open))
		return
	}
	win := a.win
	go func() { s, err := client.SignIn(ctx, id, reconsent, open); win.Update(func() { done(s, err) }) }()
}

func (a *App) showChatGPTWelcome() {
	if !a.prefs.AI.ChatGPTWelcomeSeen && a.chatGPTSession().Sharing {
		a.chatgpt.welcome = true
	}
}

func (a *App) disconnectChatGPT(remove bool) {
	c := a.prefs.AI.Current()
	if c == nil || c.Protocol != agent.ChatGPT {
		return
	}
	id := c.ID
	a.ensureChatGPT()
	g := &a.chatgpt
	if g.busyID != "" {
		a.cancelChatGPTSignIn()
	}
	a.cancelModelFetch()
	// Stop any running turn bound to this account, including another space.
	for _, s := range a.qa.sessions {
		if strings.HasPrefix(s.source, id+"|") && s.cancel != nil {
			s.cancel()
		}
	}
	if task := a.qa.editTask; task != nil && task.Running && task.connectionID == id {
		a.stopOneBotEdit()
	}
	g.busyID = id
	g.errors[id] = nil
	g.epoch++
	epoch := g.epoch
	work := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := g.client.Disconnect(ctx, id)
		if remove && (err == nil || chatgpt.IsCode(err, "revocation_unconfirmed")) {
			if e := g.client.Forget(ctx, id); e != nil {
				return e
			}
		}
		return err
	}
	done := func(err error) {
		if g.epoch != epoch {
			return
		}
		g.busyID = ""
		g.errors[id] = err
		if err != nil && !chatgpt.IsCode(err, "revocation_unconfirmed") {
			return
		}
		s := g.sessions[id]
		s.Status = chatgpt.Disconnected
		s.Sharing = false
		s.Pending = false
		g.sessions[id] = s
		if remove {
			if err != nil {
				a.notify("%s", chatGPTErrorText(err))
			}
			a.prefs.AI.Connections = slices.DeleteFunc(a.prefs.AI.Connections, func(c prefs.AIConnection) bool { return c.ID == id })
			delete(g.sessions, id)
			delete(g.loaded, id)
			delete(g.errors, id)
			if a.prefs.AI.Active == id {
				a.prefs.AI.Active = ""
				if len(a.prefs.AI.Connections) > 0 {
					a.selectAIConnection(a.prefs.AI.Connections[0].ID)
				}
			}
			a.savePrefs()
		}
	}
	if a.win == nil || a.qa.sync {
		done(work())
		return
	}
	win := a.win
	go func() { err := work(); win.Update(func() { done(err) }) }()
}

func (a *App) openChatGPTUsage() {
	if a.chatgpt.openBrowser == nil {
		return
	}
	if err := a.chatgpt.openBrowser(chatgpt.UsageURL); err != nil {
		a.notify("无法打开 ChatGPT 用量页面。")
	}
}

func chatGPTErrorText(err error) string {
	if errors.Is(err, context.Canceled) {
		return i18n.T("已取消 ChatGPT 登录。")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return i18n.T("ChatGPT 操作超时，请重试。")
	}
	var e *chatgpt.Error
	if !errors.As(err, &e) {
		return errText(err)
	}
	messages := map[string]string{
		"sign_in_required": "请先登录 ChatGPT。", "sharing_disabled": "已登录 ChatGPT，但尚未允许使用套餐。请在设置中授权。",
		"browser_unavailable": "无法打开浏览器，请检查默认浏览器后重试。", "authorization_denied": "未完成 ChatGPT 授权。你可以重新登录。",
		"account_mismatch": "此连接属于另一个 ChatGPT 账号。请登录原账号，或添加新的 ChatGPT 供应商。",
		"invalid_identity": "无法验证 ChatGPT 身份，请重新登录。", "identity_unavailable": "ChatGPT 身份验证暂时不可用。连接已保留，请稍后重试。",
		"encryption_unavailable": "无法访问系统凭据保护服务。请解锁系统密钥服务后重试；Linux 需要 secret-tool。",
		"storage_unavailable":    "无法访问 ChatGPT 凭据文件。请检查配置目录权限。", "storage_invalid": "ChatGPT 凭据文件损坏或版本不受支持。原文件已保留。",
		"revocation_unconfirmed": "已在本机断开连接，但未确认远端撤销。请到 ChatGPT 设置中断开此应用。",
		"network_error":          "无法连接 ChatGPT，请检查网络后重试。", "refresh_not_ready": "ChatGPT 暂时不允许刷新登录，请稍后重试。",
		"connection_changed": "ChatGPT 连接已更改，请重新登录。", "callback_unavailable": "无法启动本机登录回调，请稍后重试。",
		"registration_incomplete": "ChatGPT 注册未完成，请重新登录。",
	}
	if terminal := e.Code; terminal == "invalid_grant" || terminal == "invalid_refresh_token" || strings.HasPrefix(terminal, "refresh_token_") || terminal == "token_expired" {
		return i18n.T("ChatGPT 登录已失效，请重新登录。")
	}
	if text := messages[e.Code]; text != "" {
		return i18n.T(text)
	}
	return i18n.T("ChatGPT 请求失败：") + e.Code
}
