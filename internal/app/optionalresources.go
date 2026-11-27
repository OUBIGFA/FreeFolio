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
	tab                       int
	dir, busy, confirm        string
	installed                 map[string]bool
	cancel                    context.CancelFunc
	err                       error
	received                  atomic.Int64
	total                     int64
	ownStart, dependencyTotal int64
	dependencyLabel           string
	paused, pausing, stopping bool
	resume                    func()

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

const resourceBusyMigrate = "__migrate__"

func (a *App) setResourceDirectory(dir string) {
	if !filepath.IsAbs(dir) || a.resources.busy != "" {
		return
	}
	newDir := filepath.Clean(dir)
	oldDir := filepath.Clean(a.resourceDirectory())
	if localresource.SameDirectory(oldDir, newDir) {
		return
	}
	a.stopComfyUI()
	items, total, hasComfy := localresource.CollectMigratableFiles(oldDir)
	if len(items) == 0 && !hasComfy {
		a.prefs.ResourceDir = newDir
		a.savePrefs()
		a.resources.dir = ""
		a.resources.confirm = ""
		a.resources.err = nil
		a.resources.comfyCaps = nil
		return
	}
	s := &a.resources
	s.busy, s.confirm, s.err = resourceBusyMigrate, "", nil
	s.comfyCaps = nil
	s.received.Store(0)
	s.total = total
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	mgr := s.comfy
	a.backgroundResult(func() func() {
		defer cancel()
		err := localresource.Migrate(ctx, oldDir, newDir, func(done, _ int64) {
			s.received.Store(done)
		})
		if mgr != nil {
			mgr.InvalidateCaps()
		}
		return func() {
			s.cancel = nil
			s.busy = ""
			s.dir = ""
			s.comfyCaps = nil
			if err == nil {
				a.prefs.ResourceDir = newDir
				a.savePrefs()
			} else if !errors.Is(err, context.Canceled) {
				s.err = err
			}
		}
	})
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
	s.paused, s.pausing, s.stopping = false, false, false
	if !remove {
		s.resume = func() { s.busy = ""; a.resourceAction(item, false) }
	} else {
		s.resume = nil
	}
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
	s.ownStart, s.dependencyTotal, s.dependencyLabel = 0, 0, ""
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
			a.finishResourceDownload(err)
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
	s.paused, s.pausing, s.stopping = false, false, false
	if !remove {
		s.resume = func() { s.busy = ""; a.packAction(pack, false) }
	} else {
		s.resume = nil
	}
	download := pack
	if !remove {
		var err error
		download, err = localresource.PackDownload(pack)
		if err != nil {
			s.busy = ""
			s.err = err
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	if remove {
		s.cancel = nil
	}
	a.setPackDownloadProgress(pack, download)
	mgr := a.resources.comfy
	a.backgroundResult(func() func() {
		defer cancel()
		var err error
		if remove {
			err = localresource.RemovePack(root, pack, a.trashItem)
		} else {
			err = localresource.InstallPack(ctx, root, download, func(done, _ int64) {
				s.received.Store(done)
			})
			if err == nil {
				_, err = localresource.WriteComfyExtraModelPaths(root)
			}
		}
		if mgr != nil {
			mgr.InvalidateCaps()
		}
		return func() {
			s.comfyCaps = nil
			a.finishResourceDownload(err)
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
			a.resourceDownloadControls(c)
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
				a.resourceDownloadControls(c)
			case s.confirm == pack.ID:
				if ui.Button(c.Key("confirm"), i18n.T("移到回收站")).Clicked() {
					a.packAction(pack, true)
				}
				if ui.Button(c.Key("keep"), i18n.T("取消")).Clicked() {
					s.confirm = ""
				}
			case s.installed[pack.ID] && (pack.BaseModel == "" || s.installed[pack.BaseModel]):
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
	sizeText := formatResourceBytes(pack.TotalSize())
	license := pack.License
	if license == "Author permission" {
		license = i18n.T("作者授权")
	}
	if pack.BaseModel != "" {
		base, _ := localresource.ComfyPack(pack.BaseModel)
		return i18n.T(pack.Category), false, fmt.Sprintf(i18n.T("LoRA：%s · %s\n底模：%s"), sizeText, license, base.Name)
	}
	if pack.Kind == "seedvr2" {
		sizeText = formatResourceBytes(pack.Files[0].Size)
	}
	purpose := ""
	switch pack.Kind {
	case "rmbg3":
		purpose = i18n.T("精细抠图，可填写材质说明")
	case "sa2va":
		purpose = i18n.T("按文字抠图，需 Sa2VA 插件")
		if pack.ID == "sa2va-qwen3-vl-4b" {
			purpose = i18n.T("按文字抠图，需 Sa2VA 插件；≤16 GB 显存时自动量化")
		}
	case "seedvr2":
		purpose = i18n.T("2× / 4× 重绘放大，保留透明通道")
	case "upscale":
		purpose = i18n.T("2× / 4× 动漫放大")
	case "zimage":
		purpose = i18n.T("文字生图，8 步生成")
	case "qwen-image":
		purpose = i18n.T("文字生图与参考图编辑")
	}
	if pack.MinVRAMGiB > 0 {
		badge = fmt.Sprintf(i18n.T("建议 %d GB+ 显存"), pack.MinVRAMGiB)
	}
	detail = fmt.Sprintf("%s · %s · %s", purpose, sizeText, license)
	if pack.Kind == "seedvr2" {
		detail += "\n" + fmt.Sprintf(i18n.T("另需 VAE：%s"), formatResourceBytes(pack.TotalSize()-pack.Files[0].Size))
	}
	return
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

	settingsHeading(c, i18n.T("可选资源"), i18n.T("下载后可在白板节点中使用"))

	settingsSection(c, i18n.T("硬件与保存位置"))
	settingsGroup(c, "resource-overview", func() {
		gpuDesc := i18n.T("正在检测显卡")
		if s.gpuDone {
			if s.gpuFound {
				gpuDesc = fmt.Sprintf(i18n.T("%s · %d GB 显存"), s.gpu.Name, s.gpu.VRAMGiB())
			} else {
				gpuDesc = i18n.T("未检测到 NVIDIA 显卡")
			}
		}
		settingRow(c, "gpu-info", i18n.T("本机显卡"), gpuDesc, func() {})
		settingsSeparator(c)
		settingRow(c, "path", i18n.T("资源目录"), root, func() {
			if s.busy == resourceBusyMigrate {
				if s.total > 0 {
					ui.Text(c, fmt.Sprintf("%s %s / %s", i18n.T("正在转移资源"), formatResourceBytes(s.received.Load()), formatResourceBytes(s.total))).FontSize(12)
				} else {
					ui.Text(c, i18n.T("正在转移资源")).FontSize(12)
				}
				if s.cancel != nil && ui.Button(c.Key("cancel-migrate"), i18n.T("取消")).Clicked() {
					s.cancel()
				}
				c.After(150 * time.Millisecond)
				return
			}
			if ui.Button(c.Key("change"), i18n.T("更改位置")).Disabled(s.busy != "").Clicked() {
				a.chooseResourceDirectory()
			}
		})
		ui.Text(c, i18n.T("更改位置会迁移已下载资源")).FontSize(12).TextColor(t.TextMuted).Padding(0, 12, 10, 12)
	})

	ui.Tabs(c.Key("resource-tabs"), &s.tab, i18n.T("本地模型"), i18n.T("ComfyUI 模型"), i18n.T("ComfyUI 增强"), "ComfyUI LoRA").Label(i18n.T("资源类型")).Disabled(s.busy != "")

	if s.tab == 0 {
		settingsSection(c, i18n.T("直接运行"))
		ui.Text(c, i18n.T("以下资源无需配置 ComfyUI")).FontSize(12).TextColor(t.TextMuted).Padding(0, 2, 2, 2)
		settingsGroup(c, "builtin-resources", func() {
			rowAdded := false
			if s.tab == 0 {
				if !supported {
					settingRow(c, "no-cutout", i18n.T("本地抠图"), i18n.T("此平台暂不支持本地抠图"), func() {})
					rowAdded = true
				} else {
					for _, item := range items {
						if rowAdded {
							settingsSeparator(c)
						}
						detail := i18n.T("通用物体与人像抠图")
						switch item.ID {
						case "birefnet-hr-matting":
							detail = i18n.T("2048×2048 抠图，内存占用较高")
						case engine.ID:
							detail = i18n.T("共用运行库，下载抠图模型时自动安装")
						}
						a.settingsResourceRow(c, item, fmt.Sprintf("%s · %.1f MiB · MIT", detail, float64(item.Size)/(1<<20)))
						rowAdded = true
					}
				}
			}
			if s.tab == 0 {
				if !upscaleSupported {
					settingRow(c, "no-upscale", i18n.T("本地图片放大"), i18n.T("此平台暂不支持本地图片放大"), func() {})
				} else {
					detail := fmt.Sprintf("%s · %.1f MiB · MIT / BSD-3-Clause", i18n.T("2× / 4× 放大，含照片与动漫模型"), float64(upscaleEngine.Size)/(1<<20))
					a.settingsResourceRow(c, upscaleEngine, detail)
				}
			}
		})
	}

	if s.tab == 1 {
		settingsSection(c, i18n.T("ComfyUI 引擎（GPU 加速）"))
		ui.Text(c, i18n.T("选择目录后，点击「检测并启动」")).FontSize(12).TextColor(t.TextMuted).Padding(0, 2, 2, 2)
		if !s.comfyURLInit {
			s.comfyURL = a.prefs.ComfyUI.URL
			s.comfyURLInit = true
		}
		settingsGroup(c, "comfyui-engine", func() {
			dirNote := a.prefs.ComfyUI.Dir
			if dirNote == "" {
				dirNote = i18n.T("请选择本机 ComfyUI 目录")
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
			status := i18n.T("尚未连接")
			if s.comfyBusy {
				status = i18n.T("正在连接")
			} else if s.comfyCaps != nil {
				dev := s.comfyCaps.Device
				if dev == "" {
					dev = "ComfyUI"
				}
				var available []string
				caps := s.comfyCaps
				for _, item := range []struct {
					name  string
					ready bool
				}{{"RMBG-3.0", caps.RMBG3Native}, {"Sa2VA", caps.Sa2VANode != ""}, {"SeedVR2", caps.SeedVR2Native}, {"Z-Image", caps.ZImageNative}, {"Qwen 2.1", caps.QwenImageNative}, {"LoRA", caps.LoRANative}} {
					if item.ready {
						available = append(available, item.name)
					}
				}
				status = dev
				if len(available) > 0 {
					status += "\n" + fmt.Sprintf(i18n.T("可用：%s"), strings.Join(available, "、"))
				} else {
					status += "\n" + i18n.T("未发现可用节点，请更新 ComfyUI")
				}

			}
			settingRow(c, "comfy-status", i18n.T("连接状态"), status, func() {
				if s.comfyCaps != nil {
					resourceStatusBadge(c, i18n.T("已连接"), true)
				}
				if a.comfyManager().Running() && ui.Button(c.Key("stop-comfy"), i18n.T("停止引擎")).Disabled(s.comfyBusy).Clicked() {
					a.stopComfyUI()
					s.comfyCaps = nil
				}
				canProbe := !s.comfyBusy && s.busy == "" && (a.prefs.ComfyUI.Dir != "" || strings.TrimSpace(a.prefs.ComfyUI.URL) != "")
				if ui.Button(c.Key("probe-comfy"), i18n.T("检测并启动")).Disabled(!canProbe).Clicked() {
					a.probeComfyUI()
				}
			})
			settingsSeparator(c)
			settingRow(c, "comfy-url", i18n.T("服务地址（可选）"), i18n.T("连接已运行的服务时填写；留空则按所选目录启动"), func() {
				if ui.TextInput(c.Key("comfy-url-input"), &s.comfyURL).Placeholder("http://127.0.0.1:8188").Width(210).Disabled(s.comfyBusy).Changed() {
					a.prefs.ComfyUI.URL = strings.TrimSpace(s.comfyURL)
					a.savePrefs()
					s.comfyCaps = nil
					s.comfyErr = nil
				}
			})
		})
	} else if s.tab > 1 {
		ui.Text(c, i18n.T("先在「ComfyUI 模型」页连接引擎")).FontSize(12).TextColor(t.TextMuted)
	}

	if s.tab == 3 {
		ui.Text(c, i18n.T("LoRA 用于调整生成风格或细节，下载时会补齐缺少的底模")).FontSize(12).TextColor(t.TextMuted)
	}
	if s.tab > 0 {
		settingsSection(c, []string{"", i18n.T("ComfyUI 模型"), i18n.T("ComfyUI 增强"), "ComfyUI LoRA"}[s.tab])
		settingsGroup(c, "comfyui-packs", func() {
			added := false
			for _, pack := range localresource.ComfyPacks {
				if pack.ResourceTab() != s.tab {
					continue
				}
				if added {
					settingsSeparator(c)
				}
				added = true
				badge, accent, detail := a.packDetailAndBadge(pack)
				a.settingsPackRow(c, pack, badge, accent, detail)
			}
			ui.Text(c, i18n.T("支持断点下载；暂停或停止后保留进度，完成后校验")).FontSize(12).TextColor(t.TextMuted).Padding(0, 12, 10, 12)
		})

	}
	if s.comfyErr != nil {
		ui.Text(c, i18n.T("ComfyUI 连接失败：")+s.comfyErr.Error()).TextColor(t.Danger).FontSize(12)
	}
	if s.confirm != "" {
		ui.Text(c, i18n.T("所选资源将移到回收站")).FontSize(12)
		if supported && s.confirm == engine.ID {
			ui.Text(c, i18n.T("移除运行库后，需重新下载才能抠图")).FontSize(12)
		}
	}
	if s.err != nil {
		ui.Text(c, i18n.T("资源操作失败：")+s.err.Error()).TextColor(t.Danger).FontSize(12)
	}
}
