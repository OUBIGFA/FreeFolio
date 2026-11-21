package app

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// oneBotQuote is a selection added to OneBot's input as a reference, as
// obsidian-yolo's block mention: a badge naming the file and the length,
// sent as <user_selected_content> with the Markdown source's line numbers,
// the same numbers OneBot's file tools read.
type oneBotQuote struct {
	SpaceID string
	Path    string
	// Block is the first selected top-level block, revealed on a click.
	Block string
	// From and To are the source lines; zero when they cannot be mapped.
	From, To int
	Text     string
}

// addSelectionToOneBot adds d's selection to OneBot's input and opens it.
func (a *App) addSelectionToOneBot(d *document) {
	if d == nil {
		return
	}
	r := d.ed.Selection().Range
	text := d.ed.SelectedMarkdown()
	if r.Start == r.End || strings.TrimSpace(text) == "" {
		a.notify("%s", i18n.T("请先选择需要引用的内容"))
		return
	}
	q := oneBotQuote{SpaceID: d.space.ID, Path: d.rel, Block: d.ed.LeafAt(r.Start).Owner, Text: text}
	last := d.ed.LeafAt(max(r.Start, r.End-1)).Owner
	_, spans := editor.MarkdownSourceSpans(d.ed.Doc)
	for _, s := range spans {
		if s.Block == q.Block && q.From == 0 {
			q.From = s.From
		}
		if s.Block == last {
			q.To = s.To
		}
	}
	if q.From == 0 || q.To < q.From {
		q.From, q.To = 0, 0
	}
	for _, old := range a.qa.quotes {
		if old == q {
			a.qa.open, a.qa.focusInput = true, true
			return
		}
	}
	a.qa.quotes = append(a.qa.quotes, q)
	a.qa.open, a.qa.focusInput = true, true
}

// oneBotQuotePrompt renders the quotes before the question, in
// obsidian-yolo's format; each line carries its source line number.
func oneBotQuotePrompt(quotes []oneBotQuote) string {
	var b strings.Builder
	attr := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	for _, q := range quotes {
		content := q.Text
		fmt.Fprintf(&b, `<user_selected_content path="%s"`, attr.Replace(q.Path))
		if q.From > 0 {
			fmt.Fprintf(&b, ` startLine="%d" endLine="%d"`, q.From, q.To)
			lines := strings.Split(content, "\n")
			for i, line := range lines {
				lines[i] = fmt.Sprintf("%d|%s", q.From+i, line)
			}
			content = strings.Join(lines, "\n")
		}
		fmt.Fprintf(&b, ">\n```%s\n%s\n```\n</user_selected_content>\n", q.Path, content)
	}
	return b.String()
}

// count is the badge's length of the quote: its characters other than
// spaces, which reads the same for Chinese and for English.
func (q oneBotQuote) count() int {
	n := 0
	for _, r := range q.Text {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// revealOneBotQuote opens the quoted document at its first block.
func (a *App) revealOneBotQuote(q oneBotQuote) {
	if !a.openAnywhere(q.SpaceID, q.Path) || a.doc == nil || a.doc.loadErr != nil {
		a.notify("找不到引用的文档：%s", q.Path)
		return
	}
	if a.doc.source {
		a.doc.toggleMode()
	}
	a.doc.ed.RevealBlock(q.Block)
}

// oneBotQuoteBadges shows quotes as badges: file icon, name and length. A
// click opens the source; removable ones carry a × that drops them.
func (a *App) oneBotQuoteBadges(c *ui.Context, key string, quotes []oneBotQuote, removable bool) {
	if len(quotes) == 0 {
		return
	}
	t := c.Theme()
	ui.Row(c.Key(key)).Gap(6).Wrap().Children(func() {
		for i, q := range quotes {
			name := path.Base(q.Path)
			tip := q.Path
			if q.From > 0 {
				tip += fmt.Sprintf(" · L%d–%d", q.From, q.To)
			}
			ui.Row(c.Key(fmt.Sprint(i))).Height(24).Radius(6).AlignItems(ui.Center).
				Background(t.Text.Alpha(0.055)).Border(1, t.Border.Alpha(0.45)).Children(func() {
				body := ui.Row(c.Key("open")).Height(24).PaddingX(7).Gap(4).Radius(6).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("引用：") + name).Tooltip(tip)
				if body.Hovered() {
					body.Background(hoverFill(t))
				}
				body.Children(func() {
					ui.Icon(c, iconFile).FontSize(12).TextColor(t.TextMuted)
					ui.Text(c, name).FontSize(11.5).TextColor(t.Text.Alpha(0.82)).SingleLine().MaxWidth(160)
					ui.Text(c, fmt.Sprintf(i18n.T("(%d 字)"), q.count())).FontSize(11.5).TextColor(t.TextMuted).NoWrap()
				})
				if body.Clicked() {
					a.revealOneBotQuote(q)
				}
				if removable {
					x := ui.Row(c.Key("remove")).Size(20, 24).Radius(6).Center().Cursor(ui.CursorPointer).
						Focusable().Role(ui.RoleButton).Label(i18n.T("移除引用：") + name)
					if x.Hovered() {
						x.Background(hoverFill(t))
					}
					x.Children(func() { ui.Icon(c, iconX).FontSize(11).TextColor(t.TextMuted) })
					if x.Clicked() {
						a.qa.quotes = append(a.qa.quotes[:i:i], a.qa.quotes[i+1:]...)
					}
				}
			})
		}
	})
}
