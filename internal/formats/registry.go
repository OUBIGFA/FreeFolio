// Package formats is FreeFolio's registry of document formats beyond its
// native Markdown, .arcmd and whiteboards, and the readers that turn them
// into Markdown. It has no dependency on the window or drawing toolkit.
//
// A format is one of two modes. Source formats are plain text (code,
// configuration, data, markup) that FreeFolio edits and writes back byte for
// byte as typed. Converted formats (Office, OpenDocument, OFD, e-books, mail,
// archives…) are read through a converter into Markdown, shown read-only and
// never written: an editable copy is a separate .md file.
//
// Plugins add converted formats at run time (SetPluginFormats); their
// conversion runs in the plugin host the application registers
// (SetPluginConverter).
package formats

import (
	"errors"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Mode is how FreeFolio opens a format.
type Mode uint8

const (
	// None is a file FreeFolio does not open through this package.
	None Mode = iota
	// Source is plain text edited in place.
	Source
	// Converted is read into Markdown and shown read-only.
	Converted
)

// Format describes a file extension.
type Format struct {
	Ext  string // lowercase, with the dot, as ".docx"
	Name string // short display name
	Mode Mode
	// Language names the syntax of a source format for highlighting.
	Language string
	// Plugin is the ID of the plugin converting the format, "" for a
	// built-in reader.
	Plugin string
}

// MaxSourceSize bounds the source files FreeFolio edits.
const MaxSourceSize = 4 << 20

// MaxConvertSize bounds the files the converters read.
const MaxConvertSize = 64 << 20

// ErrTooLarge reports a file beyond MaxSourceSize or MaxConvertSize.
var ErrTooLarge = errors.New("formats: the file is too large to open")

// ErrUnsupported reports content a reader cannot handle, as an encrypted
// or unknown variant of the format.
var ErrUnsupported = errors.New("formats: unsupported variant of the format")

func src(lang, name string, exts ...string) []Format {
	out := make([]Format, 0, len(exts))
	for _, e := range exts {
		out = append(out, Format{Ext: e, Name: name, Mode: Source, Language: lang})
	}
	return out
}

func conv(name string, exts ...string) []Format {
	out := make([]Format, 0, len(exts))
	for _, e := range exts {
		out = append(out, Format{Ext: e, Name: name, Mode: Converted})
	}
	return out
}

var builtin = func() map[string]Format {
	lists := [][]Format{
		src("text", "Text", ".txt", ".text", ".log", ".srt", ".vtt", ".nfo"),
		src("csv", "CSV", ".csv", ".tsv", ".tab", ".dsv"),
		src("json", "JSON", ".json", ".jsonl", ".ndjson", ".geojson", ".webmanifest"),
		src("jsonc", "JSON", ".jsonc", ".json5"),
		src("yaml", "YAML", ".yaml", ".yml"),
		src("toml", "TOML", ".toml"),
		src("xml", "XML", ".xml", ".xsd", ".xsl", ".xslt", ".plist", ".svg", ".rss", ".atom", ".csproj", ".props"),
		src("html", "HTML", ".html", ".htm", ".xhtml"),
		src("css", "CSS", ".css", ".scss", ".sass", ".less"),
		src("ini", "INI", ".ini", ".cfg", ".conf", ".env", ".properties", ".editorconfig", ".reg"),
		src("go", "Go", ".go"),
		src("rust", "Rust", ".rs"),
		src("c", "C/C++", ".c", ".h", ".cpp", ".hpp", ".cc", ".cxx", ".hh", ".ino"),
		src("java", "Java", ".java"),
		src("kotlin", "Kotlin", ".kt", ".kts"),
		src("swift", "Swift", ".swift"),
		src("csharp", "C#", ".cs"),
		src("js", "JavaScript", ".js", ".mjs", ".cjs", ".jsx"),
		src("ts", "TypeScript", ".ts", ".tsx", ".mts", ".cts"),
		src("js", "Vue", ".vue", ".svelte"),
		src("python", "Python", ".py", ".pyw", ".pyi"),
		src("ruby", "Ruby", ".rb"),
		src("php", "PHP", ".php"),
		src("shell", "Shell", ".sh", ".bash", ".zsh", ".fish"),
		src("powershell", "PowerShell", ".ps1", ".psm1", ".psd1"),
		src("bat", "Batch", ".bat", ".cmd"),
		src("sql", "SQL", ".sql"),
		src("lua", "Lua", ".lua"),
		src("dart", "Dart", ".dart"),
		src("r", "R", ".r"),
		src("perl", "Perl", ".pl", ".pm"),
		src("scala", "Scala", ".scala"),
		src("zig", "Zig", ".zig"),
		src("gradle", "Gradle", ".gradle"),
		src("mermaid", "Mermaid", ".mmd", ".mermaid"),
		src("diff", "Diff", ".diff", ".patch"),
		src("tex", "TeX", ".tex", ".sty", ".cls", ".bib"),
		src("typst", "Typst", ".typ"),
		src("org", "Org", ".org"),
		src("rst", "reStructuredText", ".rst"),
		src("asciidoc", "AsciiDoc", ".adoc", ".asciidoc"),
		src("textile", "Textile", ".textile"),

		conv("Word", ".docx", ".docm", ".dotx", ".dotm", ".doc", ".dot"),
		conv("Excel", ".xlsx", ".xlsm", ".xltx", ".xltm", ".xls", ".xlt"),
		conv("PowerPoint", ".pptx", ".pptm", ".potx", ".potm", ".ppsx", ".ppt", ".pps", ".pot"),
		conv("WPS", ".wps", ".wpt", ".et", ".ett", ".dps", ".dpt"),
		conv("OpenDocument", ".odt", ".ott", ".ods", ".ots", ".odp", ".otp", ".fodt", ".fods", ".fodp"),
		conv("RTF", ".rtf"),
		conv("AbiWord", ".abw"),
		conv("iWork", ".pages", ".numbers", ".key"),
		conv("OFD", ".ofd"),
		conv("XPS", ".xps", ".oxps"),
		conv("EPUB", ".epub"),
		conv("Kindle", ".mobi", ".azw", ".azw3", ".prc"),
		conv("FictionBook", ".fb2"),
		conv("Email", ".eml", ".msg"),
		conv("Calendar", ".ics", ".ical"),
		conv("vCard", ".vcf", ".vcard"),
		conv("Mind map", ".xmind", ".mm", ".opml"),
		conv("Draw.io", ".drawio", ".dio"),
		conv("Jupyter", ".ipynb"),
		conv("SQLite", ".sqlite", ".sqlite3", ".db", ".db3"),
		conv("Archive", ".zip", ".tar", ".tgz", ".gz", ".bz2", ".cbz", ".cbt", ".jar", ".apk"),
	}
	m := map[string]Format{}
	for _, l := range lists {
		for _, f := range l {
			m[f.Ext] = f
		}
	}
	return m
}()

var (
	pluginMu      sync.RWMutex
	pluginFormats map[string]Format
	pluginConvert func(f Format, name string, data []byte) (string, error)
)

// SetPluginFormats replaces the formats enabled plugins convert, by
// extension. Built-in formats keep their readers: a plugin cannot take an
// extension FreeFolio already reads.
func SetPluginFormats(list []Format) {
	m := map[string]Format{}
	for _, f := range list {
		f.Ext = strings.ToLower(f.Ext)
		if _, ok := builtin[f.Ext]; ok || f.Plugin == "" || !strings.HasPrefix(f.Ext, ".") {
			continue
		}
		f.Mode = Converted
		m[f.Ext] = f
	}
	pluginMu.Lock()
	pluginFormats = m
	pluginMu.Unlock()
}

// SetPluginConverter registers how plugin formats are converted.
func SetPluginConverter(fn func(f Format, name string, data []byte) (string, error)) {
	pluginMu.Lock()
	pluginConvert = fn
	pluginMu.Unlock()
}

// ext is the extension of name, lowercase; ".tar.gz" counts as ".tgz".
func ext(name string) string {
	base := strings.ToLower(path.Base(filepath.ToSlash(name)))
	if strings.HasSuffix(base, ".tar.gz") {
		return ".tgz"
	}
	e := path.Ext(base)
	if e == base {
		return "" // a dotfile such as .env is not an extension of nothing
	}
	return e
}

// Lookup returns the format of the file name.
func Lookup(name string) (Format, bool) {
	e := ext(name)
	if e == "" {
		return Format{}, false
	}
	if f, ok := builtin[e]; ok {
		return f, true
	}
	pluginMu.RLock()
	f, ok := pluginFormats[e]
	pluginMu.RUnlock()
	return f, ok
}

// ModeOf returns how FreeFolio opens name.
func ModeOf(name string) Mode {
	f, _ := Lookup(name)
	return f.Mode
}

// IsSource reports whether name is a source format.
func IsSource(name string) bool { return ModeOf(name) == Source }

// IsConverted reports whether name is a converted format.
func IsConverted(name string) bool { return ModeOf(name) == Converted }

// Extensions lists every extension FreeFolio opens through this package,
// without dots, sorted, for open dialogs.
func Extensions() []string {
	var out []string
	for e := range builtin {
		out = append(out, e[1:])
	}
	pluginMu.RLock()
	for e := range pluginFormats {
		out = append(out, e[1:])
	}
	pluginMu.RUnlock()
	slices.Sort(out)
	return out
}
