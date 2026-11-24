package solar

import (
	"image/color"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestIconsDraw parses and draws every embedded icon at a small size.
func TestIconsDraw(t *testing.T) {
	if Count() == 0 {
		t.Fatal("no icons embedded")
	}
	for name := range bodies {
		s := Icon(name)
		if s == nil {
			t.Fatalf("%s: not parsed", name)
		}
		if u := s.Unsupported(); len(u) > 0 {
			t.Errorf("%s: unsupported %v", name, u)
		}
		if w, h := s.Size(); w != 24 || h != 24 {
			t.Errorf("%s: size %vx%v", name, w, h)
		}
		img := s.Image(16, 16, color.Black)
		var ink int
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] > 0 {
				ink++
			}
		}
		if ink == 0 {
			t.Errorf("%s: draws nothing", name)
		}
		if strings.Contains(bodies[name], `"black"`) {
			t.Errorf("%s: keeps black instead of currentColor", name)
		}
	}
	if Icon("no-such-icon") != nil || Has("no-such-icon") {
		t.Fatal("unknown name returned an icon")
	}
	if Icon("check") != Icon("check") {
		t.Fatal("icons are not cached")
	}
}

// TestManifestMatchesGenerated keeps icons_gen.go in step with the manifest.
func TestManifestMatchesGenerated(t *testing.T) {
	data, err := os.ReadFile("manifest.txt")
	if err != nil {
		t.Fatal(err)
	}
	entry := regexp.MustCompile(`^[a-z0-9-]+/([a-z0-9-]+)(?:@([A-Za-z]+))?$`)
	want := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := entry.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("bad manifest line %q", line)
		}
		key := m[1]
		if m[2] != "" {
			key += "@" + strings.ToLower(m[2])
		}
		want[key] = true
	}
	for k := range want {
		if !Has(k) {
			t.Errorf("%s is in manifest.txt but not generated; run go run ./tools/solaricons", k)
		}
	}
	for k := range bodies {
		if !want[k] {
			t.Errorf("%s is generated but not in manifest.txt; run go run ./tools/solaricons", k)
		}
	}
}
