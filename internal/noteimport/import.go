// Package noteimport copies existing Markdown and SiYuan libraries into a space.
// Originals are never modified. Every conversion loss is recorded in Report.
package noteimport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
)

type Issue struct{ File, Kind, Detail string }
type Report struct {
	Folder                 string
	ReportPath             string
	Documents, Attachments int
	Issues                 []Issue
}

func (r *Report) issue(file, kind, detail string) {
	r.Issues = append(r.Issues, Issue{file, kind, detail})
}

type source struct {
	root    string
	files   []string
	archive map[string]*zip.File
	data    []byte
}

var markdownResources = regexp.MustCompile(`!?\[[^\]\n]*\]\((?:<([^>]+)>|([^\s)]+))(?:\s+"[^"]*")?\)|!\[\[([^\]\n]+)\]\]|(?:src|href)="([^"]+)"`)

func safeName(name string) bool {
	return name != "" && name != "." && path.Clean(name) == name && !path.IsAbs(name) && name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\:\x00")
}
func openSource(name string) (*source, error) {
	info, e := os.Lstat(name)
	if e != nil {
		return nil, e
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("import source is a symbolic link")
	}
	s := &source{root: name}
	if info.IsDir() {
		e = filepath.WalkDir(name, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("import refuses symbolic link: %s", p)
			}
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "_temp" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, e := filepath.Rel(name, p)
			if e != nil {
				return e
			}
			s.files = append(s.files, filepath.ToSlash(rel))
			return nil
		})
		return s, e
	}
	if strings.EqualFold(filepath.Ext(name), ".md") {
		s.root = filepath.Dir(name)
		s.files = []string{filepath.Base(name)}
		raw, e := os.ReadFile(name)
		if e != nil {
			return nil, e
		}
		seen := map[string]bool{s.files[0]: true}
		for _, m := range markdownResources.FindAllStringSubmatch(string(raw), -1) {
			candidate := ""
			for _, part := range m[1:] {
				if part != "" {
					candidate = part
					break
				}
			}
			candidate = strings.SplitN(candidate, "|", 2)[0]
			candidate = strings.SplitN(candidate, "#", 2)[0]
			candidate = strings.Trim(candidate, " <>")
			if decoded, e := url.PathUnescape(candidate); e == nil {
				candidate = decoded
			}
			if safeName(candidate) && !seen[candidate] {
				seen[candidate] = true
				s.files = append(s.files, candidate)
			}
		}
		return s, nil
	}
	if !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return nil, errors.New("choose a Markdown library, SiYuan workspace or ZIP export")
	}
	if info.Size() > 256<<20 {
		return nil, errors.New("import archive exceeds 256 MiB")
	}
	s.data, e = os.ReadFile(name)
	if e != nil {
		return nil, e
	}
	zr, e := zip.NewReader(bytes.NewReader(s.data), int64(len(s.data)))
	if e != nil {
		return nil, e
	}
	s.archive = map[string]*zip.File{}
	seen := map[string]bool{}
	var total uint64
	for _, f := range zr.File {
		n := strings.TrimSuffix(f.Name, "/")
		if !safeName(n) || f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe import archive path: %s", f.Name)
		}
		key := strings.ToLower(n)
		if seen[key] {
			return nil, fmt.Errorf("duplicate import archive path: %s", n)
		}
		seen[key] = true
		if f.FileInfo().IsDir() {
			continue
		}
		total += f.UncompressedSize64
		if total > 512<<20 || f.UncompressedSize64 > 64<<20 {
			return nil, errors.New("expanded import archive exceeds safety limit")
		}
		s.archive[n] = f
		s.files = append(s.files, n)
	}
	slices.Sort(s.files)
	return s, nil
}
func (s *source) read(name string) ([]byte, error) {
	if !safeName(name) {
		return nil, errors.New("invalid source path")
	}
	if s.archive != nil {
		f := s.archive[name]
		if f == nil {
			return nil, os.ErrNotExist
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		defer r.Close()
		b, e := io.ReadAll(io.LimitReader(r, 64<<20+1))
		if len(b) > 64<<20 {
			return nil, errors.New("source file exceeds 64 MiB")
		}
		return b, e
	}
	p, e := vault.Resolve(s.root, name)
	if e != nil {
		return nil, e
	}
	info, e := os.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return nil, fmt.Errorf("source is not a regular file below 64 MiB: %s", name)
	}
	return os.ReadFile(p)
}
func cleanTitle(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '-'
		}
		return r
	}, s))
	s = strings.TrimRight(s, ". ")
	if s == "" {
		return "Untitled"
	}
	return s
}
func relative(from, to string) string {
	r, e := filepath.Rel(filepath.Dir(filepath.FromSlash(from)), filepath.FromSlash(to))
	if e != nil {
		return to
	}
	return filepath.ToSlash(r)
}

// Import creates a new folder. Existing destination files are never replaced.
func Import(src, destination string) (report Report, err error) {
	return ImportWith(src, destination, nil)
}

// ImportWith lets the host's existing codec canonicalize generated SiYuan
// Markdown. Original Markdown copies never pass through this conversion.
func ImportWith(src, destination string, canonicalize func(string) (string, error)) (report Report, err error) {
	s, err := openSource(src)
	if err != nil {
		return report, err
	}
	absSource, _ := filepath.Abs(src)
	absDest, _ := filepath.Abs(destination)
	if rel, e := filepath.Rel(absSource, absDest); e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return report, errors.New("destination must be outside the source library")
	}
	name := cleanTitle(strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)))
	for i := 0; ; i++ {
		folder := name
		if i > 0 {
			folder = fmt.Sprintf("%s (%d)", name, i+1)
		}
		p, e := vault.Resolve(destination, folder)
		if e != nil {
			return report, e
		}
		e = os.Mkdir(p, 0755)
		if errors.Is(e, os.ErrExist) {
			continue
		}
		if e != nil {
			return report, e
		}
		report.Folder = folder
		break
	}
	root := filepath.Join(destination, report.Folder)
	write := func(rel string, data []byte) error {
		p, e := vault.Resolve(root, rel)
		if e != nil {
			return e
		}
		return fsutil.CreateFileAtomic(p, data, 0644)
	}
	sy := false
	for _, file := range s.files {
		if strings.HasSuffix(strings.ToLower(file), ".sy") {
			sy = true
			break
		}
	}
	if sy {
		writeConverted := write
		if canonicalize != nil {
			writeConverted = func(rel string, data []byte) error {
				if strings.HasSuffix(rel, ".md") {
					text, e := canonicalize(string(data))
					if e != nil {
						return e
					}
					data = []byte(text)
				}
				return write(rel, data)
			}
		}
		err = importSiYuan(s, writeConverted, &report)
	} else {
		for _, file := range s.files {
			hidden := false
			for _, part := range strings.Split(file, "/") {
				if strings.HasPrefix(part, ".") && part != ".assets" || part == "credentials" || part == "_temp" {
					hidden = true
					break
				}
			}
			if hidden {
				continue
			}
			ext := strings.ToLower(path.Ext(file))
			if ext == ".exe" || ext == ".dll" || ext == ".app" {
				report.issue(file, "skipped", "Executable is not a note attachment")
				continue
			}
			data, e := s.read(file)
			if e == nil {
				e = write(file, data)
			}
			if e != nil {
				report.issue(file, "failed", e.Error())
				continue
			}
			if ext == ".md" {
				report.Documents++
			} else {
				report.Attachments++
			}
		}
	}
	if err != nil {
		return report, err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# 导入报告 / Import report\n\n文档 / Documents: %d\n\n附件 / Attachments: %d\n\n原文件保持不变。Original files were not changed.\n\n", report.Documents, report.Attachments)
	for _, issue := range report.Issues {
		fmt.Fprintf(&out, "- **%s** `%s`: %s\n", issue.Kind, strings.ReplaceAll(issue.File, "`", "'"), issue.Detail)
	}
	if len(report.Issues) == 0 {
		out.WriteString("未发现转换问题。No conversion issues were found.\n")
	}
	for i := 0; ; i++ {
		name := "Import Report.md"
		if i > 0 {
			name = fmt.Sprintf("Import Report (%d).md", i+1)
		}
		err = write(name, []byte(out.String()))
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err == nil {
			report.ReportPath = path.Join(report.Folder, name)
		}
		break
	}
	return report, err
}

type Found struct {
	App, Name, Path string
	Documents       int
}

// Discover reads only the known applications' library registries and locations.
// It never imports or modifies a discovered library.
func Discover(home, config string) []Found {
	var candidates []Found
	if raw, e := os.ReadFile(filepath.Join(config, "obsidian", "obsidian.json")); e == nil {
		var v struct {
			Vaults map[string]struct {
				Path string `json:"path"`
			} `json:"vaults"`
		}
		if json.Unmarshal(raw, &v) == nil {
			for _, x := range v.Vaults {
				candidates = append(candidates, Found{App: "Obsidian", Path: x.Path})
			}
		}
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".logseq", "graphs"))
	for _, entry := range entries {
		n := entry.Name()
		if strings.HasPrefix(n, "logseq_local_") && strings.HasSuffix(n, ".transit") {
			n = strings.TrimSuffix(strings.TrimPrefix(n, "logseq_local_"), ".transit")
			candidates = append(candidates, Found{App: "Logseq", Path: strings.ReplaceAll(n, "++", "/")})
		}
	}
	mobile := filepath.Join(home, "Library", "Mobile Documents")
	for _, v := range []struct{ app, folder string }{{"Obsidian", "iCloud~md~obsidian/Documents"}, {"Logseq", "iCloud~com~logseq~logseq/Documents"}, {"iA Writer", "27N4MQEA55~pro~writer/Documents"}, {"Typora", "iCloud~abnerworks~Typora/Documents"}} {
		p := filepath.Join(mobile, filepath.FromSlash(v.folder))
		candidates = append(candidates, Found{App: v.app, Path: p})
	}
	candidates = append(candidates, Found{App: "SiYuan", Path: filepath.Join(home, "SiYuan")})
	if raw, e := os.ReadFile(filepath.Join(home, ".config", "siyuan", "workspace.json")); e == nil {
		var paths []string
		if json.Unmarshal(raw, &paths) == nil {
			for _, p := range paths {
				candidates = append(candidates, Found{App: "SiYuan", Path: p})
			}
		}
	}
	var out []Found
	seen := map[string]bool{}
	for _, c := range candidates {
		if c.Path == "" {
			continue
		}
		p, e := filepath.Abs(c.Path)
		if e != nil {
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		info, e := os.Stat(p)
		if e != nil || !info.IsDir() {
			continue
		}
		count := 0
		err := filepath.WalkDir(p, func(file string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			ext := strings.ToLower(path.Ext(file))
			if ext == ".md" || ext == ".sy" {
				count++
			}
			return nil
		})
		if err == nil && count > 0 {
			c.Path = p
			c.Name = filepath.Base(p)
			c.Documents = count
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Found) int { return strings.Compare(a.App+a.Path, b.App+b.Path) })
	return out
}
