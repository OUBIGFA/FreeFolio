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
	if role == whiteboard.AICutout {
		for _, model := range localresource.Models {
			out = append(out, editor.BoardAIModel{Connection: "local", Model: model.ID, Name: model.Name})
		}
		return out
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
				if role == whiteboard.AIImage && c.Protocol == agent.Gemini && !strings.HasPrefix(strings.TrimPrefix(m.ID, "models/"), "imagen") {
					item.ImageSizes = []string{"1K", "2K", "4K"}
				}
				out = append(out, item)
			}
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
	if len(models) > 0 {
		return models[0]
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
	ai := &editor.BoardAI{Models: a.boardAIModels, Default: a.boardAIDefault, Remember: a.rememberBoardAIModel, ReadFile: read}
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
	r.Media = func(ctx context.Context, job editor.BoardAIJob, progress func(string)) ([]editor.BoardAIMedia, error) {
		if job.Node.Role == whiteboard.AICutout {
			model, ok := localresource.Model(job.Node.Model)
			engine, supported := localresource.Runtime()
			if !supported {
				return nil, errors.New(i18n.T("此平台暂不支持本地抠图"))
			}
			if !ok || !localresource.Present(resourceDir, model) || !localresource.Present(resourceDir, engine) {
				return nil, errors.New(i18n.T("请在设置的可选资源中下载抠图模型和运行库"))
			}
			if len(job.Images) != 1 {
				return nil, errors.New(i18n.T("请连接一张输入图片"))
			}
			data, err := localresource.Cutout(ctx, resourceDir, model.ID, job.Images[0].Data)
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
