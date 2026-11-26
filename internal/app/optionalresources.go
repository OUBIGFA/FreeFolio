package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type resourceState struct {
	dir, busy, confirm string
	installed          map[string]bool
	cancel             context.CancelFunc
	err                error
	received           atomic.Int64
	total              int64

	comfy        *localresource.ComfyManager
	comfyURL     string
	comfyURLInit bool
	comfyBusy    bool
	comfyCaps    *localresource.ComfyCapabilities
	comfyErr     error
}

func (a *App) comfyManager() *localresource.ComfyManager {
	if a.resources.comfy == nil {
		a.resources.comfy = localresource.NewComfyManager()
	}
	return a.resources.comfy
}

func (a *App) comfyConfig() localresource.ComfyConfig {
	return localresource.ComfyConfig{
		Dir:         strings.TrimSpace(a.prefs.ComfyUI.Dir),
		URL:         strings.TrimSpace(a.prefs.ComfyUI.URL),
		ResourceDir: a.resourceDirectory(),
	}
}

func (a *App) stopComfyUI() {
	if a.resources.comfy != nil {
		a.resources.comfy.Stop()
	}
}

func (a *App) setComfyDirectory(dir string) {
	dir = strings.TrimSpace(dir)
	if dir != "" && !filepath.IsAbs(dir) {
		return
	}
	if dir != "" {
		dir = filepath.Clean(dir)
	}
	a.prefs.ComfyUI.Dir = dir
	a.savePrefs()
	a.resources.comfyCaps = nil
	a.resources.comfyErr = nil
	a.stopComfyUI()
}

func (a *App) chooseComfyDirectory() {
	win := a.win
	if win == nil {
		return
	}
	opts := mygo.OpenDialogOptions{Parent: win, Directory: true, Title: i18n.T("选择 ComfyUI 目录"), DefaultPath: a.prefs.ComfyUI.Dir}
	a.backgroundResult(func() func() {
		picked, err := mygo.Dialog.Open(opts)
		return func() {
			if err != nil {
				a.resources.comfyErr = err
			} else if len(picked) > 0 {
				a.setComfyDirectory(picked[0])
			}
		}
	})
}

func (a *App) probeComfyUI() {
	s := &a.resources
	if s.comfyBusy {
		return
	}
	s.comfyBusy, s.comfyErr = true, nil
	mgr := a.comfyManager()
	cfg := a.comfyConfig()
	a.backgroundResult(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel()
		caps, err := mgr.Probe(ctx, cfg)
		return func() {
			s.comfyBusy = false
			s.comfyErr = err
			if err == nil {
				s.comfyCaps = caps
			}
		}
	})
}

func (a *App) resourceDirectory() string {
	if a.prefs.ResourceDir != "" {
		return a.prefs.ResourceDir
	}
	return filepath.Join(a.cfg.ConfigDir, "resources")
}

func (a *App) setResourceDirectory(dir string) {
	if !filepath.IsAbs(dir) || a.resources.busy != "" {
		return
	}
	a.prefs.ResourceDir = filepath.Clean(dir)
	a.savePrefs()
	a.resources.dir = ""
	a.resources.confirm = ""
	a.resources.err = nil
}

func (a *App) chooseResourceDirectory() {
	win := a.win
	if win == nil {
		return
	}
	opts := mygo.OpenDialogOptions{Parent: win, Directory: true, Title: i18n.T("选择资源保存位置"), DefaultPath: a.resourceDirectory()}
	a.backgroundResult(func() func() {
		picked, err := mygo.Dialog.Open(opts)
		return func() {
			if err != nil {
				a.resources.err = err
			} else if len(picked) > 0 {
				a.setResourceDirectory(picked[0])
			}
		}
	})
}

func (a *App) inspectResources(root string, items []localresource.Resource) {
	state := &a.resources
	state.dir = root
	state.installed = nil
	a.backgroundResult(func() func() {
		installed := map[string]bool{}
		for _, item := range items {
			installed[item.ID] = localresource.Present(root, item)
		}
		return func() {
			if state.dir == root {
				state.installed = installed
			}
		}
	})
}

func (a *App) resourceAction(item localresource.Resource, remove bool) {
	s := &a.resources
	if s.busy != "" || (remove && s.confirm != item.ID) {
		return
	}
	root := a.resourceDirectory()
	s.busy, s.confirm, s.err = item.ID, "", nil
	items := []localresource.Resource{item}
	engine, supported := localresource.Runtime()
	upscaleEngine, _ := localresource.UpscaleRuntime()
	if !remove && supported && item.ID != engine.ID && item.ID != upscaleEngine.ID && !s.installed[engine.ID] {
		items = append([]localresource.Resource{engine}, items...)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	if remove {
		s.cancel = nil
	}
	s.received.Store(0)
	s.total = 0
	for _, part := range items {
		s.total += part.Size
	}
	a.backgroundResult(func() func() {
		defer cancel()
		var err error
		if remove {
			err = localresource.Remove(root, item, a.trashItem)
		} else {
			var before int64
			for _, part := range items {
				err = localresource.Install(ctx, root, part, func(done int64) { s.received.Store(before + done) })
				if err != nil {
					break
				}
				before += part.Size
			}
		}
		return func() {
			s.cancel = nil
			s.busy = ""
			s.dir = ""
			if !errors.Is(err, context.Canceled) {
				s.err = err
			}
		}
	})
}

func (a *App) settingsResourceRow(c *ui.Context, item localresource.Resource, detail string) {
	s := &a.resources
	settingRow(c, item.ID, item.Name, detail, func() {
		switch {
		case s.busy == item.ID:
			if s.cancel != nil {
				ui.Text(c, fmt.Sprintf("%.0f / %.0f MiB", float64(s.received.Load())/(1<<20), float64(s.total)/(1<<20))).FontSize(12)
				if ui.Button(c.Key("cancel"), i18n.T("取消")).Clicked() {
					s.cancel()
				}
				c.After(150 * time.Millisecond)
			} else {
				ui.Text(c, i18n.T("正在移除资源")).FontSize(12)
			}
		case s.confirm == item.ID:
			if ui.Button(c.Key("confirm"), i18n.T("移到回收站")).Clicked() {
				a.resourceAction(item, true)
			}
			if ui.Button(c.Key("keep"), i18n.T("取消")).Clicked() {
				s.confirm = ""
			}
		case s.installed[item.ID]:
			ui.Text(c, i18n.T("已下载")).FontSize(12)
			if ui.Button(c.Key("remove"), i18n.T("删除")).Label(i18n.T("删除") + " " + item.Name).Disabled(s.busy != "").Clicked() {
				s.confirm = item.ID
			}
		default:
			if ui.Button(c.Key("download"), i18n.T("下载")).Label(i18n.T("下载") + " " + item.Name).Disabled(s.busy != "" || s.installed == nil).Clicked() {
				a.resourceAction(item, false)
			}
		}
	})
}

func (a *App) settingsResources(c *ui.Context) {
	s := &a.resources
	root := a.resourceDirectory()
	engine, supported := localresource.Runtime()
	upscaleEngine, upscaleSupported := localresource.UpscaleRuntime()
	items := append([]localresource.Resource(nil), localresource.Models...)
	if supported {
		items = append(items, engine)
	}
	inspectItems := append([]localresource.Resource(nil), items...)
	if upscaleSupported {
		inspectItems = append(inspectItems, upscaleEngine)
	}
	if s.dir != root {
		a.inspectResources(root, inspectItems)
	}
	settingsHeading(c, i18n.T("可选资源"), i18n.T("按需下载到本机，白板抠图与图片放大不会上传图片"))
	settingsSection(c, i18n.T("保存位置"))
	settingsGroup(c, "resource-folder", func() {
		settingRow(c, "path", i18n.T("资源目录"), root, func() {
			if ui.Button(c.Key("change"), i18n.T("更改位置")).Disabled(s.busy != "").Clicked() {
				a.chooseResourceDirectory()
			}
		})
		ui.Text(c, i18n.T("切换目录不会移动或删除旧文件，选回原目录可以继续使用")).FontSize(12).TextColor(c.Theme().TextMuted).Padding(0, 16, 12, 16)
	})
	settingsSection(c, i18n.T("本地抠图"))
	if !supported {
		ui.Text(c, i18n.T("此平台暂不支持本地抠图"))
	} else {
		settingsGroup(c, "cutout-resources", func() {
			for index, item := range items {
				if index > 0 {
					settingsSeparator(c)
				}
				detail := i18n.T("通用与人像发丝均衡，推荐优先试用")
				switch item.ID {
				case "birefnet-hr-matting":
					detail = i18n.T("2048×2048 高清精细抠图，发丝与半透明细节更强，占用更多时间与内存")
				case engine.ID:
					detail = i18n.T("两个模型共用，首次下载模型时一并下载")
				}
				a.settingsResourceRow(c, item, fmt.Sprintf("%s · %.1f MiB · MIT", detail, float64(item.Size)/(1<<20)))
			}
		})
	}
	settingsSection(c, i18n.T("本地图片放大"))
	if !upscaleSupported {
		ui.Text(c, i18n.T("此平台暂不支持本地图片放大"))
	} else {
		settingsGroup(c, "upscale-resources", func() {
			detail := fmt.Sprintf("%s · %.1f MiB · MIT / BSD-3-Clause", i18n.T("含 Real-ESRGAN x4plus（通用写实）与 x4plus Anime（动漫插画），支持 Vulkan GPU 加速"), float64(upscaleEngine.Size)/(1<<20))
			a.settingsResourceRow(c, upscaleEngine, detail)
		})
	}
	settingsSection(c, i18n.T("ComfyUI 引擎（GPU 加速）"))
	if !s.comfyURLInit {
		s.comfyURL = a.prefs.ComfyUI.URL
		s.comfyURLInit = true
	}
	settingsGroup(c, "comfyui-engine", func() {
		dirNote := a.prefs.ComfyUI.Dir
		if dirNote == "" {
			dirNote = i18n.T("未配置")
		}
		settingRow(c, "comfy-dir", i18n.T("ComfyUI 目录"), dirNote, func() {
			if a.prefs.ComfyUI.Dir != "" && ui.Button(c.Key("clear-comfy"), i18n.T("清除")).Disabled(s.comfyBusy).Clicked() {
				a.setComfyDirectory("")
			}
			if ui.Button(c.Key("pick-comfy"), i18n.T("选择目录")).Disabled(s.comfyBusy).Clicked() {
				a.chooseComfyDirectory()
			}
		})
		settingsSeparator(c)
		settingRow(c, "comfy-url", i18n.T("服务地址（可选）"), i18n.T("留空则按 ComfyUI 目录静默启动，也可填已运行的地址"), func() {
			if ui.TextInput(c.Key("comfy-url-input"), &s.comfyURL).Placeholder("http://127.0.0.1:8188").Width(210).Disabled(s.comfyBusy).Changed() {
				a.prefs.ComfyUI.URL = strings.TrimSpace(s.comfyURL)
				a.savePrefs()
				s.comfyCaps = nil
				s.comfyErr = nil
			}
		})
		settingsSeparator(c)
		status := i18n.T("未检测")
		switch {
		case s.comfyBusy:
			status = i18n.T("正在启动并检测 ComfyUI")
		case s.comfyCaps != nil:
			dev := s.comfyCaps.Device
			if dev == "" {
				dev = "ComfyUI"
			}
			status = fmt.Sprintf(i18n.T("已连接 %s · 放大模型 %d 个 · 抠图模型 %d 个"), dev, len(s.comfyCaps.UpscaleModels), len(s.comfyCaps.CutoutModels))
		}
		settingRow(c, "comfy-status", i18n.T("运行状态"), status, func() {
			if a.comfyManager().Running() && ui.Button(c.Key("stop-comfy"), i18n.T("停止引擎")).Disabled(s.comfyBusy).Clicked() {
				a.stopComfyUI()
				s.comfyCaps = nil
			}
			canProbe := !s.comfyBusy && (a.prefs.ComfyUI.Dir != "" || strings.TrimSpace(a.prefs.ComfyUI.URL) != "")
			if ui.Button(c.Key("probe-comfy"), i18n.T("检测并启动")).Disabled(!canProbe).Clicked() {
				a.probeComfyUI()
			}
		})
		ui.Text(c, i18n.T("FreeFolio 会通过 --extra-model-paths-config 挂载资源目录下的 comfyui 文件夹，不修改原有 ComfyUI 目录")).FontSize(12).TextColor(c.Theme().TextMuted).Padding(0, 16, 12, 16)
	})
	if s.comfyErr != nil {
		ui.Text(c, i18n.T("ComfyUI 连接失败：")+s.comfyErr.Error()).TextColor(c.Theme().Danger).FontSize(12)
	}
	if s.confirm != "" {
		ui.Text(c, i18n.T("仅移除所选资源，不影响原图或已生成的图片")).FontSize(12)
		if supported && s.confirm == engine.ID {
			ui.Text(c, i18n.T("删除共用运行库后，两个模型都需要重新下载运行库才能运行")).FontSize(12)
		}
	}
	if s.err != nil {
		ui.Text(c, i18n.T("资源操作失败：")+s.err.Error()).TextColor(c.Theme().Danger).FontSize(12)
	}
}
