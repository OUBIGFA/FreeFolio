// Command solaricons generates internal/solar/icons_gen.go from the Solar
// icon set, keeping only the icons internal/solar/manifest.txt lists.
//
// The pinned upstream archive is downloaded once into .local/downloads,
// verified by SHA-256 and unpacked into .local/upstream, so every icon of
// the set stays available offline for later additions:
//
//	go run ./tools/solaricons            # regenerate from the manifest
//	go run ./tools/solaricons -find pen  # list available icons by name
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/OUBIGFA/FreeFolio/tools/internal/toolutil"
)

const (
	revision      = "ab241e1bf578dcfe45b0197aee59bd9a08b5fe68"
	archiveSHA256 = "3a2a416eda190d089eab9283cd99f2f1896ff9005e86fbe0e18195e477a0a4ba"
	archiveURL    = "https://codeload.github.com/saoudi-h/solar-icons/tar.gz/" + revision
	sourceDir     = ".local/upstream/solar-icons-ab241e1"
	archivePath   = ".local/downloads/solar-icons-" + revision + ".tar.gz"
	manifestPath  = "internal/solar/manifest.txt"
	outputPath    = "internal/solar/icons_gen.go"
)

// styles are the Solar styles a manifest entry may ask for.
var styles = []string{"Linear", "Outline", "Bold", "BoldDuotone", "LineDuotone", "Broken"}

func main() {
	find := flag.String("find", "", "list available icons whose name contains this text")
	style := flag.String("style", "Linear", "style that -find lists")
	flag.Parse()
	root, err := toolutil.Root()
	if err == nil {
		err = ensureSource(root)
	}
	if err == nil {
		if *find != "" {
			err = list(root, *find, *style)
		} else {
			err = generate(root)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "solaricons:", err)
		os.Exit(1)
	}
}

func svgRoot(root string) string {
	return filepath.Join(root, sourceDir, "packages", "core", "svgs")
}

// ensureSource unpacks the pinned archive, downloading it when missing.
func ensureSource(root string) error {
	if _, err := os.Stat(svgRoot(root)); err == nil {
		return nil
	}
	archive := filepath.Join(root, archivePath)
	data, err := os.ReadFile(archive)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("downloading", archiveURL)
		data, err = download(archiveURL)
		if err == nil && toolutil.Hash(data) == archiveSHA256 {
			if err = os.MkdirAll(filepath.Dir(archive), 0o755); err == nil {
				err = os.WriteFile(archive, data, 0o644)
			}
		}
	}
	if err != nil {
		return err
	}
	if got := toolutil.Hash(data); got != archiveSHA256 {
		return fmt.Errorf("%s: SHA-256 %s, want %s", archivePath, got, archiveSHA256)
	}
	return unpack(data, filepath.Join(root, sourceDir))
}

func download(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// unpack extracts the licenses and every SVG of the core package.
func unpack(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	prefix := "solar-icons-" + revision + "/"
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(h.Name, prefix)
		keep := name == "LICENSE" || name == "LICENSE-THIRD-PARTY" ||
			(strings.HasPrefix(name, "packages/core/svgs/") && strings.HasSuffix(name, ".svg"))
		if h.Typeflag != tar.TypeReg || !keep || strings.Contains(name, "..") {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		body, err := io.ReadAll(tr)
		if err == nil {
			err = os.WriteFile(target, body, 0o644)
		}
		if err != nil {
			return err
		}
	}
}

func list(root, find, style string) error {
	matches, err := filepath.Glob(filepath.Join(svgRoot(root), "*", style, "*.svg"))
	if err != nil {
		return err
	}
	var out []string
	for _, m := range matches {
		category := filepath.Base(filepath.Dir(filepath.Dir(m)))
		name := strings.TrimSuffix(filepath.Base(m), ".svg")
		if strings.Contains(name, find) {
			out = append(out, category+"/"+name)
		}
	}
	sort.Strings(out)
	fmt.Println(strings.Join(out, "\n"))
	fmt.Fprintf(os.Stderr, "%d %s icons\n", len(out), style)
	return nil
}

var (
	svgOpen = regexp.MustCompile(`^<svg width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"(?: xmlns:xlink="http://www.w3.org/1999/xlink")?>`)
	spaces  = regexp.MustCompile(`>\s+<`)
	colors  = regexp.MustCompile(`(fill|stroke)="([^"]*)"`)
	entry   = regexp.MustCompile(`^([a-z0-9-]+)/([a-z0-9-]+)(?:@([A-Za-z]+))?$`)
)

// body returns an icon's shapes, painted in currentColor.
func body(data []byte, path string) (string, error) {
	s := strings.TrimSpace(string(data))
	open := svgOpen.FindString(s)
	if open == "" || !strings.HasSuffix(s, "</svg>") {
		return "", fmt.Errorf("%s: unexpected root element", path)
	}
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, open), "</svg>"))
	s = spaces.ReplaceAllString(s, "><")
	var bad error
	s = colors.ReplaceAllStringFunc(s, func(m string) string {
		parts := colors.FindStringSubmatch(m)
		switch parts[2] {
		case "black":
			return parts[1] + `="currentColor"`
		case "white", "none":
			// White only fills clip paths and masks, which keep it.
			return m
		}
		bad = fmt.Errorf("%s: unexpected color %q", path, parts[2])
		return m
	})
	if strings.Contains(s, "`") {
		return "", fmt.Errorf("%s: backquote in SVG", path)
	}
	return s, bad
}

func generate(root string) error {
	manifest, err := os.ReadFile(filepath.Join(root, manifestPath))
	if err != nil {
		return err
	}
	bodies := map[string]string{}
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := entry.FindStringSubmatch(line)
		if m == nil {
			return fmt.Errorf("%s:%d: want category/name or category/name@Style", manifestPath, i+1)
		}
		style, key := "Linear", m[2]
		if m[3] != "" {
			style, key = m[3], m[2]+"@"+strings.ToLower(m[3])
		}
		if !slices.Contains(styles, style) {
			return fmt.Errorf("%s:%d: unknown style %s", manifestPath, i+1, style)
		}
		if _, dup := bodies[key]; dup {
			return fmt.Errorf("%s:%d: %s listed twice", manifestPath, i+1, key)
		}
		data, err := os.ReadFile(filepath.Join(svgRoot(root), m[1], style, m[2]+".svg"))
		if err != nil {
			return fmt.Errorf("%s:%d: %w", manifestPath, i+1, err)
		}
		if bodies[key], err = body(data, line); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(bodies))
	for k := range bodies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by tools/solaricons from Solar Icons %s. DO NOT EDIT.\n\n", revision[:7])
	b.WriteString("package solar\n\n// bodies are the shapes of the icons manifest.txt lists, by name.\nvar bodies = map[string]string{\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "\t%q: `%s`,\n", k, bodies[k])
	}
	b.WriteString("}\n")
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, outputPath), src, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d icons\n", outputPath, len(keys))
	return nil
}
