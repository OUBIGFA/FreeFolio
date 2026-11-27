package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/video"
)

// The whiteboard's AI nodes run on OneBot's connections: the same
// providers, keys, ChatGPT sign-ins and model choices, never a separate
// configuration. Generated pictures and videos are attachments of the
// board, written with the space's attachment rules.

const boardAISystem = "You are OneBot working inside the user's FreeFolio whiteboard. The user's instruction comes first, then the inputs connected to this node, in order. Answer with the requested content only, without preamble or commentary. Use the language of the instruction unless asked otherwise."

// boardAIModels lists the enabled models able to run a node of the role.
func (a *App) boardAIModels(role whiteboard.AIRole) []editor.BoardAIModel {
	var out []editor.BoardAIModel
	if role == whiteboard.AIModel {
		a.ensureResourcesInspected()
		for _, p := range localresource.ComfyPacks {
			connection := ""
			if p.Kind == "zimage" {
				connection = "comfyui-zimage"
			} else if p.Kind == "qwen-image" {
				connection = "comfyui-qwen-image"
			} else {
				continue
			}
			out = append(out, editor.BoardAIModel{Connection: connection, ConnectionName: "ComfyUI", Model: p.ModelID, Name: p.Name, Local: true, NeedsDownload: !a.resources.installed[p.ID]})
		}
		return out
	}
	if role == whiteboard.AILoRA {
		for _, preset := range a.boardEnhancementPresets() {
			if preset.Model.BaseModel != "" {
				out = append(out, preset.Model)
			}
		}
		return out
	}
	if role == whiteboard.AICutout {
		for _, model := range localresource.Models {
			out = append(out, editor.BoardAIModel{Connection: "local", Model: model.ID, Name: model.Name})
		}
		a.ensureResourcesInspected()
		caps := a.resources.comfyCaps
		for _, pack := range localresource.ComfyPacks {
			installed := a.resources.installed[pack.ID]
			switch pack.Kind {
			case "rmbg3":
				if installed || (caps != nil && caps.RMBG3Native && len(caps.RMBG3Models) > 0) {
					out = append(out, editor.BoardAIModel{Connection: "comfyui-rmbg3", ConnectionName: "ComfyUI", Model: pack.ModelID, Name: pack.Name})
				}
			case "sa2va":
				if installed || (caps != nil && caps.Sa2VANode != "") {
					out = append(out, editor.BoardAIModel{Connection: "comfyui-sa2va", ConnectionName: "ComfyUI", Model: pack.ModelID, Name: pack.Name})
				}
			}
		}
		if caps != nil {
			for _, model := range caps.CutoutModels {
				out = append(out, editor.BoardAIModel{Connection: "comfyui", ConnectionName: "ComfyUI", Model: model, Name: model})
			}
		}
		return out
	}
	if role == whiteboard.AIUpscale {
		for _, model := range localresource.UpscaleModels {
			label := model.Name
			switch model.ID {
			case "realesrgan-x4plus":
				label = model.Name + " · " + i18n.T("通用写实")
			case "realesrgan-x4plus-anime":
				label = model.Name + " · " + i18n.T("动漫插画")
			}
			out = append(out, editor.BoardAIModel{Connection: "local-ncnn", Model: model.ID, Name: label})
		}
		a.ensureResourcesInspected()
		caps := a.resources.comfyCaps
		for _, pack := range localresource.ComfyPacks {
			if pack.Kind == "seedvr2" && (a.resources.installed[pack.ID] || (caps != nil && caps.SeedVR2Native && slices.Contains(caps.SeedVR2Models, path.Base(pack.Files[0].RelPath)))) {
				out = append(out, editor.BoardAIModel{
					Connection:     "comfyui-seedvr2",
					ConnectionName: "ComfyUI",
					Model:          pack.ModelID,
					Name:           pack.Name + " · " + i18n.T("重绘放大"),
				})
			}
		}
		if caps != nil {
			for _, model := range caps.UpscaleModels {
				out = append(out, editor.BoardAIModel{Connection: "comfyui", ConnectionName: "ComfyUI", Model: model, Name: model})
			}
		}
		for _, preset := range a.boardEnhancementPresets() {
			if preset.Role != role {
				continue
			}
			found := false
			for _, m := range out {
				if m.Connection == preset.Model.Connection && m.Model == preset.Model.Model {
					found = true
					break
				}
			}
			if !found {
				out = append(out, preset.Model)
			}
		}
		return out
	}
	if role == whiteboard.AIImage || role == whiteboard.AIEnhance {
		a.ensureResourcesInspected()
		caps := a.resources.comfyCaps
		for _, pack := range localresource.ComfyPacks {
			installed := a.resources.installed[pack.ID]
			switch pack.Kind {
			case "zimage":
				if role == whiteboard.AIImage && (installed || (caps != nil && caps.ZImageNative && len(caps.ZImageModels) > 0)) {
					out = append(out, editor.BoardAIModel{Connection: "comfyui-zimage", ConnectionName: "ComfyUI", Model: pack.ModelID, Name: pack.Name})
				}
			case "qwen-image":
				native := caps != nil && ((pack.ID == "qwen-image-2.1-turbo-int8" && caps.QwenImageNative) || (pack.ID == "qwen-image-2.1-int8" && caps.QwenImageBaseNative))
				if installed || (native && slices.Contains(caps.QwenImageModels, path.Base(pack.Files[0].RelPath))) {
					out = append(out, editor.BoardAIModel{Connection: "comfyui-qwen-image", ConnectionName: "ComfyUI", Model: pack.ModelID, Name: pack.Name})
				}
			}
		}
	}
	for _, c := range a.prefs.AI.Connections {
		for _, m := range c.Models {
			if !m.Enabled {
				continue
			}
			ok := false
			switch role {
			case whiteboard.AIText:
				ok = c.Protocol != agent.Media
			case whiteboard.AIImage:
				ok = agent.SupportsMedia(c.Protocol, c.Vendor, m.Capabilities, agent.MediaImage)
			case whiteboard.AIVideo:
				ok = agent.SupportsMedia(c.Protocol, c.Vendor, m.Capabilities, agent.MediaVideo)
			}
			if ok {
				item := editor.BoardAIModel{Connection: c.ID, ConnectionName: c.Name, Model: m.ID, Name: m.Name}
				// Ollama can also point to a remote host; only a loopback
				// Ollama connection identifies a model on this computer.
				item.Local = c.Protocol == agent.Ollama && agent.IsLocal(c.BaseURL)
				if role == whiteboard.AIImage && c.Protocol == agent.Gemini && !strings.HasPrefix(strings.TrimPrefix(m.ID, "models/"), "imagen") {
					item.ImageSizes = []string{"1K", "2K", "4K"}
				}
				out = append(out, item)
			}
		}
	}
	for _, preset := range a.boardEnhancementPresets() {
		if preset.Role == role {
			out = append(out, preset.Model)
		}
	}
	return out
}

// boardAIDefault is the model the role last used, while it still exists,
// else OneBot's own model for text, else the first that can run it.
func (a *App) boardAIDefault(role whiteboard.AIRole) editor.BoardAIModel {
	models := a.boardAIModels(role)
	if ref, ok := a.prefs.AI.Whiteboard[string(role)]; ok {
		for _, m := range models {
			if m.Connection == ref.Connection && m.Model == ref.Model {
				return m
			}
		}
	}
	if c := a.prefs.AI.Current(); c != nil && role == whiteboard.AIText {
		for _, m := range models {
			if m.Connection == c.ID && m.Model == c.Model {
				return m
			}
		}
	}
	for _, model := range models {
		if !model.NeedsDownload && (!(role == whiteboard.AIImage || role == whiteboard.AIEnhance) || model.BaseModel == "") {
			return model
		}
	}
	return editor.BoardAIModel{}
}

func (a *App) rememberBoardAIModel(role whiteboard.AIRole, m editor.BoardAIModel) {
	if a.prefs.AI.Whiteboard == nil {
		a.prefs.AI.Whiteboard = map[string]prefs.AIModelRef{}
	}
	a.prefs.AI.Whiteboard[string(role)] = prefs.AIModelRef{Connection: m.Connection, Model: m.Model}
	a.savePrefs()
}

// connectionSettings are the settings of a connection and model, as
// qaSettings makes them for OneBot's current one.
func (a *App) connectionSettings(c *prefs.AIConnection, model string, keys map[string][]string) agent.Settings {
	s := agent.Settings{ConnectionID: c.ID, BaseURL: strings.TrimSpace(c.BaseURL), Model: model, KeyStrategy: c.KeyStrategy, Protocol: c.Protocol, Vendor: c.Vendor, Parameters: c.Parameters}
	if c.Protocol == agent.ChatGPT {
		a.ensureChatGPT()
		s.Vendor, s.BaseURL = "chatgpt", "https://api.openai.com/v1"
		if session, ok := a.chatgpt.sessions[c.ID]; ok && (session.Sharing || session.Pending) {
			s.TokenSource = a.chatgpt.client.Source(c.ID)
		}
	} else {
		s.APIKeys = slices.Clone(keys[c.ID])
		if len(s.APIKeys) > 0 {
			s.APIKey = s.APIKeys[0]
		}
	}
	if c.Parameters.Temperature != nil {
		v := *c.Parameters.Temperature
		s.Parameters.Temperature = &v
	}
	for _, m := range c.Models {
		if m.ID == model {
			s.Capabilities = m.Capabilities
		}
	}
	return s
}

// boardAI lends a board document OneBot's connections.
func (a *App) boardAI(d *document) *editor.BoardAI {
	dir := path.Dir(d.rel)
	full := func(rel string) (string, error) {
		return vault.Resolve(d.space.Root, path.Clean(path.Join(dir, unescapeImagePath(rel))))
	}
	read := func(rel string) ([]byte, error) {
		f, err := full(rel)
		if err != nil {
			return nil, err
		}
		return readBoardBytes(f, 256<<20)
	}
	ai := &editor.BoardAI{Models: a.boardAIModels, Presets: a.boardEnhancementPresets, Default: a.boardAIDefault, Remember: a.rememberBoardAIModel, ReadFile: read}
	ai.ReplaceImage = func(nodeID string) { a.pickBoardNodeImage(d, nodeID) }
	ai.OpenImage = func(id string, edit bool) { a.openBoardImage(d, id, edit) }
	ai.SaveImage = func(rel string) {
		a.saveBoardImageAs(a.win, rel, read, func(err error) {
			if err != nil {
				a.notify("另存为失败；%v", err)
			}
		})
	}
	ai.Prepare = func() (*editor.BoardAIRunner, error) { return a.boardAIRunner(d) }
	ai.OpenFile = func(rel string) {
		if f, err := full(rel); err == nil {
			mygo.Shell.OpenPath(f)
		}
	}
	ai.OpenVideo = func(rel string) (*ui.VideoPlayer, error) {
		f, err := full(rel)
		if err != nil {
			return nil, err
		}
		return video.Open(f)
	}
	if a.win != nil {
		ai.ImportCanvas = func(done func([]byte, error)) {
			win := a.win
			go func() {
				files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: i18n.T("导入 JSON Canvas"), Filters: []mygo.FileFilter{{Name: "JSON Canvas", Extensions: []string{"canvas"}}}})
				if err != nil || len(files) == 0 {
					return
				}
				data, err := readBoardBytes(files[0], whiteboard.MaxSceneBytes)
				win.Update(func() { done(data, err) })
			}()
		}
	}
	return ai
}

// boardAIRunner captures the settings and credentials of every connection
// on the main thread; the runner's calls then run off it.
func (a *App) boardAIRunner(d *document) (*editor.BoardAIRunner, error) {
	keyring, keyErr := a.readAIKeys()
	settings := map[string]agent.Settings{}
	for i := range a.prefs.AI.Connections {
		c := &a.prefs.AI.Connections[i]
		for _, m := range c.Models {
			settings[c.ID+"\x00"+m.ID] = a.connectionSettings(c, m.ID, keyring.Keys)
		}
	}
	find := func(n whiteboard.AINode) (agent.Settings, error) {
		if keyErr != nil {
			return agent.Settings{}, keyErr
		}
		s, ok := settings[n.Connection+"\x00"+n.Model]
		if !ok {
			return s, errors.New(i18n.T("节点的模型已不在 OneBot 设置中，请重新选择"))
		}
		return s, nil
	}
	providerFor := a.qa.provider
	sp, rel := d.space, d.rel
	post := func(fn func()) { fn() }
	if a.win != nil {
		post = a.win.Update
	}
	r := &editor.BoardAIRunner{}
	r.Text = func(ctx context.Context, job editor.BoardAIJob, delta func(string)) (string, error) {
		s, err := find(job.Node)
		if err != nil {
			return "", err
		}
		if s.Protocol == agent.Media {
			return "", errors.New(i18n.T("图像/视频供应商不能生成文字，请为文本节点选择对话模型"))
		}
		var provider agent.Provider = s.NewProvider()
		if providerFor != nil {
			provider = providerFor(s)
		}
		msg := agent.Message{Role: agent.User, Text: job.Prompt}
		for _, img := range job.Images {
			msg.Parts = append(msg.Parts, agent.ContentPart{Type: "image", MIME: img.MIME, Data: "data:" + img.MIME + ";base64," + base64.StdEncoding.EncodeToString(img.Data), Name: img.Label})
		}
		ag := &agent.Agent{Provider: provider, System: boardAISystem, Retries: 2}
		added, err := ag.Run(ctx, []agent.Message{msg}, func(e agent.Event) {
			if e.Kind == agent.TextDelta {
				delta(e.Text)
			}
		})
		if err != nil {
			return "", err
		}
		if len(added) == 0 {
			return "", errors.New(i18n.T("服务没有返回结果"))
		}
		return added[len(added)-1].Text, nil
	}
	resourceDir := a.resourceDirectory()
	comfyMgr := a.comfyManager()
	comfyCfg := a.comfyConfig()
	r.Media = func(ctx context.Context, job editor.BoardAIJob, progress func(string)) ([]editor.BoardAIMedia, error) {
		if job.Node.Connection == "comfyui-media" {
			if comfyCfg.Dir == "" && comfyCfg.URL == "" {
				return nil, errors.New(i18n.T("请先在设置的可选资源中配置 ComfyUI 目录或服务地址"))
			}
			pack, ok := localresource.ComfyPack(job.Node.Model)
			if !ok || (job.Node.Role == whiteboard.AIAudio) != strings.HasPrefix(pack.Kind, "audio-") || (job.Node.Role == whiteboard.AIVideo) != strings.HasPrefix(pack.Kind, "video-") {
				return nil, errors.New(i18n.T("模型与节点类型不匹配"))
			}
			p := job.Node.Params
			req := localresource.ComfyMediaRequest{Model: job.Node.Model, Prompt: job.Prompt, Lyrics: p.Lyrics, Speaker: p.Speaker, Language: p.Language, Seconds: p.Seconds, BPM: p.BPM, KeyScale: p.KeyScale, TimeSignature: p.TimeSignature, VoiceStyle: p.VoiceStyle, Size: p.Size, FPS: p.FPS, Options: boardComfyOptions(p)}
			if len(job.Images) > 1 {
				return nil, errors.New(i18n.T("请只连接一张首帧图片"))
			}
			if len(job.Images) == 1 {
				req.Image = job.Images[0].Data
			}
			if progress != nil {
				progress(i18n.T("正在本机生成"))
			}
			result, err := comfyMgr.GenerateLocalMedia(ctx, comfyCfg, req)
			if err != nil {
				return nil, err
			}
			kind, mime, ext := "audio", "audio/flac", ".flac"
			if job.Node.Role == whiteboard.AIVideo {
				kind, mime, ext = "video", "video/mp4", ".mp4"
			}
			if strings.ToLower(path.Ext(result.Filename)) != ext {
				return nil, errors.New(i18n.T("生成文件格式不正确"))
			}
			asset, err := a.writeAssetFor(d, path.Dir(rel), "generated-"+kind+ext, result.Data)
			if err != nil {
				return nil, err
			}
			return []editor.BoardAIMedia{{Output: whiteboard.AIOutput{Kind: kind, Path: asset, MIME: mime, Prompt: job.Prompt}, Data: result.Data}}, nil
		}

		if job.Node.Role == whiteboard.AICutout {
			if len(job.Images) != 1 {
				return nil, errors.New(i18n.T("请连接一张输入图片"))
			}
			var data []byte
			var err error
			switch job.Node.Connection {
			case "comfyui-rmbg3":
				if comfyCfg.Dir == "" && comfyCfg.URL == "" {
					return nil, errors.New(i18n.T("请先在设置的可选资源中配置 ComfyUI 目录或服务地址"))
				}
				data, err = comfyMgr.CutoutRMBG3(ctx, comfyCfg, job.Prompt, job.Images[0].Data, boardComfyOptions(job.Node.Params))
			case "comfyui-sa2va":
				if comfyCfg.Dir == "" && comfyCfg.URL == "" {
					return nil, errors.New(i18n.T("请先在设置的可选资源中配置 ComfyUI 目录或服务地址"))
				}
				data, err = comfyMgr.CutoutSa2VA(ctx, comfyCfg, job.Node.Model, job.Prompt, job.Images[0].Data, boardComfyOptions(job.Node.Params))
			case "comfyui":
				if comfyCfg.Dir == "" && comfyCfg.URL == "" {
					return nil, errors.New(i18n.T("请先在设置的可选资源中配置 ComfyUI 目录或服务地址"))
				}
				data, err = comfyMgr.Cutout(ctx, comfyCfg, job.Node.Model, job.Images[0].Data, boardComfyOptions(job.Node.Params))
			default:
				model, ok := localresource.Model(job.Node.Model)
				engine, supported := localresource.Runtime()
				if !supported {
					return nil, errors.New(i18n.T("此平台暂不支持本地抠图"))
				}
				if !ok || !localresource.Present(resourceDir, model) || !localresource.Present(resourceDir, engine) {
					return nil, errors.New(i18n.T("请在设置的可选资源中下载抠图模型和运行库"))
				}
				data, err = localresource.Cutout(ctx, resourceDir, model.ID, job.Images[0].Data)
			}
			if err != nil {
				return nil, err
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			url, err := a.writeAssetFor(d, path.Dir(rel), "cutout.png", data)
			if err != nil {
				return nil, err
			}
			return []editor.BoardAIMedia{{Output: whiteboard.AIOutput{Kind: "image", Path: url, MIME: "image/png"}, Data: data}}, nil
		}
		if job.Node.Role == whiteboard.AIUpscale {
			if len(job.Images) != 1 {
				return nil, errors.New(i18n.T("请连接一张输入图片"))
			}
			var data []byte
			var err error
			switch job.Node.Connection {
			case "", "local-ncnn":
				model, ok := localresource.UpscaleModel(job.Node.Model)
				engine, supported := localresource.UpscaleRuntime()
				if !supported {
					return nil, errors.New(i18n.T("此平台暂不支持本地图片放大"))
				}
				if !ok || !localresource.Present(resourceDir, engine) {
					return nil, errors.New(i18n.T("请在设置的可选资源中下载图片放大资源"))
				}
				data, err = localresource.Upscale(ctx, resourceDir, model.ID, job.Node.Params.Scale, job.Images[0].Data)
			case "comfyui-seedvr2":
				if comfyCfg.Dir == "" && comfyCfg.URL == "" {
					return nil, errors.New(i18n.T("请先在设置的可选资源中配置 ComfyUI 目录或服务地址"))
				}
				data, err = comfyMgr.UpscaleSeedVR2Model(ctx, comfyCfg, job.Node.Model, job.Node.Params.Scale, job.Images[0].Data, boardComfyOptions(job.Node.Params))
			case "comfyui":
				if comfyCfg.Dir == "" && comfyCfg.URL == "" {
					return nil, errors.New(i18n.T("请先在设置的可选资源中配置 ComfyUI 目录或服务地址"))
				}
				data, err = comfyMgr.Upscale(ctx, comfyCfg, job.Node.Model, job.Node.Params.Scale, job.Images[0].Data)
			default:
				return nil, errors.New(i18n.T("不支持的图片放大提供方"))
			}
			if err != nil {
				return nil, err
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			url, err := a.writeAssetFor(d, path.Dir(rel), "upscale.png", data)
			if err != nil {
				return nil, err
			}
			return []editor.BoardAIMedia{{Output: whiteboard.AIOutput{Kind: "image", Path: url, MIME: "image/png"}, Data: data}}, nil
		}
		if job.Node.ImageHistory() && (job.Node.Connection == "comfyui-zimage" || job.Node.Connection == "comfyui-qwen-image") {
			if job.Node.Model == "qwen-detail" {
				return nil, errors.New(i18n.T("Detail Enhancer 已移除，请选择其他 LoRA"))
			}
			if comfyCfg.Dir == "" && comfyCfg.URL == "" {
				return nil, errors.New(i18n.T("请先在设置的可选资源中配置 ComfyUI 目录或服务地址"))
			}
			p := job.Node.Params
			req := localresource.ComfyImageRequest{
				Prompt:      job.Prompt,
				Size:        p.Size,
				AspectRatio: p.AspectRatio,
				Count:       p.Count,
				Options:     boardComfyOptions(p),
			}
			if pack, ok := localresource.ComfyPackByModel(job.Node.Model); ok && pack.BaseModel != "" {
				req.BaseModel = pack.BaseModel
				req.LoRA, req.Strength = pack.ID, pack.DefaultStrength
				if p.LoRAStrength != nil {
					req.Strength = *p.LoRAStrength
				}
				if strings.TrimSpace(req.Prompt) == "" {
					req.Prompt = pack.Instruction
				}
			} else if !ok || (pack.Kind != "zimage" && pack.Kind != "qwen-image") {
				return nil, errors.New(i18n.T("所选本地模型不可用，请重新选择"))
			} else {
				req.BaseModel = pack.ModelID
			}
			req.Enhance = job.Node.Role == whiteboard.AIEnhance
			for _, img := range job.Images {
				req.Images = append(req.Images, img.Data)
			}
			if job.ModelFlow != nil {
				req.LoRA = ""
				for _, patch := range job.ModelFlow.LoRAs {
					if patch.ID == "qwen-detail" {
						return nil, errors.New(i18n.T("Detail Enhancer 已移除，请选择其他 LoRA"))
					}
					pack, ok := localresource.ComfyPack(patch.ID)
					if !ok || pack.BaseModel != job.Node.Model {
						return nil, errors.New(i18n.T("LoRA 与连接的底模不匹配"))
					}
					strength := pack.DefaultStrength
					if patch.Strength != nil {
						strength = *patch.Strength
					}
					req.LoRAs = append(req.LoRAs, localresource.ComfyLoRA{ID: pack.ID, Strength: strength})
					if strings.TrimSpace(req.Prompt) == "" {
						req.Prompt = pack.Instruction
					}
				}
			}
			var imgs [][]byte
			var err error
			if job.Node.Connection == "comfyui-zimage" {
				imgs, err = comfyMgr.GenerateZImageTurbo(ctx, comfyCfg, req)
			} else {
				imgs, err = comfyMgr.GenerateQwenImage21(ctx, comfyCfg, req)
			}
			if err != nil {
				return nil, err
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			out := make([]editor.BoardAIMedia, 0, len(imgs))
			for i, imgData := range imgs {
				name := fmt.Sprintf("ai-image-%d.png", i+1)
				url, err := a.writeAssetFor(d, path.Dir(rel), name, imgData)
				if err != nil {
					return out, err
				}
				out = append(out, editor.BoardAIMedia{Output: whiteboard.AIOutput{Kind: "image", Path: url, MIME: "image/png"}, Data: imgData})
			}
			return out, nil
		}
		s, err := find(job.Node)
		if err != nil {
			return nil, err
		}
		kind := agent.MediaImage
		if job.Node.Role == whiteboard.AIVideo {
			kind = agent.MediaVideo
		}
		p := job.Node.Params
		req := agent.MediaRequest{Kind: kind, Prompt: job.Prompt, Size: p.Size, AspectRatio: p.AspectRatio, Count: p.Count, Quality: p.Quality, Seconds: p.Seconds, Extra: p.Extra}
		part := func(img editor.BoardAIImage) agent.ContentPart {
			return agent.ContentPart{Type: "image", MIME: img.MIME, Data: "data:" + img.MIME + ";base64," + base64.StdEncoding.EncodeToString(img.Data)}
		}
		for _, img := range job.Images {
			req.Images = append(req.Images, part(img))
		}
		if job.Mask != nil {
			m := part(*job.Mask)
			req.Mask = &m
		}
		res, err := s.GenerateMedia(ctx, req, func(pr agent.MediaProgress) {
			text := pr.Status
			if pr.Percent > 0 {
				text = fmt.Sprintf("%d%%", pr.Percent)
			}
			progress(text)
		})
		if err != nil {
			return nil, err
		}
		var out []editor.BoardAIMedia
		for i, asset := range res.Assets {
			ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif", "video/mp4": ".mp4", "video/webm": ".webm", "video/quicktime": ".mov"}[asset.MIME]
			if ext == "" {
				return out, fmt.Errorf(i18n.T("服务返回了不支持的文件类型：%s"), asset.MIME)
			}
			name := fmt.Sprintf("ai-%s-%d%s", kind, i+1, ext)
			url, err := a.writeAssetFor(d, path.Dir(rel), name, asset.Data)
			if err != nil {
				return out, err
			}
			outKind := "image"
			if strings.HasPrefix(asset.MIME, "video/") {
				outKind = "video"
			}
			out = append(out, editor.BoardAIMedia{Output: whiteboard.AIOutput{Kind: outKind, Path: url, MIME: asset.MIME, Prompt: asset.RevisedPrompt}, Data: asset.Data})
		}
		return out, nil
	}
	r.Note = func(name string) (string, error) {
		type found struct {
			rel string
			ok  bool
		}
		ch := make(chan found, 1)
		post(func() {
			target, file, ok := a.findNamedFrom(sp.ID, name)
			ch <- found{file, ok && target == sp}
		})
		f := <-ch
		if !f.ok {
			return "", fmt.Errorf(i18n.T("找不到笔记 [[%s]]"), name)
		}
		text, err := readFile(sp, f.rel)
		if err != nil {
			return "", err
		}
		if vault.IsWhiteboard(f.rel) {
			s, err := whiteboard.Decode(text)
			if err != nil {
				return "", err
			}
			var lines []string
			for _, l := range s.MarkdownSummary() {
				lines = append(lines, l.Text)
			}
			text = strings.Join(lines, "\n")
		}
		if r := []rune(text); len(r) > 60000 {
			text = string(r[:60000]) + "\n…"
		}
		return text, nil
	}
	return r, nil
}

// chatImageTool lets OneBot's conversation make pictures with the image
// model the whiteboard last used (or the first able one), saved as an
// attachment of the open document d. It is nil without such a model.
func (a *App) chatImageTool(d *document) *agent.Tool {
	if d == nil || d.ed.ReadOnly || d.loadErr != nil {
		return nil
	}
	m := a.boardAIDefault(whiteboard.AIImage)
	c := a.prefs.AI.Find(m.Connection)
	if c == nil || m.Model == "" {
		return nil
	}
	keyring, err := a.readAIKeys()
	if err != nil {
		return nil
	}
	s := a.connectionSettings(c, m.Model, keyring.Keys)
	rel := d.rel
	sizeHelp := "size is WxH or auto."
	if len(m.ImageSizes) > 0 {
		sizeHelp = "size is 1K, 2K or 4K; aspect_ratio sets the shape, for example 16:9. Omit either for the model default."
	}
	return &agent.Tool{
		Spec:    agent.ToolSpec{Name: "generate_image", Description: "Generate a picture with the user's image model (" + m.Model + ") and save it beside the open document " + rel + ". Returns a Markdown image reference relative to that document, which the edit tools can insert. " + sizeHelp, Parameters: schema(`"prompt":{"type":"string"},"size":{"type":"string"},"aspect_ratio":{"type":"string"}`, "prompt")},
		Mutates: true,
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Prompt, Size string
				AspectRatio  string `json:"aspect_ratio"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", err
			}
			res, err := s.GenerateMedia(ctx, agent.MediaRequest{Kind: agent.MediaImage, Prompt: args.Prompt, Size: args.Size, AspectRatio: args.AspectRatio, Count: 1}, nil)
			if err != nil {
				return "", err
			}
			var refs []string
			for i, asset := range res.Assets {
				ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}[asset.MIME]
				if ext == "" {
					return "", fmt.Errorf("unsupported image type %s", asset.MIME)
				}
				url, err := a.writeAssetFor(d, path.Dir(rel), fmt.Sprintf("onebot-image-%d%s", i+1, ext), asset.Data)
				if err != nil {
					return "", err
				}
				refs = append(refs, "![]("+url+")")
			}
			return "saved: " + strings.Join(refs, " "), nil
		},
	}
}

// Keep the saved controls identical across native image and media requests.
func boardComfyOptions(p whiteboard.AIParams) localresource.ComfyOptions {
	return localresource.ComfyOptions{Seed: p.Seed, Steps: p.Steps, Guidance: p.Guidance, NegativePrompt: p.NegativePrompt, ColorCorrection: p.ColorCorrection, MaskThreshold: p.MaskThreshold, Sensitivity: p.Sensitivity, MaskBlur: p.MaskBlur, MaskOffset: p.MaskOffset}
}
