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

	gpuChecked bool
	gpuDone    bool
	gpuFound   bool
	gpu        localresource.GPUInfo

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
		ctx, cancel := context.WithTimeout(context.Background(), 130*time.Second)
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

func (a *App) ensureLocalGPU() {
	s := &a.resources
	if s.gpuChecked {
		return
	}
	s.gpuChecked = true
	a.backgroundResult(func() func() {
		info, ok := localresource.DetectLocalGPU(context.Background())
		return func() {
			s.gpuDone = true
			s.gpuFound = ok
			s.gpu = info
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
	a.resources.comfyCaps = nil
	a.stopComfyUI()
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

func allInspectableSingleResources() []localresource.Resource {
	items := append([]localresource.Resource(nil), localresource.Models...)
	if engine, ok := localresource.Runtime(); ok {
		items = append(items, engine)
	}
	if upscale, ok := localresource.UpscaleRuntime(); ok {
		items = append(items, upscale)
	}
	return items
}

func (a *App) ensureResourcesInspected() {
	root := a.resourceDirectory()
	if a.resources.dir == root {
		return
	}
	a.inspectResources(root, allInspectableSingleResources())
}

func (a *App) inspectResources(root string, items []localresource.Resource) {
	state := &a.resources
	state.dir = root
	state.installed = nil
	packs := localresource.ComfyPacks
	a.backgroundResult(func() func() {
		installed := map[string]bool{}
		for _, item := range items {
			installed[item.ID] = localresource.Present(root, item)
		}
		for _, pack := range packs {
			installed[pack.ID] = localresource.PackPresent(root, pack)
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

func (a *App) packAction(pack localresource.ResourcePack, remove bool) {
	s := &a.resources
	if s.busy != "" || (remove && s.confirm != pack.ID) {
		return
	}
	root := a.resourceDirectory()
	s.busy, s.confirm, s.err = pack.ID, "", nil
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	if remove {
		s.cancel = nil
	}
	s.received.Store(0)
	s.total = pack.TotalSize()
	mgr := a.resources.comfy
	a.backgroundResult(func() func() {
		defer cancel()
		var err error
		if remove {
			err = localresource.RemovePack(root, pack, a.trashItem)
		} else {
			err = localresource.InstallPack(ctx, root, pack, func(done, _ int64) {
				s.received.Store(done)
			})
			if err == nil {
				_, _ = localresource.WriteComfyExtraModelPaths(root)
			}
		}
		if mgr != nil {
			mgr.InvalidateCaps()
		}
		return func() {
			s.cancel = nil
			s.busy = ""
			s.dir = ""
			s.comfyCaps = nil
			if !errors.Is(err, context.Canceled) {
				s.err = err
			}
		}
	})
}

func formatResourceBytes(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
}

func resourceStatusBadge(c *ui.Context, text string, accent bool) {
	t := c.Theme()
	bg, ink := t.SurfaceHover, t.TextMuted
	if accent {
		bg, ink = t.Accent.Alpha(0.14), t.Accent
	}
	ui.Box(c).Padding(2, 8).Radius(999).Background(bg).Children(func() {
		ui.Text(c, text).FontSize(11).FontWeight(600).TextColor(ink).NoWrap()
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
			resourceStatusBadge(c, i18n.T("已下载"), true)
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

func (a *App) settingsPackRow(c *ui.Context, pack localresource.ResourcePack, badgeText string, badgeAccent bool, detail string) {
	s := &a.resources
	t := c.Theme()
	ui.Row(c.Key(pack.ID)).Padding(settingsRowInset(c), 12).Gap(16).AlignItems(ui.Center).MinHeight(56).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, pack.Name).FontSize(13).FontWeight(500)
				if badgeText != "" {
					resourceStatusBadge(c, badgeText, badgeAccent)
				}
			})
			ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted).LineHeight(1.45)
		})
		ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
			switch {
			case s.busy == pack.ID:
				if s.cancel != nil {
					ui.Text(c, fmt.Sprintf("%s / %s", formatResourceBytes(s.received.Load()), formatResourceBytes(s.total))).FontSize(12)
					if ui.Button(c.Key("cancel"), i18n.T("取消")).Clicked() {
						s.cancel()
					}
					c.After(150 * time.Millisecond)
				} else {
					ui.Text(c, i18n.T("正在移除资源")).FontSize(12)
				}
			case s.confirm == pack.ID:
				if ui.Button(c.Key("confirm"), i18n.T("移到回收站")).Clicked() {
					a.packAction(pack, true)
				}
				if ui.Button(c.Key("keep"), i18n.T("取消")).Clicked() {
					s.confirm = ""
				}
			case s.installed[pack.ID]:
				resourceStatusBadge(c, i18n.T("已下载"), true)
				if ui.Button(c.Key("remove"), i18n.T("删除")).Label(i18n.T("删除") + " " + pack.Name).Disabled(s.busy != "").Clicked() {
					s.confirm = pack.ID
				}
			default:
				if ui.Button(c.Key("download"), i18n.T("下载")).Label(i18n.T("下载") + " " + pack.Name).Disabled(s.busy != "" || s.installed == nil).Clicked() {
					a.packAction(pack, false)
				}
			}
		})
	})
}

func (a *App) packDetailAndBadge(pack localresource.ResourcePack) (badge string, accent bool, detail string) {
	s := &a.resources
	vram := s.gpu.VRAMGiB()
	sizeText := formatResourceBytes(pack.TotalSize())
	switch pack.ID {
	case "rmbg3-fp8":
		switch {
		case s.gpuFound && vram >= pack.MinVRAMGiB:
			badge = fmt.Sprintf(i18n.T("推荐 · 适配 %d GB 显存"), vram)
			accent = true
		case s.gpuFound && vram > 0:
			badge = fmt.Sprintf(i18n.T("需 %d GB+ 显存"), pack.MinVRAMGiB)
		default:
			badge = i18n.T("8 GB+ 显存 · FP8 量化")
		}
		detail = fmt.Sprintf(i18n.T("生成级精细抠图，发丝、玻璃与半透明材质最佳（含文本编码器与 VAE） · %s · %s"), sizeText, pack.License)
	case "sa2va-internvl3-2b":
		switch {
		case s.gpuFound && vram >= pack.MinVRAMGiB:
			badge = i18n.T("推荐 · 轻量快速")
			accent = true
		case s.gpuFound && vram > 0:
			badge = fmt.Sprintf(i18n.T("需 %d GB+ 显存"), pack.MinVRAMGiB)
		default:
			badge = i18n.T("6 GB+ 显存 · 轻量快速")
		}
		detail = fmt.Sprintf(i18n.T("按文字指定抠出画面中的特定对象，速度快、显存占用低 · %s · %s · 需安装 ComfyUI-Sa2VA 插件"), sizeText, pack.License)
	case "sa2va-qwen3-vl-4b":
		switch {
		case s.gpuFound && vram >= 12:
			badge = fmt.Sprintf(i18n.T("高精度 · 适配 %d GB 显存（自动 8-bit）"), vram)
			accent = true
		case s.gpuFound && vram >= pack.MinVRAMGiB:
			badge = i18n.T("可用 · 自动 8-bit 量化")
		default:
			badge = i18n.T("10 GB+ 显存 · 自动 8-bit 量化")
		}
		detail = fmt.Sprintf(i18n.T("复杂画面指代理解更精准，16 GB 及以下显存自动启用 8-bit 量化 · %s · %s · 需安装 ComfyUI-Sa2VA 插件"), sizeText, pack.License)
	default:
		detail = fmt.Sprintf("%s · %s", sizeText, pack.License)
	}
	return badge, accent, detail
}

func (a *App) settingsResources(c *ui.Context) {
	s := &a.resources
	t := c.Theme()
	a.ensureLocalGPU()
	a.ensureResourcesInspected()
	root := a.resourceDirectory()
	engine, supported := localresource.Runtime()
	upscaleEngine, upscaleSupported := localresource.UpscaleRuntime()
	items := append([]localresource.Resource(nil), localresource.Models...)
	if supported {
		items = append(items, engine)
	}

	settingsHeading(c, i18n.T("可选资源"), i18n.T("按需下载到本地运行，所有图片处理均不上传云端；入门用户用第 1 组即可，独显用户可按步骤开启第 2 组"))

	settingsSection(c, i18n.T("概览：本机硬件与保存位置"))
	settingsGroup(c, "resource-overview", func() {
		gpuDesc := i18n.T("正在检测本机显卡")
		gpuBadge, gpuAccent := "", false
		if s.gpuDone {
			if s.gpuFound {
				vram := s.gpu.VRAMGiB()
				if vram >= 8 {
					gpuDesc = fmt.Sprintf(i18n.T("%s · %d GB 显存 · 可流畅运行 ComfyUI FP8 与 8-bit 量化模型"), s.gpu.Name, vram)
					gpuBadge, gpuAccent = fmt.Sprintf(i18n.T("%d GB 显存 · 推荐 FP8 / 量化方案"), vram), true
				} else {
					gpuDesc = fmt.Sprintf(i18n.T("%s · %d GB 显存 · 建议优先使用下方开箱即用模型"), s.gpu.Name, vram)
					gpuBadge = fmt.Sprintf(i18n.T("%d GB 显存"), vram)
				}
			} else {
				gpuDesc = i18n.T("未检测到 NVIDIA 独立显卡 · 建议优先使用开箱即用模型")
				gpuBadge = i18n.T("通用模式")
			}
		}
		settingRow(c, "gpu-info", i18n.T("本机显卡"), gpuDesc, func() {
			if gpuBadge != "" {
				resourceStatusBadge(c, gpuBadge, gpuAccent)
			}
		})
		settingsSeparator(c)
		settingRow(c, "path", i18n.T("资源目录"), root, func() {
			if ui.Button(c.Key("change"), i18n.T("更改位置")).Disabled(s.busy != "").Clicked() {
				a.chooseResourceDirectory()
			}
		})
		ui.Text(c, i18n.T("切换目录不会移动或删除旧文件，选回原目录可以继续使用")).FontSize(12).TextColor(t.TextMuted).Padding(0, 12, 10, 12)
	})

	settingsSection(c, i18n.T("开箱即用（免配置 · 下载即用）"))
	ui.Text(c, i18n.T("无需安装 Python 或 ComfyUI，下载后直接在白板的「本地抠图」与「图片放大」节点使用")).FontSize(12).TextColor(t.TextMuted).Padding(0, 2, 2, 2)
	settingsGroup(c, "builtin-resources", func() {
		rowAdded := false
		if !supported {
			settingRow(c, "no-cutout", i18n.T("本地抠图"), i18n.T("此平台暂不支持本地抠图"), func() {})
			rowAdded = true
		} else {
			for _, item := range items {
				if rowAdded {
					settingsSeparator(c)
				}
				detail := i18n.T("推荐新手优先试用 · 日常通用与人像发丝均衡")
				switch item.ID {
				case "birefnet-hr-matting":
					detail = i18n.T("2048×2048 高清精细抠图 · 发丝与半透明细节更强，耗时与内存占用较高")
				case engine.ID:
					detail = i18n.T("抠图共享引擎 · 首次下载上方模型时自动附带")
				}
				a.settingsResourceRow(c, item, fmt.Sprintf("%s · %.1f MiB · MIT", detail, float64(item.Size)/(1<<20)))
				rowAdded = true
			}
		}
		if rowAdded {
			settingsSeparator(c)
		}
		if !upscaleSupported {
			settingRow(c, "no-upscale", i18n.T("本地图片放大"), i18n.T("此平台暂不支持本地图片放大"), func() {})
		} else {
			detail := fmt.Sprintf("%s · %.1f MiB · MIT / BSD-3-Clause", i18n.T("2× / 4× 高清放大（含写实照片与动漫插画），支持 Vulkan GPU 加速"), float64(upscaleEngine.Size)/(1<<20))
			a.settingsResourceRow(c, upscaleEngine, detail)
		}
	})

	settingsSection(c, i18n.T("ComfyUI 引擎（GPU 加速）"))
	ui.Text(c, i18n.T("适合独显用户：第 1 步连接本机 ComfyUI，第 2 步按显卡容量下载专业模型，即可在白板直接调用")).FontSize(12).TextColor(t.TextMuted).Padding(0, 2, 2, 2)
	if !s.comfyURLInit {
		s.comfyURL = a.prefs.ComfyUI.URL
		s.comfyURLInit = true
	}
	settingsGroup(c, "comfyui-engine", func() {
		dirNote := a.prefs.ComfyUI.Dir
		if dirNote == "" {
			dirNote = i18n.T("未配置 · 点击右侧选择本机 ComfyUI 文件夹即可，FreeFolio 会静默拉起且不修改原目录")
		}
		settingRow(c, "comfy-dir", i18n.T("步骤 1 · ComfyUI 目录"), dirNote, func() {
			if a.prefs.ComfyUI.Dir != "" && ui.Button(c.Key("clear-comfy"), i18n.T("清除")).Disabled(s.comfyBusy).Clicked() {
				a.setComfyDirectory("")
			}
			if ui.Button(c.Key("pick-comfy"), i18n.T("选择目录")).Disabled(s.comfyBusy).Clicked() {
				a.chooseComfyDirectory()
			}
		})
		settingsSeparator(c)
		status := i18n.T("选好目录后点击右侧「检测并启动」，确认显卡与可用节点")
		if s.comfyBusy {
			status = i18n.T("正在启动并检测 ComfyUI")
		} else if s.comfyCaps != nil {
			dev := s.comfyCaps.Device
			if dev == "" {
				dev = "ComfyUI"
			}
			rmbgState := i18n.T("未就绪（需升级 ComfyUI 至 0.4.60+）")
			if s.comfyCaps.RMBG3Native {
				rmbgState = i18n.T("原生节点已就绪")
			}
			sa2vaState := i18n.T("未安装（可选装 ComfyUI-Sa2VA-XJ 或 ComfyUI-Sa2VA）")
			if s.comfyCaps.Sa2VANode != "" {
				sa2vaState = s.comfyCaps.Sa2VANode
			}
			status = fmt.Sprintf(i18n.T("已连接 %s · RMBG-3.0：%s · Sa2VA：%s · 放大模型 %d 个"), dev, rmbgState, sa2vaState, len(s.comfyCaps.UpscaleModels))
		}
		settingRow(c, "comfy-status", i18n.T("连接与能力状态"), status, func() {
			if s.comfyCaps != nil {
				resourceStatusBadge(c, i18n.T("已连接"), true)
			}
			if a.comfyManager().Running() && ui.Button(c.Key("stop-comfy"), i18n.T("停止引擎")).Disabled(s.comfyBusy).Clicked() {
				a.stopComfyUI()
				s.comfyCaps = nil
			}
			canProbe := !s.comfyBusy && (a.prefs.ComfyUI.Dir != "" || strings.TrimSpace(a.prefs.ComfyUI.URL) != "")
			if ui.Button(c.Key("probe-comfy"), i18n.T("检测并启动")).Disabled(!canProbe).Clicked() {
				a.probeComfyUI()
			}
		})
		settingsSeparator(c)
		settingRow(c, "comfy-url", i18n.T("服务地址（进阶 · 可选）"), i18n.T("留空即可自动启动；仅需连接已手动启动的 ComfyUI 窗口时填写"), func() {
			if ui.TextInput(c.Key("comfy-url-input"), &s.comfyURL).Placeholder("http://127.0.0.1:8188").Width(210).Disabled(s.comfyBusy).Changed() {
				a.prefs.ComfyUI.URL = strings.TrimSpace(s.comfyURL)
				a.savePrefs()
				s.comfyCaps = nil
				s.comfyErr = nil
			}
		})
	})

	settingsSection(c, i18n.T("步骤 2 · 下载 GPU 专业模型（自动挂载到 ComfyUI）"))
	settingsGroup(c, "comfyui-packs", func() {
		for index, pack := range localresource.ComfyPacks {
			if index > 0 {
				settingsSeparator(c)
			}
			badge, accent, detail := a.packDetailAndBadge(pack)
			a.settingsPackRow(c, pack, badge, accent, detail)
		}
		ui.Text(c, i18n.T("下载文件经 --extra-model-paths-config 自动挂载并做 SHA-256 校验，中断后再次点击下载可断点继续")).FontSize(12).TextColor(t.TextMuted).Padding(0, 12, 10, 12)
	})

	if s.comfyErr != nil {
		ui.Text(c, i18n.T("ComfyUI 连接失败：")+s.comfyErr.Error()).TextColor(t.Danger).FontSize(12)
	}
	if s.confirm != "" {
		ui.Text(c, i18n.T("仅移除所选资源，不影响原图或已生成的图片")).FontSize(12)
		if supported && s.confirm == engine.ID {
			ui.Text(c, i18n.T("删除共用运行库后，两款本地抠图模型都需要重新下载运行库才能运行")).FontSize(12)
		}
	}
	if s.err != nil {
		ui.Text(c, i18n.T("资源操作失败：")+s.err.Error()).TextColor(t.Danger).FontSize(12)
	}
}
