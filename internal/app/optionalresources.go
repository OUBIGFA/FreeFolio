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
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var loraBaseModels = []string{"z-image-turbo-int8", "qwen-image-2.1-turbo-int8", "qwen-image-2.1-int8"}

type resourceTransfer struct {
	installing                       bool
	busy                             string
	cancel                           context.CancelFunc
	err                              error
	received                         atomic.Int64
	total, ownStart, dependencyTotal int64
	dependencyLabel                  string
	paused, pausing, stopping        bool
	resume                           func()
}

// resourcesHost is what the optional resources page needs from the window.
type resourcesHost interface {
	appConfig() Config
	backgroundResult(work func() func())
	preferences() *prefs.Prefs
	savePrefs()
	trashItem(p string) error
	window() *mygo.Window
}

type resourceState struct {
	host             resourcesHost
	resourceTransfer // Directory migration and exclusive removal state.
	downloads        map[string]*resourceTransfer
	tab, loraBaseTab int
	dir, confirm     string
	installed        map[string]bool

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
	inspection   uint64
}

func (rs *resourceState) comfyManager() *localresource.ComfyManager {
	if rs.comfy == nil {
		rs.comfy = localresource.NewComfyManager()
	}
	return rs.comfy
}

func (rs *resourceState) comfyConfig() localresource.ComfyConfig {
	return localresource.ComfyConfig{
		Dir:         strings.TrimSpace(rs.host.preferences().ComfyUI.Dir),
		URL:         strings.TrimSpace(rs.host.preferences().ComfyUI.URL),
		ResourceDir: rs.resourceDirectory(),
	}
}

func (rs *resourceState) stopComfyUI() {
	if rs.comfy != nil {
		rs.comfy.Stop()
	}
	rs.comfyCaps = nil
}

func (rs *resourceState) setComfyDirectory(dir string) {
	dir = strings.TrimSpace(dir)
	if dir != "" && !filepath.IsAbs(dir) {
		return
	}
	if dir != "" {
		dir = filepath.Clean(dir)
	}
	rs.host.preferences().ComfyUI.Dir = dir
	rs.host.savePrefs()
	rs.comfyCaps = nil
	rs.comfyErr = nil
	rs.dir = ""
	rs.stopComfyUI()
}

func (rs *resourceState) chooseComfyDirectory() {
	win := rs.host.window()
	if win == nil {
		return
	}
	opts := mygo.OpenDialogOptions{Parent: win, Directory: true, Title: i18n.T("选择 ComfyUI 目录"), DefaultPath: rs.host.preferences().ComfyUI.Dir}
	rs.host.backgroundResult(func() func() {
		picked, err := mygo.Dialog.Open(opts)
		return func() {
			if err != nil {
				rs.comfyErr = err
			} else if len(picked) > 0 {
				rs.setComfyDirectory(picked[0])
			}
		}
	})
}

func (rs *resourceState) probeComfyUI() {
	s := rs
	if s.comfyBusy {
		return
	}
	s.comfyBusy, s.comfyErr = true, nil
	mgr := rs.comfyManager()
	cfg := rs.comfyConfig()
	rs.host.backgroundResult(func() func() {
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

func (rs *resourceState) ensureLocalGPU() {
	s := rs
	if s.gpuChecked {
		return
	}
	s.gpuChecked = true
	rs.host.backgroundResult(func() func() {
		info, ok := localresource.DetectLocalGPU(context.Background())
		return func() {
			s.gpuDone = true
			s.gpuFound = ok
			s.gpu = info
		}
	})
}

func (rs *resourceState) resourceDirectory() string {
	if rs.host.preferences().ResourceDir != "" {
		return rs.host.preferences().ResourceDir
	}
	return filepath.Join(rs.host.appConfig().ConfigDir, "resources")
}

const resourceBusyMigrate = "__migrate__"

func (rs *resourceState) setResourceDirectory(dir string) {
	if !filepath.IsAbs(dir) || rs.busy != "" || rs.resourcesDownloading() {
		return
	}
	newDir := filepath.Clean(dir)
	oldDir := filepath.Clean(rs.resourceDirectory())
	if localresource.SameDirectory(oldDir, newDir) {
		return
	}
	rs.stopComfyUI()
	items, total, hasComfy := localresource.CollectMigratableFiles(oldDir)
	if len(items) == 0 && !hasComfy {
		rs.host.preferences().ResourceDir = newDir
		rs.host.savePrefs()
		rs.dir = ""
		rs.confirm = ""
		rs.err = nil
		rs.comfyCaps = nil
		return
	}
	s := rs
	s.busy, s.confirm, s.err = resourceBusyMigrate, "", nil
	s.comfyCaps = nil
	s.received.Store(0)
	s.total = total
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	mgr := s.comfy
	rs.host.backgroundResult(func() func() {
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
				rs.host.preferences().ResourceDir = newDir
				rs.host.savePrefs()
			} else if !errors.Is(err, context.Canceled) {
				s.err = err
			}
		}
	})
}

func (rs *resourceState) chooseResourceDirectory() {
	win := rs.host.window()
	if win == nil {
		return
	}
	opts := mygo.OpenDialogOptions{Parent: win, Directory: true, Title: i18n.T("选择资源保存位置"), DefaultPath: rs.resourceDirectory()}
	rs.host.backgroundResult(func() func() {
		picked, err := mygo.Dialog.Open(opts)
		return func() {
			if err != nil {
				rs.err = err
			} else if len(picked) > 0 {
				rs.setResourceDirectory(picked[0])
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

func (rs *resourceState) ensureResourcesInspected() {
	root := rs.resourceDirectory()
	if rs.dir == root {
		return
	}
	rs.inspectResources(root, allInspectableSingleResources())
}

func (rs *resourceState) inspectResources(root string, items []localresource.Resource) {
	state := rs
	state.dir = root
	state.inspection++
	inspection := state.inspection
	state.installed = nil
	packs := localresource.ComfyPacks
	engineDir := rs.host.preferences().ComfyUI.Dir
	rs.host.backgroundResult(func() func() {
		installed := map[string]bool{}
		for _, item := range items {
			installed[item.ID] = localresource.Present(root, item)
		}
		for _, pack := range packs {
			installed[pack.ID] = localresource.PackPresent(root, pack)
			if pack.ID == "z-image-gguf-q8" {
				installed[pack.ID] = installed[pack.ID] && localresource.GGUFPluginInstalled(root, engineDir)
			}
			if pack.Kind == "audio-tts" {
				installed[pack.ID] = installed[pack.ID] && localresource.VoicePluginInstalled(root, engineDir)
			}
		}
		return func() {
			if state.dir == root && state.inspection == inspection {
				state.installed = installed
			}
		}
	})
}

func (rs *resourceState) resourceAction(item localresource.Resource, remove bool) {
	state := rs
	if state.busy != "" || (remove && (state.confirm != item.ID || rs.resourcesDownloading())) {
		return
	}
	if job := state.downloads[item.ID]; job != nil && job.busy != "" {
		return
	}
	s := rs.resourceTransferFor(item.ID)

	root := rs.resourceDirectory()
	s.busy, s.err = item.ID, nil
	state.confirm = ""
	if remove {
		state.busy = item.ID
	}
	s.paused, s.pausing, s.stopping = false, false, false
	if !remove {
		s.resume = func() { s.busy = ""; rs.resourceAction(item, false) }
	} else {
		s.resume = nil
	}
	items := []localresource.Resource{item}
	engine, supported := localresource.Runtime()
	upscaleEngine, _ := localresource.UpscaleRuntime()
	if !remove && supported && item.ID != engine.ID && item.ID != upscaleEngine.ID && !state.installed[engine.ID] {
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
	rs.host.backgroundResult(func() func() {
		defer cancel()
		var err error
		if remove {
			err = localresource.Remove(root, item, rs.host.trashItem)
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
			rs.finishResourceDownload(s, err)
			if remove {
				state.busy = ""
			}
		}
	})
}

func (rs *resourceState) packAction(pack localresource.ResourcePack, remove bool) {
	state := rs
	if state.busy != "" || (remove && (state.confirm != pack.ID || rs.resourcesDownloading())) {
		return
	}
	if job := state.downloads[pack.ID]; job != nil && job.busy != "" {
		return
	}
	s := rs.resourceTransferFor(pack.ID)

	root := rs.resourceDirectory()
	engineDir := rs.host.preferences().ComfyUI.Dir
	if !remove && (pack.Kind == "audio-tts" || pack.ID == "z-image-gguf-q8") {
		if strings.TrimSpace(rs.host.preferences().ComfyUI.URL) != "" {
			s.err = errors.New(i18n.T("自动安装模型组件需由 FreeFolio 启动引擎，请清空服务地址后下载"))
			return
		}
		layout, err := localresource.DetectComfyLayout(engineDir)
		if err != nil || !layout.IsolatedPython {
			s.err = fmt.Errorf("%s", i18n.T("请先选择含独立运行环境的 ComfyUI 目录"))
			return
		}
	}
	s.busy, s.err = pack.ID, nil
	state.confirm = ""
	if remove {
		state.busy = pack.ID
	}
	s.paused, s.pausing, s.stopping = false, false, false
	if !remove {
		s.resume = func() { s.busy = ""; rs.packAction(pack, false) }
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
	rs.setPackDownloadProgress(s, pack, download)
	mgr := rs.comfy
	rs.host.backgroundResult(func() func() {
		defer cancel()
		var err error
		if remove {
			if pack.Kind == "audio-tts" {
				err = localresource.RemoveVoicePlugin(root, rs.host.trashItem)
			} else if pack.ID == "z-image-gguf-q8" {
				err = localresource.RemoveGGUFPlugin(root, rs.host.trashItem)
			}
			if err == nil {
				err = localresource.RemovePack(root, pack, rs.host.trashItem)
			}
		} else {
			err = localresource.InstallPack(ctx, root, download, func(done, _ int64) {
				s.received.Store(done)
			})
			if err == nil && (pack.Kind == "audio-tts" || pack.ID == "z-image-gguf-q8") {
				if rs.host.window() != nil {
					rs.host.window().Update(func() { s.installing = true })
				}
				if pack.ID == "z-image-gguf-q8" {
					err = localresource.InstallGGUFPlugin(ctx, root, engineDir)
				} else {
					err = localresource.InstallVoicePlugin(ctx, root, engineDir)
				}
				if err == nil && mgr != nil {
					mgr.Stop()
				}
			}
			if err == nil {
				_, err = localresource.WriteComfyExtraModelPaths(root)
			}
		}
		if mgr != nil {
			mgr.InvalidateCaps()
		}
		return func() {
			state.comfyCaps = nil
			rs.finishResourceDownload(s, err)
			if remove {
				state.busy = ""
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
	tone := ""
	if accent {
		tone = "accent"
	}
	settingsBadge(c, text, tone)
}

func (rs *resourceState) settingsResourceRow(c *ui.Context, item localresource.Resource, detail string) {
	s := rs
	t := c.Theme()
	job := s.downloads[item.ID]
	activeDL := job != nil && job.busy != ""
	ui.Column(c.Key(item.ID)).Padding(settingsRowInset(c), 12).Gap(8).Children(func() {
		ui.Row(c.Key("main")).Gap(16).AlignItems(ui.Center).MinHeight(40).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
					ui.Text(c, item.Name).FontSize(13).FontWeight(500)
					settingsBadge(c, formatResourceBytes(item.Size), "")
					if s.installed[item.ID] {
						resourceStatusBadge(c, i18n.T("已下载"), true)
					}
				})
				ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted).LineHeight(1.45)
			})
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
				switch {
				case activeDL && job.resume == nil:
					ui.Text(c, i18n.T("正在移除资源")).FontSize(12).TextColor(t.TextMuted)
				case activeDL:
				case s.confirm == item.ID:
					if ui.Button(c.Key("confirm"), i18n.T("移到回收站")).Clicked() {
						rs.resourceAction(item, true)
					}
					if ui.Button(c.Key("keep"), i18n.T("取消")).Clicked() {
						s.confirm = ""
					}
				case s.installed[item.ID]:
					if ui.Button(c.Key("remove"), i18n.T("删除")).Label(i18n.T("删除") + " " + item.Name).Disabled(s.busy != "" || rs.resourcesDownloading()).Clicked() {
						s.confirm = item.ID
					}
				default:
					if ui.Button(c.Key("download"), i18n.T("下载")).Label(i18n.T("下载") + " " + item.Name).Disabled(s.busy != "" || s.installed == nil).Clicked() {
						rs.resourceAction(item, false)
					}
				}
			})
		})
		if activeDL && job.resume != nil {
			rs.resourceDownloadControls(c, job)
		}
		if s.confirm == item.ID {
			warn := i18n.T("所选资源将移到回收站")
			if engine, supported := localresource.Runtime(); supported && item.ID == engine.ID {
				warn += " · " + i18n.T("移除运行库后，需重新下载才能抠图")
			}
			settingsBanner(c, "confirm-warn", warn, "danger", nil)
		}
		if job != nil && job.err != nil {
			settingsBanner(c, "err", job.err.Error(), "danger", nil)
		}
	})
}

func (rs *resourceState) settingsPackRow(c *ui.Context, pack localresource.ResourcePack, badgeText string, badgeAccent bool, detail string) {
	s := rs
	job := s.downloads[pack.ID]
	t := c.Theme()
	activeDL := job != nil && job.busy != ""
	installed := s.installed[pack.ID] && (pack.BaseModel == "" || s.installed[pack.BaseModel])
	sizeBytes := pack.TotalSize()
	if pack.Kind == "seedvr2" && len(pack.Files) > 0 {
		sizeBytes = pack.Files[0].Size
	}
	ui.Column(c.Key(pack.ID)).Padding(settingsRowInset(c), 12).Gap(8).Children(func() {
		ui.Row(c.Key("main")).Gap(16).AlignItems(ui.Center).MinHeight(44).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
					ui.Text(c, pack.Name).FontSize(13).FontWeight(500)
					settingsBadge(c, formatResourceBytes(sizeBytes), "")
					if badgeText != "" {
						resourceStatusBadge(c, badgeText, badgeAccent)
					}
					if installed {
						resourceStatusBadge(c, i18n.T("已下载"), true)
					}
				})
				ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted).LineHeight(1.45)
			})
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
				switch {
				case activeDL && job.resume == nil:
					ui.Text(c, i18n.T("正在移除资源")).FontSize(12).TextColor(t.TextMuted)
				case activeDL:
				case s.confirm == pack.ID:
					if ui.Button(c.Key("confirm"), i18n.T("移到回收站")).Clicked() {
						rs.packAction(pack, true)
					}
					if ui.Button(c.Key("keep"), i18n.T("取消")).Clicked() {
						s.confirm = ""
					}
				case installed:
					if ui.Button(c.Key("remove"), i18n.T("删除")).Label(i18n.T("删除") + " " + pack.Name).Disabled(s.busy != "" || rs.resourcesDownloading()).Clicked() {
						s.confirm = pack.ID
					}
				default:
					if ui.Button(c.Key("download"), i18n.T("下载")).Label(i18n.T("下载") + " " + pack.Name).Disabled(s.busy != "" || s.installed == nil).Clicked() {
						rs.packAction(pack, false)
					}
				}
			})
		})
		if activeDL && job.resume != nil {
			rs.resourceDownloadControls(c, job)
		}
		if s.confirm == pack.ID {
			settingsBanner(c, "confirm-warn", i18n.T("所选资源将移到回收站"), "danger", nil)
		}
		if job != nil && job.err != nil {
			settingsBanner(c, "err", job.err.Error(), "danger", nil)
		}
	})
}

func (rs *resourceState) packDetailAndBadge(pack localresource.ResourcePack) (badge string, accent bool, detail string) {
	sizeText := formatResourceBytes(pack.TotalSize())
	license := pack.License
	if license == "Author permission" {
		license = i18n.T("作者授权")
	}
	if pack.BaseModel != "" {
		base, _ := localresource.ComfyPack(pack.BaseModel)
		detail = fmt.Sprintf(i18n.T("LoRA：%s · %s\n底模：%s"), sizeText, license, base.Name)
		if pack.Experimental {
			detail += "\n" + i18n.T("实验版，效果需验证")
		}
		if len(pack.Dependencies) > 0 {
			var size int64
			for _, f := range pack.Dependencies {
				size += f.Size
			}
			detail += "\n" + fmt.Sprintf(i18n.T("另需文本编码器：%s"), formatResourceBytes(size))
		}
		if pack.Description != "" {
			detail += "\n" + i18n.T(pack.Description)
		}
		return i18n.T(pack.Category), false, detail
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
		if pack.ID != "z-image-turbo-int8" {
			purpose = i18n.T("普通版文字生图，默认 40 步，支持负面提示词")
		}
		if pack.ID == "z-image-bf16" {
			purpose = i18n.T("普通版原精度，12 GB 显卡需内存卸载")
		}
		if pack.ID == "z-image-gguf-q8" {
			purpose = i18n.T("普通版 Q8，下载时安装 GGUF 组件")
		}
	case "audio-music":
		purpose = i18n.T("音乐与歌曲，可填歌词，默认 30 秒")
	case "audio-sfx":
		purpose = i18n.T("音效与环境声，默认 10 秒")
	case "audio-tts":
		purpose = i18n.T("多语言朗读，自动安装语音插件")
	case "video-wan":
		purpose = i18n.T("文字或首帧生成视频，832×480，默认 4 秒")
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

func (rs *resourceState) settingsResources(c *ui.Context) {
	s := rs
	t := c.Theme()
	rs.ensureLocalGPU()
	rs.ensureResourcesInspected()
	root := rs.resourceDirectory()
	engine, supported := localresource.Runtime()
	upscaleEngine, upscaleSupported := localresource.UpscaleRuntime()
	items := append([]localresource.Resource(nil), localresource.Models...)
	if supported {
		items = append(items, engine)
	}

	settingsHeading(c, i18n.T("可选资源"), i18n.T("可同时下载多个资源，下载后在白板节点中使用"))

	settingsSection(c, i18n.T("硬件与保存位置"))
	settingsGroup(c, "resource-overview", func() {
		gpuDesc := i18n.T("正在检测显卡")
		gpuBadge := ""
		if s.gpuDone {
			if s.gpuFound {
				gpuDesc = s.gpu.Name
				gpuBadge = fmt.Sprintf(i18n.T("%d GB 显存"), s.gpu.VRAMGiB())
			} else {
				gpuDesc = i18n.T("未检测到 NVIDIA 显卡")
			}
		}
		settingRow(c, "gpu-info", i18n.T("本机显卡"), gpuDesc, func() {
			if gpuBadge != "" {
				resourceStatusBadge(c, gpuBadge, true)
			}
		})
		settingsSeparator(c)
		settingRow(c, "path", i18n.T("资源目录"), shortPath(root)+" · "+i18n.T("更改位置会迁移已下载资源"), func() {
			if s.busy == resourceBusyMigrate {
				if s.cancel != nil && ui.Button(c.Key("cancel-migrate"), i18n.T("取消")).Clicked() {
					s.cancel()
				}
				return
			}
			if ui.Button(c.Key("change"), i18n.T("更改位置")).Tooltip(root).Disabled(s.busy != "" || rs.resourcesDownloading()).Clicked() {
				rs.chooseResourceDirectory()
			}
		})
		if s.busy == resourceBusyMigrate {
			ui.Column(c.Key("migrate-progress")).Padding(0, 12, 10, 12).Gap(4).Children(func() {
				val := float64(0)
				if s.total > 0 {
					val = float64(s.received.Load()) / float64(s.total)
				}
				ui.Row(c).WidthPercent(100).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, i18n.T("正在转移资源")).FontSize(12).TextColor(t.TextMuted).Grow(1)
					if s.total > 0 {
						ui.Text(c, fmt.Sprintf("%s / %s", formatResourceBytes(s.received.Load()), formatResourceBytes(s.total))).FontSize(12).TextColor(t.TextMuted)
					}
				})
				ui.Progress(c.Key("migrate-bar"), val).WidthPercent(100).Label(i18n.T("正在转移资源"))
			})
			c.After(150 * time.Millisecond)
		}
	})

	ui.Tabs(c.Key("resource-tabs"), &s.tab, i18n.T("本地模型"), i18n.T("ComfyUI 模型"), i18n.T("ComfyUI 增强"), "ComfyUI LoRA", i18n.T("音频模型"), i18n.T("视频模型")).Label(i18n.T("资源类型")).Disabled(s.busy != "")

	if s.tab == 0 {
		settingsSection(c, i18n.T("直接运行"))
		ui.Text(c, i18n.T("以下资源无需配置 ComfyUI")).FontSize(12).TextColor(t.TextMuted).Padding(0, 2, 2, 2)
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
					detail := i18n.T("通用物体与人像抠图")
					switch item.ID {
					case "birefnet-hr-matting":
						detail = i18n.T("2048×2048 抠图，内存占用较高")
					case engine.ID:
						detail = i18n.T("共用运行库，下载抠图模型时自动安装")
					}
					rs.settingsResourceRow(c, item, fmt.Sprintf("%s · MIT", detail))
					rowAdded = true
				}
			}
			if rowAdded {
				settingsSeparator(c)
			}
			if !upscaleSupported {
				settingRow(c, "no-upscale", i18n.T("本地图片放大"), i18n.T("此平台暂不支持本地图片放大"), func() {})
			} else {
				detail := fmt.Sprintf("%s · MIT / BSD-3-Clause", i18n.T("2× / 4× 放大，含照片与动漫模型"))
				rs.settingsResourceRow(c, upscaleEngine, detail)
			}
		})
	}

	if s.tab == 1 {
		settingsSection(c, i18n.T("ComfyUI 引擎（GPU 加速）"))
		if !s.comfyURLInit {
			s.comfyURL = rs.host.preferences().ComfyUI.URL
			s.comfyURLInit = true
		}
		settingsGroup(c, "comfyui-engine", func() {
			dirNote := rs.host.preferences().ComfyUI.Dir
			if dirNote == "" {
				dirNote = i18n.T("请选择本机 ComfyUI 目录")
			} else {
				dirNote = shortPath(dirNote)
			}
			settingRow(c, "comfy-dir", i18n.T("ComfyUI 目录"), dirNote, func() {
				if rs.host.preferences().ComfyUI.Dir != "" && ui.Button(c.Key("clear-comfy"), i18n.T("清除")).Disabled(s.comfyBusy || rs.resourcesDownloading()).Clicked() {
					rs.setComfyDirectory("")
				}
				if ui.Button(c.Key("pick-comfy"), i18n.T("选择目录")).Tooltip(rs.host.preferences().ComfyUI.Dir).Disabled(s.comfyBusy || rs.resourcesDownloading()).Clicked() {
					rs.chooseComfyDirectory()
				}
			})
			settingsSeparator(c)
			status := i18n.T("选择目录后点击「检测并启动」")
			statusBadge, statusAccent := i18n.T("尚未连接"), false
			var available []string
			if s.comfyBusy {
				status = i18n.T("正在探测引擎与可用节点")
				statusBadge = i18n.T("正在连接")
			} else if s.comfyCaps != nil {
				status = s.comfyCaps.Device
				if status == "" {
					status = "ComfyUI"
				}
				statusBadge, statusAccent = i18n.T("已连接"), true
				caps := s.comfyCaps
				for _, item := range []struct {
					name  string
					ready bool
				}{{"RMBG-3.0", caps.RMBG3Native}, {"Sa2VA", caps.Sa2VANode != ""}, {"SeedVR2", caps.SeedVR2Native}, {"Z-Image", caps.ZImageNative}, {"Qwen 2.1", caps.QwenImageNative}, {"LoRA", caps.LoRANative}} {
					if item.ready {
						available = append(available, item.name)
					}
				}
			}
			settingRow(c, "comfy-status", i18n.T("连接状态"), status, func() {
				resourceStatusBadge(c, statusBadge, statusAccent)
				if rs.comfyManager().Running() && ui.Button(c.Key("stop-comfy"), i18n.T("停止引擎")).Disabled(s.comfyBusy).Clicked() {
					rs.stopComfyUI()
				}
				canProbe := !s.comfyBusy && s.busy == "" && (rs.host.preferences().ComfyUI.Dir != "" || strings.TrimSpace(rs.host.preferences().ComfyUI.URL) != "")
				if ui.Button(c.Key("probe-comfy"), i18n.T("检测并启动")).Disabled(!canProbe).Clicked() {
					rs.probeComfyUI()
				}
			})
			if s.comfyCaps != nil {
				ui.Row(c.Key("comfy-caps")).Padding(0, 12, 10, 12).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					if len(available) == 0 {
						ui.Text(c, i18n.T("未发现可用节点，请更新 ComfyUI")).FontSize(12).TextColor(t.TextMuted)
					} else {
						for _, name := range available {
							settingsBadge(c, name, "")
						}
					}
				})
			}
			settingsSeparator(c)
			settingRow(c, "comfy-url", i18n.T("服务地址（可选）"), i18n.T("连接已运行的服务时填写；留空则按所选目录启动"), func() {
				if ui.TextInput(c.Key("comfy-url-input"), &s.comfyURL).Placeholder("http://127.0.0.1:8188").Width(210).Disabled(s.comfyBusy || rs.resourcesDownloading()).Changed() {
					rs.host.preferences().ComfyUI.URL = strings.TrimSpace(s.comfyURL)
					rs.host.savePrefs()
					s.comfyCaps = nil
					s.comfyErr = nil
				}
			})
		})
	} else if s.tab > 1 {
		if s.comfyCaps != nil {
			dev := s.comfyCaps.Device
			if dev == "" {
				dev = "ComfyUI"
			}
			settingsBanner(c, "comfy-ready-banner", fmt.Sprintf(i18n.T("ComfyUI 已连接：%s"), dev), "accent", nil)
		} else {
			settingsBanner(c, "comfy-need-banner", i18n.T("先在「ComfyUI 模型」页连接引擎"), "", func() {
				if ui.Button(c.Key("goto-comfy"), i18n.T("前往配置")).Clicked() {
					s.tab = 1
				}
			})
		}
	}

	if s.tab == 3 {
		ui.Text(c, i18n.T("LoRA 用于调整生成风格或细节，下载时会补齐缺少的底模")).FontSize(12).TextColor(t.TextMuted)
		ui.Tabs(c.Key("lora-base-tabs"), &s.loraBaseTab, "Z-Image Turbo", "Qwen 2.1 Turbo", i18n.T("Qwen 2.1 普通版")).Label(i18n.T("LoRA 底模")).Disabled(s.busy != "")
	}
	if s.tab > 0 {
		settingsSection(c, []string{"", i18n.T("ComfyUI 模型"), i18n.T("ComfyUI 增强"), "ComfyUI LoRA", i18n.T("音频模型"), i18n.T("视频模型")}[s.tab])
		settingsGroup(c, "comfyui-packs", func() {
			added := false
			for _, pack := range localresource.ComfyPacks {
				if pack.ResourceTab() != s.tab {
					continue
				}
				if s.tab == 3 && pack.BaseModel != loraBaseModels[s.loraBaseTab] {
					continue
				}
				if added {
					settingsSeparator(c)
				}
				added = true
				badge, accent, detail := rs.packDetailAndBadge(pack)
				rs.settingsPackRow(c, pack, badge, accent, detail)
			}
			settingsSeparator(c)
			ui.Text(c, i18n.T("支持断点下载；暂停或停止后保留进度，完成后校验")).FontSize(12).TextColor(t.TextMuted).Padding(8, 12)
		})
	}
	if s.comfyErr != nil {
		settingsBanner(c, "comfy-err-banner", i18n.T("ComfyUI 连接失败：")+s.comfyErr.Error(), "danger", nil)
	}
	if s.err != nil {
		settingsBanner(c, "res-err-banner", i18n.T("资源操作失败：")+s.err.Error(), "danger", nil)
	}
}
