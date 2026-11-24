package editor

import (
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/mermaid"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func isSafeLangClass(lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	for i := 0; i < len(lang); i++ {
		c := lang[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

func safeHTMLURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https", "mailto", "file", "tel":
		return true
	}
	return false
}

// ExportHTML supplies semantic clipboard content without depending on a web
// runtime. Escaping also keeps raw Markdown/TeX and untrusted links inert.
// Code blocks keep their text and Mermaid blocks their source: a clipboard
// has no page style sheet to color or place them.
func ExportHTML(doc *document.Document) string { return exportHTML(doc, nil) }

// HTMLStyle is the paper an exported page draws with: the document theme and
// the page's colors beyond it.
type HTMLStyle struct {
	Theme *ui.Theme
	Page  Page
}

// HTMLTheme is the pair of papers an exported page follows: the light one,
// and the dark one a reader whose system prefers dark gets.
type HTMLTheme struct {
	Light, Dark HTMLStyle
}

// ExportHTMLPage renders the body of an exported page in its paper theme:
// code blocks keep the editor's highlighting and Mermaid diagrams are drawn
// as inline SVG, one picture for each system appearance (ArcMD 8f593e5).
func ExportHTMLPage(doc *document.Document, theme HTMLTheme) string {
	if theme.Light.Theme == nil {
		theme.Light.Theme = ui.LightTheme()
	}
	if theme.Dark.Theme == nil {
		theme.Dark.Theme = ui.DarkTheme()
	}
	return exportHTML(doc, &theme)
}

// exportHTML writes a document's blocks; a nil theme is the clipboard's
// plain HTML, which has no page style sheet.
func exportHTML(doc *document.Document, theme *HTMLTheme) string {
	var blocks func([]*document.Block) string
	runs := func(rs []document.Run) string {
		return exportHTMLRuns(rs, false)
	}
	blocks = func(bs []*document.Block) string {
		var out strings.Builder
		for i := 0; i < len(bs); i++ {
			b := bs[i]
			body := runs(b.Runs)
			tag := "p"
			switch b.Kind {
			case "heading":
				tag = fmt.Sprintf("h%d", max(1, min(6, b.Level)))
			case "quote", "callout":
				tag = "blockquote"
			case "bullet", "numbered", "task":
				var list func(int)
				list = func(depth int) {
					kind := bs[i].Kind
					listTag := "ul"
					if kind == "numbered" {
						listTag = "ol"
					}
					out.WriteString("<" + listTag + ">")
					for i < len(bs) && bs[i].Indent == depth && bs[i].Kind == kind {
						item := bs[i]
						out.WriteString("<li>")
						if item.Kind == "task" {
							if item.Checked {
								out.WriteString("☑ ")
							} else {
								out.WriteString("☐ ")
							}
						}
						out.WriteString(runs(item.Runs))
						i++
						for i < len(bs) && isHTMLList(bs[i]) && bs[i].Indent > depth {
							list(bs[i].Indent)
						}
						out.WriteString("</li>")
					}
					out.WriteString("</" + listTag + ">")
				}
				list(b.Indent)
				i--
				continue
			case "code":
				if isWhiteboardBlock(b) {
					if scene, err := whiteboard.Decode(b.Text()); err == nil {
						if svg, err := whiteboard.ExportSVG(scene, whiteboard.SVGOptions{EmbedScene: true}); err == nil {
							out.WriteString(svg)
							continue
						}
					}
				}
				if theme != nil && normalizeLexLang(b.Language) == langMermaid {
					if svg, ok := mermaidSVG(b.Text(), *theme); ok {
						out.WriteString(`<figure class="freefolio-mermaid">` + svg + `</figure>`)
						continue
					}
				}
				classAttr := ""
				if isSafeLangClass(b.Language) {
					classAttr = ` class="language-` + html.EscapeString(strings.ToLower(strings.TrimSpace(b.Language))) + `"`
				}
				code := html.EscapeString(b.Text())
				if theme != nil {
					code = htmlCode(b.Text(), b.Language)
				}
				out.WriteString("<pre><code" + classAttr + ">" + code + "</code></pre>")
				continue
			case "toggle":
				var inner []*document.Block
				for i+1 < len(bs) && bs[i+1].Kind != "heading" && bs[i+1].Indent > b.Indent {
					c := *bs[i+1]
					c.Indent -= b.Indent + 1
					inner = append(inner, &c)
					i++
				}
				open := ""
				if !b.Folded {
					open = " open"
				}
				out.WriteString("<details" + open + "><summary>" + body + "</summary>" + blocks(inner) + "</details>")
				continue
			case "raw":
				if isHTMLCommentBlock(b.Text()) {
					continue
				}
				tag = "pre"
				body = html.EscapeString(b.Text())
			case "math":
				tag = "pre"
				body = "$$\n" + html.EscapeString(b.Text()) + "\n$$"
			case "rule":
				out.WriteString("<hr>")
				continue
			case "image":
				if safeHTMLURL(b.URL) {
					out.WriteString("<img src=\"" + html.EscapeString(b.URL) + "\" alt=\"" + html.EscapeString(b.Text()) + "\"")
					if b.ImageWidth > 0 {
						fmt.Fprintf(&out, " width=\"%.0f\"", b.ImageWidth)
					}
					out.WriteString(">")
				}
				continue
			case "table":
				out.WriteString("<table><tbody>")
				covered := document.CoveredCells(b)
				for i, row := range b.Children {
					out.WriteString("<tr>")
					for _, cell := range row.Children {
						if covered[cell] {
							continue
						}
						tag := "td"
						if i == 0 {
							tag = "th"
						}
						cols, rows := document.Span(cell)
						fmt.Fprintf(&out, "<%s colspan=\"%d\" rowspan=\"%d\">%s</%s>", tag, cols, rows, runs(cell.Runs), tag)
					}
					out.WriteString("</tr>")
				}
				out.WriteString("</tbody></table>")
				continue
			case "columns":
				ratios := columnRatioStrings(b)
				if len(ratios) == len(b.Children) {
					fmt.Fprintf(&out, `<div class="freefolio-columns" data-widths="%s" style="display:flex;gap:24px">`, strings.Join(ratios, ","))
				} else {
					out.WriteString(`<div class="freefolio-columns" style="display:flex;gap:24px">`)
				}
				for ci, col := range b.Children {
					flexStyle := "flex:1;min-width:0"
					if len(ratios) == len(b.Children) {
						flexStyle = "flex:" + ratios[ci] + ";min-width:0"
					}
					fmt.Fprintf(&out, `<div class="freefolio-column" style="%s">%s</div>`, flexStyle, blocks(col.Children))
				}
				out.WriteString(`</div>`)
				continue
			case "column":
				out.WriteString(blocks(b.Children))
				continue
			case "footnote":
				body = "[" + html.EscapeString(b.NoteLabel) + "] " + body
			}
			attrs := ""
			if (b.Align == "center" || b.Align == "right") && tag != "pre" {
				attrs = ` style="text-align:` + b.Align + `"`
			}
			nest := 1
			if b.Kind == "quote" {
				nest += min(b.Indent, 6)
			}
			out.WriteString(strings.Repeat("<"+tag+attrs+">", nest) + body + strings.Repeat("</"+tag+">", nest))
		}
		return out.String()
	}
	return "<html><body><!--StartFragment-->" + blocks(doc.Blocks) + "<!--EndFragment--></body></html>"
}

func isHTMLList(b *document.Block) bool {
	return b.Kind == "bullet" || b.Kind == "numbered" || b.Kind == "task"
}

// htmlLexerPage gives every token kind a color of its own so htmlCode can
// recover the kind from the lexer's spans. These colors never reach the
// page: the exported page's CSS colors the classes instead.
var htmlLexerPage = Page{
	CodeText: ui.RGB(0, 0, 0),
	Tokens: CodeTokens{
		Comment:  ui.RGB(1, 0, 0),
		Keyword:  ui.RGB(2, 0, 0),
		String:   ui.RGB(3, 0, 0),
		Number:   ui.RGB(4, 0, 0),
		Type:     ui.RGB(5, 0, 0),
		Function: ui.RGB(6, 0, 0),
	},
}

// htmlTokenClasses are the token classes htmlCode emits, by their lexer
// color, and the CodeTokens field CodeTokenCSS colors each with.
var htmlTokenClasses = []struct {
	name  string
	lexed ui.Color
	color func(CodeTokens) ui.Color
}{
	{"comment", htmlLexerPage.Tokens.Comment, func(t CodeTokens) ui.Color { return t.Comment }},
	{"keyword", htmlLexerPage.Tokens.Keyword, func(t CodeTokens) ui.Color { return t.Keyword }},
	{"string", htmlLexerPage.Tokens.String, func(t CodeTokens) ui.Color { return t.String }},
	{"number", htmlLexerPage.Tokens.Number, func(t CodeTokens) ui.Color { return t.Number }},
	{"type", htmlLexerPage.Tokens.Type, func(t CodeTokens) ui.Color { return t.Type }},
	{"function", htmlLexerPage.Tokens.Function, func(t CodeTokens) ui.Color { return t.Function }},
}

// htmlCode renders a code block with its tokens wrapped in the classes the
// exported page's style sheet colors, so a shared file keeps its
// highlighting and follows the reader's light or dark preference.
func htmlCode(text, language string) string {
	var out strings.Builder
	for _, sp := range codeSpansFor("", language, text, htmlLexerPage, nil) {
		class := ""
		for _, c := range htmlTokenClasses {
			if c.lexed == sp.Color {
				class = "freefolio-tok-" + c.name
			}
		}
		if class == "" {
			out.WriteString(html.EscapeString(sp.Text))
			continue
		}
		out.WriteString(`<span class="` + class + `">` + html.EscapeString(sp.Text) + `</span>`)
	}
	return out.String()
}

// CodeTokenCSS writes the rules that color the token classes htmlCode
// emits, from the page tokens the editor paints with.
func CodeTokenCSS(tokens CodeTokens) string {
	var b strings.Builder
	for _, c := range htmlTokenClasses {
		fmt.Fprintf(&b, "pre code .freefolio-tok-%s { color: %s; }\n", c.name, htmlColor(c.color(tokens)))
	}
	return b.String()
}

// mermaidSVG lays a Mermaid block out with the system font's metrics and
// draws it as inline SVG in the colors the editor paints it with, once for
// each appearance; the page's CSS shows one. A block FreeFolio cannot draw
// keeps its source.
func mermaidSVG(src string, theme HTMLTheme) (string, bool) {
	diagram, err := mermaid.ParseAndLayout(src, exportMermaidMeasure)
	if err != nil || diagram.W <= 0 || diagram.H <= 0 {
		return "", false
	}
	alt := diagramTitle(diagram.Kind)
	light := mermaid.ExportSVG(diagram, exportMermaidStyle(theme.Light), alt)
	dark := mermaid.ExportSVG(diagram, exportMermaidStyle(theme.Dark), alt)
	return `<span class="freefolio-diagram-light">` + light + `</span><span class="freefolio-diagram-dark">` + dark + `</span>`, true
}

// exportMermaidMeasure measures a label with the system font the exported
// page shows, so the boxes and their text agree.
func exportMermaidMeasure(text string, size float32, bold bool) (w, h float32) {
	if text == "" {
		return 0, size * 1.3
	}
	weight := 400
	if bold {
		weight = 600
	}
	return ui.ShapeText(text, ui.Font{Family: mermaid.FontFamily, Size: size, Weight: weight}, 0).Size()
}

// exportMermaidStyle colors an exported diagram with the editor's own
// mermaid colors on the page's paper.
func exportMermaidStyle(style HTMLStyle) mermaid.Style {
	t := style.Theme
	pg := pageOf(style.Page, t)
	blue, yellow := mermaidBlueCallout(pg), pg.Callouts["yellow"]
	return mermaid.Style{
		Paper: htmlColor(t.Background),
		Shape: func(s mermaid.ShapeOp) (string, string, float64) {
			fill, border, w := mermaidShapeColors(s, t, pg, blue, yellow)
			return htmlColor(fill), htmlColor(border), float64(w)
		},
		Path: func(op mermaid.PathOp) (string, float64) {
			c, w := mermaidPathStyle(op, t, pg)
			return htmlColor(c), float64(w)
		},
		Label: func(l mermaid.LabelOp) string { return htmlColor(mermaidLabelColor(l, t, pg)) },
		Back:  isMermaidBackShape,
	}
}

// htmlColor writes a color as a CSS value.
func htmlColor(c ui.Color) string {
	s := c.SRGB()
	if s.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", s.R, s.G, s.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", s.R, s.G, s.B, float64(s.A)/255)
}
