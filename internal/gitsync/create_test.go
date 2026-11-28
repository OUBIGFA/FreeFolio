package gitsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRepositoryAndUploadOnlyReviewedFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	put(t, root, "note.md", "reviewed\n")
	put(t, root, "private.md", "not selected\n")
	put(t, root, ".gitignore", "ignored.txt\n")
	put(t, root, "ignored.txt", "ignored\n")
	g := &Git{Path: "git", StateDir: t.TempDir(), AllowLocal: true, Env: []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}}
	ctx := context.Background()
	repo, err := g.PrepareRepository(ctx, root)
	if err != nil || repo.Head != "" || repo.Branch != "main" || len(repo.Remotes) != 0 {
		t.Fatalf("prepare: %+v %v", repo, err)
	}
	for _, c := range repo.Changes {
		if c.Path == "ignored.txt" || c.Staged {
			t.Fatal("preparation staged a file or included ignored content")
		}
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	fixtureGit(t, root, "init", "--bare", remote)
	b, err := g.BindNewRepository(ctx, root, remote, repo.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	g.Env = append(g.Env, "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test")
	if err := g.Commit(ctx, root, b, []string{"note.md"}, "Initial notes", repo.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(ctx, root, b); err != nil {
		t.Fatal(err)
	}
	if got := fixtureGit(t, remote, "ls-tree", "-r", "--name-only", "main"); got != "note.md" {
		t.Fatalf("uploaded unselected files: %s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "private.md")); err != nil {
		t.Fatal("unselected local file lost", err)
	}
	if _, err := g.PrepareRepository(ctx, root); err == nil {
		t.Fatal("existing history accepted as new repository")
	}
}

func TestNewRepositoryRejectsChangedPreviewAndExistingRemote(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	put(t, root, "note.md", "before")
	g := &Git{Path: "git", StateDir: t.TempDir(), Env: []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull}}
	ctx := context.Background()
	repo, err := g.PrepareRepository(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "note.md", "after")
	if _, err := g.BindNewRepository(ctx, root, "https://github.com/owner/notes.git", repo.Fingerprint); err != ErrChanged {
		t.Fatalf("changed preview accepted: %v", err)
	}
	fixtureGit(t, root, "remote", "add", "origin", "https://github.com/owner/existing.git")
	if _, err := g.PrepareRepository(ctx, root); err == nil {
		t.Fatal("existing remote accepted for creation")
	}
}

func TestPrepareRepositoryRejectsParentRepository(t *testing.T) {
	root := t.TempDir()
	fixtureGit(t, root, "init", "-b", "main")
	child := filepath.Join(root, "space")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	g := &Git{Path: "git", StateDir: t.TempDir()}
	if _, err := g.PrepareRepository(context.Background(), child); err == nil {
		t.Fatal("nested repository created inside existing history")
	}
	if _, err := os.Stat(filepath.Join(child, ".git")); !os.IsNotExist(err) {
		t.Fatal("nested Git metadata created")
	}
}

func TestCreatePrivateRepositoryUsesSelectedAccountAndValidatesPrivacy(t *testing.T) {
	dir := t.TempDir()
	manager := fixtureManager(t, filepath.Join(dir, "manager"))
	client := NewGitHub(filepath.Join(dir, "credentials"))
	client.ensureManager = func(context.Context, func(RuntimeProgress)) (*credentialManager, error) { return manager, nil }
	publicResponse, creates := false, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "octo"})
			return
		}
		creates++
		var body struct {
			Name     string `json:"name"`
			Private  bool   `json:"private"`
			AutoInit bool   `json:"auto_init"`
		}
		if r.URL.Path != "/user/repos" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fixture-token" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			t.Error("invalid repository creation request")
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name != "notes" || !body.Private || body.AutoInit {
			t.Error("repository must be private and empty", body, err)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"full_name": "octo/notes", "clone_url": "https://github.com/octo/notes.git", "private": !publicResponse, "default_branch": "main"})
	}))
	defer server.Close()
	client.api = server.URL
	account, err := client.Login(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := client.CreatePrivateRepository(context.Background(), account.ID, "notes")
	if err != nil || !repo.Private || repo.URL != "https://github.com/octo/notes.git" {
		t.Fatal(repo, err)
	}
	publicResponse = true
	if _, err := client.CreatePrivateRepository(context.Background(), account.ID, "notes"); err == nil {
		t.Fatal("public response accepted")
	}
	for _, name := range []string{"", "../notes", "notes/other", "notes\n", ".", ".."} {
		if _, err := client.CreatePrivateRepository(context.Background(), account.ID, name); err == nil {
			t.Fatal("invalid name accepted", name)
		}
	}
	if creates != 2 {
		t.Fatal("invalid names reached GitHub", creates)
	}
}
