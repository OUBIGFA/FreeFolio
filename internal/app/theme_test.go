package app

import (
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The paper follows the system appearance, back and forth, in the editor
// and the Markdown preview alike.
func TestPaperFollowsSystemAppearance(t *testing.T) {
	a, tt := newTestApp(t)
	for _, dark := range []bool{false, true, false} {
		tt.SetDark(dark)
		tt.Frame()
		tt.Frame()
		if a.docTheme.Dark != dark {
			t.Fatal("test did not apply the system appearance")
		}
		for _, source := range []bool{false, true} {
			a.doc.source = source
			tt.Frame()
			label := "文档"
			if source {
				label = "Markdown 源码编辑区"
			}
			bounds, ok := tt.Find(label)
			if !ok {
				t.Fatal("writing surface missing")
			}
			pixel := color.NRGBAModel.Convert(tt.Image().At(int(bounds.X+bounds.W-20), int(bounds.Y+bounds.H-20))).(color.NRGBA)
			want := documentTheme(dark).Background
			if pixel.R != want.R || pixel.G != want.G || pixel.B != want.B {
				t.Fatalf("dark=%v source=%v paper=%v, want %v", dark, source, pixel, want)
			}
		}
	}
	if theme := documentTheme(false); theme.Dark || theme.Text != ui.Hex("#1F2329") {
		t.Fatal("light paper did not use the light text palette")
	}
	if theme := documentTheme(true); !theme.Dark || theme.Background != ui.Hex("#1F2023") {
		t.Fatal("dark paper did not use the dark palette")
	}
}

// Every color a document keeps by name has a color on dark paper too, so
// nothing falls back to the theme's generic one there.
func TestDarkPageKeepsNamedColors(t *testing.T) {
	light, dark := pageStyle(false), pageStyle(true)
	for name, m := range map[string][2]map[string]ui.Color{
		"text colors": {light.TextColors, dark.TextColors},
		"highlights":  {light.Highlights, dark.Highlights},
		"cell fills":  {light.CellFills, dark.CellFills},
		"tints":       {light.Tints, dark.Tints},
	} {
		for k := range m[0] {
			if _, ok := m[1][k]; !ok {
				t.Errorf("%s: %q missing on dark paper", name, k)
			}
		}
	}
	for k := range light.Callouts {
		if _, ok := dark.Callouts[k]; !ok {
			t.Errorf("callout %q missing on dark paper", k)
		}
	}
}

// Exercise both endpoints of every preset, including selected tree rows.
func TestSpaceChromeContrast(t *testing.T) {
	lum := func(c ui.Color) float64 {
		channel := func(v uint8) float64 {
			n := float64(v) / 255
			if n <= 0.04045 {
				return n / 12.92
			}
			return math.Pow((n+0.055)/1.055, 2.4)
		}
		return .2126*channel(c.R) + .7152*channel(c.G) + .0722*channel(c.B)
	}
	lowest := math.Inf(1)
	for _, page := range space.PresetPages {
		for _, colors := range page.Swatches {
			for _, dark := range []bool{false, true} {
				for _, intensity := range []float64{0, 1} {
					l := lookOf(space.Theme{Colors: colors, Intensity: intensity}, dark)
					for _, stop := range l.stops {
						for _, bg := range []ui.Color{stop, l.accent.Alpha(.12).Over(stop)} {
							for _, fg := range []ui.Color{l.text, l.muted, l.accent} {
								a, b := lum(fg.Over(bg)), lum(bg)
								ratio := (max(a, b) + .05) / (min(a, b) + .05)
								lowest = min(lowest, ratio)
								if ratio < 4.5 {
									t.Fatalf("preset %s %v dark=%v ratio %.2f", page.Name, colors, dark, ratio)
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("Minimum text and icon contrast %.2f:1", lowest)
}
