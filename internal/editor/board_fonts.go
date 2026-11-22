package editor

import (
	"bytes"
	"compress/zlib"
	"embed"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/OUBIGFA/FreeMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/font/sfnt"
)

// Font bytes are unchanged from Quiet.md e59c49a's Excalidraw font bundle.
// Source, OFL and Comic Shanns MIT notices are under third_party/whiteboard.
// Xiaolai-Regular.ttf.zlib (~14 MB) is shipped as an external app resource
// rather than embedded inside the executable so cold-start binary scanning
// stays fast.
//
//go:embed boardfonts/Virgil-Regular.ttf boardfonts/CascadiaCode-Regular.ttf boardfonts/Excalifont-Regular.ttf boardfonts/Nunito-Medium.ttf boardfonts/LilitaOne-Regular.ttf boardfonts/ComicShanns-Regular.ttf boardfonts/Assistant-Regular.ttf
var boardFonts embed.FS
var boardFontFiles = map[int]string{1: "Virgil-Regular.ttf", 3: "CascadiaCode-Regular.ttf", 5: "Excalifont-Regular.ttf", 6: "Nunito-Medium.ttf", 7: "LilitaOne-Regular.ttf", 8: "ComicShanns-Regular.ttf", 10: "Assistant-Regular.ttf", 100: "Xiaolai-Regular.ttf.zlib"}

func readExternalBoardFont(name string) ([]byte, error) {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, name),
			filepath.Join(dir, "boardfonts", name),
			filepath.Join(dir, "..", "Resources", name),
			filepath.Join(dir, "..", "Resources", "boardfonts", name),
		)
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Join(filepath.Dir(file), "boardfonts", name))
	}
	candidates = append(candidates, filepath.Join("internal", "editor", "boardfonts", name))
	var lastErr error
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return nil, lastErr
}

type loadedBoardFont struct {
	data   []byte
	parsed *sfnt.Font
	err    error
}

var boardFontMu sync.Mutex
var boardFontCache = map[int]loadedBoardFont{}

func boardFont(id int) loadedBoardFont {
	boardFontMu.Lock()
	defer boardFontMu.Unlock()
	if f, ok := boardFontCache[id]; ok {
		return f
	}
	name, ok := boardFontFiles[id]
	if !ok {
		return loadedBoardFont{}
	}
	var data []byte
	var err error
	if id == 100 {
		data, err = readExternalBoardFont(name)
	} else {
		data, err = boardFonts.ReadFile("boardfonts/" + name)
	}
	if err == nil && id == 100 {
		var z io.ReadCloser
		z, err = zlib.NewReader(bytes.NewReader(data))
		if err == nil {
			data, err = io.ReadAll(io.LimitReader(z, 32<<20))
			z.Close()
		}
	}
	var parsed *sfnt.Font
	if err == nil {
		parsed, err = sfnt.Parse(data)
	}
	if err == nil {
		family := whiteboard.FontName(id)
		if id == 100 {
			family = "Xiaolai"
		}
		err = ui.RegisterFont(data, family)
	}
	f := loadedBoardFont{data, parsed, err}
	boardFontCache[id] = f
	return f
}
func boardFontSpans(text string, font whiteboard.Font) []ui.Span {
	f := boardFont(font.Family)
	family := whiteboard.FontName(font.Family)
	var spans []ui.Span
	var buf sfnt.Buffer
	for _, r := range text {
		use := family
		if font.Family == 5 && f.parsed != nil {
			gid, err := f.parsed.GlyphIndex(&buf, r)
			if err == nil && gid == 0 {
				fallback := boardFont(100)
				if fallback.parsed != nil {
					g, _ := fallback.parsed.GlyphIndex(&buf, r)
					if g != 0 {
						use = "Xiaolai"
					}
				}
			}
		}
		if len(spans) > 0 && spans[len(spans)-1].Font == use {
			spans[len(spans)-1].Text += string(r)
		} else {
			spans = append(spans, ui.Span{Text: string(r), Font: use, Size: float32(font.Size)})
		}
	}
	return spans
}
func boardShapeText(text string, font whiteboard.Font, width float64, align string) *ui.TextLayout {
	a := ui.Start
	if align == "center" {
		a = ui.Center
	} else if align == "right" {
		a = ui.End
	}
	return ui.ShapeRichTextWith(boardFontSpans(text, font), ui.Font{Family: whiteboard.FontName(font.Family), Size: float32(font.Size)}, ui.TextShape{Width: float32(width), Align: a})
}
func boardMeasure(text string, font whiteboard.Font) float64 {
	layout := boardShapeText(text, font, 0, "left")
	w, _ := layout.Size()
	return float64(w)
}
func BoardFontData(id int) ([]byte, error) {
	f := boardFont(id)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.data) == 0 {
		return nil, fmt.Errorf("font %d uses a system face", id)
	}
	return f.data, nil
}

// BoardFontCSS embeds only the glyphs needed by this export. Chinese fallback
// is subset independently so one short label does not embed the entire font.
func BoardFontCSS(scene *whiteboard.Scene) (string, error) {
	chars := map[int][]rune{}
	for _, e := range scene.Elements {
		if e.IsDeleted || e.Kind != whiteboard.KindText {
			continue
		}
		id := e.FontFamily
		if id == 0 {
			id = 5
		}
		if boardFontFiles[id] == "" {
			continue
		}
		f := boardFont(id)
		if f.err != nil {
			return "", f.err
		}
		var buf sfnt.Buffer
		for _, r := range e.Text {
			target := id
			if id == 5 {
				g, _ := f.parsed.GlyphIndex(&buf, r)
				if g == 0 {
					target = 100
				}
			}
			chars[target] = append(chars[target], r)
		}
	}
	var css strings.Builder
	for id, rs := range chars {
		f := boardFont(id)
		if f.err != nil {
			return "", f.err
		}
		sub, err := whiteboard.SubsetFont(f.data, rs, id != 100)
		if err != nil {
			return "", err
		}
		if len(sub) == 0 {
			continue
		}
		name := whiteboard.FontName(id)
		if id == 100 {
			name = "Xiaolai"
		}
		fmt.Fprintf(&css, "@font-face{font-family:'%s';src:url(data:font/ttf;base64,%s) format('truetype');}\n", name, base64.StdEncoding.EncodeToString(sub))
	}
	return css.String(), nil
}

func BoardSVG(scene *whiteboard.Scene, o BoardOptions) (string, error) {
	css, err := BoardFontCSS(scene)
	if err != nil {
		return "", err
	}
	return whiteboard.ExportSVG(scene, whiteboard.SVGOptions{EmbedScene: true, FontCSS: css, ResolveImage: func(f *whiteboard.FileData) (string, error) {
		if f.DataURL != "" {
			return f.DataURL, nil
		}
		data, err := boardFileBytes(f, o)
		if err != nil {
			return "", err
		}
		return boardDataURL(f.MIMEType, data), nil
	}})
}
