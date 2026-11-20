package app

import (
	"fmt"
	"image/color"
	"math"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/prefs"
	"github.com/egoist/mygo/ui"
)

// Measure the painted face, including its outer pixels. A downward shadow
// can make a geometrically centered segment look low inside its track.
func TestSegmentPaintCentered(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, scale := range []float32{1, 1.25, 1.5} {
			t.Run(fmt.Sprintf("dark=%t/scale=%g", dark, scale), func(t *testing.T) {
				selected := 0
				var track ui.Rect
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Row(c).Padding(12).Children(func() {
						track = segmentedChoice(c, &selected, "系统", "衬线", "楷体", "等宽").Bounds()
					})
				}, 340, 80)
				tt.SetDark(dark)
				tt.SetScale(scale)
				for i, name := range []string{"系统", "衬线", "楷体", "等宽"} {
					if i == 0 {
						if err := tt.Click(name); err != nil {
							t.Fatal(err)
						}
					} else {
						tt.Key(0, ui.KeyRight)
					}
					tt.Frame()
					if selected != i {
						t.Fatalf("selected %d, want %d", selected, i)
					}
					img := tt.Image()
					r, ok := tt.Find(name)
					if !ok {
						t.Fatal(name)
					}
					px := float64(img.Bounds().Dx()) / 340
					x := int(float64(r.X-4) * px) // in the segment's padding, clear of the glyphs
					top := int(math.Round(float64(track.Y) * px))
					bottom := int(math.Round(float64(track.Y+track.H)*px)) - 1
					at := func(y int) color.NRGBA { return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA) }
					face := at((top + bottom) / 2)
					if face == at(top+1) {
						t.Fatal("selected face blends into its track")
					}
					first, last := top, bottom
					for first < bottom && at(first) != face {
						first++
					}
					for last > top && at(last) != face {
						last--
					}
					above, below := first-top, bottom-last
					if above < 2 || below < 2 || above-below > 1 || below-above > 1 {
						t.Fatalf("%s: upper and lower insets %d, %d pixels", name, above, below)
					}
				}
			})
		}
	}
}

func TestSidebarPositionAndSettingsExit(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.SetTitleBar(windowsTitleBar)
	for _, side := range []string{"右侧", "左侧", "右侧"} {
		a.openSettings(tabAppearance)
		tt.Frame()
		if err := tt.Click(side); err != nil {
			t.Fatal(err)
		}
		back, ok := tt.Find("返回")
		if !ok || back.X > 200 || back.Y > 80 {
			t.Fatalf("settings back is outside the rail head: %+v", back)
		}
		if err := tt.Click("返回"); err != nil || a.settingsOpen {
			t.Fatalf("exit failed: %v", err)
		}
		settle(tt)
		search, ok := tt.Find("搜索或打开文档")
		wantLeft := side == "左侧"
		if !ok || (search.X < 600) != wantLeft {
			t.Fatalf("sidebar on %s: search at %+v", side, search)
		}
		next, err := New(a.cfg)
		if err != nil || next.prefs.SidebarLeft != wantLeft {
			t.Fatalf("sidebar position after reopening: %v", err)
		}
		a.toggleSidebar()
		tt.Frame()
		edge := float32(1197)
		if wantLeft {
			edge = 3
		}
		tt.Move(1200-edge, 400)
		if a.peek {
			t.Fatal("sidebar peeked from the opposite edge")
		}
		tt.Move(edge, 400)
		if !a.peek {
			t.Fatal("sidebar failed to peek from the chosen edge")
		}
		a.toggleSidebar()
		tt.Move(600, 400)
	}
	tt.SetSize(640, 420)
	a.openSettings(tabAppearance)
	tt.Frame()
	back, ok := tt.Find("返回")
	if !ok || back.Y < 0 || back.Y+back.H > 80 {
		t.Fatalf("minimum window exit: %+v", back)
	}
}

// The miniature window must keep its brand at the leading edge when the
// selected sidebar position changes, just like the application window.
func TestAppearancePreviewBrandStaysAtWindowStart(t *testing.T) {
	a, _ := newTestApp(t)
	tt := ui.NewTester(a.appearancePreview, 680, 350)
	for _, left := range []bool{false, true, false} {
		a.prefs.SidebarLeft = left
		tt.Frame()
		frame, ok := tt.Find("外观预览")
		if !ok {
			t.Fatal("appearance preview is missing")
		}
		logo, ok := tt.Find("FreeMD")
		if !ok || logo.X < frame.X || logo.X > frame.X+40 || logo.Y < frame.Y || logo.Y+logo.H > frame.Y+30 {
			t.Errorf("left=%v: preview brand %+v is not at the start of %+v", left, logo, frame)
		}
		count := 0
		for _, text := range tt.Texts() {
			if text == "FreeMD" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("left=%v: got %d preview brands, want one", left, count)
		}
	}
}

func TestAppearancePreviewTitleCentersOverPage(t *testing.T) {
	a, _ := newTestApp(t)
	a.prefs.SidebarLeft = true
	tt := ui.NewTester(a.appearancePreview, 680, 350)
	title, found := tt.Find("留一点空白.md")
	// 9-DIP frame inset, 136-DIP sidebar, 8-DIP gap and 9-DIP trailing inset.
	center := float32((153 + 671) / 2)
	if !found || math.Abs(float64(title.X+title.W/2-center)) > 1 {
		t.Fatalf("preview title %+v is not centered over the page at %g", title, center)
	}
}

func TestAppearanceFontAndPageWidth(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1500, 860)
	tt.Frame()
	standard := a.doc.ed.Padding()
	a.openSettings(tabAppearance)
	tt.Frame()
	if err := tt.Click("宽"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("衬线"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.Page != "wide" || a.prefs.Font != "serif" {
		t.Fatal("choices", a.prefs.Page, a.prefs.Font)
	}
	saved, err := (prefs.Store{Path: a.prefStore.Path}).Load()
	if err != nil || saved.Page != "wide" || saved.Font != "serif" {
		t.Fatal("saved", saved, err)
	}
	a.settingsOpen = false
	tt.Frame()
	if wide := a.doc.ed.Padding(); wide >= standard {
		t.Fatal("a wide page has narrower margins", wide, standard)
	}
	a.openSettings(tabAppearance)
	tt.Frame()
	// The reset is below the sample: scroll to it.
	scrollIntoView(tt, "恢复默认外观", 1500, 860)
	if err := tt.Click("恢复默认外观"); err != nil {
		t.Fatal(err)
	}
	a.settingsOpen = false
	tt.Frame()
	if a.prefs.Page != "" || a.prefs.Font != "" || a.doc.ed.Padding() != standard {
		t.Fatal("reset")
	}
}

func TestSuggestEmoji(t *testing.T) {
	got := suggestEmoji("smil")
	if len(got) == 0 || got[0].Name != "smile" || got[0].Emoji != "😄" {
		t.Fatal(got)
	}
	if got := suggestEmoji("thumbs"); len(got) == 0 || got[0].Name != "thumbs_up" {
		t.Fatal("catalog names as shortcodes", got)
	}
	if len(suggestEmoji("a")) > 8 {
		t.Fatal("at most eight")
	}
	if shortcode("face with tears of joy") != "face_with_tears_of_joy" || shortcode("OK hand") != "ok_hand" {
		t.Fatal(shortcode("OK hand"))
	}
}
