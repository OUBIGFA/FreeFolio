package app

import (
	_ "embed"
	"errors"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/chatgpt"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// OpenAI's unmodified sign-in mark has separate trademark terms. It identifies
// this connection only; provenance and usage terms are in third_party/chatgpt.
//
//go:embed assets/chatgpt/mark.svg
var chatGPTMarkData []byte
var chatGPTMark = ui.MustParseSVG(chatGPTMarkData)

func chatGPTSignInButton(c *ui.Context, label string, disabled bool) ui.Element {
	e := ui.Row(c.Key("signin")).Height(38).Padding(8, 14).Gap(9).AlignItems(ui.Center).Radius(8).Background(white).TextColor(black).Border(1, black.Alpha(0.3)).Focusable().Role(ui.RoleButton).Label(label).Cursor(ui.CursorPointer).Disabled(disabled)
	if e.Hovered() {
		e.Background(ui.RGB(240, 240, 240))
	}
	e.Children(func() {
		ui.Icon(c, chatGPTMark).Size(21, 21)
		ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(black).NoWrap()
	})
	return e
}

func (a *App) chatGPTAccountView(c *ui.Context) {
	t := c.Theme()
	g := &a.chatgpt
	id := a.prefs.AI.Active
	s := a.chatGPTSession()
	ui.Column(c.Key("chatgpt-account")).Padding(12).Gap(10).Children(func() {
		ui.Row(c.Key("chatgpt-header")).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, i18n.T("使用 ChatGPT 套餐")).FontSize(14).FontWeight(500)
			if s.Email != "" {
				ui.Text(c, s.Email).FontSize(12.5).TextColor(t.TextMuted).Selectable()
			}
			ui.Spacer(c)
			status, tone := i18n.T("尚未连接 ChatGPT"), ""
			switch {
			case g.busyID == id:
				status, tone = i18n.T("正在处理 ChatGPT 连接"), "accent"
			case g.loading[id]:
				status = i18n.T("正在读取 ChatGPT 连接")
			case s.Pending:
				status, tone = i18n.T("正在等待验证 ChatGPT 身份；请重试获取模型"), "accent"
			case s.Sharing:
				status, tone = i18n.T("正在使用 ChatGPT 套餐"), "success"
			case s.Status == chatgpt.Connected:
				status, tone = i18n.T("已登录，尚未启用套餐使用"), "accent"
			case s.Status == chatgpt.ReauthRequired:
				status, tone = i18n.T("ChatGPT 登录已失效，请重新登录"), "danger"
			}
			settingsBadge(c, status, tone)
		})
		ui.Text(c, i18n.T("授权后，OneBot 中符合条件的请求使用你的 ChatGPT 套餐或点数；具体模型和额度由账号决定")).FontSize(12.5).TextColor(t.TextMuted).LineHeight(1.45)
		ui.Row(c.Key("actions")).Gap(8).Wrap().Children(func() {
			if g.busyID == id && g.cancel != nil {
				if ui.Button(c.Key("cancel"), i18n.T("取消登录")).Clicked() {
					a.cancelChatGPTSignIn()
				}
			} else {
				label := i18n.T("使用 ChatGPT 继续")
				if s.Status == chatgpt.Connected {
					label = i18n.T("重新登录 ChatGPT")
				}
				if chatGPTSignInButton(c, label, g.busyID != "" || g.loading[id]).Clicked() {
					a.startChatGPTSignIn(false)
				}
				if s.Status == chatgpt.Connected && !s.Sharing && ui.Button(c.Key("consent"), i18n.T("允许使用 ChatGPT 套餐")).Disabled(g.busyID != "").Clicked() {
					a.startChatGPTSignIn(true)
				}
				if (s.Status != chatgpt.Disconnected || s.Pending) && ui.Button(c.Key("disconnect"), i18n.T("断开 ChatGPT 连接")).Disabled(g.busyID != "").Clicked() {
					a.disconnectChatGPT(false)
				}
			}
			if ui.Button(c.Key("usage"), i18n.T("管理用量")).Clicked() {
				a.openChatGPTUsage()
			}
		})
		if err := g.errors[id]; err != nil {
			ui.Text(c, chatGPTErrorText(err)).FontSize(13).TextColor(t.Danger).Selectable()
		}
	})
}

func (a *App) chatGPTUsageView(c *ui.Context) {
	conn := a.prefs.AI.Current()
	if conn == nil || conn.Protocol != agent.ChatGPT {
		return
	}
	text := i18n.T("ChatGPT 未连接")
	if a.chatGPTSession().Sharing {
		text = i18n.T("正在使用 ChatGPT 套餐")
	}
	ui.Row(c.Key("chatgpt-usage")).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		ui.Text(c, text).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
		if quietOneBotAction(c, "manage", i18n.T("管理用量")).Clicked() {
			a.openChatGPTUsage()
		}
	})
}

func (a *App) chatGPTWelcomeView(c *ui.Context) {
	if !a.chatgpt.welcome {
		return
	}
	ui.Overlay(c.Key("chatgpt-welcome"), func() {
		w, _ := c.Size()
		ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(ui.RGBA(0, 0, 0, 0.3)).Center().Modal().Role(ui.RoleDialog).Label(i18n.T("正在使用 ChatGPT 套餐")).Children(func() {
			ui.Column(c).Width(min(420, max(240, w-32))).Padding(24).Gap(16).Radius(14).Background(c.Theme().Surface).Children(func() {
				ui.Box(c).Size(42, 42).Radius(9).Background(white).Center().Children(func() { ui.Icon(c, chatGPTMark).Size(30, 30) })
				ui.Text(c, i18n.T("正在使用 ChatGPT 套餐")).FontSize(18).FontWeight(500)
				ui.Text(c, i18n.T("OneBot 中符合条件的请求将使用你的 ChatGPT 套餐或点数；你可以随时在 ChatGPT 设置中查看用量和调整此应用的限额")).FontSize(13)
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c.Key("usage"), i18n.T("管理用量")).Clicked() {
						a.openChatGPTUsage()
					}
					if ui.Button(c.Key("ok"), i18n.T("知道了")).Clicked() {
						a.prefs.AI.ChatGPTWelcomeSeen = true
						a.chatgpt.welcome = false
						a.savePrefs()
					}
				})
			})
		})
	})
}

func chatGPTUsageLimit(err error) bool {
	var e *agent.APIError
	return errors.As(err, &e) && e.Code == "subscription_sharing_usage_limit_exceeded"
}
