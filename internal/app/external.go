package app

import (
	"path/filepath"

	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/OUBIGFA/FreeMD/internal/space"
	"github.com/OUBIGFA/FreeMD/internal/vault"
	"github.com/egoist/mygo"
)

// fileLocation separates a document's list identity from its disk location.
// Every ordinary space keeps the vault boundary; external entries must be
// explicitly registered and are resolved relative to their actual parent.
func fileLocation(sp *space.Space, rel string) (root, name string, err error) {
	if !sp.IsExternal() {
		return sp.Root, rel, nil
	}
	full, err := vault.Files(sp.ExternalFiles).Resolve(rel)
	if err != nil {
		return "", "", err
	}
	return filepath.Dir(full), filepath.Base(full), nil
}

func resolveFile(sp *space.Space, rel string) (string, error) {
	root, name, err := fileLocation(sp, rel)
	if err != nil {
		return "", err
	}
	return vault.Resolve(root, name)
}

// keptPath is a document's resolved path and what it was resolved from.
// Resolving walks the file system link by link, which is too much to do in
// every frame for a path that is only shown or asked for its time: the
// answer is kept until the document or its space moves. Reading and writing
// never use it: they resolve again, as the space's boundary asks.
type keptPath struct {
	root, rel, external, path string
	err                       error
	known                     bool
}

// resolve is resolveFile, asked again only for another document or place.
func (k *keptPath) resolve(sp *space.Space, rel string) (string, error) {
	external := ""
	if sp.IsExternal() {
		external = sp.ExternalFiles[rel]
	}
	if !k.known || k.root != sp.Root || k.rel != rel || k.external != external {
		*k = keptPath{root: sp.Root, rel: rel, external: external, known: true}
		k.path, k.err = resolveFile(sp, rel)
	}
	return k.path, k.err
}

func readFile(sp *space.Space, rel string) (string, error) {
	root, name, err := fileLocation(sp, rel)
	if err != nil {
		return "", err
	}
	return vault.Read(root, name)
}

func writeFile(sp *space.Space, rel, text string) error {
	root, name, err := fileLocation(sp, rel)
	if err != nil {
		return err
	}
	return vault.Write(root, name, text)
}

func renameFile(sp *space.Space, rel, name string) (string, error) {
	root, disk, err := fileLocation(sp, rel)
	if err != nil {
		return "", err
	}
	to, err := vault.Rename(root, disk, name)
	if err == nil && sp.IsExternal() {
		to = sp.AddExternal(filepath.Join(root, to))
	}
	return to, err
}

func (d *document) localImage(url string) (string, error) {
	return resolveSpaceImage(d.space, d.rel, url)
}

// forgetExternal removes only the reference. Saving and conflict handling are
// identical to leaving a normal document; no user's file goes to the trash.
func (a *App) forgetExternal(sp *space.Space, rel string) {
	if !sp.IsExternal() {
		return
	}
	if f := a.files; f != nil && sp == f.locations {
		a.closeFileTab(rel)
	}

}

func (a *App) chooseExternalFiles() {
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: i18n.T("打开文件…"), Multiple: true,
			Filters: []mygo.FileFilter{{Name: "FreeMD", Extensions: []string{"md", "markdown", "mdx", "arcmd", "excalidraw", "svg"}}}})
		if err != nil {
			return
		}
		win.Update(func() {
			for _, file := range files {
				a.openSystemFile(file)
			}
		})
	}()
}
