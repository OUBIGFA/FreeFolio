package app

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFontPreviewCancelAndCommit(t *testing.T) {
	a, tt := newTestApp(t)
	a.fonts.families = []ui.FontFamily{{Name: "Arial"}, {Name: "SimSun"}}
	a.fonts.loaded = true
	a.fonts.openFontPicker(false)
	a.fonts.previewFont("SimSun")
	tt.Frame()
	if a.fonts.bodyFont() != "SimSun" || a.prefs.BodyFont != "" {
		t.Fatal("preview must not persist choice")
	}
	a.fonts.closeFontPicker()
	if a.fonts.bodyFont() != "" {
		t.Fatal("cancel did not restore font")
	}
	a.fonts.openFontPicker(false)
	a.fonts.chooseFont("Arial")
	if a.prefs.BodyFont != "Arial" || len(a.prefs.RecentBodyFonts) != 1 {
		t.Fatal("font choice not saved")
	}
	a.fonts.openFontPicker(true)
	a.fonts.previewFont("SimSun")
	if a.fonts.interfaceFont() != "SimSun" || a.fonts.bodyFont() != "Arial" {
		t.Fatal("UI preview changed body font")
	}
	a.fonts.closeFontPicker()
	a.fonts.openFontPicker(false)
	a.fonts.previewFont("")
	if a.fonts.bodyFont() != "" {
		t.Fatal("system-font preview kept the saved custom font")
	}
	a.fonts.closeFontPicker()
}

func TestFontKeyboardSelectionStaysVisibleAndCancelDoesNotPersist(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabAppearance)
	a.fonts.loaded = true
	for i := 0; i < 80; i++ {
		a.fonts.families = append(a.fonts.families, ui.FontFamily{Name: fmt.Sprintf("Font %03d", i)})
	}
	a.fonts.openFontPicker(false)
	tt.Frame()
	tt.Frame()
	for i := 0; i < 35; i++ {
		tt.Key(0, ui.KeyDown)
	}
	tt.Frame()
	selected := a.fonts.fontChoices()[a.fonts.index]
	if a.fonts.bodyFont() != selected.name || a.prefs.BodyFont != "" {
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

func TestFontUIPreviewButtonsDoNotJumpAndUseNewSpecimen(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1280, 820)
	a.openSettings(tabAppearance)
	a.fonts.loaded = true
	a.fonts.families = []ui.FontFamily{
		{Name: "Arial"},
		{Name: "Georgia"},
		{Name: "Microsoft YaHei"},
		{Name: "SimSun"},
		{Name: "Consolas", Monospaced: true},
	}
	a.fonts.openFontPicker(true)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("自由的文字胜于一切 · Abc 123") {
		t.Fatalf("missing updated specimen copy: %q", tt.Texts())
	}
	if tt.HasText("春风与笔记 · Abc 123") {
		t.Fatal("old specimen copy still present")
	}
	wantSettings, ok := tt.Find("设置")
	if !ok {
		t.Fatal("preview settings button missing")
	}
	wantSave, ok := tt.Find("保存")
	if !ok {
		t.Fatal("preview save button missing")
	}
	sameCenter := func(a, b ui.Rect) bool {
		return math.Abs(float64((a.X+a.W/2)-(b.X+b.W/2))) < 1.0 &&
			math.Abs(float64((a.Y+a.H/2)-(b.Y+b.H/2))) < 1.0
	}
	for _, family := range []string{"Arial", "Georgia", "Microsoft YaHei", "SimSun", "Consolas", ""} {
		a.fonts.previewFont(family)
		tt.Frame()
		tt.Frame()
		gotSettings, ok := tt.Find("设置")
		if !ok || !sameCenter(gotSettings, wantSettings) {
			t.Fatalf("settings button jumped for %q: got %+v, want %+v", family, gotSettings, wantSettings)
		}
		gotSave, ok := tt.Find("保存")
		if !ok || !sameCenter(gotSave, wantSave) {
			t.Fatalf("save button jumped for %q: got %+v, want %+v", family, gotSave, wantSave)
		}
	}
	if err := tt.Click("等宽"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	rows := a.fonts.fontChoices()
	if len(rows) != 1 || rows[0].name != "Consolas" {
		t.Fatalf("category filter failed: %+v", rows)
	}
}
