package app

import (
	"slices"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo/ui"
)

func TestNewSpaceColorEditor(t *testing.T) {
	a, tt := newTestApp(t)
	if err := tt.Click("新建空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.spaceForm.open || len(a.spaceForm.theme.Colors) != 1 {
		t.Fatalf("form = %+v", a.spaceForm)
	}
	// The card is MyGo's own color picker: a color typed in hex becomes
	// the space's one color.
	settle(tt)
	typeHex(t, tt, "#c0504d")
	tt.Frame()
	if got := a.spaceForm.theme.Colors; !slices.Equal(got, []string{"#C0504D"}) {
		t.Fatalf("picker gave %v", got)
	}
	a.spaceForm.name = "Reading"
	if err := tt.Click("创建空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	sp := a.state.ActiveSpace()
	if sp.Name != "Reading" || sp.Theme == nil || sp.Color != "#C0504D" {
		t.Fatalf("space = %+v %+v", sp, sp.Theme)
	}
	tt.Advance(time.Second)
	tt.Frame()
	// The chrome stays MyGo's light surface; the color is only the accent.
	if a.look.Dark || hexOf(a.look.opaqueTop()) != hexOf(chromeFill(false)) || hexOf(a.look.accent) == hexOf(ui.LightTheme().Accent) {
		t.Errorf("look = %+v", a.look)
	}
}

func TestEditSpacePreviewsAndSavesOnDismiss(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()
	before := space.ThemeOf(sp)
	a.editSpace(sp)
	a.spaceForm.theme.Colors = []string{"#4E3A5A"}
	tt.Frame()
	want := spaceAccent("#4E3A5A", false)
	if a.look.accent != want || a.look.Dark {
		t.Error("editing should preview the new accent in the window")
	}
	// Dismissing the independent editor commits its preview.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.spaceForm.open || slices.Equal(space.ThemeOf(sp).Colors, before.Colors) || a.look.accent != want {
		t.Fatalf("dismiss did not commit the preview: %+v", space.ThemeOf(sp))
	}
	saved, err := a.store.Load()
	if err != nil || !slices.Equal(space.ThemeOf(saved.Get(sp.ID)).Colors, []string{"#4E3A5A"}) {
		t.Fatalf("theme was not persisted: %v", err)
	}
}

func TestNewSpaceInSidebar(t *testing.T) {
	a, tt := newTestApp(t)
	before := len(a.state.Spaces)
	a.sidebarHidden = true
	if err := tt.Click("新建空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sidebarHidden || !a.spaceForm.cardOpen || !tt.HasText("新建空间") || !tt.HasText("编辑主题") {
		t.Fatalf("draft not shown: hidden %v, card %v", a.sidebarHidden, a.spaceForm.cardOpen)
	}

	// The icon picker finds an emoji by name, keeping the theme card open.
	if err := tt.Click("空间图标"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.spaceForm.picker.query = "books"
	tt.Frame()
	if err := tt.Click("📚 books"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.spaceForm.icon != "📚" || a.spaceForm.symbol != "" || !a.spaceForm.cardOpen {
		t.Fatalf("icon %q symbol %q, card %v", a.spaceForm.icon, a.spaceForm.symbol, a.spaceForm.cardOpen)
	}

	// The Icon tab sets a symbol in place of the emoji.
	tt.Click("空间图标")
	tt.Frame()
	if err := tt.Click("图标集"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("图标 leaf"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.spaceForm.icon != "" || a.spaceForm.symbol != "leaf" {
		t.Fatalf("icon %q symbol %q", a.spaceForm.icon, a.spaceForm.symbol)
	}

	// Escape twice gives the draft up.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.spaceForm.open || len(a.state.Spaces) != before {
		t.Fatalf("draft kept: open %v, %d spaces", a.spaceForm.open, len(a.state.Spaces))
	}

	// Enter in the name field makes the space.
	a.newSpaceForm()
	tt.Frame()
	a.spaceForm.name = "Books"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if sp := a.state.ActiveSpace(); a.spaceForm.open || sp.Name != "Books" || len(a.state.Spaces) != before+1 || sp.Icon == "" {
		t.Fatalf("Enter did not make the space: %+v", sp)
	}
}

func TestNewSpaceThemeButtonTogglesCard(t *testing.T) {
	a, tt := newTestApp(t)
	a.newSpaceForm()
	tt.Frame()
	if !a.spaceForm.cardOpen {
		t.Fatal("the theme card should open with the draft")
	}
	// The button lies in the card's anchor, so a press there toggles the
	// card instead of dismissing it and opening it again.
	for _, want := range []bool{false, true} {
		if err := tt.Click("编辑主题"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.spaceForm.cardOpen != want {
			t.Fatalf("card open = %v, want %v", a.spaceForm.cardOpen, want)
		}
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.spaceForm.open {
		t.Fatal("cancel should drop the draft")
	}
}

// typeHex types a color into the space card's hex field.
func typeHex(t *testing.T, tt *ui.Tester, hex string) {
	t.Helper()
	field, ok := tt.Access("十六进制")
	if !ok {
		t.Fatal("the color card has no hex field")
	}
	tt.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(hex)
}
