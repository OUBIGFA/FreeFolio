package app

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/fuzzy"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/search"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/OUBIGFA/FreeFolio/internal/wiki"
	"github.com/egoist/mygo/ui"
)

// linksHost is what wiki links, tags and the page footer need from the window.
type linksHost interface {
	activateDocument(d *document) bool
	activeDocument() *document
	activeTree() []*vault.Node
	currentDir() string
	docHint(id, rel string) string
	flushForTask() bool
	indexedIcon(spaceID, rel string) string
	indexedLive(d *document) (liveStamp, bool)
	liveIndex() *search.Index
	mapDocumentText(d *document, fn func(string) string) bool
	navigateInPane(d *document, fn func())
	notify(format string, args ...any)
	open(rel string)
	openAnywhere(id, rel string) bool
	openTagRename(from string)
	paperIsDark() bool
	queryPalette(q string)
	refreshSearch()
	refreshTree()
	residentDocument(id, rel string) *document
	residentDocuments() []*document
	setPaletteQuery(q string)
	spaces() *space.State
	tagCount(name string) int
	treeOf(sp *space.Space) ([]*vault.Node, error)
}

// linkFeature resolves wiki links and tags, keeps them in step with renames,
// and draws the page footer of backlinks.
type linkFeature struct {
	host linksHost
}

// Links between documents are [[Name]] in their text (package wiki): ⌘-click
// opens the document named, or makes it when there is none; typing [[
// offers the documents; below each document are the ones linking to it;
// renaming a document renames the links to it.

// eachDoc calls fn with every document of every space, the active space's
// first, until fn returns false.
func (lk *linkFeature) eachDoc(fn func(sp *space.Space, n *vault.Node) bool) {
	active := lk.host.spaces().ActiveSpace()
	order := slices.Clone(lk.host.spaces().Spaces)
	slices.SortStableFunc(order, func(x, y *space.Space) int {
		return b2i(y == active) - b2i(x == active)
	})
	for _, sp := range order {
		var nodes []*vault.Node
		if sp == active {
			nodes = lk.host.activeTree()
		} else {
			nodes, _ = lk.host.treeOf(sp)
		}
		stop := false
		var walk func([]*vault.Node)
		walk = func(nodes []*vault.Node) {
			for _, n := range nodes {
				if stop {
					return
				}
				if n.Dir {
					walk(n.Children)
				} else if !fn(sp, n) {
					stop = true
				}
			}
		}
		walk(nodes)
		if stop {
			return
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// findNamed resolves relative to the document's own space, even when a
// favorite is displayed in another space. Other spaces keep their saved order.
func (lk *linkFeature) findNamed(name string) (*space.Space, string, bool) {
	id := lk.host.spaces().ActiveSpace().ID
	if lk.host.activeDocument() != nil {
		id = lk.host.activeDocument().space.ID
	}
	return lk.findNamedFrom(id, name)
}

func (lk *linkFeature) findNamedFrom(id, name string) (*space.Space, string, bool) {
	var candidates []place
	lk.eachDoc(func(sp *space.Space, n *vault.Node) bool {
		if wiki.Same(n.Title(), name) {
			candidates = append(candidates, place{sp.ID, n.Path})
		}
		return true
	})
	priority := func(spaceID string) int {
		if spaceID == id {
			return -1
		}
		return slices.IndexFunc(lk.host.spaces().Spaces, func(sp *space.Space) bool { return sp.ID == spaceID })
	}
	slices.SortFunc(candidates, func(x, y place) int {
		if d := priority(x.space) - priority(y.space); d != 0 {
			return d
		}
		return strings.Compare(x.rel, y.rel)
	})
	if len(candidates) == 0 {
		return nil, "", false
	}
	return lk.host.spaces().Get(candidates[0].space), candidates[0].rel, true
}

// renameSources captures resolution before the file name changes. Identical
// labels in another space must keep referring to that space's own document.
func (lk *linkFeature) renameSources(sp *space.Space, rel string) map[string]bool {
	allowed := map[string]bool{}
	for _, source := range lk.host.spaces().Spaces {
		target, file, ok := lk.findNamedFrom(source.ID, vault.Title(rel))
		allowed[source.ID] = ok && target == sp && file == rel
	}
	return allowed
}

// openWiki opens the document a [[link]] names, or makes it beside the
// open document when there is none.
func (lk *linkFeature) openWiki(name string) {
	if sp, rel, ok := lk.findNamed(name); ok {
		lk.host.openAnywhere(sp.ID, rel)
		return
	}
	lk.newDocNamed(lk.host.currentDir(), name)
}

// newDocNamed makes a document titled name in the folder dir of the active
// space and opens it, its title written.
func (lk *linkFeature) newDocNamed(dir, name string) {
	sp := lk.host.spaces().ActiveSpace()
	if sp == nil || sp.IsExternal() || !lk.host.flushForTask() {
		return
	}
	doc := model.New()
	title := model.NewBlock("heading", strings.TrimSpace(name))
	title.Level = 1
	doc.Blocks = []*model.Block{title, model.NewBlock("paragraph", "")}
	err := doc.Validate()
	if err == nil {
		text := editor.ExportMarkdown(doc)
		var rel string
		if rel, err = vault.CreateFile(sp.Root, dir, name, ".md", text); err == nil {
			lk.host.refreshTree()
			lk.host.open(rel)
			if lk.host.activeDocument() != nil && lk.host.activeDocument().rel == rel {
				lk.host.activeDocument().ensureBodyParagraph()
				end := lk.host.activeDocument().ed.Len()
				lk.host.activeDocument().ed.SetSelection(end, end)
				lk.host.activeDocument().ed.RequestFocus = true
			}
			return
		}
	}
	lk.host.notify("新建「%s」失败；%v", name, err)
}

func (lk *linkFeature) wikiSuggestFor(d *document, typed string) []editor.WikiItem {
	type scored struct {
		item  editor.WikiItem
		score int
	}
	var out []scored
	seen := map[string]bool{}
	self := ""
	if d != nil {
		self = d.title()
	}
	lk.eachDoc(func(sp *space.Space, n *vault.Node) bool {
		title := n.Title()
		key := strings.ToLower(title)
		if seen[key] || wiki.Same(title, self) && typed == "" {
			return true
		}
		s := 0
		if typed != "" {
			var ok bool
			if s, ok = fuzzy.Score(typed, title); !ok {
				return true
			}
		}
		seen[key] = true
		hint := sp.Name
		if dir := path.Dir(n.Path); dir != "." {
			hint += " / " + dir
		}
		out = append(out, scored{editor.WikiItem{Name: title, Hint: hint}, s})
		return true
	})
	slices.SortStableFunc(out, func(x, y scored) int { return y.score - x.score })
	items := make([]editor.WikiItem, 0, 8)
	for _, s := range out[:min(len(out), 8)] {
		items = append(items, s.item)
	}
	return items
}

// openTag shows the documents of a #tag in the command bar.
func (lk *linkFeature) openTag(tag string) {
	lk.host.queryPalette("#" + tag)
}

// tagResults are the command bar's results for a query starting with #:
// the documents of the tag typed, then the tags matching it.
func (lk *linkFeature) tagResults(q string) []action {
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(q), "#"))
	var out []action
	if name != "" && lk.host.tagCount(name) > 0 {
		tag := name
		out = append(out, action{kind: i18n.T("命令"), title: i18n.T("重命名或合并标签 #") + tag, run: func() { lk.host.openTagRename(tag) }})
	}
	if name != "" {
		for _, h := range lk.host.liveIndex().Tagged(name, nil) {
			id, rel := h.Space, h.Path
			hint := lk.host.docHint(id, rel)
			out = append(out, action{kind: i18n.T("标签"), title: vault.Title(rel), hint: hint, doc: place{id, rel},
				snippet: h.Snippet, match: h.Match, run: func() { lk.host.openAnywhere(id, rel) }})
		}
	}
	for _, tc := range lk.host.liveIndex().Tags(nil) {
		if name != "" {
			if _, ok := fuzzy.Score(name, tc.Name); !ok || strings.EqualFold(tc.Name, name) {
				continue
			}
		}
		tag := tc.Name
		out = append(out, action{kind: i18n.T("标签"), title: "#" + tag, hint: fmt.Sprintf(i18n.T("%d 篇文档"), tc.Count), run: func() {
			lk.host.setPaletteQuery("#" + tag)
		}})
	}
	return out
}

// Footer content follows document/index revisions, not redraws or pane widths.
type pageFooterCache struct {
	ready    bool
	index    *search.Index
	location place
	tags     []string
	hits     []search.Hit
}

// footerSettle is how long footers wait for typing on a long page to
// pause before they reread it: the page's tags and the live search index
// both need its whole text, which on a long page costs more than a frame.
const footerSettle = 400 * time.Millisecond

// footerLong is how many bytes of text make a page long enough for the
// footers to wait for it; a shorter page is reread at each keystroke.
const footerLong = 64 << 10

// settledFooter is footerData, except that while a long page is being
// typed in, footers keep what they last read until the typing pauses.
func (lk *linkFeature) settledFooter(c *ui.Context, d *document) *pageFooterCache {
	cache := &d.footer
	if cache.ready && cache.location == (place{d.space.ID, d.rel}) {
		if wait := lk.liveTyping(c); wait > 0 {
			c.After(wait)
			return cache
		}
	}
	return lk.footerData(d)
}

// liveTyping is how long until a long page whose edits the live index
// has not read yet has gone unchanged for footerSettle; 0 when none has.
func (lk *linkFeature) liveTyping(c *ui.Context) time.Duration {
	var wait time.Duration
	for _, d := range lk.host.residentDocuments() {
		if d.isDatabase() || d.isMDZ() || d.loadErr != nil || d.ed.ByteLen() < footerLong {
			continue
		}
		rev := d.ed.Revision()
		if rev != d.typedRev {
			d.typedRev, d.typedAt = rev, c.Now()
		}
		if stamp, ok := lk.host.indexedLive(d); ok && stamp.revision == rev || !ok && !d.dirty() {
			continue
		}
		wait = max(wait, footerSettle-c.Now().Sub(d.typedAt))
	}
	return wait
}

func (lk *linkFeature) footerData(d *document) *pageFooterCache {
	ix := lk.host.liveIndex()
	location := place{d.space.ID, d.rel}
	cache := &d.footer
	if !cache.ready || cache.index != ix || cache.location != location {
		text, ok := ix.TextOf(location.space, location.rel)
		if !ok || d.dirty() {
			text = d.plainText()
		}
		*cache = pageFooterCache{ready: true, index: ix, location: location,
			tags: wiki.TagNames(text),
			hits: ix.Backlinks(d.title(), search.Live{Space: location.space, Path: location.rel}, nil)}
	}
	return cache
}

func (lk *linkFeature) pageFooterFor(c *ui.Context, d *document) {
	if d == nil || d.isDatabase() || d.isMDZ() {
		return
	}
	t := c.Theme()
	pg := pageStyle(lk.host.paperIsDark())
	data := lk.settledFooter(c, d)
	tags, hits := data.tags, data.hits
	if len(hits) == 0 && len(tags) == 0 && d.ed.Blank() {
		return // a blank page shows its starter alone
	}
	ui.Column(c).Gap(10).Children(func() {
		ui.Box(c).Height(1).Background(t.Border.Alpha(0.6))
		if len(tags) > 0 {
			ui.Row(c.Key("page-tags")).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconHash).FontSize(14).TextColor(t.TextMuted)
				for _, tag := range tags {
					chip := ui.ButtonBase(c.Key("tag:" + tag)).Height(24).PaddingX(8).Radius(6).Label(i18n.T("标签 ") + tag).
						Cursor(ui.CursorPointer).Background(pg.TagFill)
					if chip.Hovered() {
						chip.Background(t.Accent.Alpha(0.18))
					}
					chip.Children(func() { ui.Text(c, "#"+tag).FontSize(13).TextColor(t.Accent) })
					if chip.Clicked() {
						lk.host.activateDocument(d)
						lk.openTag(tag)
					}
					chip.ContextMenu(func(m *ui.Menu) {
						if m.Item(i18n.T("查看使用此标签的文档")).Chosen() {
							lk.host.activateDocument(d)
							lk.openTag(tag)
						}
						if m.Item(i18n.T("重命名或合并标签")).Chosen() {
							lk.host.openTagRename(tag)
						}
					})
				}
			})
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconLink).FontSize(14).TextColor(t.TextMuted)
			label := i18n.T("反向链接")
			if len(hits) > 0 {
				label = fmt.Sprintf(i18n.T("反向链接 · %d"), len(hits))
			}
			ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(t.TextMuted)
		})
		if len(hits) == 0 {
			ui.Text(c, i18n.T("还没有文档链接到这里；在其他文档中输入 [[ 即可链接到「")+d.title()+"」").FontSize(13).TextColor(t.TextMuted.Alpha(0.8))
			return
		}
		for _, h := range hits {
			id, rel := h.Space, h.Path
			row := ui.ButtonBase(c.Key("backlink:"+id+"/"+rel)).Padding(8, 10).Radius(8).Gap(4).Label(i18n.T("打开 ") + vault.Title(rel)).
				Cursor(ui.CursorPointer).AlignItems(ui.Stretch)
			if row.Hovered() {
				row.Background(pg.Hover)
			}
			row.Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						docIcon(c, lk.iconTitle(id, rel), 16)
						ui.Text(c, docName(vault.Title(rel))).FontSize(14).SingleLine().Shrink(1).MinWidth(0)
						if hint := lk.host.docHint(id, rel); hint != "" {
							ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
						}
					})
					s, m := h.Snippet, h.Match
					ui.RichText(c,
						ui.Span{Text: s[:m[0]]},
						ui.Span{Text: s[m[0]:m[1]], Color: t.Accent},
						ui.Span{Text: s[m[1]:]},
					).FontSize(13).TextColor(t.TextMuted).SingleLine().MinWidth(0)
				})
			})
			if row.Clicked() {
				lk.host.navigateInPane(d, func() { lk.host.openAnywhere(id, rel) })
			}
		}
	})
}

// iconTitle is a document's title with its page icon before it, which
// docIcon shows in place of the letter.
func (lk *linkFeature) iconTitle(spaceID, rel string) string {
	title := vault.Title(rel)
	icon := ""
	if d := lk.host.residentDocument(spaceID, rel); d != nil && d.loadErr == nil && !d.isDatabase() && !d.isMDZ() {
		icon = d.ed.Doc.Icon
	} else {
		icon = lk.host.indexedIcon(spaceID, rel)
	}
	if icon == "" || leadingEmoji(title) != "" {
		return title
	}
	return icon + " " + title
}

// relink renames the links to the document titled from in every document
// linking to it to to, the open one through its editor, and says how many
// changed.
func (lk *linkFeature) relink(from, to string, allowed map[string]bool) {
	if wiki.Same(from, to) || from == "" || to == "" {
		return
	}
	rename := func(s string) string {
		out, _ := wiki.Rename(s, from, to)
		return out
	}
	changed := 0
	resident := map[place]bool{}
	for _, d := range lk.host.residentDocuments() {
		resident[place{d.space.ID, d.rel}] = true
		if allowed[d.space.ID] && d.loadErr == nil && !d.isDatabase() && !d.isMDZ() && wiki.LinksTo(d.plainText(), from) && lk.host.mapDocumentText(d, rename) {
			changed++
		}
	}
	var files []place
	lk.eachDoc(func(sp *space.Space, n *vault.Node) bool {
		if allowed[sp.ID] {
			files = append(files, place{sp.ID, n.Path})
		}
		return true
	})
	for _, file := range files {
		if resident[file] || vault.IsDatabase(file.rel) || vault.IsMDZ(file.rel) || !vault.IsText(file.rel) {
			continue
		}
		sp := lk.host.spaces().Get(file.space)
		text, err := readFile(sp, file.rel)
		if err != nil {
			continue
		}
		d := openDocument(sp, file.rel, text, false)
		if d.loadErr != nil || !wiki.LinksTo(d.plainText(), from) || !lk.host.mapDocumentText(d, rename) {
			continue
		}
		if err := d.save(); err != nil {
			lk.host.notify("更新 %s 的双链失败；%v", file.rel, err)
			continue
		}
		changed++
	}

	if changed > 0 {
		lk.host.refreshSearch()
		lk.host.notify("已更新 %d 篇文档中指向「%s」的链接", changed, to)
	}
}

// previewWikiFor resolves in the source note's space and prefers live editors,
// so previews include unsaved changes without changing navigation or selection.
func (lk *linkFeature) previewWikiFor(source *document, name string) editor.WikiPreview {
	p := editor.WikiPreview{Title: name, Missing: true}
	if source == nil {
		return p
	}
	sp, rel, ok := lk.findNamedFrom(source.space.ID, name)
	if !ok {
		return p
	}
	p.Location = sp.Name + " / " + rel
	var plain string
	if d := lk.host.residentDocument(sp.ID, rel); d != nil {
		if d.loadErr != nil {
			p.Text = i18n.T("无法读取这篇笔记")
			p.Missing = false
			return p
		}
		plain = d.plainText()
	} else if indexed, ok := lk.host.liveIndex().TextOf(sp.ID, rel); ok {
		plain = indexed
	} else {
		raw, err := readFile(sp, rel)
		if err != nil {
			p.Text = i18n.T("无法读取这篇笔记")
			p.Missing = false
			return p
		}
		extracted, err := extractText(rel, raw)
		if err != nil {
			p.Text = i18n.T("无法读取这篇笔记")
			p.Missing = false
			return p
		}
		plain = extracted.Text
	}
	text := []rune(plain)
	if len(text) > 500 {
		text = append(text[:500], '…')
	}
	p.Text, p.Missing = string(text), false
	return p
}
