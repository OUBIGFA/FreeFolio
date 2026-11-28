package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/trashbin"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// copyHost is what sidebar copy, paste and duplicate need from the window.
type copyHost interface {
	autosaveEnabled() bool
	cancelRenameUnder(spaceID, rel string)
	childrenOf(dir string) []*vault.Node
	clearPicks(anchor string)
	dismissPreview()
	flush() bool
	forgetPageTree(id string)
	forgetPicks(spID, rel string)
	invalidateResolvedImages()
	keepDeleted(spaceID, spaceName, root, rel string) (trashbin.Item, bool)
	markSelfWrite(full string)
	notify(format string, args ...any)
	offerUndo(title, name string, undo func())
	openAfterRenaming(rel string)
	openAnywhere(id, rel string) bool
	refreshSearch()
	refreshTree()
	renameIn(sp *space.Space, rel string)
	replaceReferences(old, replacement *document)
	residentDocument(id, rel string) *document
	residentDocuments() []*document
	saveState()
	sidebarShown() bool
	spaces() *space.State
	trashBin() *trashbin.Bin
	trashItem(p string) error
}

// copyFeature copies, pastes and duplicates sidebar files and folders, and undoes them through the trash.
type copyFeature struct {
	host           copyHost
	copied         []place
	recentCopies   []*place
	pendingDupUndo *place
}

// Sidebar copy, paste and duplicate: documents and folders copied from the
// sidebar (single row, multi-selection, favorite tile or command bar) are
// kept as space-qualified places so they can be pasted beside themselves,
// into another folder or into another space, preserving attachments while
// leaving the originals untouched.

// copyEntries puts rels of sp onto the sidebar clipboard, deduplicating items
// whose ancestor folder is already included.
func (cp *copyFeature) copyEntries(sp *space.Space, rels []string) {
	if sp == nil || len(rels) == 0 {
		return
	}
	var clean []string
	for _, r := range rels {
		if r != "" && !slices.Contains(clean, r) {
			clean = append(clean, r)
		}
	}
	items := topLevelPaths(clean)
	if len(items) == 0 {
		return
	}
	places := make([]place, len(items))
	for i, r := range items {
		places[i] = place{space: sp.ID, rel: r}
	}
	cp.copied = places
	cp.host.dismissPreview()
	if len(items) == 1 {
		cp.host.notify("已复制「%s」", entryDisplayName(items[0]))
		return
	}
	cp.host.notify("已复制 %d 个项目", len(items))
}

// copyMenu adds Copy, Duplicate and Paste for the row at rel of sp. Paste
// fills dir, placing the copies right after the sibling after when given.
// External spaces can be copied from but take no new files.
func (cp *copyFeature) copyMenu(m *ui.Menu, sp *space.Space, rel, dir, after string) {
	if m.Item(i18n.T("复制")).Shortcut(ui.Cmd, ui.KeyC).Chosen() {
		cp.copyEntries(sp, []string{rel})
	}
	if sp.IsExternal() {
		return
	}
	if m.Item(i18n.T("创建副本")).Chosen() {
		cp.duplicateEntry(sp, rel)
	}
	if m.Item(cp.pasteMenuLabel()).Shortcut(ui.Cmd, ui.KeyV).Disabled(!cp.canPasteInto(sp, dir)).Chosen() {
		cp.pasteInto(sp, dir, after)
	}
}

// entryDisplayName formats a document or folder path for toast notices.
func entryDisplayName(rel string) string {
	if vault.IsDocument(rel) {
		return docName(vault.Title(rel))
	}
	return path.Base(rel)
}

// pasteMenuLabel names the Paste menu item, showing the count when multiple
// sidebar items are on the clipboard.
func (cp *copyFeature) pasteMenuLabel() string {
	if len(cp.copied) > 1 {
		return fmt.Sprintf(i18n.T("粘贴 %d 项"), len(cp.copied))
	}
	return i18n.T("粘贴")
}

// canPasteInto reports whether the sidebar clipboard can be pasted into the
// folder dir ("" for root) of dst. External spaces cannot receive new files,
// and a folder cannot be pasted into itself or one of its descendants.
func (cp *copyFeature) canPasteInto(dst *space.Space, dir string) bool {
	if dst == nil || dst.IsExternal() || len(cp.copied) == 0 {
		return false
	}
	anyValid := false
	for _, p := range cp.copied {
		if cp.host.spaces().Get(p.space) == nil {
			continue
		}
		if p.space == dst.ID && dir != "" && under(dir, p.rel) {
			return false
		}
		anyValid = true
	}
	return anyValid
}

// pasteInto copies everything on the sidebar clipboard into dir of dst. When
// afterRel is a sibling in dir and dir uses manual ordering, the new copies
// are placed immediately after it.
func (cp *copyFeature) pasteInto(dst *space.Space, dir, afterRel string) {
	if !cp.canPasteInto(dst, dir) {
		return
	}
	if cp.host.autosaveEnabled() && !cp.host.flush() {
		return
	}
	items := slices.Clone(cp.copied)
	if len(items) == 0 {
		return
	}

	var beforeNames []string
	insertAfter := -1
	if dst == cp.host.spaces().ActiveSpace() && dst.SortBy == "" && afterRel != "" &&
		parentDir(afterRel) == dir && !dst.IsPinned(afterRel) && !cp.host.spaces().IsFavorite(dst.ID, afterRel) {
		kids := cp.host.childrenOf(dir)
		beforeNames = make([]string, len(kids))
		for i, k := range kids {
			beforeNames[i] = k.Name
		}
		insertAfter = slices.Index(beforeNames, path.Base(afterRel))
	}

	var created []string
	var createdNames []string
	for _, p := range items {
		src := cp.host.spaces().Get(p.space)
		if src == nil {
			continue
		}
		if src.ID == dst.ID && dir != "" && under(dir, p.rel) {
			continue
		}
		full, err := resolveFile(src, p.rel)
		if err != nil {
			cp.host.notify("无法粘贴「%s」：源文件已不存在", entryDisplayName(p.rel))
			continue
		}
		if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) {
			cp.host.notify("无法粘贴「%s」：源文件已不存在", entryDisplayName(p.rel))
			continue
		}
		to, err := cp.copyWithResources(src, p.rel, dst, dir)
		if err != nil {
			cp.host.notify("粘贴 %s 失败，%v", path.Base(p.rel), err)
			continue
		}
		created = append(created, to)
		createdNames = append(createdNames, path.Base(to))
	}
	if len(created) == 0 {
		return
	}
	expandDirChain(dst, dir)
	if insertAfter >= 0 {
		if dst.Order == nil {
			dst.Order = map[string][]string{}
		}
		dst.Order[dir] = slices.Insert(beforeNames, insertAfter+1, createdNames...)
	}
	cp.host.clearPicks("")
	cp.host.dismissPreview()
	cp.host.saveState()
	cp.host.forgetPageTree(dst.ID)
	cp.host.refreshTree()
	cp.host.refreshSearch()

	targets := cp.trackUndoCopies(dst.ID, created)
	cp.host.offerUndo(fmt.Sprintf(i18n.T("已粘贴到「%s」"), folderName(dst, dir)), countLabel(created), func() {
		cp.trashCopied(targets)
	})
}

// duplicateEntry makes an in-place copy of rel beside itself in sp. When the
// sidebar is visible on sp, inline renaming starts on the duplicate (and
// confirming Enter on a duplicated document opens and focuses it); otherwise
// a duplicated document opens immediately.
func (cp *copyFeature) duplicateEntry(sp *space.Space, rel string) {
	if sp == nil || sp.IsExternal() || rel == "" {
		return
	}
	live := cp.host.residentDocument(sp.ID, rel)
	if cp.host.autosaveEnabled() && !cp.host.flush() {
		return
	}
	if live != nil {
		rel = live.rel
	}
	dir := parentDir(rel)

	var beforeNames []string
	insertAfter := -1
	if sp == cp.host.spaces().ActiveSpace() && sp.SortBy == "" &&
		!sp.IsPinned(rel) && !cp.host.spaces().IsFavorite(sp.ID, rel) {
		kids := cp.host.childrenOf(dir)
		beforeNames = make([]string, len(kids))
		for i, k := range kids {
			beforeNames[i] = k.Name
		}
		insertAfter = slices.Index(beforeNames, path.Base(rel))
	}

	to, err := cp.copyWithResources(sp, rel, sp, dir)
	if err != nil {
		cp.host.notify("创建 %s 的副本失败，%v", path.Base(rel), err)
		return
	}
	expandDirChain(sp, dir)
	if insertAfter >= 0 {
		if sp.Order == nil {
			sp.Order = map[string][]string{}
		}
		sp.Order[dir] = slices.Insert(beforeNames, insertAfter+1, path.Base(to))
	}
	cp.host.clearPicks("")
	cp.host.dismissPreview()
	cp.host.saveState()
	cp.host.forgetPageTree(sp.ID)
	cp.host.refreshTree()
	cp.host.refreshSearch()

	sidebarVisible := sp == cp.host.spaces().ActiveSpace() && cp.host.sidebarShown()
	if sidebarVisible {
		cp.host.renameIn(sp, to)
		cp.pendingDupUndo = &place{space: sp.ID, rel: to}
		if vault.IsDocument(to) {
			cp.host.openAfterRenaming(to)
		}
		return
	}
	targets := cp.trackUndoCopies(sp.ID, []string{to})
	cp.host.offerUndo(i18n.T("已创建副本"), path.Base(to), func() {
		cp.trashCopied(targets)
	})
	if vault.IsDocument(to) {
		cp.host.openAnywhere(sp.ID, to)
	}
}

// flushPendingDupUndo emits the deferred Undo toast for a freshly duplicated
// item once its inline rename finishes or is canceled, using the final path
// and starting a fresh timeout.
func (cp *copyFeature) flushPendingDupUndo(finalSpace, finalRel string) {
	p := cp.pendingDupUndo
	if p == nil {
		return
	}
	cp.pendingDupUndo = nil
	if finalSpace != "" && finalRel != "" {
		p.space, p.rel = finalSpace, finalRel
	}
	targets := cp.trackUndoCopies(p.space, []string{p.rel})
	cp.host.offerUndo(i18n.T("已创建副本"), path.Base(p.rel), func() {
		cp.trashCopied(targets)
	})
}

// expandDirChain expands dir and every ancestor folder in sp so newly pasted
// or duplicated items inside it are immediately visible in the sidebar.
func expandDirChain(sp *space.Space, dir string) {
	for cur := dir; cur != "" && cur != "." && cur != "/"; cur = path.Dir(cur) {
		sp.SetExpanded(cur, true)
		if !strings.Contains(cur, "/") {
			break
		}
	}
}

// trackUndoCopies registers mutable place pointers for the items created by a
// paste or duplicate so clicking Undo still finds them even after an inline
// rename.
func (cp *copyFeature) trackUndoCopies(spID string, rels []string) []*place {
	targets := make([]*place, len(rels))
	for i, rel := range rels {
		targets[i] = &place{space: spID, rel: rel}
	}
	cp.recentCopies = append(cp.recentCopies, targets...)
	if len(cp.recentCopies) > 64 {
		cp.recentCopies = cp.recentCopies[len(cp.recentCopies)-64:]
	}
	return targets
}

// trashCopied moves the items created by an undone paste or duplicate to the
// system trash (with trashbin backups), never permanently deleting user files.
func (cp *copyFeature) trashCopied(targets []*place) {
	if len(targets) == 0 || (cp.host.autosaveEnabled() && !cp.host.flush()) {
		return
	}
	changed := false
	for i := len(targets) - 1; i >= 0; i-- {
		t := targets[i]
		if t == nil || t.rel == "" {
			continue
		}
		sp := cp.host.spaces().Get(t.space)
		if sp == nil || sp.IsExternal() {
			continue
		}
		p, err := resolveFile(sp, t.rel)
		if err != nil {
			continue
		}
		if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		cp.host.cancelRenameUnder(sp.ID, t.rel)
		kept, ok := cp.host.keepDeleted(sp.ID, sp.Name, sp.Root, t.rel)
		if err := cp.host.trashItem(p); err != nil {
			if ok {
				cp.host.trashBin().Forget(kept)
			}
			cp.host.notify("将 %s 移到废纸篓失败，%v", path.Base(t.rel), err)
			continue
		}
		for _, d := range cp.host.residentDocuments() {
			if d.space == sp && within(d.rel, t.rel) {
				cp.host.replaceReferences(d, nil)
			}
		}
		cp.host.forgetPicks(sp.ID, t.rel)
		cp.forgetCopied(sp.ID, t.rel)
		cp.host.spaces().Forget(sp.ID, t.rel)
		cp.host.forgetPageTree(sp.ID)
		changed = true
	}
	if changed {
		cp.host.saveState()
		cp.host.refreshTree()
		cp.host.refreshSearch()
	}
}

// followCopied updates copied clipboard entries and active copy-undo targets
// when a file or folder moves or is renamed.
func (cp *copyFeature) followCopied(fromSpace, fromRel, toSpace, toRel string) {
	if fromSpace == toSpace && fromRel == toRel {
		return
	}
	for i, p := range cp.copied {
		if p.space == fromSpace && under(p.rel, fromRel) {
			cp.copied[i] = place{space: toSpace, rel: toRel + p.rel[len(fromRel):]}
		}
	}
	for _, p := range cp.recentCopies {
		if p != nil && p.space == fromSpace && under(p.rel, fromRel) {
			p.space = toSpace
			p.rel = toRel + p.rel[len(fromRel):]
		}
	}
}

// forgetCopied drops rel and anything inside it from the sidebar clipboard.
func (cp *copyFeature) forgetCopied(spID, rel string) {
	if len(cp.copied) > 0 {
		cp.copied = slices.DeleteFunc(cp.copied, func(p place) bool {
			return p.space == spID && under(p.rel, rel)
		})
	}
}

// forgetCopiedSpace drops clipboard entries belonging to a removed space.
func (cp *copyFeature) forgetCopiedSpace(spID string) {
	if len(cp.copied) > 0 {
		cp.copied = slices.DeleteFunc(cp.copied, func(p place) bool {
			return p.space == spID
		})
	}
	if len(cp.recentCopies) > 0 {
		cp.recentCopies = slices.DeleteFunc(cp.recentCopies, func(p *place) bool {
			return p == nil || p.space == spID
		})
	}
}

// copyWithResources copies a document or folder from src into dir of dst,
// keeping local images and covers resolvable in the copy while leaving the
// source document, its assets and its version history untouched. Legacy
// .arcmd copies receive fresh block IDs. Any failure rolls back everything
// this call created.
func (cp *copyFeature) copyWithResources(src *space.Space, rel string, dst *space.Space, dir string) (to string, err error) {
	if dst == nil || dst.IsExternal() {
		return "", errors.New(i18n.T("外部空间只记录文件，不能作为粘贴目标"))
	}
	if cp.host.autosaveEnabled() && !cp.host.flush() {
		return "", errors.New(i18n.T("请先保存文档并解决文件冲突"))
	}
	docs, err := cp.copyDocuments(src, rel)
	if err != nil {
		return "", err
	}
	sourceRoot, sourcePath, err := fileLocation(src, rel)
	if err != nil {
		return "", err
	}
	to, err = vault.CopyTo(sourceRoot, sourcePath, dst.Root, dir)
	if err != nil {
		return "", err
	}
	copiedTo := to
	var created []string
	defer func() {
		if err == nil {
			return
		}
		for _, p := range created {
			_ = os.Remove(p)
		}
		if full, rerr := vault.Resolve(dst.Root, copiedTo); rerr == nil {
			_ = os.RemoveAll(full)
		}
	}()
	for _, d := range docs {
		target := copiedTo + d.rel[len(rel):]
		targetDir := path.Dir(target)
		if targetDir == "." {
			targetDir = ""
		}
		srcDir := path.Dir(d.rel)
		if srcDir == "." {
			srcDir = ""
		}
		coverURLs := map[string]string{}
		sameDir := src.ID == dst.ID && targetDir == srcDir
		if !sameDir {
			for old := range d.urls {
				oldBase, oldFrag, hasFrag := strings.Cut(old, "#")
				if vault.IsWhiteboard(oldBase) || vault.IsPDF(oldBase) {
					clean := strings.TrimPrefix(path.Clean(unescapeImagePath(oldBase)), "/")
					if !underAttachmentFolder(clean, assetsDir) && !underAttachmentFolder(clean, attachmentFolder(src)) {
						continue
					}
				}
				file, resolveErr := resolveSpaceImage(src, d.rel, oldBase)
				if resolveErr != nil {
					continue
				}
				asset, relErr := filepath.Rel(sourceRoot, file)
				if relErr != nil {
					return "", relErr
				}
				asset = filepath.ToSlash(asset)
				inFolder := underAttachmentFolder(asset, attachmentFolder(src))
				data, readErr := os.ReadFile(file)
				if errors.Is(readErr, fs.ErrNotExist) {
					continue // an already missing picture stays missing
				}
				if readErr != nil {
					return "", readErr
				}
				var newURL, coverURL string
				switch {
				case src.ID == dst.ID && inFolder:
					// An attachment in the space's shared attachment folder stays
					// put; only the copied document's relative URL updates.
					newURL = docRelativeURL(targetDir, asset)
					coverURL = escapeImagePath(asset)
				default:
					folderMode := attachmentFolder(dst) != ""
					destDir, useFolder := targetDir, false
					orig := oldBase
					if folderMode {
						destDir, useFolder = attachmentFolder(dst), true
						clean := path.Clean(strings.ReplaceAll(unescapeImagePath(oldBase), `\`, "/"))
						for strings.HasPrefix(clean, "../") {
							clean = strings.TrimPrefix(clean, "../")
						}
						orig = clean
						switch {
						case attachmentFolder(src) != "" && attachmentFolder(src) != "." && strings.HasPrefix(asset, attachmentFolder(src)+"/"):
							orig = strings.TrimPrefix(asset, attachmentFolder(src)+"/")
						case strings.HasPrefix(clean, assetsDir+"/"):
							orig = strings.TrimPrefix(clean, assetsDir+"/")
						}
					}
					fileRel, made, firstUsed, copyErr := publishCopy(dst.Root, destDir, orig, data, useFolder)
					if copyErr != nil {
						return "", copyErr
					}
					if made != "" {
						created = append(created, made)
						cp.host.markSelfWrite(made)
					}
					switch {
					case useFolder:
						newURL = docRelativeURL(targetDir, fileRel)
						coverURL = escapeImagePath(fileRel)
					case firstUsed:
						newURL = oldBase
						coverURL = oldBase
					default:
						newURL = path.Join(assetsDir, path.Base(fileRel))
						coverURL = newURL
					}
				}
				if hasFrag {
					newURL += "#" + oldFrag
				}
				d.urls[old] = newURL
				if model.ValidCover(newURL) {
					coverURLs[old] = newURL
				} else if model.ValidCover(coverURL) {
					coverURLs[old] = coverURL
				}
			}
		}
		text, rewriteErr := copyDocumentText(target, d.original, d.urls, coverURLs)
		if rewriteErr != nil {
			return "", rewriteErr
		}
		if text != d.original {
			if err = vault.Write(dst.Root, target, text); err != nil {
				return "", err
			}
		}
	}
	cp.host.invalidateResolvedImages()
	return copiedTo, nil
}

// copyDocuments collects the current content and referenced local image/cover
// URLs for the file or folder at rel of sp, without inspecting version history
// (copies start with no history).
func (cp *copyFeature) copyDocuments(sp *space.Space, rel string) ([]*movingDocument, error) {
	file, err := resolveFile(sp, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	paths := []vault.Doc{{Path: rel}}
	if info.IsDir() {
		paths, err = vault.List(file)
		if err != nil {
			return nil, err
		}
		for i := range paths {
			paths[i].Path = path.Join(rel, paths[i].Path)
		}
	}
	var docs []*movingDocument
	for _, p := range paths {
		if !vault.IsDocument(p.Path) {
			continue
		}
		if live := cp.host.residentDocument(sp.ID, p.Path); live != nil {
			live.waitAsyncSave()
		}
		if vault.IsPDF(p.Path) || vault.IsMDZ(p.Path) {
			docs = append(docs, &movingDocument{rel: p.Path, urls: map[string]string{}})
			continue
		}
		text, err := readFile(sp, p.Path)
		if err != nil {
			return nil, err
		}
		d := &movingDocument{rel: p.Path, original: text, urls: map[string]string{}}
		if vault.IsWhiteboard(p.Path) || vault.IsDatabase(p.Path) {
			docs = append(docs, d)
			continue
		}
		doc, err := imageDocument(p.Path, text)
		if err != nil {
			return nil, err
		}
		doc.MapImages(func(url string) string { d.urls[url] = url; return url })
		docs = append(docs, d)
	}
	return docs, nil
}

// copyDocumentText prepares a copied document's text: legacy .arcmd files
// always receive fresh block IDs (and any rebased image URLs); Markdown files
// stay byte-identical unless an image or cover URL changed.
func copyDocumentText(rel, text string, urls, coverURLs map[string]string) (string, error) {
	if vault.IsWhiteboard(rel) || vault.IsPDF(rel) || vault.IsMDZ(rel) || vault.IsDatabase(rel) {
		return text, nil
	}
	mapCopyURLs := func(doc *model.Document) {
		if doc.Cover != "" {
			if to, ok := coverURLs[doc.Cover]; ok {
				doc.Cover = to
			}
		}
		var walk func([]*model.Block)
		walk = func(blocks []*model.Block) {
			for _, b := range blocks {
				if b.Kind == "image" {
					if to, ok := urls[b.URL]; ok {
						b.URL = to
					}
				}
				walk(b.Children)
			}
		}
		walk(doc.Blocks)
	}
	if strings.EqualFold(path.Ext(rel), ".arcmd") {
		doc, err := model.Decode(text)
		if err != nil {
			return "", err
		}
		refreshBlockIDs(doc.Blocks)
		mapCopyURLs(doc)
		return doc.Encode()
	}
	changed := false
	for old, newURL := range urls {
		if old != newURL {
			changed = true
			break
		}
	}
	if !changed {
		for old, newURL := range coverURLs {
			if old != newURL {
				changed = true
				break
			}
		}
	}
	if !changed {
		return text, nil
	}
	if vault.IsMarkdown(rel) {
		if strings.HasPrefix(strings.TrimSpace(text), "{") {
			if doc, err := model.Decode(text); err == nil {
				refreshBlockIDs(doc.Blocks)
				mapCopyURLs(doc)
				return doc.Encode()
			}
		}
		doc := editor.ImportMarkdown(text)
		mapCopyURLs(doc)
		if err := doc.Validate(); err != nil {
			return "", err
		}
		return editor.ExportMarkdown(doc), nil
	}
	return relocateText(rel, text, urls)
}

func refreshBlockIDs(blocks []*model.Block) {
	for _, b := range blocks {
		b.ID = model.NewID()
		refreshBlockIDs(b.Children)
	}
}
