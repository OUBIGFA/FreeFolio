package app

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/history"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
)

type oneBotApplyDocument struct {
	doc                *document
	space              space.Space
	path, expectedDisk string
	snapshot           *model.Document
	changes            []*oneBotChange
}

// applyOneBotAsync keeps disk reads, checkpoints and saves off the UI thread.
// The main thread alone validates live state and mutates document engines.
func (a *App) applyOneBotAsync(task *oneBotTask, index, only int) error {
	if task == nil || task.Running || task.Applying || task.Stopped || index < 0 || index >= len(task.Candidates) {
		return errors.New(i18n.T("当前方案不能应用"))
	}
	candidate := task.Candidates[index]
	if !candidate.Complete {
		return errors.New(i18n.T("模型未完整结束，草稿未应用"))
	}
	var jobs []*oneBotApplyDocument
	byDoc := map[*document]*oneBotApplyDocument{}
	var creates []*oneBotChange
	for i, ch := range candidate.Changes {
		if ch.Status != "pending" || only >= 0 && only != i {
			continue
		}
		if ch.Operation.Kind == "create_document" {
			creates = append(creates, ch)
			continue
		}
		var target *oneBotTarget
		for j := range task.Targets {
			if task.Targets[j].ID == ch.TargetID {
				target = &task.Targets[j]
				break
			}
		}
		if target == nil {
			return model.ErrEditConflict
		}
		// Loaded history must be rebound explicitly before an async apply; this
		// prevents a hidden disk load or an unintended second writer here.
		d := target.doc
		if d == nil {
			if !a.openAnywhere(task.SpaceID, target.Path) {
				return model.ErrEditConflict
			}
			var err error
			d, err = a.oneBotTargetDoc(task, target)
			if err != nil {
				return err
			}
			ch.Operation.Target = target.Target
		}
		if d.closed.Load() || d.ed.ReadOnly || d.ed.Composing() || d.conflict || d.missing {
			return model.ErrEditConflict
		}
		if d.saving {
			return errors.New(i18n.T("文档正在保存，请稍后应用"))
		}
		if _, _, err := d.ed.ResolveEdit(ch.Operation.Target); err != nil {
			return err
		}
		job := byDoc[d]
		if job == nil {
			copySpace := *d.space
			if d.space.ExternalFiles != nil {
				copySpace.ExternalFiles = map[string]string{}
				for k, v := range d.space.ExternalFiles {
					copySpace.ExternalFiles[k] = v
				}
			}
			job = &oneBotApplyDocument{doc: d, space: copySpace, path: d.rel, expectedDisk: d.diskText, snapshot: d.ed.Committed().Clone()}
			byDoc[d] = job
			jobs = append(jobs, job)
		}
		job.changes = append(job.changes, ch)
	}
	if len(jobs)+len(creates) == 0 {
		return errors.New(i18n.T("没有待应用的修改"))
	}
	sp := a.qaSpaceByID(task.SpaceID)
	if sp == nil {
		return model.ErrEditConflict
	}
	root := sp.Root
	task.Applying = true
	task.Error = ""
	ctx, cancel := context.WithCancel(context.Background())
	task.cancel = cancel
	finish := func(err error) {
		cancel()
		task.Applying = false
		task.cancel = nil
		if err != nil {
			task.Error = err.Error()
		}
		a.keepOneBotTask(task)
	}
	preflight := func() error {
		for _, ch := range creates {
			full, err := vault.Resolve(root, ch.Path)
			if err != nil {
				return err
			}
			if _, err = os.Stat(full); err == nil {
				return os.ErrExist
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return err
			}
			disk, err := readFile(&job.space, job.path)
			if err != nil {
				return err
			}
			if disk != job.expectedDisk {
				return model.ErrEditConflict
			}
			content := editor.ExportMarkdown(job.snapshot)
			if strings.EqualFold(path.Ext(job.path), ".arcmd") {
				content, err = job.snapshot.Encode()
				if err != nil {
					return err
				}
			}
			if !job.doc.historyOff {
				_, err = history.SaveMilestone(job.space.Root, job.path, content, "OneBot "+task.Created.Format("2006-01-02 15:04:05"), time.Now())
			} else {
				dir := filepath.Join(a.cfg.ConfigDir, "onebot", "recovery", task.ID)
				if err = os.MkdirAll(dir, 0700); err == nil {
					err = fsutil.WriteFileAtomic(filepath.Join(dir, model.NewID()+path.Ext(job.path)), []byte(content), 0600)
				}
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	var applyNext func(int)
	applyNext = func(at int) {
		if err := ctx.Err(); err != nil {
			finish(err)
			return
		}
		if at >= len(jobs) {
			if len(creates) == 0 {
				finish(nil)
				return
			}
			go func() {
				for _, ch := range creates {
					if err := ctx.Err(); err != nil {
						a.win.Update(func() { finish(err) })
						return
					}
					full, err := vault.Resolve(root, ch.Path)
					if err == nil {
						err = os.MkdirAll(filepath.Dir(full), 0755)
					}
					if err == nil {
						err = fsutil.CreateFileAtomic(full, []byte(ch.NewText), 0644)
					}
					if err != nil {
						a.win.Update(func() { ch.Error = err.Error(); finish(err) })
						return
					}
					a.win.Update(func() { ch.Status = "saved" })
				}
				a.win.Update(func() { finish(nil) })
			}()
			return
		}
		job := jobs[at]
		d := job.doc
		if d.closed.Load() || d.rel != job.path || d.ed.ReadOnly || d.ed.Composing() || d.diskText != job.expectedDisk || d.saving {
			finish(model.ErrEditConflict)
			return
		}
		ops := make([]model.EditOperation, len(job.changes))
		for i, ch := range job.changes {
			ops[i] = ch.Operation
		}
		receipt, err := d.ed.ApplyAI(ops)
		if err != nil {
			finish(err)
			return
		}
		batch := model.NewID()
		if task.Receipts == nil {
			task.Receipts = map[string]*model.EditReceipt{}
		}
		task.Receipts[batch] = &receipt
		for _, ch := range job.changes {
			ch.Status = "applied"
			ch.Receipt = &receipt
			ch.Batch = batch
		}
		// Keep the inverse transaction durable before the autosave can finish.
		if err := a.persistOneBotTask(task); err != nil {
			finish(err)
			return
		}
		for ci, other := range task.Candidates {
			if ci == index {
				continue
			}
			for _, ch := range other.Changes {
				if ch.Status == "pending" {
					ch.Status = "stale"
				}
			}
		}
		d.startAsyncAutosave(func(poll func()) {
			a.win.Update(func() {
				poll()
				if d.saveErr != nil || d.conflict || d.missing {
					err := d.saveErr
					if err == nil {
						err = model.ErrEditConflict
					}
					for _, ch := range job.changes {
						ch.Error = err.Error()
					}
					finish(err)
					return
				}
				for _, ch := range job.changes {
					ch.Status = "saved"
					ch.Error = ""
				}
				for _, ch := range candidate.Changes {
					if ch.Status == "pending" && ch.Path == job.path {
						if target, err := d.ed.RebaseEdit(ch.Operation.Target); err == nil {
							ch.Operation.Target = target
						} else {
							ch.Status = "conflict"
							ch.Error = err.Error()
						}
					}
				}
				a.keepOneBotTask(task)
				applyNext(at + 1)
			})
		})
	}
	go func() {
		err := preflight()
		a.win.Update(func() {
			if err != nil {
				finish(err)
				return
			}
			applyNext(0)
		})
	}()
	return nil
}
