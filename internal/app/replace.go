package app

// Find and replace across documents (⇧⌘R, the command bar): the text is
// found as the find box finds it, with the same options (case, whole
// words, regular expressions), in the active space or every space, each document listed with how many times and where; 全部替换
// changes every native document, an open one through its editor as one
// step of its history, the others on disk past the conflict check, and a
// toast offers to put them back. Markdown documents are listed but kept
// byte for byte, as everywhere else: they change once converted.

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/search"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/textfind"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// replaceState is the window of the find and replace across documents.
type replaceState struct {
	open       bool
	find, with string
	// everywhere searches every space, not only the active one.
	everywhere bool
	// opts match case, whole words or a regular expression.
	opts textfind.Options
	// matcher is for matcherKey; hits were found for hitsKey in hitsIndex,
	// so the window does not search every document on every frame.
	matcher    *textfind.Matcher
	matcherKey string
	hits       []search.Hit
	hitsKey    string
	hitsIndex  *search.Index
}

// query is what is to be found: trimmed unless a regular expression,
// whose spaces mean something.
func (r *replaceState) query() string {
	if r.opts.Regex {
		return r.find
	}
	return strings.TrimSpace(r.find)
}

// worth reports whether the query is long enough to search for: any valid
// expression, or text search.Worth takes.
func (r *replaceState) worth() bool {
	if r.opts.Regex {
		return r.find != "" && r.findMatcher().Err() == nil
	}
	return search.Worth(r.find)
}

// findMatcher is the matcher of the query and options, made once for them.
func (r *replaceState) findMatcher() *textfind.Matcher {
	key := fmt.Sprint(r.query(), "\x00", r.opts)
	if r.matcher == nil || r.matcherKey != key {
		r.matcher, r.matcherKey = textfind.New(r.query(), r.opts), key
	}
	return r.matcher
}

// replaceLimit is how many documents the window lists and changes at most.
const replaceLimit = 500

// openReplaceAll shows the window, starting from the open document's
// selection when it has one.
func (a *App) openReplaceAll() {
	a.replace.open = true
	if d := a.doc; d != nil && d.loadErr == nil {
		if sel := d.ed.Selection().Range; sel.Start != sel.End {
			if text, _ := d.ed.TextForRange(sel); !strings.ContainsAny(text, "\n ") && utf8.RuneCountInString(text) <= 80 {
				if a.replace.opts.Regex {
					text = regexp.QuoteMeta(text)
				}
				a.replace.find = text
			}
		}
	}
}

// replaceHits are the documents holding what is to be found, in scope.
func (a *App) replaceHits() []search.Hit {
	r := &a.replace
	if !r.worth() {
		return nil
	}
	active := a.state.ActiveSpace()
	scope := ""
	if !r.everywhere && active != nil {
		scope = active.ID
	}
	ix := a.liveIndex()
	m := r.findMatcher()
	key := r.matcherKey + "\x00" + scope + fmt.Sprint(r.everywhere)
	if ix == r.hitsIndex && key == r.hitsKey {
		return r.hits
	}
	var out []search.Hit
	for _, h := range ix.SearchMatcher(m, nil, replaceLimit) {
		if r.everywhere || active != nil && h.Space == active.ID {
			out = append(out, h)
		}
	}
	r.hits, r.hitsKey, r.hitsIndex = out, key, ix
	return out
}

// replaceFold replaces every match of find in s, ignoring case as the
// find box does, and says how many there were.
func replaceFold(s, find, with string) (string, int) {
	return textfind.New(find, textfind.Options{}).Replace(s, with)
}

// replacedFile is a document changed on disk, with its text before and
// after, to put back when nothing changed it since.
type replacedFile struct {
	space, rel    string
	before, after string
}

// replaceEverywhere replaces in every native document of the hits, and
// offers to undo it.
func (a *App) replaceEverywhere() {
	r := &a.replace
	find := r.query()
	hits := a.replaceHits()
	if find == "" || len(hits) == 0 {
		return
	}
	matcher, with := r.findMatcher(), r.with
	resident := map[place]*document{}
	for _, d := range a.residentDocuments() {
		if d.loadErr == nil {
			resident[place{d.space.ID, d.rel}] = d
		}
	}
	count, docs, skipped, failed := 0, 0, 0, 0
	var files []replacedFile
	var edited []*document
	for _, h := range hits {
		sp := a.state.Get(h.Space)
		if sp == nil {
			continue
		}
		n := 0
		change := func(s string) string {
			out, k := matcher.Replace(s, with)
			n += k
			return out
		}
		if vault.IsReadOnly(h.Path) {
			skipped++
			continue
		}
		if d := resident[place{h.Space, h.Path}]; d != nil {
			if d.ed.ReadOnly {
				skipped++
				continue
			}
			if a.mapDocumentText(d, change) {
				count, docs = count+n, docs+1
				edited = append(edited, d)
			}
			continue
		}
		before, err := readFile(sp, h.Path)
		if err != nil {
			failed++
			continue
		}
		d := openDocument(sp, h.Path, before, false)
		if d.loadErr != nil || !a.mapDocumentText(d, change) {
			continue
		}
		if err := d.save(); err != nil {
			failed++
			a.notify("替换 %s 失败。%v", h.Path, err)
			continue
		}
		after, _ := readFile(sp, d.rel)
		files = append(files, replacedFile{sp.ID, d.rel, before, after})
		count, docs = count+n, docs+1
	}
	a.refreshSearch()
	note := ""
	if skipped > 0 {
		note = fmt.Sprintf(i18n.T("，跳过 %d 篇只读文档"), skipped)
	}
	if failed > 0 {
		note += fmt.Sprintf(i18n.T("，%d 篇失败"), failed)
	}
	if docs == 0 {
		a.notify("没有可替换的文档%s。", note)
		return
	}
	a.replace.open = false
	a.offerUndo(fmt.Sprintf(i18n.T("已在 %d 篇文档中替换 %d 处%s"), docs, count, note), "「"+find+"」→「"+with+"」", func() {
		a.undoReplace(files, edited)
	})
}

// undoReplace puts back the documents a replace changed: the open ones
// through their editors' undo, the others when they still hold what the
// replace wrote.
func (a *App) undoReplace(files []replacedFile, edited []*document) {
	for _, d := range edited {
		if d.board != nil {
			d.board.Undo()
			a.boardChanged(d)
		} else {
			d.ed.Undo()
		}
	}
	kept := 0
	for _, f := range files {
		sp := a.state.Get(f.space)
		if sp == nil {
			kept++
			continue
		}
		now, err := readFile(sp, f.rel)
		if err != nil || now != f.after {
			kept++ // changed since: leave it
			continue
		}
		if err := writeFile(sp, f.rel, f.before); err != nil {
			kept++
		}
	}
	a.refreshSearch()
	if kept > 0 {
		a.notify("%d 篇文档在替换后又被修改，没有撤销。", kept)
	}
}

// replaceView is the window.
func (a *App) replaceView(c *ui.Context) {
	r := &a.replace
	if !r.open {
		return
	}
	t := c.Theme()
	w, h := c.Size()
	ui.Modal(c.Key("replace-all"), &r.open, func() {
		ui.Column(c.Key("replace-body")).Size(min(620, w-t.Space(20)), min(520, h-t.Space(20))).Gap(12).Children(func() {
			ui.Text(c, i18n.T("在所有文档中查找和替换")).FontSize(16).FontWeight(600)
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("查找")).Width(36).TextColor(t.TextMuted)
				placeholder := i18n.T("要查找的文字")
				if r.opts.Regex {
					placeholder = i18n.T("正则表达式")
				}
				ui.TextInput(c.Key("replace-find"), &r.find).Grow(1).AutoFocus().Placeholder(placeholder).Label(i18n.T("查找"))
			})
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("替换")).Width(36).TextColor(t.TextMuted)
				placeholder := i18n.T("替换为（留空即删除）")
				if r.opts.Regex {
					placeholder = i18n.T("替换为（可用 $1 引用分组，留空即删除）")
				}
				ui.TextInput(c.Key("replace-with"), &r.with).Grow(1).Placeholder(placeholder).Label(i18n.T("替换为"))
			})
			ui.Row(c.Key("replace-options")).Gap(16).AlignItems(ui.Center).PaddingStart(46).Children(func() {
				ui.Checkbox(c.Key("replace-case"), &r.opts.CaseSensitive, i18n.T("区分大小写"))
				ui.Checkbox(c.Key("replace-word"), &r.opts.WholeWord, i18n.T("全词匹配"))
				ui.Checkbox(c.Key("replace-regex"), &r.opts.Regex, i18n.T("使用正则表达式"))
			})
			scope := 0
			if r.everywhere {
				scope = 1
			}
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("范围")).Width(36).TextColor(t.TextMuted)
				segmentedChoice(c.Key("replace-scope"), &scope, i18n.T("当前空间"), i18n.T("所有空间")).Label(i18n.T("范围"))
			})
			r.everywhere = scope == 1
			hits := a.replaceHits()
			total := 0
			for _, hit := range hits {
				total += hit.Count
			}
			ui.Scroll(c.Key("replace-hits")).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					switch {
					case r.opts.Regex && r.find != "" && r.findMatcher().Err() != nil:
						ui.Text(c, i18n.T("正则表达式无效：")+r.findMatcher().Err().Error()).FontSize(13).TextColor(t.Danger).Padding(16, 4)
					case !r.worth():
						ui.Text(c, i18n.T("输入至少两个字符（或一个汉字）开始查找。")).FontSize(13).TextColor(t.TextMuted).Padding(16, 4)
					case len(hits) == 0:
						ui.Text(c, i18n.T("没有找到「")+r.query()+"」。").FontSize(13).TextColor(t.TextMuted).Padding(16, 4)
					}
					for _, hit := range hits {
						a.replaceRow(c, hit)
					}
				})
			})
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				summary := ""
				if len(hits) > 0 {
					summary = fmt.Sprintf(i18n.T("%d 篇文档，共 %d 处"), len(hits), total)
				}
				ui.Text(c, summary).FontSize(12).TextColor(t.TextMuted).Grow(1)
				if ui.Button(c.Key("replace-cancel"), i18n.T("取消")).Clicked() {
					r.open = false
				}
				if ui.PrimaryButton(c.Key("replace-go"), i18n.T("全部替换")).Disabled(len(hits) == 0).Clicked() {
					a.replaceEverywhere()
				}
			})
		})
	})
}

// replaceRow is a document holding the text: its name, where it is, the
// count and the first match in its line; clicking opens it there.
func (a *App) replaceRow(c *ui.Context, hit search.Hit) {
	t := c.Theme()
	sp := a.state.Get(hit.Space)
	if sp == nil {
		return
	}
	row := ui.ButtonBase(c.Key("replace-hit:" + hit.Space + ":" + hit.Path)).PaddingX(10).PaddingY(6).Gap(10).Radius(8).
		AlignItems(ui.Center).Label(i18n.T("打开 ") + vault.Title(hit.Path)).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row.Background(hoverFill(t))
	}
	row.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, docName(vault.Title(hit.Path))).FontSize(14).SingleLine().Shrink(1).MinWidth(0)
				ui.Text(c, placeHint(sp, hit.Path)).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			})
			ui.RichText(c,
				ui.Span{Text: hit.Snippet[:hit.Match[0]]},
				ui.Span{Text: hit.Snippet[hit.Match[0]:hit.Match[1]], Color: t.Text, Weight: 600, Background: searchMark(t), Radius: 3},
				ui.Span{Text: hit.Snippet[hit.Match[1]:]},
			).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
		})
		ui.Text(c, fmt.Sprintf(i18n.T("%d 处"), hit.Count)).FontSize(12).TextColor(t.TextMuted).NoWrap()
	})
	if row.Clicked() {
		a.replace.open = false
		a.openAtTextWith(hit.Space, hit.Path, a.replace.query(), a.replace.opts)
	}
}

// placeHint is where a document is, for a list: its space and folder.
func placeHint(sp *space.Space, rel string) string {
	if dir := path.Dir(rel); dir != "." {
		return sp.Name + " / " + dir
	}
	return sp.Name
}
