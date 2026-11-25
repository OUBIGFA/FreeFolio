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
	if err != nil || p.Version != CurrentVersion || !p.NoAutosave || p.AI.Current() == nil || p.AI.ChatGPTWelcomeSeen {
		t.Fatalf("migration: %+v %v", p, err)
	}
}

func TestWideTablesPreferenceMigratesOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	if err := os.WriteFile(path, []byte(`{"version":12,"codeNoWrap":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := (Store{Path: path}).Load()
	if err != nil || p.Version != CurrentVersion || p.NoWideTables || !p.CodeNoWrap {
		t.Fatalf("migration: %+v %v", p, err)
	}
}

func TestRememberPlacePreferenceMigratesOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	if err := os.WriteFile(path, []byte(`{"version":13,"files":{"readOnly":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := (Store{Path: path}).Load()
	if err != nil || p.Version != CurrentVersion || p.Files.NoRememberPlace || !p.Files.ReadOnly {
		t.Fatalf("migration: %+v %v", p, err)
	}
}
