package app

import (
	"fmt"
	"github.com/OUBIGFA/FreeFolio/internal/agentmcp"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
	"os"
	"strings"
)

func (a *App) agentCommandPath() string {
	if !a.agentSkillsChecked || a.agentBinCmd == "" {
		a.agentBinCmd = agentmcp.ResolveBinaryCommand()
	}
	return a.agentBinCmd
}

func (a *App) resolveAgentHomeDir() (string, error) {
	if a.agentHomeDir != "" {
		return a.agentHomeDir, nil
	}
	return os.UserHomeDir()
}

func (a *App) refreshAgentSkillsCache() {
	home, _ := a.resolveAgentHomeDir()
	a.agentSkillTargets = agentmcp.DetectSkillTargets(home)
	a.agentBinCmd = agentmcp.ResolveBinaryCommand()
	a.agentSkillsChecked = true
}

func (a *App) installAgentSkillsAction() {
	h, err := a.resolveAgentHomeDir()
	if err != nil {
		a.notify("读取主目录失败；%v", err)
		return
	}
	paths, instErr := agentmcp.InstallSkills(h, agentmcp.ResolveBinaryCommand())
	if instErr != nil {
		a.notify("安装 Skill 失败；%v", instErr)
		return
	}
	a.refreshAgentSkillsCache()
	a.notify("已安装 FreeFolio Skill 到 %d 个 Agent 目录", len(paths))
}

func (a *App) settingsLocalAgents(c *ui.Context, sep func(), row func(key, label, note string, control func())) {
	settingsHeading(c, i18n.T("本地 Agent 接入"), i18n.T("允许 Claude Code、Codex、Cursor、Antigravity 等本机 Agent 通过 CLI 或 MCP 操作空间、富文本文档、手绘白板与多维数据库"))
	if !a.agentSkillsChecked {
		a.refreshAgentSkillsCache()
	}
	var detected []string
	for _, tgt := range a.agentSkillTargets {
		if tgt.Detected {
			detected = append(detected, tgt.Agent)
		}
	}
	skillNote := i18n.T("将 SKILL.md 与富格式规范写入本机 Agent 技能目录")
	if len(detected) > 0 {
		skillNote = fmt.Sprintf(i18n.T("已检测到 %s，点击即可安装或更新"), strings.Join(detected, " / "))
	}
	binCmd := a.agentCommandPath()
	settingsGroup(c, "local-agent", func() {
		row("agent-skill", i18n.T("Agent Skill 技能包"), skillNote, func() {
			if ui.Button(c.Key("install-skill"), i18n.T("一键安装 Skill")).Clicked() {
				a.installAgentSkillsAction()
			}
			if ui.Button(c.Key("copy-skill"), i18n.T("复制 Skill 内容")).Clicked() {
				c.WriteClipboard(agentmcp.SkillMarkdown(binCmd))
				a.notify("已复制 Agent Skill 内容")
			}
		})
		sep()
		row("agent-mcp", i18n.T("MCP Server 配置"), i18n.T("通过 freefolio mcp 接入；窗口打开时经本机 IPC 实时刷新并保留 ⌘Z 撤销"), func() {
			if ui.Button(c.Key("copy-mcp"), i18n.T("复制 MCP 配置")).Clicked() {
				c.WriteClipboard(agentmcp.MCPConfigJSON(binCmd))
				a.notify("已复制 MCP 配置 JSON")
			}
			if ui.Button(c.Key("copy-format-spec"), i18n.T("复制富格式规范")).Clicked() {
				c.WriteClipboard(agentmcp.FormatGuide("all"))
				a.notify("已复制 FreeFolio 富格式规范")
			}
		})
	})
}
