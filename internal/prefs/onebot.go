package prefs

import (
	"fmt"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
)

// AI starts a new OneBot configuration; legacy single-provider fields are not
// decoded. Credentials remain in the dedicated secret store, keyed by ID.
type AI struct {
	Active      string         `json:"active,omitempty"`
	Connections []AIConnection `json:"connections,omitempty"`
	Direct      bool           `json:"direct,omitempty"`
	Candidates  int            `json:"candidates,omitempty"`
	// ChatGPTWelcomeSeen records the first-use plan disclosure, never identity.
	ChatGPTWelcomeSeen bool `json:"chatGPTWelcomeSeen,omitempty"`
	// Whiteboard holds the connection and model each kind of whiteboard AI
	// node last used, which new nodes of the kind start with.
	Whiteboard map[string]AIModelRef `json:"whiteboard,omitempty"`
}

// AIModelRef names a model of a connection.
type AIModelRef struct {
	Connection string `json:"connection"`
	Model      string `json:"model"`
}

type AIConnection struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Vendor     string            `json:"vendor,omitempty"`
	Protocol   string            `json:"protocol"`
	BaseURL    string            `json:"baseURL"`
	Model      string            `json:"model,omitempty"`
	Models     []agent.ModelInfo `json:"models,omitempty"`
	Parameters agent.Parameters  `json:"parameters,omitempty"`
	// KeyStrategy picks among several API keys: agent.KeyRoundRobin,
	// agent.KeyRandom or agent.KeyFailover. The keys are credentials.
	KeyStrategy string `json:"keyStrategy,omitempty"`
}

func (a *AI) Current() *AIConnection { return a.Find(a.Active) }
func (a *AI) Find(id string) *AIConnection {
	for i := range a.Connections {
		if a.Connections[i].ID == id {
			return &a.Connections[i]
		}
	}
	return nil
}
func (c *AIConnection) CurrentModel() *agent.ModelInfo {
	for i := range c.Models {
		if c.Models[i].ID == c.Model {
			return &c.Models[i]
		}
	}
	return nil
}

// Validate prevents two connections from accidentally sharing credential
// identities, and rejects corrupt configuration rather than discarding it.
func (a *AI) Validate() error {
	ids := map[string]bool{}
	for _, c := range a.Connections {
		if strings.TrimSpace(c.ID) == "" || ids[c.ID] {
			return fmt.Errorf("OneBot: invalid or duplicate connection ID %q", c.ID)
		}
		ids[c.ID] = true
		models := map[string]bool{}
		for _, m := range c.Models {
			if strings.TrimSpace(m.ID) == "" || models[m.ID] {
				return fmt.Errorf("OneBot: invalid or duplicate model ID %q", m.ID)
			}
			models[m.ID] = true
		}
	}
	if a.Active != "" && !ids[a.Active] {
		return fmt.Errorf("OneBot: selected connection does not exist")
	}
	return nil
}
