package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFontPreviewCancelAndCommit(t *testing.T) {
	a, tt := newTestApp(t)
	a.fonts.families = []ui.FontFamily{{Name: "Arial"}, {Name: "SimSun"}}
	a.fonts.loaded = true
	a.openFontPicker(false)
	a.previewFont("SimSun")
	tt.Frame()
	if a.bodyFont() != "SimSun" || a.prefs.BodyFont != "" {
		t.Fatal("preview must not persist choice")
	}
	a.closeFontPicker()
	if a.bodyFont() != "" {
		t.Fatal("cancel did not restore font")
	}
	a.openFontPicker(false)
	a.chooseFont("Arial")
	if a.prefs.BodyFont != "Arial" || len(a.prefs.RecentBodyFonts) != 1 {
		t.Fatal("font choice not saved")
	}
	a.openFontPicker(true)
	a.previewFont("SimSun")
	if a.interfaceFont() != "SimSun" || a.bodyFont() != "Arial" {
		t.Fatal("UI preview changed body font")
	}
	a.closeFontPicker()
	a.openFontPicker(false)
	a.previewFont("")
	if a.bodyFont() != "" {
		t.Fatal("system-font preview kept the saved custom font")
	}
	a.closeFontPicker()
}

func TestFontKeyboardSelectionStaysVisibleAndCancelDoesNotPersist(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabAppearance)
	a.fonts.loaded = true
	for i := 0; i < 80; i++ {
		a.fonts.families = append(a.fonts.families, ui.FontFamily{Name: fmt.Sprintf("Font %03d", i)})
	}
	a.openFontPicker(false)
	tt.Frame()
	tt.Frame()
	for i := 0; i < 35; i++ {
		tt.Key(0, ui.KeyDown)
	}
	tt.Frame()
	selected := a.fontChoices()[a.fonts.index]
	if a.bodyFont() != selected.name || a.prefs.BodyFont != "" {
		t.Fatal("keyboard preview changed saved choice")
	}
	if _, ok := tt.Find(selected.label); !ok {
		t.Fatal("selected font was not rendered in the virtual list")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.fonts.open || a.prefs.BodyFont != "" {
		t.Fatal("escape did not cancel")
	}
}
