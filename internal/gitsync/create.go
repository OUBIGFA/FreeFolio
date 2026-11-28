package gitsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
)

var repositoryNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// ValidRepositoryName rejects path separators and GitHub's reserved dot names.
func ValidRepositoryName(name string) bool {
	return repositoryNamePattern.MatchString(name) && name != "." && name != ".."
}

// CreatePrivateRepository never initializes remote content: the reviewed local
// selection is the only source of the first commit. Creation is never retried.
func (c *GitHub) CreatePrivateRepository(ctx context.Context, id, name string) (RemoteRepository, error) {
	var result RemoteRepository
	if !ValidRepositoryName(name) {
		return result, errors.New(i18n.T("仓库名称只能包含英文字母、数字、点、短横线和下划线，最多 100 个字符"))
	}
	accounts, err := c.Accounts()
	if err != nil {
		return result, err
	}
	login := ""
	for _, account := range accounts {
		if account.ID == id {
			login = account.Login
		}
	}
	token, err := c.Token(ctx, id)
	if err != nil {
		return result, err
	}
	var response struct {
		Name    string `json:"full_name"`
		URL     string `json:"clone_url"`
		Branch  string `json:"default_branch"`
		Private bool   `json:"private"`
	}
	payload := struct {
		Name     string `json:"name"`
		Private  bool   `json:"private"`
		AutoInit bool   `json:"auto_init"`
	}{Name: name, Private: true}
	if err := c.request(ctx, "POST", c.api+"/user/repos", token, payload, &response); err != nil {
		return result, fmt.Errorf(i18n.T("创建结果未确认，请先在 GitHub 检查同名仓库再重试：%v"), err)
	}
	repository, err := GitHubRepository(response.URL)
	if err != nil || !response.Private || !strings.HasPrefix(response.URL, "https://github.com/") || !strings.EqualFold(repository, login+"/"+name) || !strings.EqualFold(response.Name, repository) {
		return result, errors.New(i18n.T("无法确认新仓库为当前账号的私有仓库，请在 GitHub 中检查"))
	}
	return RemoteRepository{Name: response.Name, URL: response.URL, Branch: response.Branch, Private: true, Writable: true}, nil
}

// PrepareRepository changes only Git metadata. Files are neither staged nor
// uploaded until the user reviews the selection and confirms creation.
func (g *Git) PrepareRepository(ctx context.Context, root string) (Repository, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, err := g.operationLock(ctx, root)
	if err != nil {
		return Repository{}, err
	}
	defer release()
	root, err = filepath.Abs(root)
	if err != nil {
		return Repository{}, err
	}
	// Use Git's own discovery rules, including worktrees and test ceilings.
	top, discoveryErr := g.run(ctx, root, nil, []string{"LC_ALL=C"}, "rev-parse", "--show-toplevel")
	if discoveryErr == nil && !strings.EqualFold(filepath.Clean(strings.TrimSpace(string(top))), filepath.Clean(root)) {
		return Repository{}, errors.New(i18n.T("此空间位于其他 Git 仓库内，请选择独立空间"))
	}
	if discoveryErr != nil && !strings.Contains(discoveryErr.Error(), "not a git repository") {
		return Repository{}, discoveryErr
	}
	if _, err := os.Lstat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		if _, err := g.run(ctx, root, nil, nil, "init", "--initial-branch=main"); err != nil {
			return Repository{}, err
		}
	} else if err != nil {
		return Repository{}, err
	}
	return g.newRepository(ctx, root)
}

func (g *Git) newRepository(ctx context.Context, root string) (Repository, error) {
	repo, err := g.inspect(ctx, root)
	if err != nil {
		return repo, err
	}
	if repo.Head != "" || len(repo.Remotes) != 0 || repo.Pending != nil {
		return repo, errors.New(i18n.T("此空间已有 Git 历史或远端，请使用现有仓库同步"))
	}
	for _, change := range repo.Changes {
		if change.Staged || change.Conflict {
			return repo, errors.New(i18n.T("请先处理已暂存内容，再创建仓库"))
		}
	}
	return repo, nil
}

// BindNewRepository records the remote without replacing an existing binding.
// The fingerprint prevents uploading files that changed after review.
func (g *Git) BindNewRepository(ctx context.Context, root, address, fingerprint string) (Binding, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, err := g.operationLock(ctx, root)
	if err != nil {
		return Binding{}, err
	}
	defer release()
	repo, err := g.newRepository(ctx, root)
	if err != nil {
		return Binding{}, err
	}
	if repo.Fingerprint != fingerprint {
		return Binding{}, ErrChanged
	}
	if !g.AllowLocal {
		if _, err := GitHubRepository(address); err != nil {
			return Binding{}, err
		}
	}
	if _, err := g.run(ctx, root, nil, nil, "remote", "add", "origin", address); err != nil {
		return Binding{}, err
	}
	return Binding{Remote: "origin", URL: address, Branch: repo.Branch, Root: repo.Root}, nil
}
