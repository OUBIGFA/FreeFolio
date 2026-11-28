package app

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/OUBIGFA/FreeFolio/internal/space"
)

// Navigation changes only view state. Snapshot it before dispatch so disk
// flushes do not block the transition and later saves cannot be overtaken.
func (a *App) saveNavigationState() {
	post := a.runAsync
	if post == nil && a.win != nil {
		post = a.win.Update
	}
	if post == nil {
		a.saveState()
		return
	}
	a.captureSplits()
	raw, err := json.Marshal(a.state)
	var snapshot space.State
	if err == nil {
		err = json.Unmarshal(raw, &snapshot)
	}
	if err != nil {
		a.notify("保存空间设置失败；%v", err)
		return
	}
	previous, done, store := a.stateSaveDone, make(chan struct{}), a.store
	a.stateSaveDone = done
	go func() {
		if previous != nil {
			<-previous
		}
		err := store.Save(&snapshot)
		close(done) // A synchronous save/quit must never wait for a UI callback.
		if err != nil {
			post(func() { a.notify("保存空间设置失败；%v", err) })
		}
	}()
}

func (a *App) waitNavigationSave() {
	if a.stateSaveDone != nil {
		<-a.stateSaveDone
		a.stateSaveDone = nil
	}
}

// Resume the existing board immediately, then validate its saved contents.
// Saves still compare against disk, and edits made during the check become a
// conflict rather than being replaced by the background result.
func (a *App) resumeResidentBoard(sp *space.Space) bool {
	post := a.runAsync
	if post == nil && a.win != nil {
		post = a.win.Update
	}
	d := a.residentDocument(sp.ID, sp.LastOpen)
	if post == nil || d == nil || d.board == nil || d.loadErr != nil {
		return false
	}
	a.doc = d
	a.open(d.rel) // Preserve navigation history without rereading the same file.
	d.boardCheck++
	check, baseline, rel := d.boardCheck, d.diskText, d.rel
	snapshot := treeSpaceSnapshot(sp)
	go func() {
		text, err := readFile(snapshot, rel)
		var loaded *document
		if err == nil && text != baseline {
			loaded = openDocument(snapshot, rel, text, false)
		}
		post(func() {
			if d.closed.Load() || !a.isResidentDocument(d) || d.rel != rel || d.space.Root != snapshot.Root || d.boardCheck != check || d.diskText != baseline || d.saving {
				if loaded != nil {
					loaded.close()
				}
				return
			}
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					if d.dirty() {
						d.missing, d.conflict = true, false
					} else {
						a.withDocument(d, a.closeMissing)
					}
				} else {
					a.notify("打开 %s 失败；%v", d.title(), err)
				}
				return
			}
			d.missing = false
			if loaded == nil {
				d.conflict = false
				return
			}
			if d.dirty() {
				d.conflict, d.diskNow = true, text
				loaded.close()
				return
			}
			loaded.space, loaded.ed.Scroll = d.space, d.ed.Scroll
			a.replaceReferences(d, loaded)
		})
	}()
	return true
}
