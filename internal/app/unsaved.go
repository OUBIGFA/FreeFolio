package app

import (
	"fmt"

	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// unsavedPrompt asks what to do with modified documents before an action
// that would close or replace them while background autosave is turned off.
type unsavedPrompt struct {
	open    bool
	docs    []*document
	proceed func()
}

// autosaveEnabled reports whether edits are written in the background, the
// default; Settings and the command bar turn it off (prefs.NoAutosave).
func (a *App) autosaveEnabled() bool {
	return a.prefs == nil || !a.prefs.NoAutosave
}

func (a *App) toggleAutosave() {
	a.prefs.NoAutosave = !a.prefs.NoAutosave
	a.savePrefs()
}

// flushForTask saves before a file operation that keeps the documents open
// (moving, renaming, syncing). With autosave off the edits stay in memory
// until the user saves, so there is nothing to write.
func (a *App) flushForTask() bool {
	return !a.autosaveEnabled() || a.flush()
}

// discardEdits drops in-memory edits on d, restoring what is on disk without
// touching the file.
func (a *App) discardEdits(d *document) {
	if d == nil {
		return
	}
	d.waitAsyncSave()
	clean := openDocument(d.space, d.rel, d.diskText, d.source)
	if d.ed != nil && clean.ed != nil {
		clean.ed.Scroll = d.ed.Scroll
	}
	a.replaceReferences(d, clean)
	d.close()
}

func (a *App) discardCurrentEdits() {
	if a.doc == nil || !a.doc.dirty() {
		return
	}
	a.discardEdits(a.doc)
}

func (a *App) flushDocs(docs []*document) bool {
	for _, d := range docs {
		if d == nil {
			continue
		}
		ok := false
		a.withDocument(d, func() { ok = a.flushCurrent() })
		if !ok {
			a.showSaveFailure(d)
			return false
		}
	}
	return true
}

// leaveDocs is the gate before docs close or are replaced: with autosave on
// it saves everything as before; with autosave off and edits in docs it
// opens the Save / Don't Save / Cancel dialog, which runs proceed once the
// edits are saved or dropped, and reports false so the caller stops now.
func (a *App) leaveDocs(docs []*document, proceed func()) bool {
	if a.autosaveEnabled() {
		return a.flush()
	}
	var dirty []*document
	seen := make(map[*document]bool, len(docs))
	for _, d := range docs {
		if d != nil && !seen[d] && d.loadErr == nil && d.dirty() {
			seen[d] = true
			dirty = append(dirty, d)
		}
	}
	if len(dirty) == 0 {
		return true
	}
	a.unsaved = unsavedPrompt{open: true, docs: dirty, proceed: proceed}
	return false
}

// leavingCurrentDoc is the shown document when navigating away in place
// would retire it from memory: unpinned and not held by a split group.
func (a *App) leavingCurrentDoc() []*document {
	d := a.doc
	if d == nil || d.loadErr != nil || !d.dirty() {
		return nil
	}
	if inPins(d.space, d.rel) {
		return nil
	}
	if a.split != nil && !a.paneNavigation {
		return nil
	}
	return []*document{d}
}

func (a *App) unsavedPromptView(c *ui.Context) {
	if !a.unsaved.open || len(a.unsaved.docs) == 0 {
		return
	}
	docs := a.unsaved.docs
	proceed := a.unsaved.proceed
	title := fmt.Sprintf(i18n.T("保存对「%s」的修改吗？"), docs[0].title())
	if len(docs) > 1 {
		title = fmt.Sprintf(i18n.T("保存对「%s」等 %d 篇文档的修改吗？"), docs[0].title(), len(docs))
	}
	t := c.Theme()
	ui.Modal(c.Key("unsaved-prompt"), &a.unsaved.open, func() {
		ui.Column(c.Key("unsaved-body")).Width(390).Gap(14).Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, title).FontSize(16).FontWeight(600)
				ui.Text(c, i18n.T("已关闭自动保存，如果不保存，未保存的修改将会丢失。")).FontSize(13).LineHeight(1.5).TextColor(t.TextMuted)
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Button(c.Key("discard"), i18n.T("不保存")).Clicked() {
					a.unsaved = unsavedPrompt{}
					for _, d := range docs {
						a.discardEdits(d)
					}
					if proceed != nil {
						proceed()
					}
				}
				ui.Spacer(c)
				if ui.Button(c.Key("cancel"), i18n.T("取消")).Clicked() {
					a.unsaved = unsavedPrompt{}
				}
				if ui.PrimaryButton(c.Key("save"), i18n.T("保存")).Clicked() {
					a.unsaved = unsavedPrompt{}
					if a.flushDocs(docs) && proceed != nil {
						proceed()
					}
				}
			})
		})
	})
}

// retryCloseMain closes the main window again after the unsaved prompt
// saved or dropped the edits that stopped it.
func (a *App) retryCloseMain() {
	if a.quitting {
		mygo.App.Quit()
		return
	}
	if a.win != nil {
		a.win.Close()
	}
}
