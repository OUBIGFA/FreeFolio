package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Node is a folder or a Markdown file of the vault's tree.
type Node struct {
	Name string // the file or folder name
	Path string // relative to the root, slash-separated
	Dir  bool
	// Children of a folder: its folders first, then its files, each
	// sorted by name, case-insensitively.
	Children []*Node
	// Modified is when the file last changed; for a folder, the latest of
	// what it holds, or the folder's own time when empty.
	Modified time.Time
	Missing  bool // An explicitly registered external file is no longer present.
}

// Title returns a file's name without its extension, or a folder's name.
func (n *Node) Title() string {
	if n.Dir {
		return n.Name
	}
	return Title(n.Name)
}

// Tree reads the folders and document files under root, leaving out what
// List leaves out; empty folders are kept, as the user made them.
func Tree(root string) ([]*Node, error) {
	return readBranch(root, "", root, nil)
}

// treeReaders bounds how many folders are read at once beside the
// goroutine that asked for the tree.
var treeReaders = make(chan struct{}, 8)

// branch is a folder of the one being read, and what reading it gave.
type branch struct {
	node  *Node
	dir   string
	entry fs.DirEntry
	err   error
}

// readBranch reads the folder rel of the space, which is at dir. The caller
// has checked that dir stays within root: it is the root itself, a real
// folder listed in one that was checked, or a link Resolve accepted. Asking
// Resolve again for every folder walks each one's whole path link by link,
// which took most of the time of reading a space.
func readBranch(root, rel, dir string, ancestors []os.FileInfo) ([]*Node, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	for _, parent := range ancestors {
		if os.SameFile(info, parent) {
			return nil, errors.New("vault: symbolic link cycle")
		}
	}
	// Its own copy: the folders below are read at once, each adding itself.
	ancestors = append(ancestors[:len(ancestors):len(ancestors)], info)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs, files []*Node
	var branches []*branch
	for _, e := range entries {
		name := e.Name()
		p := path.Join(rel, name)
		full := filepath.Join(dir, name)
		isDir := e.IsDir()
		link := e.Type()&fs.ModeSymlink != 0
		if link {
			if _, err := Resolve(root, p); err != nil {
				continue
			}
			if fi, err := os.Stat(full); err == nil {
				isDir = fi.IsDir()
			}
		}
		switch {
		case isDir:
			if skipDir(name) {
				continue
			}
			branches = append(branches, &branch{node: &Node{Name: name, Path: p, Dir: true}, dir: full, entry: e})
		case !strings.HasPrefix(name, ".") && IsDocument(name):
			n := &Node{Name: name, Path: p}
			// The listing already holds a file's own time; only a link's
			// is not its target's, and is asked for.
			fi, err := e.Info()
			if link {
				fi, err = os.Stat(full)
			}
			if err == nil {
				n.Modified = fi.ModTime()
			}
			files = append(files, n)
		}
	}
	// The folders are read side by side while readers are free, and by
	// this goroutine otherwise: listing a folder waits on the disk, and a
	// space of many folders was read one wait after another.
	var wg sync.WaitGroup
	for _, b := range branches {
		read := func() { b.node.Children, b.err = readBranch(root, b.node.Path, b.dir, ancestors) }
		select {
		case treeReaders <- struct{}{}:
			wg.Go(func() {
				defer func() { <-treeReaders }()
				read()
			})
		default:
			read()
		}
	}
	wg.Wait()
	for _, b := range branches {
		if b.err != nil {
			continue // unreadable folders are left out
		}
		n := b.node
		// The folder's own time changes whenever an entry comes or goes,
		// as with the temporary file of every atomic save, so it stands
		// only for an empty folder.
		for _, k := range n.Children {
			if k.Modified.After(n.Modified) {
				n.Modified = k.Modified
			}
		}
		if n.Modified.IsZero() {
			if fi, err := b.entry.Info(); err == nil {
				n.Modified = fi.ModTime()
			}
		}
		dirs = append(dirs, n)
	}
	byName := func(a, b *Node) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) }
	slices.SortFunc(dirs, byName)
	slices.SortFunc(files, byName)
	return append(dirs, files...), nil
}

// Find returns the node at rel in a tree, or nil.
func Find(nodes []*Node, rel string) *Node {
	for _, n := range nodes {
		if n.Path == rel {
			return n
		}
		if n.Dir && strings.HasPrefix(rel, n.Path+"/") {
			return Find(n.Children, rel)
		}
	}
	return nil
}

// Mkdir makes a folder named name in dir ("" for the root), adding " 2",
// " 3"… when the name is taken, and returns its path.
func Mkdir(root, dir, name string) (string, error) {
	name = sanitize(name)
	if name == "" {
		name = "New Folder"
	}
	for i := 1; i < 1000; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s %d", name, i)
		}
		rel := path.Join(dir, n)
		p, err := Resolve(root, rel)
		if err != nil {
			return "", err
		}
		err = os.Mkdir(p, 0o755)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return rel, err
	}
	return "", fmt.Errorf("vault: too many folders named %q", name)
}

// ErrExists reports a rename or move onto a name that is taken.
var ErrExists = errors.New("vault: a file or folder with that name already exists")

// Rename gives the file or folder at rel a new name in the same folder; a
// file keeps its extension when the new name has none of Markdown's. It
// returns the new path.
func Rename(root, rel, name string) (string, error) {
	name = sanitize(name)
	if name == "" {
		return "", errors.New("vault: the name is empty")
	}
	src, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() && !(IsMarkdown(rel) && IsMarkdown(name)) && !strings.EqualFold(FileExt(name), FileExt(rel)) {
		name += FileExt(rel)
	}
	to := path.Join(path.Dir(rel), name)
	if to == rel {
		return rel, nil
	}
	return to, move(root, rel, to)
}

// Move moves the file or folder at rel into the folder dir ("" for the
// root) and returns its new path.
func Move(root, rel, dir string) (string, error) {
	if dir == rel || strings.HasPrefix(dir+"/", rel+"/") {
		return "", errors.New("vault: a folder can't move into itself")
	}
	to := path.Join(dir, path.Base(rel))
	if to == rel {
		return rel, nil
	}
	return to, move(root, rel, to)
}

func move(root, from, to string) error {
	src, err := Resolve(root, from)
	if err != nil {
		return err
	}
	dst, err := Resolve(root, to)
	if err != nil {
		return err
	}
	// A case-only rename may refer to the same file on a case-insensitive
	// disk. Equal names alone do not establish that on other file systems.
	if target, err := os.Lstat(dst); err == nil {
		source, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if !strings.EqualFold(from, to) || !os.SameFile(source, target) {
			return ErrExists
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}
