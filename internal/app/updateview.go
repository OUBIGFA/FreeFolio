package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/OUBIGFA/FreeMD/internal/update"
	"github.com/egoist/mygo/ui"
)

// The update banners and toast keep release prompts clean and scannable in
// the sidebar foot (above the space switcher) and top-right toast while the
// sidebar is hidden. Full progress and actions live in Settings > Updates.

// bannerEnter slides the sidebar update card gently into place.
var bannerEnter = ui.Animation{Duration: 260 * time.Millisecond, Ease: ui.CubicBezier(0.2, 0.9, 0.3, 1)}

// updateBanner is the banner above the spaces at the foot of the sidebar.
func (a *App) updateBanner(c *ui.Context) {
	u := &a.updates
	n := a.updateNotice()
	kind := ""
	switch {
	case n != nil:
		kind = "ready"
	case u.updatedTo != "":
		kind = "updated"
	}
	if kind != u.shown {
		u.shown, u.shownSince = kind, a.now
	}
	if kind == "" {
		return
	}
	bl := bannerColors(a.look)
	entering := a.now.Sub(u.shownSince) < time.Millisecond
	ui.Column(c.Key("update-banner")).Padding(0, 12, 6, 12).Children(func() {
		if n != nil {
			a.readyCard(c, *n, bl, entering)
		} else {
			a.updatedCard(c, bl, entering)
		}
	})
	if entering && !a.reduceMotion {
		c.AnimationFrame()
	}
}

// bannerCard renders a banner container with subtle border, gradient fill,
// optional hover tint, and rounded clipping.
func bannerCard(c *ui.Context, key string, hoverable bool, bl bannerLook) ui.Element {
	card := ui.Box(c.Key(key)).Radius(12).Border(1, bl.border).Clip()
	f := bl.fill
	if hoverable && card.Hovered() {
		f = bl.hover
	}
	card.Draw(func(p *ui.Painter, r ui.Rect) {
		p.FillGradient(r, ui.LinearGradient{From: f[0], To: f[1], Angle: 120}, 12)
	})
	return card
}

func bannerDismissButton(c *ui.Context, bl bannerLook) bool {
	closeX := ui.Box(c.Key("dismiss")).Size(22, 22).Radius(6).Shrink(0).Center().
		Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
		Label(i18n.T("关闭")).Tooltip(i18n.T("关闭"))
	ink := bl.tertiary
	if closeX.Hovered() {
		closeX.Background(bl.tileHover)
		ink = bl.title
	}
	closeX.Children(func() {
		ui.Icon(c, iconX).FontSize(11).TextColor(ink)
	})
	return closeX.Clicked()
}

// readyCard notifies that a new version is downloaded and ready to restart,
// or available for download in Settings > Updates.
func (a *App) readyCard(c *ui.Context, n update.Notice, bl bannerLook, entering bool) {
	card := bannerCard(c, "update-ready", true, bl)
	if !a.reduceMotion {
		pose := ui.Pose{}
		if entering {
			pose = ui.Pose{Y: 8, Fade: 1}
		}
		card.AnimatePose(pose, bannerEnter)
	}

	badgeIcon, badgeFill, badgeInk := iconDownload, bl.badgeFill, bl.badgeInk
	if n.Kind == update.Ready {
		badgeIcon, badgeFill, badgeInk = iconCheck, bl.readyFill, bl.readyInk
	}

	var dismissed, activated bool
	card.Children(func() {
		ui.Row(c.Key("update-ready-row")).Padding(10, 8, 10, 10).Gap(4).AlignItems(ui.Center).Children(func() {
			action := ui.Row(c.Key("update-ready-action")).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).
				Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
				Label(n.Title() + "，" + n.Subtitle()).Tooltip(n.Subtitle())
			chevronInk := bl.tertiary
			if action.Hovered() {
				chevronInk = bl.title
			}
			action.Children(func() {
				ui.Box(c.Key("badge")).Size(28, 28).Radius(8).Shrink(0).Background(badgeFill).Center().Children(func() {
					ui.Icon(c, badgeIcon).FontSize(14).TextColor(badgeInk)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, n.Title()).FontSize(12.5).FontWeight(600).TextColor(bl.title).SingleLine()
					ui.Text(c, n.Subtitle()).FontSize(11).TextColor(bl.secondary).SingleLine()
				})
				ui.Icon(c, iconChevronRight).FontSize(12).Shrink(0).TextColor(chevronInk)
			})
			activated = action.Clicked()
			dismissed = bannerDismissButton(c, bl)
		})
	})
	if dismissed {
		a.dismissBanner()
		return
	}
	if activated {
		a.actOnNotice(n)
	}
}

// updatedCard shows that this launch is running a freshly installed version.
func (a *App) updatedCard(c *ui.Context, bl bannerLook, entering bool) {
	u := &a.updates
	card := bannerCard(c, "update-done", false, bl)
	if !a.reduceMotion {
		pose := ui.Pose{}
		if entering {
			pose = ui.Pose{Y: 8, Fade: 1}
		}
		card.AnimatePose(pose, bannerEnter)
	}

	version := u.updatedTo
	var dismissed bool
	card.Children(func() {
		ui.Column(c.Key("update-done-body")).Padding(10, 10, 8, 10).Gap(6).Children(func() {
			ui.Row(c.Key("update-done-head")).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Box(c.Key("badge")).Size(24, 24).Radius(7).Shrink(0).Background(bl.readyFill).Center().Children(func() {
					ui.Icon(c, iconCheck).FontSize(13).TextColor(bl.readyInk)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, i18n.T("FreeMD 已更新！")).FontSize(12.5).FontWeight(600).TextColor(bl.title).SingleLine()
					if version != "" {
						ui.Text(c, "v"+version).FontSize(10.5).TextColor(bl.secondary).SingleLine()
					}
				})
				dismissed = bannerDismissButton(c, bl)
			})
			ui.Box(c).Height(1).Background(bl.divider)
			ui.Column(c.Key("update-done-links")).Gap(2).Children(func() {
				for _, row := range []struct {
					key, label string
					icon       *ui.SVG
					url        string
				}{
					{"whats-new", i18n.T("新功能"), iconSparkles, update.ReleaseURL(version)},
					{"star", i18n.T("在 GitHub 上点星"), iconStar, update.Repository},
				} {
					r := ui.Row(c.Key(row.key)).Height(28).PaddingX(6).Radius(7).Gap(8).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Focusable().Role(ui.RoleLink).Label(row.label)
					icon, ink, arrowInk := bl.icon, bl.secondary, bl.tertiary
					if r.Hovered() {
						r.Background(bl.tileHover)
						icon, ink, arrowInk = bl.hoverIcon, bl.title, bl.secondary
					}
					r.Children(func() {
						ui.Icon(c, row.icon).FontSize(14).Shrink(0).TextColor(icon)
						ui.Text(c, row.label).Grow(1).MinWidth(0).FontSize(12).FontWeight(500).TextColor(ink).SingleLine()
						ui.Icon(c, iconArrowUpRight).FontSize(12).Shrink(0).TextColor(arrowInk)
					})
					if r.Clicked() {
						a.openUpdatePage(row.url)
					}
				}
			})
		})
	})
	if dismissed {
		a.dismissBanner()
	}
}

// toastSpring is the toast springing in from its top, ω = 2π / 0.3 s with
// a damping ratio of 0.67: Arc's 4% overshoot.
var toastSpring = ui.Animation{Spring: ui.SpringOptions{Mass: 1, Stiffness: 440, Damping: 28}}

// updateToast says the banner's news at the top right of the page while
// the sidebar is hidden (Cra's update toast), until its ✕ or its button.
func (a *App) updateToast(c *ui.Context) {
	u := &a.updates
	if u.toast == nil {
		return
	}
	n := *u.toast
	tl := toastColors(a.look)
	top := float32(8)
	if !a.focus {
		top += max(a.titleBar(c).Height, 46)
	}
	ui.Overlay(c, func() {
		pill := ui.Row(c.Key("update-toast")).Absolute().Right(8+9).Top(top).Height(42).Radius(10).
			PaddingStart(12).PaddingEnd(8).Gap(8).AlignItems(ui.Center).
			Border(1, tl.outline).Shadow(0, 4, 14, 0, black.Alpha(0.12)).TransformOrigin(0.5, 0).
			Role(ui.RoleStatus).Label(n.Title())
		pill.Draw(func(p *ui.Painter, r ui.Rect) {
			p.FillGradient(r, ui.LinearGradient{From: tl.top, To: tl.bottom, Angle: 180}, 10)
		})
		if !a.reduceMotion {
			if u.toastFrames == 0 {
				pill.AnimatePose(ui.Pose{Scale: 0.4}, toastSpring)
				c.AnimationFrame()
			} else {
				pill.AnimatePose(ui.Pose{}, toastSpring)
			}
		}
		u.toastFrames++
		toastIcon := iconDownload
		if n.Kind == update.Ready {
			toastIcon = iconCheck
		}
		pill.Children(func() {
			ui.Icon(c, toastIcon).FontSize(14).Shrink(0).TextColor(tl.text)
			ui.Text(c, n.Title()).FontSize(14).FontWeight(500).TextColor(tl.text).SingleLine()
			b := ui.Box(c.Key("act")).Height(26).PaddingX(10).Radius(13).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label(n.Button())
			fill := float32(0.1)
			if b.Hovered() {
				fill = 0.16
			}
			b.Background(tl.text.Alpha(fill)).Children(func() {
				ui.Text(c, n.Button()).FontSize(13).FontWeight(600).TextColor(tl.text)
			})
			x := ui.Box(c.Key("close")).Size(26, 26).Radius(13).Center().Cursor(ui.CursorPointer).
				Focusable().Role(ui.RoleButton).Label(i18n.T("关闭")).Tooltip(i18n.T("关闭"))
			if x.Hovered() {
				x.Background(tl.text.Alpha(0.1))
			}
			x.Children(func() { ui.Icon(c, iconX).FontSize(11).TextColor(tl.text) })
			if b.Clicked() {
				a.actOnNotice(n)
			}
			if x.Clicked() {
				u.toast = nil
			}
		})
	})
}

// updateOverview renders the version header, current status, download progress
// and contextual actions in Settings > Updates. Navigation stays available
// while checks and downloads run.
func (a *App) updateOverview(c *ui.Context) {
	u := &a.updates
	t := c.Theme()
	busy := u.checking || u.downloading || u.installing

	// Semantic icon and tint for the current update state.
	var (
		icon     = iconReload
		fg       = t.TextMuted
		bg       = t.Border.Alpha(0.45)
		spinning = false
	)
	switch {
	case u.checking || u.installing:
		spinning = true
		fg, bg = t.Accent, t.Accent.Alpha(0.10)
	case u.downloading:
		icon = iconDownload
		fg, bg = t.Accent, t.Accent.Alpha(0.10)
	case !a.updatesEnabled() || u.phase == phaseNoRelease:
		icon = iconInfo
	case u.phase == phaseFailed:
		icon = iconAlert
		fg, bg = t.Danger, t.Danger.Alpha(0.10)
	case u.ready != "" || u.phase == phaseUpToDate:
		icon = iconCheck
		fg, bg = t.Success, t.Success.Alpha(0.12)
	case u.release != nil:
		icon = iconDownload
		fg, bg = t.Accent, t.Accent.Alpha(0.10)
	}

	statusColor := t.Text
	if u.phase == phaseFailed {
		statusColor = t.Danger
	}

	settingsGroup(c, "update-overview", func() {
		ui.Row(c.Key("update-header")).Padding(14, 16).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Box(c.Key("update-badge")).Size(36, 36).Radius(8).Shrink(0).Center().Background(bg).Children(func() {
				if spinning {
					ui.Spinner(c.Key("update-spinner")).TextColor(fg).Label(a.updateSummary())
				} else {
					ui.Icon(c, icon).FontSize(17).TextColor(fg)
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c.Key("update-status"), a.updateSummary()).FontSize(14).FontWeight(600).TextColor(statusColor).LineHeight(1.4).Selectable()
				ui.Row(c.Key("update-meta")).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					ui.Text(c, i18n.T("当前版本 ")+Version).FontSize(12).TextColor(t.TextMuted)
					if !u.lastCheck.IsZero() {
						ui.Text(c, "·").FontSize(12).TextColor(t.TextMuted)
						ui.Text(c, i18n.T("上次检查：")+u.lastCheck.Format(i18n.T("1月2日 15:04"))).FontSize(12).TextColor(t.TextMuted)
					}
				})
			})
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
				if ui.Button(c.Key("check-now"), i18n.T("检查更新…")).Disabled(busy).Clicked() {
					a.checkForUpdates()
				}
			})
		})

		if busy {
			settingsSeparator(c)
			ui.Column(c.Key("update-progress-area")).Padding(12, 16).Gap(8).Children(func() {
				value := float64(-1)
				if u.downloading && u.total > 0 {
					value = float64(u.received) / float64(u.total)
				}
				ui.Progress(c.Key("update-progress"), value).Label(i18n.T("下载进度"))
				if u.downloading {
					ui.Row(c.Key("update-actions")).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
							if u.total > 0 {
								pct := min(100, max(0, int(u.received*100/u.total)))
								ui.Text(c, byteSize(u.received)+" / "+byteSize(u.total)).FontSize(12).TextColor(t.TextMuted)
								ui.Text(c, fmt.Sprintf("%d%%", pct)).FontSize(12).FontWeight(500).TextColor(t.Accent)
							}
						})
						if ui.Button(c.Key("cancel-download"), i18n.T("取消")).Clicked() {
							a.cancelUpdateDownload()
						}
					})
				}
			})
			return
		}

		hasActions := !a.updatesEnabled() || u.phase == phaseFailed || u.ready != "" || u.release != nil
		if !hasActions {
			return
		}
		stripBg := ui.Transparent
		switch {
		case u.phase == phaseFailed:
			stripBg = t.Danger.Alpha(0.06)
		case u.ready != "":
			stripBg = t.Success.Alpha(0.06)
		case u.release != nil:
			stripBg = t.Accent.Alpha(0.05)
		}
		// Use a full-width hairline above tinted strips so it meets the outer
		// border cleanly without leaving uncolored notches at the edges.
		if stripBg != ui.Transparent {
			ui.Box(c).Height(1).Background(t.Border.Alpha(0.6))
		} else {
			settingsSeparator(c)
		}
		ui.Row(c.Key("update-actions")).Padding(12, 16).Gap(8).Wrap().AlignItems(ui.Center).Background(stripBg).Children(func() {
			switch {
			case !a.updatesEnabled():
				if ui.Button(c.Key("download-page"), i18n.T("前往下载页面")).Clicked() {
					a.openUpdatePage(update.ReleaseURL(""))
				}
			case u.phase == phaseFailed:
				if ui.PrimaryButton(c.Key("retry-update"), i18n.T("重试")).Clicked() {
					if u.release != nil {
						u.ready = ""
						a.installUpdate()
					} else {
						a.checkUpdates(true)
					}
				}
				if ui.Button(c.Key("manual-download"), i18n.T("手动下载")).Clicked() {
					a.openUpdatePage(update.ReleaseURL(""))
				}
			case u.ready != "":
				if ui.PrimaryButton(c.Key("install-update"), i18n.T("安装并重新启动")).Clicked() {
					a.installUpdate()
				}
				if ui.Button(c.Key("release-page"), i18n.T("查看发布页")).Clicked() {
					a.openUpdatePage(update.ReleaseURL(u.ready))
				}
			case u.release != nil:
				if ui.PrimaryButton(c.Key("download-install"), i18n.T("下载并安装")).Clicked() {
					a.installUpdate()
				}
				if ui.Button(c.Key("release-page"), i18n.T("查看发布页")).Clicked() {
					a.openUpdatePage(update.ReleaseURL(u.release.Version()))
				}
			}
		})
	})
}

// updateReleaseNotes displays the structured release notes for a found or
// downloaded release in its own section below the status overview.
func (a *App) updateReleaseNotes(c *ui.Context) {
	u := &a.updates
	if u.release == nil || u.checking {
		return
	}
	lines := update.NoteLines(u.release.Notes())
	if len(lines) == 0 {
		return
	}
	t := c.Theme()
	title := i18n.T("更新说明")
	if ver := strings.TrimPrefix(u.release.Version(), "v"); ver != "" {
		title += " · v" + ver
	}
	settingsSection(c, title)
	settingsGroup(c, "update-notes", func() {
		ui.Scroll(c.Key("release-notes")).MaxHeight(220).Padding(14, 16).Gap(4).Children(func() {
			for i, line := range lines {
				switch line.Kind {
				case update.NoteBlank:
					ui.Box(c.Key(i)).Height(6)
				case update.NoteHeading:
					ui.Text(c.Key(i), line.Text).FontSize(13).FontWeight(600).TextColor(t.Text).LineHeight(1.5).Selectable()
				case update.NoteItem:
					ui.Text(c.Key(i), line.Text).FontSize(13).TextColor(t.Text).LineHeight(1.55).Padding(0, 0, 0, 4).Selectable()
				default:
					ui.Text(c.Key(i), line.Text).FontSize(13).TextColor(t.TextMuted).LineHeight(1.55).Selectable()
				}
			}
		})
	})
}

// byteSize is a size for people: "1.4 MB".
func byteSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, suffix := float64(n), []string{"KB", "MB", "GB"}
	i := -1
	for v >= unit && i < len(suffix)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", v, suffix[i])
}
