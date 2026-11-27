// Package vault reads and writes the document files of a space's folder.
// Files on disk are the source of truth: FreeFolio keeps no copy of their
// content elsewhere, so any other editor, Git or a sync service can work on
// the same folder.
//
// Paths given to and returned by the package are slash-separated and
// relative to the folder (the root); Resolve refuses those that leave it.
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
	"time"
	"unicode"

	"github.com/OUBIGFA/FreeFolio/internal/formats"
	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
)

// Extensions are the file extensions the vault lists, lowercase.
var Extensions = []string{".md", ".markdown", ".mdx"}

// IsDocument includes native rich documents, Markdown import sources,
// whiteboards, PDFs and the formats of package formats: the files a space
// lists and opens.
func IsDocument(name string) bool {
	return IsText(name) || IsDatabase(name) || IsMDZ(name) || IsPDF(name) || IsSource(name) || IsConverted(name)
}

// IsSource is a plain-text file of another format (code, configuration,
// data) that FreeFolio edits as text and writes back as typed. Native
// documents, Markdown and whiteboards are not source files.
func IsSource(name string) bool {
	return !IsText(name) && !IsMDZ(name) && !IsPDF(name) && formats.IsSource(name)
}

// IsConverted is a file FreeFolio reads through a converter and shows
// read-only; it is never written.
func IsConverted(name string) bool {
	return !IsText(name) && !IsMDZ(name) && !IsPDF(name) && formats.IsConverted(name)
}

// IsReadOnly reports whether FreeFolio never writes the document's file.
func IsReadOnly(name string) bool { return IsPDF(name) || IsMDZ(name) || IsConverted(name) }

// IsText is a document whose file is text FreeFolio reads and writes: every
// document but a PDF, which is only read.
func IsText(name string) bool {
	return strings.EqualFold(path.Ext(name), ".arcmd") || IsMarkdown(name) || IsDatabase(name) || IsWhiteboard(name)
}

// IsPDF reports whether name is a PDF document (ArcMD fcb7cd3).
func IsPDF(name string) bool {
	base := path.Base(filepath.ToSlash(name))
	return len(base) > len(".pdf") && strings.EqualFold(path.Ext(base), ".pdf")
}

// IsWhiteboard recognizes editable scenes without treating arbitrary SVGs as documents.
func IsWhiteboard(name string) bool {
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".excalidraw") || strings.HasSuffix(low, ".excalidraw.svg")
}

// IsMarkdown reports whether name has one of Extensions.
func IsMarkdown(name string) bool {
	base := path.Base(filepath.ToSlash(name))
	if len(base) >= len(ExtDatabase) && strings.EqualFold(base[len(base)-len(ExtDatabase):], ExtDatabase) {
		return false
	}
	return slices.Contains(Extensions, strings.ToLower(path.Ext(base)))
}

// Doc is a Markdown file of the vault.
type Doc struct {
	// Path is relative to the root, slash-separated, as "journal/today.md".
	Path string
}

// Dir returns the folder of the document, "" at the root.
func (d Doc) Dir() string {
	if dir := path.Dir(d.Path); dir != "." {
		return dir
	}
	return ""
}

// Title returns the document's name without its extension.
func (d Doc) Title() string { return Title(d.Path) }

// Title returns the name of the file at rel without its extension.
func Title(rel string) string {
	base := path.Base(rel)
	if strings.HasSuffix(strings.ToLower(base), ".excalidraw.svg") {
		return base[:len(base)-len(".excalidraw.svg")]
	}
	return strings.TrimSuffix(base, FileExt(base))
}

// ErrOutside reports a path that leaves the root.
var ErrOutside = errors.New("vault: path is outside the space folder")

// Resolve returns the path of rel under root. Existing symbolic links must
// resolve within the root, including when the final file does not exist yet.
func Resolve(root, rel string) (string, error) {
	rel = filepath.ToSlash(rel)
	if rel == "" || path.IsAbs(rel) || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", ErrOutside
	}
	for seg := range strings.SplitSeq(rel, "/") {
		if seg == ".." {
			return "", ErrOutside
		}
	}
	clean := path.Clean(rel)
	if clean == "." {
		return "", ErrOutside
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	base, err := existingPath(root)
	if err != nil {
		return "", err
	}
	target, err := existingPath(full)
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(base, target)
	if (err != nil || !filepath.IsLocal(inside)) && !sameRootAncestor(base, target) {
		return "", ErrOutside
	}
	return full, nil
}

// EvalSymlinks preserves filename casing. On a case-insensitive filesystem,
// an absolute link may spell the root differently while staying inside it.
func sameRootAncestor(root, target string) bool {
	base, err := os.Stat(root)
	if err != nil || !base.IsDir() {
		return false
	}
	for {
		if info, err := os.Stat(target); err == nil && os.SameFile(base, info) {
			return true
		}
		parent := filepath.Dir(target)
		if parent == target {
			return false
		}
		target = parent
	}
}

// existingPath resolves the existing prefix of a path without requiring the
// file or its new folders to exist. A dangling link is not a missing suffix:
// failing closed there prevents creating files through an unchecked target.
func existingPath(name string) (string, error) {
	name, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		if _, err := os.Lstat(name); err == nil {
			base, err := filepath.EvalSymlinks(name)
			if err != nil {
				return "", err
			}
			if len(suffix) > 0 {
				if info, err := os.Stat(base); err != nil {
					return "", err
				} else if !info.IsDir() {
					return "", fmt.Errorf("vault: %s is not a directory", base)
				}
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				base = filepath.Join(base, suffix[i])
			}
			return base, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(name)
		if parent == name {
			return "", fs.ErrNotExist
		}
		suffix = append(suffix, filepath.Base(name))
		name = parent
	}
}

// skipDir reports whether the walk leaves out a directory: hidden ones (as
// .git and FreeFolio's own SystemDir) and dependency folders.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

// List returns the document files under root, sorted by folder then name,
// case-insensitively, the files of a folder before its subfolders'.
func List(root string) ([]Doc, error) {
	var docs []Doc
	err := walkDocuments(root, func(rel string) bool {
		docs = append(docs, Doc{Path: rel})
		return true
	})
	slices.SortFunc(docs, func(a, b Doc) int {
		if c := strings.Compare(strings.ToLower(a.Dir()), strings.ToLower(b.Dir())); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
	})
	return docs, err
}

// HasDocument reports whether root holds any document List would return,
// stopping at the first: a large folder need not be walked to the end.
func HasDocument(root string) (bool, error) {
	found := false
	err := walkDocuments(root, func(string) bool {
		found = true
		return false
	})
	return found, err
}

// errStopWalk ends walkDocuments early.
var errStopWalk = errors.New("vault: stop walk")

// walkDocuments calls visit with the slash-separated path of each document
// file under root, in walk order, until visit returns false.
func walkDocuments(root string, visit func(rel string) bool) error {
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // unreadable entries are left out, not fatal
		}
		if d.IsDir() {
			if p != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !IsDocument(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if _, err := Resolve(root, rel); err != nil {
				return nil
			}
			if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
				return nil
			}
		}
		if !visit(filepath.ToSlash(rel)) {
			return errStopWalk
		}
		return nil
	})
	if errors.Is(err, errStopWalk) {
		return nil
	}
	return err
}

// CreateFile atomically chooses an unused document name and writes its content.
func CreateFile(root, dir, title, extension, text string) (string, error) {
	if extension != ".arcmd" && extension != ".md" && extension != ".excalidraw" && extension != ".excalidraw.svg" && extension != ExtDatabase {
		return "", errors.New("vault: unsupported document extension")
	}
	title = sanitize(title)
	if title == "" {
		title = "Untitled"
	}
	for i := 1; i < 1000; i++ {
		name := title
		if i > 1 {
			name = fmt.Sprintf("%s %d", title, i)
		}
		rel := path.Join(dir, name+extension)
		full, err := Resolve(root, rel)
		if err != nil {
			return "", err
		}
		err = fsutil.CreateFileAtomic(full, []byte(text), 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return rel, err
	}
	return "", errors.New("vault: too many documents with that name")
}

// Read returns the text of the document at rel.
func Read(root, rel string) (string, error) {
	p, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	return string(b), err
}

// Write replaces the document at rel with text, atomically, creating its
// folders.
func Write(root, rel, text string) error {
	p, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(p, []byte(text), 0o644)
}

// Create makes a new document named after title in the folder dir (""
// for the root), adding " 2", " 3"… when the name is taken, and returns
// its path. The file starts with title as a heading.
func Create(root, dir, title string) (string, error) {
	title = sanitize(title)
	if title == "" {
		title = "Untitled"
	}
	for i := 1; i < 1000; i++ {
		name := title
		if i > 1 {
			name = fmt.Sprintf("%s %d", title, i)
		}
		rel := path.Join(dir, name+".md")
		p, err := Resolve(root, rel)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = fmt.Fprintf(f, "# %s\n\n", name)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return rel, err
	}
	return "", fmt.Errorf("vault: too many documents named %q", title)
}

// sanitize makes title a file name that every platform accepts.
func sanitize(title string) string {
	return SanitizeName(title)
}

// ImportFile copies the regular file at src into the folder dir ("" for
// the root) of the vault at root, adding " 2", " 3"… to its name when the
// name is taken, and returns its path. The copy is created exclusively, so
// an existing file is never overwritten.
func ImportFile(src, root, dir string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("vault: %s is not a regular file", filepath.Base(src))
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	base := filepath.Base(src)
	ext := FileExt(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		rel := path.Join(dir, name)
		full, err := Resolve(root, rel)
		if err != nil {
			return "", err
		}
		err = fsutil.CreateFileAtomic(full, data, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return rel, err
	}
	return "", errors.New("vault: too many files with that name")
}

// Structured document formats retain their complete compound suffix.
const (
	ExtDatabase      = ".db.md"
	ExtMDZ           = ".mdz"
	ExtWhiteboard    = ".excalidraw.svg"
	ExtWhiteboardRaw = ".excalidraw"
	ExtPDF           = ".pdf"
)

func FileExt(name string) string {
	base := path.Base(filepath.ToSlash(name))
	if len(base) > len(ExtWhiteboard) && strings.EqualFold(base[len(base)-len(ExtWhiteboard):], ExtWhiteboard) {
		return base[len(base)-len(ExtWhiteboard):]
	}
	if len(base) > len(ExtDatabase) && strings.EqualFold(base[len(base)-len(ExtDatabase):], ExtDatabase) {
		return base[len(base)-len(ExtDatabase):]
	}
	return path.Ext(base)
}

func IsDatabase(name string) bool {
	base := path.Base(filepath.ToSlash(name))
	return len(base) > len(ExtDatabase) && strings.EqualFold(FileExt(base), ExtDatabase)
}

func IsMDZ(name string) bool {
	base := path.Base(filepath.ToSlash(name))
	ext := strings.ToLower(FileExt(base))
	return len(base) > len(ext) && ext == ExtMDZ
}

func SanitizeName(title string) string {
	title = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20:
			return -1
		case strings.ContainsRune(`/\:*?"<>|`, r):
			return '-'
		}
		return r
	}, title)
	title = strings.TrimFunc(title, func(r rune) bool {
		return unicode.IsSpace(r) || r == '.'
	})
	if title == "" {
		return ""
	}
	stem := title
	if dot := strings.IndexByte(title, '.'); dot >= 0 {
		stem = title[:dot]
	}
	stem = strings.TrimRightFunc(stem, unicode.IsSpace)
	if isWindowsReservedName(stem) {
		return "_" + title
	}
	return title
}

func isWindowsReservedName(stem string) bool {
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL",
		"COM0", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT0", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
		"COM\u00b9", "COM\u00b2", "COM\u00b3",
		"LPT\u00b9", "LPT\u00b2", "LPT\u00b3":
		return true
	}
	return false
}

// DocStat describes one indexed file with its current filesystem metadata.
type DocStat struct {
	Doc
	Size     int64
	Modified time.Time
}

// ListStat shares List's path filtering and reports readable file metadata.
func ListStat(root string) ([]DocStat, error) {
	docs, err := List(root)
	if err != nil {
		return nil, err
	}
	result := make([]DocStat, 0, len(docs))
	for _, doc := range docs {
		full, err := Resolve(root, doc.Path)
		if err != nil {
			continue
		}
		info, err := os.Stat(full)
		if err == nil && info.Mode().IsRegular() {
			result = append(result, DocStat{Doc: doc, Size: info.Size(), Modified: info.ModTime()})
		}
	}
	return result, nil
}
