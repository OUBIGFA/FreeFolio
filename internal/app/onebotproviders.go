package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
)

// aiKeyring is credentials/ai-api-key: each connection's API keys by its ID.
// Version 1 held one key per connection; it reads as a list of one.
type aiKeyring struct {
	Version int                 `json:"version"`
	Keys    map[string][]string `json:"keys"`
}

// Legacy plaintext credentials are deliberately not reused. A malformed new
// keyring is an error, not permission to discard its other connections.
func (a *App) readAIKeys() (aiKeyring, error) {
	out := aiKeyring{Version: 2, Keys: map[string][]string{}}
	if a.qa.secrets == nil {
		return out, errors.New(i18n.T("本地凭据存储不可用"))
	}
	data, err := a.qa.secrets.Get(apiKeySecret)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		return out, nil
	}
	var raw struct {
		Version int             `json:"version"`
		Keys    json.RawMessage `json:"keys"`
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		return out, err
	}
	invalid := errors.New(i18n.T("OneBot 凭据格式无效"))
	switch raw.Version {
	case 1:
		var single map[string]string
		if json.Unmarshal(raw.Keys, &single) != nil || single == nil {
			return out, invalid
		}
		for id, key := range single {
			out.Keys[id] = []string{key}
		}
	case 2:
		if json.Unmarshal(raw.Keys, &out.Keys) != nil || out.Keys == nil {
			return out, invalid
		}
	default:
		return out, invalid
	}
	return out, nil
}

func (a *App) writeAIKeys(keys aiKeyring) error {
	if len(keys.Keys) == 0 {
		return a.qa.secrets.Delete(apiKeySecret)
	}
	keys.Version = 2
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return a.qa.secrets.Set(apiKeySecret, data)
}

func (a *App) qaSettings() agent.Settings {
	c := a.prefs.AI.Current()
	if c == nil {
		return agent.Settings{}
	}
	if c.Protocol == agent.ChatGPT {
		return a.chatGPTSettings()
	}
	q := &a.qa
	if !q.keyRead || q.keyID != c.ID {
		q.keyRead = true
		q.keyID = c.ID
		q.keys = nil
		keys, err := a.readAIKeys()
		q.keyErr = err
		if err == nil {
			q.keys = keys.Keys[c.ID]
		}
	}
	s := agent.Settings{ConnectionID: c.ID, BaseURL: strings.TrimSpace(c.BaseURL), Model: c.Model, APIKeys: slices.Clone(q.keys), KeyStrategy: c.KeyStrategy, Protocol: c.Protocol, Vendor: c.Vendor, Parameters: c.Parameters}
	if len(q.keys) > 0 {
		s.APIKey = q.keys[0]
	}
	if c.Parameters.Temperature != nil {
		value := *c.Parameters.Temperature
		s.Parameters.Temperature = &value
	}
	if m := c.CurrentModel(); m != nil {
		s.Capabilities = m.Capabilities
		if !m.Enabled {
			s.Model = ""
		}
	} else {
		s.Model = ""
	}
	return s
}

// splitAPIKeys reads the keys pasted into the field: one or several,
// separated by commas, spaces or lines, as gateways list them.
func splitAPIKeys(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '，' || r == ';' || unicode.IsSpace(r) })
}

// saveAIKey adds the field's keys to the connection's, skipping any it
// already has.
func (a *App) saveAIKey() error {
	added := splitAPIKeys(a.qa.keyInput)
	if len(added) == 0 {
		return nil
	}
	c := a.prefs.AI.Current()
	if c == nil {
		return errors.New(i18n.T("请先添加供应商"))
	}
	keys, err := a.readAIKeys()
	var list []string
	if err == nil {
		list = keys.Keys[c.ID]
		for _, key := range added {
			if !slices.Contains(list, key) {
				list = append(list, key)
			}
		}
		keys.Keys[c.ID] = list
		err = a.writeAIKeys(keys)
	}
	if err != nil {
		a.notify("无法保存 API Key：%s", err.Error())
		return fmt.Errorf(i18n.T("无法保存 API Key：%s"), err.Error())
	}
	a.qa.keys, a.qa.keyID, a.qa.keyRead, a.qa.keyErr, a.qa.keyInput = list, c.ID, true, nil, ""
	return nil
}

func (a *App) deleteAIKey(id string) error {
	keys, err := a.readAIKeys()
	if err != nil {
		return err
	}
	delete(keys.Keys, id)
	return a.writeAIKeys(keys)
}

// removeAIKey removes one key of the current connection.
func (a *App) removeAIKey(key string) {
	c := a.prefs.AI.Current()
	if c == nil {
		return
	}
	keys, err := a.readAIKeys()
	if err == nil {
		list := slices.DeleteFunc(slices.Clone(keys.Keys[c.ID]), func(k string) bool { return k == key })
		if len(list) == 0 {
			delete(keys.Keys, c.ID)
		} else {
			keys.Keys[c.ID] = list
		}
		err = a.writeAIKeys(keys)
	}
	if err != nil {
		a.notify("无法移除 API Key：%s", err.Error())
		return
	}
	a.qa.keyRead = false
	delete(a.qa.keyShown, key)
	a.qaSettings()
}

func (a *App) addAIConnection(preset string) {
	p := agent.PresetOf(preset)
	protocol := p.Protocol
	if protocol == "" {
		protocol = agent.Chat
	}
	base, name := p.BaseURL, p.Name
	if p.Protocol == agent.ChatGPT {
		for suffix := 2; slices.ContainsFunc(a.prefs.AI.Connections, func(c prefs.AIConnection) bool { return c.Name == name }); suffix++ {
			name = fmt.Sprintf("%s %d", p.Name, suffix)
		}
	}
	c := prefs.AIConnection{ID: model.NewID(), Name: name, Vendor: preset, Protocol: protocol, BaseURL: base}
	a.prefs.AI.Connections = append(a.prefs.AI.Connections, c)
	a.selectAIConnection(c.ID)
}

func (a *App) selectAIConnection(id string) {
	if a.prefs.AI.Find(id) == nil {
		return
	}
	a.cancelModelFetch()
	a.prefs.AI.Active = id
	a.qa.keyRead = false
	a.qa.keyInput = ""
	a.qa.manualName = ""
	a.qa.modelSearch = ""
	a.qa.capabilityModel = ""
	a.qa.clearModelsFor = ""
	a.qa.modelsFor = ""
	a.qa.modelsErr = nil
	a.qa.catalogErr = nil
	a.savePrefs()
	loaded := a.chatgpt.loaded[id]
	a.loadChatGPTSession(id, false)
	if loaded && a.prefs.AI.Current().Protocol == agent.ChatGPT && a.chatGPTSession().Sharing {
		a.fetchModels()
	}
}

func (a *App) removeAIConnection() {
	id := a.prefs.AI.Active
	if id == "" {
		return
	}
	if c := a.prefs.AI.Current(); c != nil && c.Protocol == agent.ChatGPT {
		a.disconnectChatGPT(true)
		return
	}
	if err := a.deleteAIKey(id); err != nil {
		a.notify("无法移除 API Key：%s", err.Error())
		return
	}
	a.cancelModelFetch()
	a.prefs.AI.Connections = slices.DeleteFunc(a.prefs.AI.Connections, func(c prefs.AIConnection) bool { return c.ID == id })
	a.prefs.AI.Active = ""
	a.qa.keyRead = false
	a.qa.keyInput = ""
	if len(a.prefs.AI.Connections) > 0 {
		a.selectAIConnection(a.prefs.AI.Connections[0].ID)
	} else {
		a.savePrefs()
	}
}

func modelsKey(s agent.Settings) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s.ConnectionID+"\x00"+s.Protocol+"\x00"+s.BaseURL+"\x00"+s.APIKey)))
}
func (a *App) modelListSettings() agent.Settings {
	s := a.qaSettings()
	if key := strings.TrimSpace(a.qa.keyInput); key != "" {
		s.APIKey = key
	}
	return s
}
func (a *App) cancelModelFetch() {
	if a.qa.modelsCancel != nil {
		a.qa.modelsCancel()
		a.qa.modelsCancel = nil
	}
	a.qa.modelsEpoch++
	a.qa.modelsBusy = false
}
func (a *App) dropStaleModels() {
	if a.qa.modelsFor != "" && a.qa.modelsFor != modelsKey(a.qaSettings()) {
		a.cancelModelFetch()
		a.qa.modelsFor = ""
		a.qa.modelsErr = nil
	}
}

func (a *App) fetchModels() {
	q := &a.qa
	if q.modelsBusy {
		return
	}
	c := a.prefs.AI.Current()
	if c == nil {
		return
	}
	if strings.TrimSpace(q.keyInput) != "" {
		if err := a.saveAIKey(); err != nil {
			q.modelsErr = err
			return
		}
	}
	s := a.qaSettings()
	if err := s.CheckService(); err != nil {
		q.modelsErr = err
		return
	}
	a.cancelModelFetch()
	epoch := q.modelsEpoch
	id, key := c.ID, modelsKey(s)
	q.modelsFor = key
	q.modelsBusy = true
	q.modelsErr = nil
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	q.modelsCancel = cancel
	load := q.listModels
	var catalogErr error
	if load == nil {
		load = func(s agent.Settings) ([]agent.ModelInfo, error) {
			models, err := s.DiscoverModels(ctx)
			if err != nil {
				return nil, err
			}
			if s.Protocol != agent.ChatGPT && s.Vendor != "" && s.Vendor != "custom" && s.Vendor != "ollama" {
				var catalog []agent.ModelInfo
				catalog, catalogErr = agent.FetchCatalog(ctx, s.Vendor)
				if catalogErr == nil {
					byID := map[string]agent.ModelInfo{}
					for _, m := range catalog {
						byID[m.ID] = m
					}
					for i, m := range models {
						if fallback, ok := byID[m.ID]; ok {
							models[i] = agent.MergeModelInfo(m, fallback)
						}
					}
				}
			}
			return models, nil
		}
	}
	done := func(models []agent.ModelInfo, err error) {
		defer cancel()
		if q.modelsEpoch != epoch {
			return
		}
		q.modelsBusy = false
		q.modelsCancel = nil
		c := a.prefs.AI.Find(id)
		if c == nil || a.prefs.AI.Active != id || modelsKey(a.qaSettings()) != key {
			q.modelsFor = ""
			return
		}
		q.modelsErr = err
		q.catalogErr = catalogErr
		if s.Protocol == agent.ChatGPT {
			a.loadChatGPTSession(id, true)
		}
		if err != nil {
			return
		}
		old := map[string]agent.ModelInfo{}
		for _, m := range c.Models {
			old[m.ID] = m
		}
		var merged []agent.ModelInfo
		seen := map[string]bool{}
		for _, m := range models {
			if m.ID == "" || seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			if prior, ok := old[m.ID]; ok {
				if prior.UserOverride {
					m = prior
				} else {
					m = agent.MergeModelInfo(m, prior)
					m.Enabled = prior.Enabled
				}
			}
			merged = append(merged, m)
		}
		for _, m := range c.Models {
			if s.Protocol != agent.ChatGPT && !seen[m.ID] && (m.Enabled || m.Source == "user") {
				merged = append(merged, m)
			}
		}
		if s.Protocol != agent.ChatGPT {
			slices.SortFunc(merged, func(a, b agent.ModelInfo) int { return strings.Compare(a.ID, b.ID) })
		}
		c.Models = merged
		if s.Protocol == agent.ChatGPT && !seen[c.Model] {
			c.Model = ""
		}
		q.models = models
		a.savePrefs()
	}
	if q.sync || a.win == nil {
		done(load(s))
		return
	}
	go func() { models, err := load(s); a.win.Update(func() { done(models, err) }) }()
}

func (a *App) fetchModelCapabilities() {
	c := a.prefs.AI.Current()
	if c == nil || c.Protocol == agent.ChatGPT || a.qa.modelsBusy {
		return
	}
	s := a.qaSettings()
	id := c.ID
	a.cancelModelFetch()
	epoch := a.qa.modelsEpoch
	a.qa.modelsBusy = true
	a.qa.catalogErr = nil
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	a.qa.modelsCancel = cancel
	done := func(models []agent.ModelInfo, err error) {
		defer cancel()
		if a.qa.modelsEpoch != epoch {
			return
		}
		a.qa.modelsBusy = false
		a.qa.modelsCancel = nil
		a.qa.catalogErr = err
		c := a.prefs.AI.Find(id)
		if err != nil || c == nil {
			return
		}
		byID := map[string]agent.ModelInfo{}
		for _, m := range models {
			byID[m.ID] = m
		}
		for i, m := range c.Models {
			if fallback, ok := byID[m.ID]; ok {
				c.Models[i] = agent.MergeModelInfo(m, fallback)
			}
		}
		a.savePrefs()
	}
	if a.qa.sync || a.win == nil {
		done(agent.FetchCatalog(ctx, s.Vendor))
		return
	}
	go func() { models, err := agent.FetchCatalog(ctx, s.Vendor); a.win.Update(func() { done(models, err) }) }()
}

func (a *App) addManualAIModel() {
	c := a.prefs.AI.Current()
	id := strings.TrimSpace(a.qa.manualName)
	if c == nil || id == "" {
		return
	}
	for i := range c.Models {
		if c.Models[i].ID == id {
			c.Models[i].Enabled = true
			c.Model = id
			a.qa.manualName = ""
			a.savePrefs()
			return
		}
	}
	c.Models = append(c.Models, agent.ModelInfo{ID: id, Name: id, Enabled: true, Source: "user"})
	c.Model = id
	a.qa.manualName = ""
	a.savePrefs()
}

func (a *App) clearProviderModels() {
	conn := a.prefs.AI.Current()
	if conn == nil || a.qa.clearModelsFor != conn.ID {
		return
	}
	a.cancelModelFetch()
	conn.Models, conn.Model = nil, ""
	a.qa.models, a.qa.modelsErr, a.qa.catalogErr = nil, nil, nil
	a.qa.modelsFor, a.qa.modelSearch, a.qa.capabilityModel, a.qa.clearModelsFor = "", "", "", ""
	for role, model := range a.prefs.AI.Whiteboard {
		if model.Connection == conn.ID {
			delete(a.prefs.AI.Whiteboard, role)
		}
	}
	a.savePrefs()
}
