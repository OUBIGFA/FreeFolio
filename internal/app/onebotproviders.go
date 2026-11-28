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
func (ob *qaState) readAIKeys() (aiKeyring, error) {
	out := aiKeyring{Version: 2, Keys: map[string][]string{}}
	if ob.secrets == nil {
		return out, errors.New(i18n.T("本地凭据存储不可用"))
	}
	data, err := ob.secrets.Get(apiKeySecret)
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

func (ob *qaState) writeAIKeys(keys aiKeyring) error {
	if len(keys.Keys) == 0 {
		return ob.secrets.Delete(apiKeySecret)
	}
	keys.Version = 2
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return ob.secrets.Set(apiKeySecret, data)
}

func (ob *qaState) qaSettings() agent.Settings {
	c := ob.host.preferences().AI.Current()
	if c == nil {
		return agent.Settings{}
	}
	if c.Protocol == agent.ChatGPT {
		return ob.host.chatGPT().chatGPTSettings()
	}
	q := ob
	if !q.keyRead || q.keyID != c.ID {
		q.keyRead = true
		q.keyID = c.ID
		q.keys = nil
		keys, err := ob.readAIKeys()
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
func (ob *qaState) saveAIKey() error {
	added := splitAPIKeys(ob.keyInput)
	if len(added) == 0 {
		return nil
	}
	c := ob.host.preferences().AI.Current()
	if c == nil {
		return errors.New(i18n.T("请先添加供应商"))
	}
	keys, err := ob.readAIKeys()
	var list []string
	if err == nil {
		list = keys.Keys[c.ID]
		for _, key := range added {
			if !slices.Contains(list, key) {
				list = append(list, key)
			}
		}
		keys.Keys[c.ID] = list
		err = ob.writeAIKeys(keys)
	}
	if err != nil {
		ob.host.notify("无法保存 API Key：%s", err.Error())
		return fmt.Errorf(i18n.T("无法保存 API Key：%s"), err.Error())
	}
	ob.keys, ob.keyID, ob.keyRead, ob.keyErr, ob.keyInput = list, c.ID, true, nil, ""
	return nil
}

func (ob *qaState) deleteAIKey(id string) error {
	keys, err := ob.readAIKeys()
	if err != nil {
		return err
	}
	delete(keys.Keys, id)
	return ob.writeAIKeys(keys)
}

// removeAIKey removes one key of the current connection.
func (ob *qaState) removeAIKey(key string) {
	c := ob.host.preferences().AI.Current()
	if c == nil {
		return
	}
	keys, err := ob.readAIKeys()
	if err == nil {
		list := slices.DeleteFunc(slices.Clone(keys.Keys[c.ID]), func(k string) bool { return k == key })
		if len(list) == 0 {
			delete(keys.Keys, c.ID)
		} else {
			keys.Keys[c.ID] = list
		}
		err = ob.writeAIKeys(keys)
	}
	if err != nil {
		ob.host.notify("无法移除 API Key：%s", err.Error())
		return
	}
	ob.keyRead = false
	delete(ob.keyShown, key)
	ob.qaSettings()
}

func (ob *qaState) addAIConnection(preset string) {
	p := agent.PresetOf(preset)
	protocol := p.Protocol
	if protocol == "" {
		protocol = agent.Chat
	}
	base, name := p.BaseURL, p.Name
	if p.Protocol == agent.ChatGPT {
		for suffix := 2; slices.ContainsFunc(ob.host.preferences().AI.Connections, func(c prefs.AIConnection) bool { return c.Name == name }); suffix++ {
			name = fmt.Sprintf("%s %d", p.Name, suffix)
		}
	}
	c := prefs.AIConnection{ID: model.NewID(), Name: name, Vendor: preset, Protocol: protocol, BaseURL: base}
	ob.host.preferences().AI.Connections = append(ob.host.preferences().AI.Connections, c)
	ob.selectAIConnection(c.ID)
}

func (ob *qaState) selectAIConnection(id string) {
	if ob.host.preferences().AI.Find(id) == nil {
		return
	}
	ob.cancelModelFetch()
	ob.host.preferences().AI.Active = id
	ob.keyRead = false
	ob.keyInput = ""
	ob.manualName = ""
	ob.modelSearch = ""
	ob.capabilityModel = ""
	ob.clearModelsFor = ""
	ob.modelsFor = ""
	ob.modelsErr = nil
	ob.catalogErr = nil
	ob.host.savePrefs()
	loaded := ob.host.chatGPT().loaded[id]
	ob.host.chatGPT().loadChatGPTSession(id, false)
	if loaded && ob.host.preferences().AI.Current().Protocol == agent.ChatGPT && ob.host.chatGPT().chatGPTSession().Sharing {
		ob.fetchModels()
	}
}

func (ob *qaState) removeAIConnection() {
	id := ob.host.preferences().AI.Active
	if id == "" {
		return
	}
	if c := ob.host.preferences().AI.Current(); c != nil && c.Protocol == agent.ChatGPT {
		ob.host.chatGPT().disconnectChatGPT(true)
		return
	}
	if err := ob.deleteAIKey(id); err != nil {
		ob.host.notify("无法移除 API Key：%s", err.Error())
		return
	}
	ob.cancelModelFetch()
	ob.host.preferences().AI.Connections = slices.DeleteFunc(ob.host.preferences().AI.Connections, func(c prefs.AIConnection) bool { return c.ID == id })
	ob.host.preferences().AI.Active = ""
	ob.keyRead = false
	ob.keyInput = ""
	if len(ob.host.preferences().AI.Connections) > 0 {
		ob.selectAIConnection(ob.host.preferences().AI.Connections[0].ID)
	} else {
		ob.host.savePrefs()
	}
}

func modelsKey(s agent.Settings) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s.ConnectionID+"\x00"+s.Protocol+"\x00"+s.BaseURL+"\x00"+s.APIKey)))
}
func (ob *qaState) modelListSettings() agent.Settings {
	s := ob.qaSettings()
	if key := strings.TrimSpace(ob.keyInput); key != "" {
		s.APIKey = key
	}
	return s
}
func (ob *qaState) cancelModelFetch() {
	if ob.modelsCancel != nil {
		ob.modelsCancel()
		ob.modelsCancel = nil
	}
	ob.modelsEpoch++
	ob.modelsBusy = false
}
func (ob *qaState) dropStaleModels() {
	if ob.modelsFor != "" && ob.modelsFor != modelsKey(ob.qaSettings()) {
		ob.cancelModelFetch()
		ob.modelsFor = ""
		ob.modelsErr = nil
	}
}

func (ob *qaState) fetchModels() {
	q := ob
	if q.modelsBusy {
		return
	}
	c := ob.host.preferences().AI.Current()
	if c == nil {
		return
	}
	if strings.TrimSpace(q.keyInput) != "" {
		if err := ob.saveAIKey(); err != nil {
			q.modelsErr = err
			return
		}
	}
	s := ob.qaSettings()
	if err := s.CheckService(); err != nil {
		q.modelsErr = err
		return
	}
	ob.cancelModelFetch()
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
		c := ob.host.preferences().AI.Find(id)
		if c == nil || ob.host.preferences().AI.Active != id || modelsKey(ob.qaSettings()) != key {
			q.modelsFor = ""
			return
		}
		q.modelsErr = err
		q.catalogErr = catalogErr
		if s.Protocol == agent.ChatGPT {
			ob.host.chatGPT().loadChatGPTSession(id, true)
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
		ob.host.savePrefs()
	}
	if q.sync || ob.host.window() == nil {
		done(load(s))
		return
	}
	go func() { models, err := load(s); ob.host.window().Update(func() { done(models, err) }) }()
}

func (ob *qaState) fetchModelCapabilities() {
	c := ob.host.preferences().AI.Current()
	if c == nil || c.Protocol == agent.ChatGPT || ob.modelsBusy {
		return
	}
	s := ob.qaSettings()
	id := c.ID
	ob.cancelModelFetch()
	epoch := ob.modelsEpoch
	ob.modelsBusy = true
	ob.catalogErr = nil
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	ob.modelsCancel = cancel
	done := func(models []agent.ModelInfo, err error) {
		defer cancel()
		if ob.modelsEpoch != epoch {
			return
		}
		ob.modelsBusy = false
		ob.modelsCancel = nil
		ob.catalogErr = err
		c := ob.host.preferences().AI.Find(id)
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
		ob.host.savePrefs()
	}
	if ob.sync || ob.host.window() == nil {
		done(agent.FetchCatalog(ctx, s.Vendor))
		return
	}
	go func() {
		models, err := agent.FetchCatalog(ctx, s.Vendor)
		ob.host.window().Update(func() { done(models, err) })
	}()
}

func (ob *qaState) addManualAIModel() {
	c := ob.host.preferences().AI.Current()
	id := strings.TrimSpace(ob.manualName)
	if c == nil || id == "" {
		return
	}
	for i := range c.Models {
		if c.Models[i].ID == id {
			c.Models[i].Enabled = true
			c.Model = id
			ob.manualName = ""
			ob.host.savePrefs()
			return
		}
	}
	c.Models = append(c.Models, agent.ModelInfo{ID: id, Name: id, Enabled: true, Source: "user"})
	c.Model = id
	ob.manualName = ""
	ob.host.savePrefs()
}

func (ob *qaState) clearProviderModels() {
	conn := ob.host.preferences().AI.Current()
	if conn == nil || ob.clearModelsFor != conn.ID {
		return
	}
	ob.cancelModelFetch()
	conn.Models, conn.Model = nil, ""
	ob.models, ob.modelsErr, ob.catalogErr = nil, nil, nil
	ob.modelsFor, ob.modelSearch, ob.capabilityModel, ob.clearModelsFor = "", "", "", ""
	for role, model := range ob.host.preferences().AI.Whiteboard {
		if model.Connection == conn.ID {
			delete(ob.host.preferences().AI.Whiteboard, role)
		}
	}
	ob.host.savePrefs()
}
