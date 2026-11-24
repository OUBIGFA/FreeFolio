package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The tabs of the space icon picker.
const (
	pickerEmoji = iota
	pickerSymbols
	pickerCustom
)

const (
	// customIconRunes bounds the user's own text as an icon, which must fit
	// a 30 DIP square.
	customIconRunes = 3
	// spaceIconLimit bounds a picture chosen as an icon.
	spaceIconLimit = 10 << 20
)

// spaceIconDir holds the pictures chosen as space icons, copied there so
// that the icon stays when the original moves.
func (a *App) spaceIconDir() string { return filepath.Join(a.cfg.ConfigDir, "space-icons") }

// spaceIconImage is the picture named name in spaceIconDir, decoded once,
// or nil for none or one that cannot be read.
func (a *App) spaceIconImage(name string) *ui.Bitmap {
	if name == "" {
		return nil
	}
	if bmp, ok := a.spaceIcons[name]; ok {
		return bmp
	}
	if a.spaceIcons == nil {
		a.spaceIcons = map[string]*ui.Bitmap{}
	}
	var bmp *ui.Bitmap
	// The name comes from spaces.json: only a plain file name is looked up.
	if filepath.Base(name) == name {
		if data, err := os.ReadFile(filepath.Join(a.spaceIconDir(), name)); err == nil {
			bmp, _ = ui.DecodeBitmapScaled(data, 128)
		}
	}
	a.spaceIcons[name] = bmp
	return bmp
}

// importSpaceIcon copies the picture at src into spaceIconDir under a name
// of its content and returns that name.
func (a *App) importSpaceIcon(src string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if info.Size() > spaceIconLimit {
		return "", errSpaceIconTooLarge
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if _, err := ui.DecodeBitmap(data); err != nil {
		return "", errSpaceIconUnreadable
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:8]) + strings.ToLower(filepath.Ext(src))
	if err := os.MkdirAll(a.spaceIconDir(), 0o755); err != nil {
		return "", err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(a.spaceIconDir(), name), data, 0o644); err != nil {
		return "", err
	}
	delete(a.spaceIcons, name)
	return name, nil
}

type spaceIconError string

func (e spaceIconError) Error() string { return i18n.T(string(e)) }

const (
	errSpaceIconTooLarge   spaceIconError = "图片超过 10 MB。"
	errSpaceIconUnreadable spaceIconError = "无法读取这张图片。"
)

// customIcon is the picker's own tab: a short text of the user's, or a
// picture from the disk.
func (a *App) customIcon(c *ui.Context, f *spaceForm) {
	t := c.Theme()
	p := &f.picker
	ui.Column(c.Key("custom-icon")).Grow(1).MinHeight(0).Gap(10).Margin(12, 0, 0, 0).Children(func() {
		ui.Text(c, i18n.T("文字或表情")).FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c.Key("custom-text"), &p.text).Grow(1).MinWidth(0).Height(30).Padding(2, 8).Radius(controlRadius).
				Placeholder(i18n.T("最多 3 个字符")).Label(i18n.T("自定义图标文字")).AutoFocus().
				Background(t.Background.Alpha(0.3)).Border(1, t.Text.Alpha(0.14))
			if in.Changed() {
				p.text = clipRunes(p.text, customIconRunes)
			}
			text := strings.TrimSpace(p.text)
			use := ui.Button(c.Key("custom-use"), i18n.T("使用")).Height(30).Disabled(text == "")
			if text != "" && (use.Clicked() || in.Submitted()) {
				f.icon, f.symbol, f.image = text, "", ""
				f.iconsOpen = false
			}
		})
		ui.Box(c).Height(1).Background(t.Text.Alpha(0.08))
		ui.Text(c, i18n.T("图片")).FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			if bmp := a.spaceIconImage(f.image); bmp != nil {
				ui.Image(c, bmp).Size(36, 36).Radius(8).Shrink(0)
			}
			if ui.Button(c.Key("custom-image"), i18n.T("选择图片…")).Height(30).Clicked() {
				a.pickSpaceIcon(f.id)
			}
		})
		ui.Text(c, i18n.T("PNG、JPG、GIF 等图片，会复制到配置文件夹。")).FontSize(12).TextColor(t.TextMuted).LineHeight(1.4)
	})
}

// clipRunes keeps the first n characters of s.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// pickSpaceIcon asks for a picture and makes it the icon of the space with
// the ID, or of the draft for "". The dialog answers between frames, after
// the picker may have closed: an existing space takes the picture at once
// and its icon editor closes, since the picker commits only choices made
// within its own frame; the draft keeps it in its form.
func (a *App) pickSpaceIcon(id string) {
	got := func(paths []string) {
		if len(paths) == 0 {
			return
		}
		name, err := a.importSpaceIcon(paths[0])
		if err != nil {
			a.notify("设置空间图标失败。%v", err)
			return
		}
		f := &a.spaceForm
		if id == "" {
			if f.open && f.id == "" {
				f.icon, f.symbol, f.image = "", "", name
				f.iconsOpen = false
			}
			return
		}
		if s := a.state.Get(id); s != nil {
			s.SetIcon("", "", name)
			a.saveState()
		}
		if f.open && f.id == id {
			a.cancelSpaceForm()
		}
	}
	if a.pickFiles != nil {
		a.pickFiles(got)
		return
	}
	if a.win == nil {
		return
	}
	win := a.win
	opts := mygo.OpenDialogOptions{Parent: win, Title: i18n.T("选择空间图标"),
		Filters: []mygo.FileFilter{{Name: i18n.T("图片"), Extensions: imageExtensions}}}
	go func() {
		paths, err := mygo.Dialog.Open(opts)
		if err == nil && len(paths) > 0 {
			win.Update(func() { got(paths) })
		}
	}()
}
