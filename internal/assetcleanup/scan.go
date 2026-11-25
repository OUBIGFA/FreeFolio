// Package assetcleanup finds unused attachments conservatively. It never deletes.
package assetcleanup

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

type Request struct {
	Root, AssetDir                       string
	ReferenceRoots, ReferenceFiles, Live []string
}
type Candidate struct {
	Path  string
	Bytes int64
	Hash  [32]byte
}

// Unchanged binds a confirmed cleanup choice to the bytes that were inspected.
func Unchanged(root string, c Candidate) (bool, error) {
	p, e := vault.Resolve(root, c.Path)
	if e != nil {
		return false, e
	}
	info, e := os.Lstat(p)
	if e != nil {
		return false, e
	}
	if !info.Mode().IsRegular() || info.Size() != c.Bytes {
		return false, nil
	}
	f, e := os.Open(p)
	if e != nil {
		return false, e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return false, e
	}
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash == c.Hash, nil
}

// References conservatively retains plain mentions as well as escaped links.
func References(text, name string) bool {
	return strings.Contains(referenceText(text), strings.ToLower(name))
}

func referenceText(text string) string {
	parts := []string{text, html.UnescapeString(text)}
	if decoded, e := url.PathUnescape(text); e == nil {
		parts = append(parts, decoded)
	}
	var value any
	if json.Unmarshal([]byte(text), &value) == nil {
		var collect func(any)
		collect = func(v any) {
			switch v := v.(type) {
			case string:
				parts = append(parts, v)
			case []any:
				for _, x := range v {
					collect(x)
				}
			case map[string]any:
				for _, x := range v {
					collect(x)
				}
			}
		}
		collect(value)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}
func skipped(name string) bool {
	return name == ".git" || name == "node_modules" || name == "credentials" || name == "_temp" || name == "cache"
}
func textFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".arcmd", ".excalidraw", ".svg", ".html", ".htm", ".txt", ".json", ".bak", ".xml", "":
		return true
	}
	return false
}

// Scan includes historical and recovery references. A failed read stops the
// scan, because unread documents cannot prove that an attachment is unused.
func Scan(req Request) ([]Candidate, error) {
	root, e := filepath.Abs(req.Root)
	if e != nil {
		return nil, e
	}
	assetRoot := ""
	if req.AssetDir != "" {
		assetRoot, e = vault.Resolve(root, req.AssetDir)
		if e != nil {
			return nil, e
		}
	}
	var candidates []Candidate
	err := filepath.WalkDir(root, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if file != root && d.IsDir() && skipped(d.Name()) {
			return filepath.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachment scan refuses symbolic link: %s", file)
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, file)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		candidate := false
		if assetRoot != "" {
			r, e := filepath.Rel(assetRoot, file)
			candidate = e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
		} else {
			for _, p := range strings.Split(rel, "/") {
				if p == "assets" || p == ".assets" {
					candidate = true
					break
				}
			}
		}
		if !candidate || strings.HasPrefix(rel, ".freefolio/") {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("attachment is not a regular file: %s", rel)
		}
		stream, e := os.Open(file)
		if e != nil {
			return e
		}
		hash := sha256.New()
		_, e = io.Copy(hash, stream)
		stream.Close()
		if e != nil {
			return e
		}
		var digest [32]byte
		copy(digest[:], hash.Sum(nil))
		candidates = append(candidates, Candidate{rel, info.Size(), digest})
		return nil
	})
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	check := func(text string) {
		decoded := referenceText(text)
		for _, c := range candidates {
			if !used[c.Path] && strings.Contains(decoded, strings.ToLower(filepath.Base(c.Path))) {
				used[c.Path] = true
			}
		}
	}
	for _, text := range req.Live {
		check(text)
	}
	seen := map[string]bool{}
	read := func(file string) error {
		key := strings.ToLower(filepath.Clean(file))
		if seen[key] {
			return nil
		}
		seen[key] = true
		info, e := os.Lstat(file)
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("reference is a symbolic link: %s", file)
		}
		if !info.Mode().IsRegular() || !textFile(file) {
			return nil
		}
		if info.Size() > 64<<20 {
			return fmt.Errorf("reference is too large to inspect safely: %s", file)
		}
		data, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		if utf8.Valid(data) {
			check(string(data))
			if strings.Contains(string(data), "payload-type:"+whiteboard.SceneMIME) {
				payload, e := whiteboard.DecodeSVGPayload(string(data))
				if e != nil {
					return fmt.Errorf("cannot inspect board attachment references: %w", e)
				}
				check(payload)
			}
		} else if filepath.Ext(file) != "" {
			return fmt.Errorf("reference is not valid UTF-8: %s", file)
		}
		return nil
	}
	for _, dir := range append([]string{root}, req.ReferenceRoots...) {
		if _, e := os.Lstat(dir); errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e = filepath.WalkDir(dir, func(file string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				if skipped(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			return read(file)
		}); e != nil {
			return nil, e
		}
	}
	for _, file := range req.ReferenceFiles {
		if e = read(file); e != nil {
			return nil, e
		}
	}
	out := slices.DeleteFunc(candidates, func(c Candidate) bool { return used[c.Path] })
	slices.SortFunc(out, func(a, b Candidate) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}
