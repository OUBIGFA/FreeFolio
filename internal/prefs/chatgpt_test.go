package prefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChatGPTPreferenceMigrationPreservesConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	if err := os.WriteFile(path, []byte(`{"version":11,"noAutosave":true,"ai":{"active":"a","connections":[{"id":"a","name":"Existing","protocol":"openai-responses","baseURL":"https://api.openai.com/v1"}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := (Store{Path: path}).Load()
	if err != nil || p.Version != 12 || !p.NoAutosave || p.AI.Current() == nil || p.AI.ChatGPTWelcomeSeen {
		t.Fatalf("migration: %+v %v", p, err)
	}
}
