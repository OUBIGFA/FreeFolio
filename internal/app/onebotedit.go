package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/agent"
	model "github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/fsutil"
	"github.com/OUBIGFA/FreeMD/internal/history"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/OUBIGFA/FreeMD/internal/vault"
)

type oneBotTarget struct {
	Whole    bool             `json:"whole,omitempty"`
	Plain    bool             `json:"plain,omitempty"`
	Derived  bool             `json:"derived,omitempty"`
	ID       string           `json:"id"`
	Path     string           `json:"path"`
	Target   model.EditTarget `json:"target"`
	Markdown string           `json:"markdown"`
	Native   *model.Document  `json:"native"`
	doc      *document
}
type oneBotChange struct {
	Batch       string              `json:"batch,omitempty"`
	TargetID    string              `json:"target"`
	Path        string              `json:"path"`
	Operation   model.EditOperation `json:"operation"`
	NewText     string              `json:"newText,omitempty"`
	Explanation string              `json:"explanation,omitempty"`
	Status      string              `json:"status"`
	Error       string              `json:"error,omitempty"`
	Diff        []history.Line      `json:"-"`
	Receipt     *model.EditReceipt  `json:"-"`
}
type oneBotCandidate struct {
	preview  qaTurn
	Title    string          `json:"title"`
	Text     string          `json:"text,omitempty"`
	Error    string          `json:"error,omitempty"`
	Changes  []*oneBotChange `json:"changes,omitempty"`
	Complete bool            `json:"complete"`
}
type oneBotTask struct {
	ShowDiff    bool                          `json:"-"`
	Receipts    map[string]*model.EditReceipt `json:"receipts,omitempty"`
	Parts       []agent.ContentPart           `json:"-"`
	Applying    bool                          `json:"-"`
	Version     int                           `json:"version"`
	ID          string                        `json:"id"`
	SpaceID     string                        `json:"space"`
	Scope       string                        `json:"scope"`
	Instruction string                        `json:"instruction"`
	Targets     []oneBotTarget                `json:"targets"`
	Candidates  []*oneBotCandidate            `json:"candidates"`
	Selected    int                           `json:"selected"`
	NewPath     string                        `json:"newPath,omitempty"`
	Direct      bool                          `json:"direct"`
	Running     bool                          `json:"-"`
	Stopped     bool                          `json:"stopped,omitempty"`
	Error       string                        `json:"error,omitempty"`
	Created     time.Time                     `json:"created"`
	Generation  uint64                        `json:"-"`
	cancel      context.CancelFunc
}

func (a *App) captureOneBotTask(scope string) (*oneBotTask, error) {
	sp := a.state.ActiveSpace()
	if sp == nil {
		return nil, errors.New(i18n.T("空间已不存在"))
	}
	t := &oneBotTask{Version: 1, ID: model.NewID(), SpaceID: sp.ID, Scope: scope, Direct: a.prefs.AI.Direct, Created: time.Now()}
	if scope == "new" {
		return t, nil
	}
	var docs []*document
	if scope == "documents" {
		for _, rel := range a.qa.editPaths {
			var d *document
			for _, open := range a.residentDocuments() {
				if open.space.ID == sp.ID && open.rel == rel {
					d = open
					break
				}
			}
			if d == nil {
				text, err := readFile(sp, rel)
				if err != nil {
					return nil, err
				}
				d = openDocument(sp, rel, text, false)
			}
			docs = append(docs, d)
		}
	} else if a.doc != nil {
		docs = []*document{a.doc}
	}
	if len(docs) == 0 && scope != "new" {
		return nil, errors.New(i18n.T("请先打开文档"))
	}
	for _, d := range docs {
		if !vault.IsMarkdown(d.rel) && !strings.EqualFold(path.Ext(d.rel), ".arcmd") {
			return nil, errors.New(i18n.T("请在文本中选择需要修改的内容"))
		}
		if d.loadErr != nil {
			return nil, d.loadErr
		}
		if d.ed.ReadOnly {
			return nil, errors.New(i18n.T("文档处于只读状态"))
		}
		if d.ed.Composing() {
			return nil, errors.New(i18n.T("请先完成输入法输入"))
		}
		if d.conflict || d.missing {
			return nil, model.ErrEditConflict
		}
		if scope == "selection" {
			r := d.ed.Selection().Range
			if r.Start == r.End && len(d.ed.BlockSelection()) == 0 {
				return nil, errors.New(i18n.T("请先选择需要修改的内容"))
			}
			if ids := d.ed.BlockSelection(); len(ids) > 0 {
				first, last := -1, -1
				for _, leaf := range d.ed.Doc.Leaves() {
					if slices.Contains(ids, leaf.Owner) {
						if first < 0 {
							first = leaf.Start
						}
						last = leaf.End
					}
				}
				if first >= 0 {
					r.Start, r.End = first, last
				}
			}
			target, err := d.ed.CaptureEdit(r.Start, r.End)
			if err != nil {
				return nil, err
			}
			t.Targets = append(t.Targets, makeOneBotTarget(d, target))
			continue
		}
		start, end := 0, len(d.ed.Doc.Blocks)
		if scope == "section" {
			owner := d.ed.LeafAt(d.ed.Caret).Owner
			current := 0
			for i, b := range d.ed.Doc.Blocks {
				if b.ID == owner {
					current = i
					break
				}
			}
			start = current
			for start > 0 && d.ed.Doc.Blocks[start].Kind != "heading" {
				start--
			}
			end = d.ed.Doc.Section(start)
			if d.ed.Doc.Blocks[start].Kind != "heading" {
				end = len(d.ed.Doc.Blocks)
				for i := start + 1; i < len(d.ed.Doc.Blocks); i++ {
					if d.ed.Doc.Blocks[i].Kind == "heading" {
						end = i
						break
					}
				}
			}
		}
		leaves := d.ed.Doc.Leaves()
		lo, hi := -1, -1
		for _, leaf := range leaves {
			if leaf.Owner == d.ed.Doc.Blocks[start].ID && lo < 0 {
				lo = leaf.Start
			}
			if leaf.Owner == d.ed.Doc.Blocks[end-1].ID {
				hi = leaf.End
			}
		}
		target, err := d.ed.CaptureEdit(lo, hi)
		if err != nil {
			return nil, err
		}
		captured := makeOneBotTarget(d, target)
		if scope == "document" || scope == "documents" {
			captured.Whole = true
			captured.Native = d.ed.Committed().Clone()
			captured.Markdown = editor.ExportMarkdown(captured.Native)
		}
		t.Targets = append(t.Targets, captured)
	}
	return t, nil
}

func makeOneBotTarget(d *document, target model.EditTarget) oneBotTarget {
	e := model.NewEngine(d.ed.Committed())
	a, z, _ := e.ResolveEdit(target)
	e.SetSelection(a, z)
	fragment := e.Fragment()
	fragment.ImportSource = ""
	captured := oneBotTarget{ID: model.NewID(), Path: d.rel, Target: target, Markdown: editor.ExportMarkdown(fragment), Native: fragment, doc: d}
	if target.First == target.Last {
		leaf, _ := e.LeafByID(target.First)
		switch leaf.Block.Kind {
		case "code", "raw", "math", "cell":
			captured.Plain = true
			captured.Markdown = target.Text
		}
	}
	return captured
}

func (a *App) beginOneBotEdit(scope string) {
	t, err := a.captureOneBotTask(scope)
	if err != nil {
		a.notify("%s", err.Error())
		return
	}
	a.qa.editTask = t
	a.qa.editScope = scope
	a.qa.open = true
	a.qa.focusInput = true
	a.qa.editMode = true
}

func (a *App) oneBotTargetDoc(task *oneBotTask, target *oneBotTarget) (*document, error) {
	if target.doc != nil && !target.doc.closed.Load() {
		return target.doc, nil
	}
	if target.doc != nil {
		return nil, model.ErrEditConflict
	}
	// Reopening requires semantic equality with the persisted original target;
	// Markdown imports assign fresh IDs, so bind only after exact block matching.
	sp := a.state.Get(task.SpaceID)
	if sp == nil {
		return nil, errors.New(i18n.T("空间已不存在"))
	}
	var d *document
	for _, open := range a.residentDocuments() {
		if open.space == sp && open.rel == target.Path {
			d = open
			break
		}
	}
	if d == nil {
		for _, other := range task.Targets {
			if other.Path == target.Path && other.doc != nil && !other.doc.closed.Load() {
				d = other.doc
				break
			}
		}
	}
	if d == nil {
		text, err := readFile(sp, target.Path)
		if err != nil {
			return nil, err
		}
		d = openDocument(sp, target.Path, text, false)
	}
	if d.loadErr != nil {
		return nil, d.loadErr
	}
	if _, _, err := d.ed.ResolveEdit(target.Target); err != nil {
		matches := 0
		var replacement model.EditTarget
		for i := 0; i+len(target.Target.Blocks) <= len(d.ed.Doc.Blocks); i++ {
			same := true
			for j, b := range target.Target.Blocks {
				if !sameBlockWithoutIDs(b, d.ed.Doc.Blocks[i+j]) {
					same = false
					break
				}
			}
			if !same {
				continue
			}
			ids := map[string]string{}
			for j, b := range target.Target.Blocks {
				mapOneBotBlockIDs(b, d.ed.Doc.Blocks[i+j], ids)
			}
			first, found := d.ed.LeafByID(ids[target.Target.First])
			last, lastFound := d.ed.LeafByID(ids[target.Target.Last])
			if !found || !lastFound {
				continue
			}
			candidate, e := d.ed.CaptureEdit(first.Start+target.Target.Start, last.Start+target.Target.End)
			if e == nil && candidate.Text == target.Target.Text {
				replacement = candidate
				matches++
			}
		}
		if matches != 1 {
			return nil, model.ErrEditConflict
		}
		target.Target = replacement
	}
	target.doc = d
	return d, nil
}

func sameBlockWithoutIDs(a, b *model.Block) bool {
	a, b = a.Clone(), b.Clone()
	var clear func(*model.Block)
	clear = func(b *model.Block) {
		b.ID = ""
		for _, c := range b.Children {
			clear(c)
		}
	}
	clear(a)
	clear(b)
	return reflect.DeepEqual(a, b)
}

func mapOneBotBlockIDs(before, after *model.Block, ids map[string]string) {
	ids[before.ID] = after.ID
	for i, child := range before.Children {
		if i < len(after.Children) {
			mapOneBotBlockIDs(child, after.Children[i], ids)
		}
	}
}

func (a *App) runOneBotEdit(task *oneBotTask, instruction string, count int, follow bool) {
	if task == nil || task.Running || task.Applying {
		return
	}
	if err := a.qaReady(); err != nil {
		task.Error = err.Error()
		return
	}
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return
	}
	count = max(1, min(count, 5))
	task.Instruction = instruction
	task.Error = ""
	task.Stopped = false
	task.Running = true
	task.Generation++
	epoch := task.Generation
	settings := a.qaSettings()
	if len(a.qa.attachments) > 0 {
		task.Parts = append([]agent.ContentPart(nil), a.qa.attachments...)
		a.qa.attachments = nil
	}
	provider := settings.NewProvider()
	if a.qa.provider != nil {
		provider = a.qa.provider(settings)
	}
	var prior string
	if follow && task.Selected < len(task.Candidates) {
		for _, ch := range task.Candidates[task.Selected].Changes {
			content := ch.Operation.Text
			if ch.NewText != "" {
				content = ch.NewText
			}
			prior += "\n" + ch.Path + "\n" + content
		}
	}
	task.Candidates = make([]*oneBotCandidate, count)
	for i := range task.Candidates {
		task.Candidates[i] = &oneBotCandidate{Title: fmt.Sprintf(i18n.T("方案 %d"), i+1)}
	}
	task.Selected = 0
	ctx, cancel := context.WithCancel(context.Background())
	task.cancel = cancel
	// Freeze native context on the UI thread before the worker starts.
	files := oneBotEditFiles(task)
	var newPaths []string
	if task.NewPath != "" {
		newPaths = []string{task.NewPath}
	}
	prompt := instruction + "\nWritable paths: " + strings.Join(oneBotEditPaths(files, newPaths), ", ")
	if task.Scope == "selection" || task.Scope == "section" {
		prompt += "\nThese files expose only the selected excerpt. Keep all edits inside it."
	}
	if prior != "" {
		prompt += "\nPrevious draft to revise (not yet saved):\n" + prior
	}
	var readTools []agent.Tool
	if settings.Capabilities.Tools != agent.Unsupported && task.Scope != "selection" {
		if fs := a.qaSpace(task.SpaceID); fs != nil {
			readTools = agent.SpaceTools(fs)
		}
	}
	deliver := func(f func()) { f() }
	if !a.qa.sync && a.win != nil {
		deliver = func(f func()) { a.win.Update(f) }
	}
	work := func() {
		var wg sync.WaitGroup
		gate := make(chan struct{}, 2)
		for i := 0; i < count; i++ {
			i := i
			wg.Add(1)
			run := func() {
				defer wg.Done()
				select {
				case gate <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-gate }()
				workspace, resultErr := agent.NewEditWorkspace(files, newPaths)
				var draft agent.FileDraft
				if resultErr == nil {
					draft, resultErr = agent.GenerateFileEdit(ctx, provider, prompt+fmt.Sprintf("\nCandidate %d of %d. Offer distinct valid wording.", i+1, count), task.Parts, workspace, readTools, settings.Capabilities.Tools != agent.Unsupported, func(d agent.Delta) {
						if d.Text != "" && settings.Capabilities.Tools != agent.Unsupported {
							deliver(func() {
								if task.Generation == epoch && !task.Stopped {
									task.Candidates[i].Text += d.Text
								}
							})
						}
					})
				}
				deliver(func() {
					if task.Generation != epoch || task.Stopped {
						return
					}
					if resultErr != nil {
						task.Candidates[i].Error = resultErr.Error()
						return
					}
					prepared, err := a.prepareOneBotFileCandidate(task, draft)
					if err != nil {
						task.Candidates[i].Error = err.Error()
						return
					}
					prepared.Text = draft.Summary
					task.Candidates[i] = prepared
				})
			}
			if a.qa.sync || a.win == nil {
				run()
			} else {
				go run()
			}
		}

		wg.Wait()
		deliver(func() {
			if task.Generation != epoch {
				return
			}
			task.Running = false
			task.cancel = nil
			cancel()
			if task.Stopped {
				return
			}
			if err := a.persistOneBotTask(task); err != nil {
				task.Error = err.Error()
				return
			}
			if task.Direct && count == 1 && task.Candidates[0].Complete && len(task.Candidates[0].Changes) > 0 {
				if err := a.applyOneBotCandidate(task, 0, -1); err != nil {
					task.Error = err.Error()
				}
			}
		})
	}
	if a.qa.sync || a.win == nil {
		work()
	} else {
		go work()
	}
}

// Continue from the exact result of the previous edit instead of reusing its
// obsolete original selection. The earlier task stays in persisted history.
func (a *App) continueOneBotTask(previous *oneBotTask) (*oneBotTask, error) {
	next := &oneBotTask{Version: 1, ID: model.NewID(), SpaceID: previous.SpaceID, Scope: previous.Scope, Direct: a.prefs.AI.Direct, Created: time.Now(), NewPath: previous.NewPath}
	if previous.Selected < 0 || previous.Selected >= len(previous.Candidates) {
		return nil, model.ErrEditConflict
	}
	candidate := previous.Candidates[previous.Selected]
	if previous.Scope == "new" {
		for _, ch := range candidate.Changes {
			if ch.Operation.Kind != "create_document" || ch.Status != "saved" {
				continue
			}
			sp := a.state.Get(previous.SpaceID)
			if sp == nil {
				return nil, model.ErrEditConflict
			}
			var d *document
			for _, open := range a.residentDocuments() {
				if open.space.ID == sp.ID && open.rel == ch.Path {
					d = open
					break
				}
			}
			if d == nil {
				text, err := readFile(sp, ch.Path)
				if err != nil {
					return nil, err
				}
				d = openDocument(sp, ch.Path, text, false)
			}
			if d.loadErr != nil {
				return nil, d.loadErr
			}
			full, err := d.ed.CaptureEdit(0, d.ed.TextLength())
			if err != nil {
				return nil, err
			}
			target := makeOneBotTarget(d, full)
			target.Whole = true
			target.Native = d.ed.Committed().Clone()
			target.Markdown = editor.ExportMarkdown(target.Native)
			next.Scope = "document"
			next.NewPath = ""
			next.Targets = []oneBotTarget{target}
			return next, nil
		}
		return nil, model.ErrEditConflict
	}
	targets := map[string]*oneBotTarget{}
	for i := range previous.Targets {
		targets[previous.Targets[i].ID] = &previous.Targets[i]
	}
	for _, old := range previous.Targets {
		if old.Derived {
			continue
		}
		original := targets[old.ID]
		d, err := a.oneBotTargetDoc(previous, original)
		if err != nil {
			return nil, err
		}
		if old.Whole {
			full, err := d.ed.CaptureEdit(0, d.ed.TextLength())
			if err != nil {
				return nil, err
			}
			captured := makeOneBotTarget(d, full)
			captured.Whole = true
			captured.Native = d.ed.Committed().Clone()
			captured.Markdown = editor.ExportMarkdown(captured.Native)
			next.Targets = append(next.Targets, captured)
			continue
		}
		if previous.Scope == "section" {
			ids := map[string]bool{}
			for _, b := range old.Target.Blocks {
				ids[b.ID] = true
			}
			for _, ch := range candidate.Changes {
				if ch.Path == old.Path && (ch.Status == "saved" || ch.Status == "applied") {
					for _, b := range ch.Operation.Blocks {
						ids[b.ID] = true
					}
				}
			}
			lo, hi := -1, -1
			for _, leaf := range d.ed.Doc.Leaves() {
				if ids[leaf.Owner] {
					if lo < 0 {
						lo = leaf.Start
					}
					hi = leaf.End
				}
			}
			if lo < 0 {
				return nil, model.ErrEditConflict
			}
			captured, err := d.ed.CaptureEdit(lo, hi)
			if err != nil {
				return nil, err
			}
			next.Targets = append(next.Targets, makeOneBotTarget(d, captured))
			continue
		}
		target := original.Target
		for _, ch := range candidate.Changes {
			if ch.TargetID != old.ID || ch.Status != "saved" && ch.Status != "applied" {
				continue
			}
			if ch.Receipt == nil {
				return nil, model.ErrEditConflict
			}
			narrow := ch.Operation.Kind == "replace_text" || ch.Operation.Kind == "paste_fragment" && ch.Operation.Fragment != nil && len(ch.Operation.Fragment.Blocks) == 1 && ch.Operation.Fragment.Blocks[0].Kind == "paragraph"
			if narrow {
				leaf, ok := d.ed.LeafByID(target.First)
				if !ok {
					return nil, model.ErrEditConflict
				}
				start := leaf.Start + target.Start
				length := model.Len(ch.Operation.Text)
				if ch.Operation.Kind == "paste_fragment" {
					length = model.Len(ch.Operation.Fragment.PlainText())
				}
				target, err = d.ed.CaptureEdit(start, start+length)
				if err != nil {
					return nil, err
				}
			}
			if !narrow {
				beforeIDs := map[string]*model.Block{}
				for _, b := range ch.Receipt.Before.Blocks {
					beforeIDs[b.ID] = b
				}
				var ids []string
				for _, b := range ch.Receipt.After.Blocks {
					if !model.SameBlock(beforeIDs[b.ID], b) {
						ids = append(ids, b.ID)
					}
				}
				lo, hi := -1, -1
				for _, leaf := range d.ed.Doc.Leaves() {
					if slices.Contains(ids, leaf.Owner) {
						if lo < 0 {
							lo = leaf.Start
						}
						hi = leaf.End
					}
				}
				if lo < 0 {
					return nil, model.ErrEditConflict
				}
				target, err = d.ed.CaptureEdit(lo, hi)
				if err != nil {
					return nil, err
				}
			}
		}
		if _, _, err = d.ed.ResolveEdit(target); err != nil {
			return nil, err
		}
		next.Targets = append(next.Targets, makeOneBotTarget(d, target))
	}
	return next, nil
}

func (a *App) stopOneBotEdit() {
	if task := a.qa.editTask; task != nil {
		task.Stopped = true
		task.Running = false
		task.Generation++
		if task.cancel != nil {
			task.cancel()
			task.cancel = nil
		}
		if err := a.persistOneBotTask(task); err != nil {
			task.Error = err.Error()
		}
	}
}

func (a *App) persistOneBotTask(t *oneBotTask) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(a.cfg.ConfigDir, "onebot", "tasks")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, t.ID+".json"), data, 0600)
}

func (a *App) checkpointOneBot(task *oneBotTask, d *document) error {
	content := d.text()
	if strings.EqualFold(path.Ext(d.rel), ".arcmd") {
		var err error
		content, err = d.ed.Committed().Encode()
		if err != nil {
			return err
		}
	}
	if !d.historyOff {
		_, err := history.SaveMilestone(d.space.Root, d.rel, content, "OneBot "+task.Created.Format("2006-01-02 15:04:05"), time.Now())
		return err
	}
	dir := filepath.Join(a.cfg.ConfigDir, "onebot", "recovery", task.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, model.NewID()+path.Ext(d.rel)), []byte(content), 0600)
}

func (a *App) applyOneBotCandidate(task *oneBotTask, index, only int) error {
	if a.win != nil && !a.qa.sync {
		return a.applyOneBotAsync(task, index, only)
	}
	return a.applyOneBotSync(task, index, only)
}

func (a *App) applyOneBotSync(task *oneBotTask, index, only int) error {
	if task == nil || task.Running || task.Stopped || index < 0 || index >= len(task.Candidates) {
		return errors.New(i18n.T("当前方案不能应用"))
	}
	candidate := task.Candidates[index]
	if !candidate.Complete {
		return errors.New(i18n.T("模型未完整结束，草稿未应用"))
	}
	var selected []*oneBotChange
	for i, ch := range candidate.Changes {
		if (only < 0 || only == i) && ch.Status == "pending" {
			selected = append(selected, ch)
		}
	}
	if len(selected) == 0 {
		return errors.New(i18n.T("没有待应用的修改"))
	}
	targets := map[string]*oneBotTarget{}
	for i := range task.Targets {
		targets[task.Targets[i].ID] = &task.Targets[i]
	}
	groups := map[*document][]*oneBotChange{}
	var order []*document
	for _, ch := range selected {
		if ch.Operation.Kind == "create_document" {
			sp := a.state.Get(task.SpaceID)
			if sp == nil || sp.IsExternal() {
				return model.ErrEditConflict
			}
			full, err := vault.Resolve(sp.Root, ch.Path)
			if err != nil {
				return err
			}
			if _, err = os.Stat(full); err == nil {
				return os.ErrExist
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		target := targets[ch.TargetID]
		if target == nil {
			return model.ErrEditConflict
		}
		d, err := a.oneBotTargetDoc(task, target)
		if err != nil {
			return err
		}
		if d.closed.Load() || d.ed.Composing() || d.ed.ReadOnly || d.conflict || d.missing {
			return model.ErrEditConflict
		}
		d.waitAsyncSave()
		disk, err := readFile(d.space, d.rel)
		if err != nil {
			return err
		}
		if disk != d.diskText {
			return model.ErrEditConflict
		}
		if target.Target.First != ch.Operation.Target.First {
			ch.Operation.Target = target.Target
		}
		if _, _, err = d.ed.ResolveEdit(ch.Operation.Target); err != nil {
			return err
		}
		if _, ok := groups[d]; !ok {
			order = append(order, d)
		}
		groups[d] = append(groups[d], ch)
	}
	// All recovery points are durable before the first document changes.
	for _, d := range order {
		if err := a.checkpointOneBot(task, d); err != nil {
			return err
		}
	}
	if err := a.persistOneBotTask(task); err != nil {
		return err
	}
	for _, d := range order {
		changes := groups[d]
		batch := model.NewID()
		ops := make([]model.EditOperation, len(changes))
		for i, ch := range changes {
			ops[i] = ch.Operation
		}
		receipt, err := d.ed.ApplyAI(ops)
		if err != nil {
			return err
		}
		if task.Receipts == nil {
			task.Receipts = map[string]*model.EditReceipt{}
		}
		task.Receipts[batch] = &receipt
		for _, ch := range changes {
			ch.Status = "applied"
			ch.Receipt = &receipt
			ch.Batch = batch
		}
		if err = a.persistOneBotTask(task); err != nil {
			task.Error = err.Error()
			return err
		}
		if err = d.save(); err != nil {
			for _, ch := range changes {
				ch.Error = err.Error()
			}
			a.persistOneBotTask(task)
			return err
		}
		for _, ch := range changes {
			ch.Status = "saved"
			ch.Error = ""
		}
		// Only our accepted transaction may update sibling preconditions.
		for _, ch := range candidate.Changes {
			if ch.Status != "pending" || ch.Path != d.rel {
				continue
			}
			if updated, e := d.ed.RebaseEdit(ch.Operation.Target); e == nil {
				ch.Operation.Target = updated
			} else {
				ch.Status = "conflict"
				ch.Error = e.Error()
			}
		}
	}
	for _, ch := range selected {
		if ch.Operation.Kind != "create_document" {
			continue
		}
		sp := a.state.Get(task.SpaceID)
		if sp == nil {
			return errors.New(i18n.T("空间已不存在"))
		}
		full, err := vault.Resolve(sp.Root, ch.Path)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return err
		}
		if err = fsutil.CreateFileAtomic(full, []byte(ch.NewText), 0644); err != nil {
			ch.Error = err.Error()
			a.persistOneBotTask(task)
			return err
		}
		ch.Status = "saved"
	}
	for i, other := range task.Candidates {
		if i == index {
			continue
		}
		for _, ch := range other.Changes {
			if ch.Status == "pending" {
				ch.Status = "stale"
			}
		}
	}
	return a.persistOneBotTask(task)
}
