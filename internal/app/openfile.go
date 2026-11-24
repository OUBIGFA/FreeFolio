package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/space"
	"github.com/OUBIGFA/FreeMD/internal/vault"
)

// openSystemFile handles file associations and command-line documents. The
// deepest existing folder space wins. Loose files stay in memory for this session;
// opening a Markdown file never converts it or copies the user's document.
func (a *App) openSystemFile(file string) bool {
	full, err := filepath.Abs(file)
	if err == nil {
		full, err = filepath.EvalSymlinks(full)
	}
	if err != nil {
		a.notify("无法打开文件。%v", err)
		return false
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() || !vault.IsDocument(full) {
		a.notify("请选择 FreeMD、Markdown 或 PDF 文档：%s", filepath.Base(full))
		return false
	}
	if !a.flushForTask() {
		return false
	}
	if a.files != nil {
		// The browser keeps what opens in it, even a file of a space: only
		// "返回空间" leads back to the spaces.
		return a.openLooseFile(full)
	}
	var target *space.Space
	var rel string
	depth := -1
	for _, sp := range a.state.Spaces {
		if sp.IsExternal() {
			continue
		}
		root, err := filepath.EvalSymlinks(sp.Root)
		if err != nil {
			continue
		}
		r, err := filepath.Rel(root, full)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
			continue
		}
		if len(root) > depth {
			target, rel, depth = sp, filepath.ToSlash(r), len(root)
		}
	}
	if target == nil {
		return a.openLooseFile(full)
	}
	if !a.openAnywhere(target.ID, rel) {
		return false
	}
	a.focus = false
	if a.library.open {
		a.library.open, a.library.frames = false, 0
	}
	return true
}

// documentArguments accepts arguments without argv[0], as MyGo forwards them.
func documentArguments(args []string, workingDir string) []string {
	var files []string
	for _, file := range args {
		if !vault.IsDocument(file) {
			continue
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(workingDir, file)
		}
		files = append(files, file)
	}
	return files
}
