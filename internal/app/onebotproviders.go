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

	"github.com/OUBIGFA/FreeMD/internal/agent"
	model "github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/OUBIGFA/FreeMD/internal/prefs"
)

type aiKeyring struct {
	Version int               `json:"version"`
	Keys    map[string]string `json:"keys"`
}

// Legacy plaintext credentials are deliberately not reused. A malformed new
// keyring is an error, not permission to discard its other connections.
func (a *App) readAIKeys() (aiKeyring, error) {
	out := aiKeyring{Version: 1, Keys: map[string]string{}}
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
	if err = json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	if out.Version != 1 || out.Keys == nil {
		return out, errors.New(i18n.T("OneBot 凭据格式无效"))
	}
	return out, nil
}

func (a *App) qaSettings() agent.Settings {
	c := a.prefs.AI.Current()
	if c == nil {
		return agent.Settings{}
	}
	q := &a.qa
	if !q.keyRead || q.keyID != c.ID {
		q.keyRead = true
		q.keyID = c.ID
		q.key = ""
		keys, err := a.readAIKeys()
		q.keyErr = err
		if err == nil {
			q.key = keys.Keys[c.ID]
		}
	}
	s := agent.Settings{ConnectionID: c.ID, BaseURL: strings.TrimSpace(c.BaseURL), Model: c.Model, APIKey: q.key, Protocol: c.Protocol, Vendor: c.Vendor, Parameters: c.Parameters}
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

func (a *App) saveAIKey() error {
	value := strings.TrimSpace(a.qa.keyInput)
	if value == "" {
		return nil
	}
	c := a.prefs.AI.Current()
	if c == nil {
		return errors.New(i18n.T("请先添加供应商"))
	}
	keys, err := a.readAIKeys()
	if err == nil {
		keys.Keys[c.ID] = value
		var data []byte
		data, err = json.Marshal(keys)
		if err == nil {
			err = a.qa.secrets.Set(apiKeySecret, data)
		}
	}
	if err != nil {
		a.notify("无法保存 API Key：%s", err.Error())
		return fmt.Errorf(i18n.T("无法保存 API Key：%s"), err.Error())
	}
	a.qa.key, a.qa.keyID, a.qa.keyRead, a.qa.keyErr, a.qa.keyInput = value, c.ID, true, nil, ""
	return nil
}

func (a *App) deleteAIKey(id string) error {
	keys, err := a.readAIKeys()
	if err != nil {
		return err
	}
	delete(keys.Keys, id)
	if len(keys.Keys) == 0 {
		return a.qa.secrets.Delete(apiKeySecret)
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return a.qa.secrets.Set(apiKeySecret, data)
}
func (a *App) removeAIKey() {
	c := a.prefs.AI.Current()
	if c == nil {
		return
	}
	if err := a.deleteAIKey(c.ID); err != nil {
		a.notify("无法移除 API Key：%s", err.Error())
		return
	}
	a.qa.key, a.qa.keyRead, a.qa.keyErr = "", true, nil
}

func (a *App) addAIConnection(preset string) {
	p := agent.PresetOf(preset)
	protocol := p.Protocol
	if protocol == "" {
		protocol = agent.Chat
	}
	base, name := p.BaseURL, p.Name
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
	a.qa.modelsFor = ""
	a.qa.modelsErr = nil
	a.qa.catalogErr = nil
	a.savePrefs()
}

func (a *App) removeAIConnection() {
	id := a.prefs.AI.Active
	if id == "" {
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
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s.Protocol+"\x00"+s.BaseURL+"\x00"+s.APIKey)))
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
			if s.Vendor != "" && s.Vendor != "custom" && s.Vendor != "ollama" {
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
			if !seen[m.ID] && (m.Enabled || m.Source == "user") {
				merged = append(merged, m)
			}
		}
		slices.SortFunc(merged, func(a, b agent.ModelInfo) int { return strings.Compare(a.ID, b.ID) })
		c.Models = merged
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
	if c == nil || a.qa.modelsBusy {
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
