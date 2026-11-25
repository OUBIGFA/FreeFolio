package prefs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
)

func TestOneBotConnectionsPersistIndependently(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p := &Prefs{AI: AI{Active: "two", Connections: []AIConnection{
		{ID: "one", Name: "Office", BaseURL: "https://one.example/v1", Protocol: agent.Chat, Model: "a", Models: []agent.ModelInfo{{ID: "a", Enabled: true}, {ID: "b"}}},
		{ID: "two", Name: "Local", BaseURL: "http://localhost:11434", Protocol: agent.Ollama, Model: "local", Models: []agent.ModelInfo{{ID: "local", Enabled: true}}},
	}}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load()
	if err != nil || loaded.AI.Current() == nil || loaded.AI.Current().ID != "two" || len(loaded.AI.Connections[0].Models) != 2 || loaded.AI.Connections[0].Models[1].Enabled {
		t.Fatalf("connections changed: %+v %v", loaded, err)
	}
}

func TestOneBotStartsFreshWithoutLegacyAI(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(s.Path, []byte(`{"version":6,"fontSize":19,"ai":{"provider":"openai","model":"legacy","baseURL":"https://old.example"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.Load()
	if err != nil || p.FontSize != 19 || len(p.AI.Connections) != 0 {
		t.Fatalf("fresh AI: %+v %v", p, err)
	}
}

func TestOneBotRejectsDuplicateConnectionIdentity(t *testing.T) {
	p := &Prefs{AI: AI{Connections: []AIConnection{{ID: "same"}, {ID: "same"}}}}
	if err := (Store{Path: filepath.Join(t.TempDir(), "prefs.json")}).Save(p); err == nil {
		t.Fatal("duplicate credential identities accepted")
	}
}

func TestOneBotWidthMigratesAndPersists(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(s.Path, []byte(`{"version":7,"fontSize":19,"ai":{"active":"a","connections":[{"id":"a","name":"Local","protocol":"ollama","baseURL":"http://localhost:11434"}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.Load()
	if err != nil || p.Version != CurrentVersion || p.OneBotWidth != 0 || p.FontSize != 19 || p.AI.Current() == nil {
		t.Fatalf("migration %+v %v", p, err)
	}
	p.OneBotWidth = 560
	if err = s.Save(p); err != nil {
		t.Fatal(err)
	}
	q, err := s.Load()
	if err != nil || q.OneBotWidth != 560 || q.AI.Current().ID != "a" {
		t.Fatalf("round trip %+v %v", q, err)
	}
}

func TestWhiteboardAIModelsMigrateAndPersist(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(s.Path, []byte(`{"version":14,"fontSize":18}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.Load()
	if err != nil || p.Version != CurrentVersion || p.FontSize != 18 || p.AI.Whiteboard != nil {
		t.Fatalf("%+v %v", p, err)
	}
	p.AI.Whiteboard = map[string]AIModelRef{"image": {Connection: "c", Model: "gpt-image-1"}}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	p, err = s.Load()
	if err != nil || p.AI.Whiteboard["image"].Model != "gpt-image-1" {
		t.Fatalf("%+v %v", p.AI, err)
	}
}
