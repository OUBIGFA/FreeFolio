package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/chatgpt"
	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/formats"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/spacefs"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// Questions about a space (Q&A): the panel right of the document asks a
// model about the active space's documents. The model explores them with
// read-only tools (package spacefs) the way a coding agent explores a
// repository, and answers citing the lines it read; a citation opens the
// document at that block. There is no vector index.
//
// Each space keeps its own conversation for the session. A question runs
// on a goroutine bound to the space it was asked in; its events come back
// to the main thread through win.Update.

// apiKeySecret is the API key's filename in the local credential store.
const apiKeySecret = "ai-api-key"

// secretStore is the local credential store; tests can use a map.
type secretStore interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
}

type qaState struct {
	editOptionsOpen     bool
	attachments         []oneBotAttachmentItem
	dropHover           bool
	editMode            bool
	editScope           string
	editPaths           []string
	editTask            *oneBotTask
	editHistory         []*oneBotTask
	editHistoryOpen     bool
	historyOpen         bool
	historyLoading      bool
	historyLoaded       bool
	historyErr          error
	historyEpoch        uint64
	history             []oneBotConversationSummary
	historyQuery        string
	historyConfirmClear bool
	historyList         ui.ListState
	loaderSeed          int
	editFollow          bool
	editCount           int
	editNewPath         string
	editDocumentsOpen   bool
	editDocQuery        string
	open                bool
	input               string
	// quotes are selections added to the input, sent with the next message.
	quotes []oneBotQuote
	// sessions are the conversations by space ID.
	sessions map[string]*qaSession
	// spaces are the read-only views of the spaces, by ID, kept so their
	// rendered pages are reused between questions.
	spaces map[string]*spacefs.FS
	// focusInput puts the caret in the question field next frame.
	focusInput bool
	// copiedKey and copiedUntil provide transient check feedback on copy/insert.
	copiedKey   string
	copiedUntil time.Time
	// editingTurn edits a prior user prompt inline before branching/resending.
	editingTurn   *qaTurn
	editingText   string
	focusEditTurn bool
	// shown is the panel's width as last drawn, sliding open or closed.
	shown     float32
	resizing  bool
	dragging  bool
	dragWidth float32

	secrets secretStore
	// keys are the connection's API keys read from the store, keyRead once
	// they were asked; keyShown are those the settings show in full.
	keys            []string
	keyShown        map[string]bool
	keyRead         bool
	keyID           string
	manualName      string
	modelSearch     string
	capabilityModel string
	clearModelsFor  string
	modelsEpoch     uint64
	modelsCancel    context.CancelFunc
	catalogErr      error
	keyErr          error
	// keyInput is the settings' field for adding keys.
	keyInput string
	// keyTests are the last tests of keyTestFor's keys, by key.
	keyTests   map[string]keyTest
	keyTestFor string
	keyTestGen uint64

	// models are the service's models, listed for modelsFor (its address
	// and key) so a change of either discards the list; another list is
	// fetched only on request. manualModel types a name instead of choosing one.
	models      []agent.ModelInfo
	modelsFor   string
	modelsBusy  bool
	modelsErr   error
	manualModel bool
	modelsList  ui.ListState
	// listModels lists a service's models; tests replace it.
	listModels func(agent.Settings) ([]agent.ModelInfo, error)

	// provider makes the model service of the settings; tests replace it.
	provider func(agent.Settings) agent.Provider
	// sync runs questions on the calling goroutine, for tests.
	sync bool
}

// qaSession is one space's conversation.
type qaSession struct {
	id           string
	spaceID      string
	created      time.Time
	updated      time.Time
	deleted      bool
	source       string
	history      []agent.Message
	contextStart int
	turns        []*qaTurn
	cancel       context.CancelFunc
	list         ui.ListState
	rows         []qaRow
	rowState     []qaRowsState
	preview      qaPreviewState
}

func (s *qaSession) running() bool {
	return s != nil && (s.cancel != nil || len(s.turns) > 0 && s.turns[len(s.turns)-1].revealing)
}

// qaTurn is a question and what was done to answer it.
type qaTurn struct {
	edit         *oneBotTask
	model        string
	finishReason string
	question     string
	quotes       []oneBotQuote
	attachments  []oneBotAttachmentBadge
	parts        []agent.ContentPart
	// start is the length of the history before the question, to which a
	// retry rolls it back.
	start             int
	contextBreakAfter bool
	prevContextStart  int
	steps             []*qaStep
	answer            string
	thinking          bool
	running           bool
	// revealing outlives the provider: buffered text and its final fade must
	// finish even if all deltas and the done event arrive before one UI frame.
	revealing bool
	retrying  bool
	epoch     uint64
	stopped   bool
	err       error
	loader    qaLoaderState
	// expanded and userToggled control whether the exploration timeline is open.
	expanded        bool
	userToggled     bool
	sourcesOpen     bool
	hoverSource     int
	nextHoverSource int
	view            qaTurnView
	stream          qaStream
}

// stepsOpen reports whether the agent exploration timeline is expanded.
// By default it stays open while exploring and folds once the answer streams,
// unless the user explicitly toggled it.
func (t *qaTurn) stepsOpen() bool {
	if t.userToggled {
		return t.expanded
	}
	return t.expanded || ((t.running || t.revealing) && t.displayAnswer() == "")
}

// qaStep is one tool call, as the panel lists it.
type qaStep struct {
	call       agent.ToolCall
	done       bool
	failed     bool
	result     string
	open       bool
	hitsParsed bool
	hits       []qaStepHit
}

// qaStepHit is one previewable item inside a search/grep/glob/links result.
type qaStepHit struct {
	path string
	line int
	head string
	loc  string
	text string
}

// qaReady reports whether a question can be asked, and why not.
func (a *App) qaReady() error {
	if a.prefs == nil {
		return errors.New(i18n.T("无法读取设置"))
	}
	if a.prefs.AI.Current() == nil {
		return errors.New(i18n.T("请先添加供应商"))
	}
	settings := a.qaSettings()
	if settings.Protocol == agent.ChatGPT {
		if a.chatgpt.busyID == settings.ConnectionID {
			return errors.New(i18n.T("正在处理 ChatGPT 连接"))
		}
		if !a.chatGPTSession().Sharing && !a.chatGPTSession().Pending {
			if a.chatGPTSession().Status == chatgpt.Connected {
				return errors.New(i18n.T("已登录 ChatGPT，但尚未允许使用套餐；请在设置中授权"))
			}
			return errors.New(i18n.T("请先登录 ChatGPT"))
		}
	}
	switch err := settings.Check(); {
	case err == nil:
		return nil
	case errors.Is(err, agent.ErrNoKey) && a.qa.keyErr != nil:
		return errors.New(i18n.T("无法读取 API Key：") + a.qa.keyErr.Error())
	case errors.Is(err, agent.ErrBadAddress):
		return errors.New(i18n.T("服务地址须以 https:// 开头（本机服务可用 http://）"))
	case errors.Is(err, agent.ErrNoModel):
		return errors.New(i18n.T("还没有设置模型"))
	default:
		return errors.New(i18n.T("还没有设置 API Key"))
	}
}

// aiPreset is the preset with the ID; none chosen yet is the first.
func aiPreset(id string) agent.Preset {
	if id == "" {
		return agent.Presets[0]
	}
	return agent.PresetOf(id)
}

func (a *App) qaSession(spaceID string) *qaSession {
	q := &a.qa
	if q.sessions == nil {
		q.sessions = map[string]*qaSession{}
	}
	s := q.sessions[spaceID]
	if s == nil {
		s = &qaSession{spaceID: spaceID, list: ui.ListState{FollowEnd: true, FollowAnimation: &ui.Smooth}}
		q.sessions[spaceID] = s
	}
	return s
}

// QAPage is a document file as the agent reads it: its Markdown, each
// line knowing its top-level block.
func QAPage(name, content string) (*spacefs.Page, error) {
	if vault.IsPDF(name) {
		return pdfQAPage(name, content)
	}
	if vault.IsWhiteboard(name) {
		return whiteboardQAPage(name, content)
	}
	if vault.IsConverted(name) {
		md, err := formats.Convert(name, []byte(content))
		if err != nil {
			return nil, convertError(err)
		}
		return pageOfDocument(name, editor.ImportMarkdown(md)), nil
	}
	if vault.IsSource(name) {
		body, _ := splitSource(content)
		return pageOfDocument(name, sourceModel(name, body)), nil
	}
	var doc *model.Document
	if strings.EqualFold(path.Ext(name), ".arcmd") {
		d, err := model.Decode(content)
		if err != nil {
			return nil, err
		}
		doc = d
	} else {
		doc = editor.ImportMarkdown(content)
	}
	return pageOfDocument(name, doc), nil
}

func pageOfDocument(name string, doc *model.Document) *spacefs.Page {
	text, spans := editor.MarkdownSourceSpans(doc)
	page := spacefs.PageOf(name, []spacefs.Part{{Text: text}})
	page.Spans = nil
	for _, span := range spans {
		page.Spans = append(page.Spans, spacefs.Span{Block: span.Block, Index: span.Index, From: span.From, To: span.To})
	}
	return page
}

// qaSpace is the space's read-only view, with the unsaved text of its
// open documents.
func (a *App) qaSpace(spaceID string) *spacefs.FS {
	sp := a.qaSpaceByID(spaceID)
	if sp == nil {
		return nil
	}
	q := &a.qa
	if q.spaces == nil {
		q.spaces = map[string]*spacefs.FS{}
	}
	fs := q.spaces[spaceID]
	if fs == nil || fs.Root != sp.Root {
		fs = spacefs.New(sp.Root, QAPage)
		q.spaces[spaceID] = fs
	}
	if sp.IsExternal() {
		files := sp.ExternalFiles
		if files == nil {
			files = map[string]string{}
		}
		fs.SetFiles(files)
	}
	live := map[string]*spacefs.Page{}
	for _, d := range a.residentDocuments() {
		if d.space == sp && d.loadErr == nil && d.dirty() {
			live[d.rel] = d.qaPage()
		}
	}
	fs.SetLive(live)
	return fs
}

// toggleQA opens or closes the panel; opening puts the caret in it.
func (a *App) toggleQA() {
	a.qa.open = !a.qa.open
	a.qa.focusInput = a.qa.open
}

// askQA asks the active space the question typed.
func (a *App) askQA() {
	text := strings.TrimSpace(a.qa.input)
	sp := a.qaActiveSpace()
	if sp == nil || a.qaSession(sp.ID).running() {
		return
	}
	if !a.qa.editMode && len(a.qa.attachments) == 0 && len(a.qa.quotes) == 0 {
		switch text {
		case "/clear":
			a.qa.input = ""
			a.clearOneBotContext(sp.ID)
			return
		case "/new":
			a.qa.input = ""
			a.clearQA()
			return
		}
	}
	if text == "" && (a.qa.editMode || len(a.qa.attachments) == 0) {
		return
	}
	if a.qa.editMode {
		task := a.qa.editTask
		if task != nil && !task.Running && !task.Applying {
			applied := false
			for _, candidate := range task.Candidates {
				for _, ch := range candidate.Changes {
					if ch.Status == "saved" || ch.Status == "applied" {
						applied = true
					}
				}
			}
			if applied {
				next, err := a.continueOneBotTask(task)
				if err != nil {
					task.Error = err.Error()
					return
				}
				a.keepOneBotTask(task)
				task = next
				a.qa.editTask = next
				a.qa.editFollow = false
			} else if len(task.Candidates) > 0 {
				a.keepOneBotTask(task)
				next := *task
				next.ID = model.NewID()
				next.Targets = append([]oneBotTarget(nil), task.Targets...)
				next.Receipts = nil
				next.cancel = nil
				task = &next
				a.qa.editTask = task
			}
		}
		if task == nil {
			scope := a.qa.editScope
			if scope == "" {
				scope = "document"
			}
			var err error
			task, err = a.captureOneBotTask(scope)
			if err != nil {
				a.notify("%s", err.Error())
				return
			}
			a.qa.editTask = task
		}
		if task.Running || task.Applying {
			return
		}
		if task.Scope == "new" {
			task.NewPath = strings.TrimSpace(a.qa.editNewPath)
		}
		task.Direct = a.prefs.AI.Direct
		count := a.qa.editCount
		if count < 1 {
			count = 1
		}
		a.qa.input = ""
		quotes := a.qa.quotes
		a.qa.quotes = nil
		items := a.qa.attachments
		a.qa.attachments = nil
		parts, badges, _ := splitOneBotAttachmentItems(items)
		a.mergeOneBotEditableAttachments(task, items)
		task.Parts = parts
		task.Quotes = oneBotQuotePrompt(quotes)
		s := a.qaSession(task.SpaceID)
		turn := &qaTurn{model: a.qaSettings().Model, start: len(s.history), question: text, quotes: quotes, attachments: badges, parts: parts, edit: task}
		s.turns = append(s.turns, turn)
		s.list.ScrollToEnd()
		a.saveOneBotConversation(task.SpaceID, s)
		a.runOneBotEdit(task, text, count, a.qa.editFollow)
		a.qa.editFollow = false
		return
	}
	a.qa.input = ""
	quotes := a.qa.quotes
	a.qa.quotes = nil
	items := a.qa.attachments
	a.qa.attachments = nil
	parts, badges, writableRels := splitOneBotAttachmentItems(items)
	a.askWithAttachments(sp.ID, text, quotes, parts, badges, writableRels)
}

func splitOneBotAttachmentItems(items []oneBotAttachmentItem) (parts []agent.ContentPart, badges []oneBotAttachmentBadge, writableRels []string) {
	if len(items) == 0 {
		return nil, nil, nil
	}
	parts = make([]agent.ContentPart, 0, len(items))
	badges = make([]oneBotAttachmentBadge, 0, len(items))
	seenRel := map[string]bool{}
	for _, item := range items {
		parts = append(parts, item.Part)
		badges = append(badges, item.badge())
		if item.Editable && item.RelPath != "" && !seenRel[item.RelPath] {
			seenRel[item.RelPath] = true
			writableRels = append(writableRels, item.RelPath)
		}
	}
	return parts, badges, writableRels
}

// ask runs a question in the space with the ID. Quoted selections go
// before the question in the model's copy only; the turn shows badges.
func (a *App) ask(spaceID, question string, quotes []oneBotQuote) {
	a.askWithAttachments(spaceID, question, quotes, nil, nil, nil)
}

func (a *App) askWithAttachments(spaceID, question string, quotes []oneBotQuote, parts []agent.ContentPart, badges []oneBotAttachmentBadge, writableRels []string) {
	s := a.qaSession(spaceID)
	s.contextStart = max(0, min(s.contextStart, len(s.history)))
	offset := a.qa.loaderSeed
	a.qa.loaderSeed++
	settings := a.qaSettings()
	turn := &qaTurn{
		model:       settings.Model,
		question:    question,
		quotes:      quotes,
		attachments: badges,
		parts:       parts,
		start:       len(s.history),
		running:     true,
		loader:      qaLoaderState{baseOffset: offset},
	}
	s.turns = append(s.turns, turn)
	s.list.FollowEnd = true
	s.list.ScrollToEnd()
	if err := a.qaReady(); err != nil {
		turn.running, turn.err = false, err
		a.saveOneBotConversation(spaceID, s)
		return
	}
	fs := a.qaSpace(spaceID)
	if fs == nil {
		turn.running, turn.err = false, errors.New(i18n.T("空间已不存在"))
		a.saveOneBotConversation(spaceID, s)
		return
	}
	var provider agent.Provider = settings.NewProvider()
	if a.qa.provider != nil {
		provider = a.qa.provider(settings)
	}
	open := ""
	if a.doc != nil && a.doc.space.ID == spaceID && a.doc.loadErr == nil {
		open = a.doc.rel
	}
	name := a.qaSpaceByID(spaceID).Name
	now := a.now
	if s.source != "" && s.source != settings.Signature() {
		s.history = agent.NeutralHistory(s.history)
	}
	s.source = settings.Signature()
	text := question
	if text == "" && len(parts) > 0 {
		text = "请结合附件内容回答"
	}
	if len(quotes) > 0 {
		text = oneBotQuotePrompt(quotes) + "\n" + text
	}
	s.history = append(s.history, agent.Message{Role: agent.User, Text: text, Parts: parts})
	a.saveOneBotConversation(spaceID, s)
	history := append([]agent.Message(nil), s.history[s.contextStart:]...)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	deliver := func(f func()) { f() }
	if !a.qa.sync && a.win != nil {
		deliver = func(f func()) { a.win.Update(f) }
	}
	var writableAll []string
	// An open whiteboard is edited through its own tools, never as JSON.
	var board *document
	if a.doc != nil && a.doc.space.ID == spaceID && a.doc.board != nil && !a.doc.ed.ReadOnly && a.doc.loadErr == nil {
		board = a.doc
	} else if a.doc != nil && a.doc.space.ID == spaceID && !a.doc.ed.ReadOnly && !a.doc.ed.Composing() {
		writableAll = append(writableAll, a.doc.rel)
	}
	for _, rel := range writableRels {
		writableAll = append(writableAll, rel)
	}
	editTask := a.captureOneBotEditableDocs(spaceID, writableAll)
	var imageTool *agent.Tool
	if a.doc != nil && a.doc.space.ID == spaceID {
		imageTool = a.chatImageTool(a.doc)
	}
	run := func() {
		overview, _ := fs.Overview(6000)
		tools := agent.SpaceTools(fs)
		if settings.Capabilities.Tools == agent.Unsupported {
			tools = nil
			if open != "" {
				if text, err := fs.Read(open, 1, 200); err == nil {
					overview += "\nCurrent document excerpt (read-only):\n" + text
				}
			}
		}
		system := agent.Prompt(agent.Context{Space: name, Overview: overview, Open: open, Now: now})
		if board != nil && settings.Capabilities.Tools != agent.Unsupported {
			system += "\n" + boardAgentSystem
			tools = append(tools, a.boardAgentTools(board, deliver)...)
		}
		if imageTool != nil && settings.Capabilities.Tools != agent.Unsupported {
			tools = append(tools, *imageTool)
		}
		var workspace *agent.EditWorkspace
		if editTask != nil && settings.Capabilities.Tools != agent.Unsupported {
			workspace, _ = agent.NewEditWorkspace(oneBotEditFiles(editTask), nil)
			if workspace != nil {
				system += "\n" + agent.EditSystem + "\nWritable paths: " + strings.Join(workspace.Paths(), ", ") + "\nUse offset/limit for read. The tool results describe review snapshots, not saved files."
				tools = workspace.Tools(tools)
			}
		}
		ag := &agent.Agent{
			Provider: provider,
			System:   system,
			Tools:    tools,
			Retries:  2,
		}
		steps := 0
		added, err := ag.Run(ctx, history, func(e agent.Event) {
			if e.Kind == agent.StepDone {
				steps++
			}
			deliver(func() { turn.apply(e) })
		})
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if err == nil && workspace != nil && len(workspace.Changes()) > 0 && steps >= agent.DefaultMaxSteps {
			err = agent.ErrIncomplete
		}
		deliver(func() {
			a.finishQA(s, turn, added, err)
			if err == nil && !turn.stopped && a.qa.sessions[spaceID] == s && workspace != nil && len(workspace.Changes()) > 0 && turn.finishReason == "stop" {
				candidate, e := a.prepareOneBotFileCandidate(editTask, agent.FileDraft{Summary: turn.answer, Changes: workspace.Changes()})
				if e != nil {
					turn.err = e
					a.saveOneBotConversation(spaceID, s)
					return
				}
				editTask.Instruction = question
				editTask.Candidates = []*oneBotCandidate{candidate}
				turn.edit = editTask
				a.keepOneBotTask(editTask)
				if editTask.Direct {
					if e := a.applyOneBotCandidate(editTask, 0, -1); e != nil {
						turn.err = e
					}
				}
				a.saveOneBotConversation(spaceID, s)
			}
		})
	}
	if a.qa.sync || a.win == nil {
		run()
		return
	}
	go run()
}

// apply shows an event of the run.
func (t *qaTurn) apply(e agent.Event) {
	switch e.Kind {
	case agent.Retrying:
		t.retrying = true
	case agent.ReasoningDelta:
		t.retrying = false
		t.thinking = true
	case agent.TextDelta:
		t.retrying = false
		t.thinking = false
		t.answer += e.Text
		if e.Text != "" && !t.stopped {
			t.revealing = true
		}
	case agent.StepDone:
		t.finishReason = e.FinishReason
		t.retrying = false
		t.thinking = false
		if len(e.Message.Calls) > 0 {
			// What the model said before calling tools was thinking aloud,
			// not the answer.
			t.answer = ""
			t.revealing = false
			t.epoch++
			t.stream = qaStream{}
		}
	case agent.ToolStart:
		t.retrying = false
		t.steps = append(t.steps, &qaStep{call: e.Call})
	case agent.ToolDone:
		for _, st := range t.steps {
			if st.call.ID == e.Call.ID && !st.done {
				st.done, st.failed, st.result = true, e.IsError, e.Result
				break
			}
		}
	}
}

// finishQA ends a run: its messages join the conversation, closed with an
// answer even when it stopped, as a model must see every call answered.
func (a *App) finishQA(s *qaSession, t *qaTurn, added []agent.Message, err error) {
	if id, _, ok := strings.Cut(s.source, "|"); ok {
		a.loadChatGPTSession(id, true)
	}
	s.cancel = nil
	t.running, t.thinking, t.retrying = false, false, false
	s.history = append(s.history, added...)
	if n := len(s.history); n == 0 || s.history[n-1].Role != agent.Assistant || len(s.history[n-1].Calls) > 0 {
		note := "[The answer was stopped by the user.]"
		if err != nil && !errors.Is(err, context.Canceled) {
			note = "[The answer failed: " + err.Error() + "]"
		}
		if t.answer != "" {
			note = t.answer + "\n\n" + note
		}
		s.history = append(s.history, agent.Message{Role: agent.Assistant, Text: note})
	}
	switch {
	case errors.Is(err, context.Canceled):
		t.stopped = true
	case err != nil:
		t.err = err
	}
	a.saveOneBotConversation(s.spaceID, s)
}

// stopQA stops the active space's question.
func (a *App) stopQA() {
	sp := a.qaActiveSpace()
	if sp == nil {
		return
	}
	s := a.qaSession(sp.ID)
	if s.cancel != nil {
		s.cancel()
	}
	if len(s.turns) > 0 {
		t := s.turns[len(s.turns)-1]
		t.revealing = false
		if t.running {
			t.stopped = true
		}
		t.stream.sync(t.answer, a.now)
		a.saveOneBotConversation(sp.ID, s)
	}
}

// retryQA asks any turn again, truncating downstream turns cleanly.
func (a *App) retryQA(spaceID string, t *qaTurn) {
	s := a.qaSession(spaceID)
	idx := slices.Index(s.turns, t)
	if s.running() || t == nil || idx < 0 {
		return
	}
	if a.qa.editingTurn != nil && slices.Index(s.turns[:idx], a.qa.editingTurn) < 0 {
		a.cancelEditQATurn()
	}
	if s.preview.turn != nil || s.preview.pendingTurn != nil {
		s.preview = qaPreviewState{}
	}
	s.turns = s.turns[:idx]
	s.history = s.history[:min(max(t.start, 0), len(s.history))]
	recomputeContextStart(s)
	a.askWithAttachments(spaceID, t.question, t.quotes, t.parts, t.attachments, writableRelsForTurn(spaceID, t))
}

func writableRelsForTurn(spaceID string, t *qaTurn) []string {
	var writableRels []string
	for _, b := range t.attachments {
		if b.SpaceID == spaceID && b.RelPath != "" && isEditableSpaceDoc(b.RelPath, false) {
			writableRels = append(writableRels, b.RelPath)
		}
	}
	return writableRels
}

func recomputeContextStart(s *qaSession) {
	if s == nil {
		return
	}
	for i := len(s.turns) - 1; i >= 0; i-- {
		if !s.turns[i].contextBreakAfter {
			continue
		}
		if i+1 < len(s.turns) {
			s.contextStart = min(max(s.turns[i+1].start, 0), len(s.history))
		} else {
			s.contextStart = len(s.history)
		}
		return
	}
	s.contextStart = 0
}

func activeContextTurns(s *qaSession) int {
	if s == nil || len(s.turns) == 0 {
		return 0
	}
	for i := len(s.turns) - 1; i >= 0; i-- {
		if s.turns[i].contextBreakAfter {
			return len(s.turns) - 1 - i
		}
	}
	return len(s.turns)
}

// clearOneBotContext drops prior turns from future model context while preserving
// the visible conversation above an inline divider.
func (a *App) clearOneBotContext(spaceID string) {
	s := a.qaSession(spaceID)
	if s == nil || s.running() || len(s.turns) == 0 {
		return
	}
	last := s.turns[len(s.turns)-1]
	if !last.contextBreakAfter {
		last.prevContextStart = min(max(s.contextStart, 0), len(s.history))
		last.contextBreakAfter = true
	}
	s.contextStart = len(s.history)
	a.cancelEditQATurn()
	a.qa.editMode = false
	a.qa.editTask = nil
	a.qa.editFollow = false
	a.qa.focusInput = true
	s.list.ScrollToEnd()
	a.saveOneBotConversation(spaceID, s)
}

func (a *App) undoOneBotContextBreak(spaceID string, turn *qaTurn) {
	s := a.qaSession(spaceID)
	if s == nil || s.running() || len(s.turns) == 0 || turn == nil || s.turns[len(s.turns)-1] != turn || !turn.contextBreakAfter {
		return
	}
	turn.contextBreakAfter = false
	s.contextStart = min(max(turn.prevContextStart, 0), len(s.history))
	a.saveOneBotConversation(spaceID, s)
}

func (a *App) continueQA(spaceID string, turn *qaTurn) {
	s := a.qaSession(spaceID)
	if s == nil || s.running() || len(s.turns) == 0 || s.turns[len(s.turns)-1] != turn {
		return
	}
	a.askWithAttachments(spaceID, i18n.T("请从上次中断处继续，不要重复已输出的内容"), nil, nil, nil, nil)
}

func (a *App) beginEditQATurn(turn *qaTurn) {
	if turn == nil {
		return
	}
	a.qa.editingTurn = turn
	a.qa.editingText = turn.question
	a.qa.focusEditTurn = true
}

func (a *App) cancelEditQATurn() {
	a.qa.editingTurn = nil
	a.qa.editingText = ""
	a.qa.focusEditTurn = false
}

func (a *App) submitEditQATurn(spaceID string) {
	turn := a.qa.editingTurn
	text := strings.TrimSpace(a.qa.editingText)
	s := a.qaSession(spaceID)
	idx := slices.Index(s.turns, turn)
	if turn == nil || idx < 0 || s.running() || (text == "" && len(turn.attachments) == 0 && len(turn.quotes) == 0) {
		return
	}
	a.cancelEditQATurn()
	s.preview = qaPreviewState{}
	s.turns = s.turns[:idx]
	s.history = s.history[:min(max(turn.start, 0), len(s.history))]
	recomputeContextStart(s)
	a.askWithAttachments(spaceID, text, turn.quotes, turn.parts, turn.attachments, writableRelsForTurn(spaceID, turn))
}

func (a *App) forkOneBotFromTurn(spaceID string, target *qaTurn) {
	s := a.qaSession(spaceID)
	idx := slices.Index(s.turns, target)
	if s.running() || target == nil || idx < 0 {
		return
	}
	a.saveOneBotConversation(spaceID, s)
	histEnd := len(s.history)
	if idx+1 < len(s.turns) {
		histEnd = min(max(s.turns[idx+1].start, 0), len(s.history))
	}
	turns := make([]*qaTurn, idx+1)
	for i := 0; i <= idx; i++ {
		src := s.turns[i]
		cp := &qaTurn{
			edit:              src.edit,
			model:             src.model,
			finishReason:      src.finishReason,
			question:          src.question,
			quotes:            append([]oneBotQuote(nil), src.quotes...),
			attachments:       append([]oneBotAttachmentBadge(nil), src.attachments...),
			parts:             append([]agent.ContentPart(nil), src.parts...),
			start:             min(max(src.start, 0), histEnd),
			contextBreakAfter: src.contextBreakAfter,
			prevContextStart:  src.prevContextStart,
			answer:            src.answer,
			stopped:           src.stopped,
			err:               src.err,
			expanded:          src.expanded,
			userToggled:       src.userToggled,
			sourcesOpen:       src.sourcesOpen,
		}
		if len(src.steps) > 0 {
			cp.steps = make([]*qaStep, len(src.steps))
			for j, st := range src.steps {
				stepCopy := *st
				cp.steps[j] = &stepCopy
			}
		}
		cp.stream.sync(cp.answer, a.clockNow())
		turns[i] = cp
	}
	fork := &qaSession{
		spaceID: spaceID,
		source:  s.source,
		history: sanitizeOneBotHistoryMessages(s.history[:histEnd]),
		turns:   turns,
		list:    ui.ListState{FollowEnd: true, FollowAnimation: &ui.Smooth},
	}
	recomputeContextStart(fork)
	a.qa.sessions[spaceID] = fork
	a.cancelEditQATurn()
	a.qa.editTask = nil
	a.qa.editMode = false
	a.qa.editFollow = false
	a.qa.historyOpen = false
	a.qa.focusInput = true
	a.saveOneBotConversation(spaceID, fork)
	fork.list.ScrollToEnd()
	a.notify("%s", i18n.T("已从此轮新建分支对话"))
}

func (a *App) deleteQATurnFrom(spaceID string, target *qaTurn) {
	s := a.qaSession(spaceID)
	idx := slices.Index(s.turns, target)
	if s.running() || target == nil || idx < 0 {
		return
	}
	a.cancelEditQATurn()
	s.preview = qaPreviewState{}
	if idx == 0 {
		a.qa.sessions[spaceID] = &qaSession{spaceID: spaceID, list: ui.ListState{FollowEnd: true, FollowAnimation: &ui.Smooth}}
		a.qa.focusInput = true
		return
	}
	s.turns = s.turns[:idx]
	s.history = s.history[:min(max(target.start, 0), len(s.history))]
	recomputeContextStart(s)
	a.saveOneBotConversation(spaceID, s)
}

const qaCopiedDuration = 1800 * time.Millisecond

func (a *App) copyQAText(c *ui.Context, key, text string) {
	if text == "" {
		return
	}
	c.WriteClipboard(text)
	a.qa.copiedKey = key
	a.qa.copiedUntil = c.Now().Add(qaCopiedDuration)
	c.After(qaCopiedDuration)
}

func (a *App) isQACopied(c *ui.Context, key string) bool {
	if key == "" || a.qa.copiedKey != key {
		return false
	}
	rem := a.qa.copiedUntil.Sub(c.Now())
	if rem <= 0 {
		a.qa.copiedKey = ""
		return false
	}
	c.After(rem)
	return true
}

func (a *App) canInsertQAToDoc() bool {
	return a.doc != nil && a.doc.loadErr == nil && a.doc.board == nil && !a.doc.source && a.doc.ed != nil && !a.doc.ed.ReadOnly
}

func (a *App) insertQATurnToDoc(c *ui.Context, key string, turn *qaTurn) {
	if turn == nil || !a.canInsertQAToDoc() {
		return
	}
	cleaned := qaCleanCopyMarkdown(turn.answer)
	if cleaned == "" {
		return
	}
	if a.doc.ed.InsertMarkdown(cleaned) {
		a.qa.copiedKey = key
		a.qa.copiedUntil = c.Now().Add(qaCopiedDuration)
		c.After(qaCopiedDuration)
	}
}

var qaCiteStripRe = regexp.MustCompile(`\s*\[[^\[\]\n]+#L\d+(?:-\d+)?\](?:\s*,\s*\[[^\[\]\n]+#L\d+(?:-\d+)?\])*`)

// qaCleanCopyMarkdown removes inline citation anchors while keeping Markdown formatting intact.
func qaCleanCopyMarkdown(answer string) string {
	lines := strings.Split(strings.ReplaceAll(answer, "\r\n", "\n"), "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = qaCiteStripRe.ReplaceAllString(line, "")
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// qaPlainCopyText strips citation anchors and common Markdown tokens for plain-text copy.
func qaPlainCopyText(answer string) string {
	md := qaCleanCopyMarkdown(answer)
	lines := strings.Split(md, "\n")
	for i, line := range lines {
		line = oneBotMdMarkRe.ReplaceAllString(line, "")
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func exportOneBotConversationMarkdown(s *qaSession) string {
	if s == nil || len(s.turns) == 0 {
		return ""
	}
	var out strings.Builder
	for i, turn := range s.turns {
		if i > 0 {
			out.WriteString("\n\n---\n\n")
		}
		q := strings.TrimSpace(turn.question)
		if q == "" && len(turn.attachments) > 0 {
			names := make([]string, len(turn.attachments))
			for j, b := range turn.attachments {
				names[j] = b.Name
			}
			q = strings.Join(names, ", ")
		}
		if q != "" {
			out.WriteString("### ")
			out.WriteString(q)
		}
		ans := qaCleanCopyMarkdown(turn.answer)
		if ans == "" && turn.edit != nil && turn.edit.Selected >= 0 && turn.edit.Selected < len(turn.edit.Candidates) {
			ans = qaCleanCopyMarkdown(turn.edit.Candidates[turn.edit.Selected].Text)
		}
		if ans != "" {
			if q != "" {
				out.WriteString("\n\n")
			}
			out.WriteString(ans)
		}
	}
	return strings.TrimSpace(out.String())
}

// clearQA starts a new conversation in the active space.
func (a *App) clearQA() {
	sp := a.qaActiveSpace()
	if sp == nil {
		return
	}
	s := a.qaSession(sp.ID)
	if s.cancel != nil {
		s.cancel()
	}
	if len(s.turns) > 0 && s.turns[len(s.turns)-1].running {
		s.turns[len(s.turns)-1].running = false
		s.turns[len(s.turns)-1].stopped = true
	}
	if len(s.turns) > 0 {
		a.saveOneBotConversation(sp.ID, s)
	}
	a.qa.sessions[sp.ID] = &qaSession{spaceID: sp.ID, list: ui.ListState{FollowEnd: true, FollowAnimation: &ui.Smooth}}
	a.qa.historyOpen = false
	a.qa.historyConfirmClear = false
	a.qa.focusInput = true
	a.qa.copiedKey = ""
	a.cancelEditQATurn()
	if a.qa.editTask != nil && (a.qa.editTask.Running || a.qa.editTask.Applying) {
		a.stopOneBotEdit()
	}
	a.qa.editTask = nil
	a.qa.editMode = false
	a.qa.editFollow = false
	a.qa.attachments = nil
	a.qa.quotes = nil
}

// openCitation opens the cited document of the space at the cited block.
func (a *App) openCitation(spaceID string, c agent.Citation) {
	fs := a.qa.spaces[spaceID]
	if fs == nil {
		fs = a.qaSpace(spaceID)
	}
	if fs == nil || !a.openAnywhere(spaceID, c.Path) || a.doc == nil || a.doc.loadErr != nil {
		a.notify("找不到引用的文档：%s", c.Path)
		return
	}
	if a.doc.source {
		a.doc.toggleMode()
	}
	page, err := fs.Page(c.Path)
	if err != nil {
		return
	}
	at, ok := page.Locate(c.From)
	if !ok {
		return
	}
	if a.doc.isPDF() {
		// A PDF's parts are its pages, "page:N" (pdfQAPage).
		if n, err := strconv.Atoi(strings.TrimPrefix(at.Block, "page:")); err == nil && n >= 1 {
			a.doc.pdf.GoToPage(n-1, false)
		}
		a.doc.pdf.RequestFocus = true
		return
	}
	if a.doc.board != nil {
		a.doc.board.RevealElement(at.Block)
		return
	}
	blocks := a.doc.ed.Doc.Blocks
	id := ""
	for _, b := range blocks {
		if at.Block != "" && b.ID == at.Block {
			id = b.ID
		}
	}
	if id == "" && at.Index < len(blocks) {
		// A Markdown file's blocks get new IDs each time it is read; its
		// order is the same.
		id = blocks[at.Index].ID
	}
	if id != "" {
		a.doc.ed.RevealBlock(id)
	}
}

// qaStepMeta breaks a tool call into structured UI parts for the timeline.
type qaStepMeta struct {
	verb    string
	active  string
	subject string
	scope   string
	docPath string
	from    int
	to      int
}

func stepMeta(call agent.ToolCall) qaStepMeta {
	var args struct {
		Query   string `json:"query"`
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Start   int    `json:"start_line"`
		End     int    `json:"end_line"`
		Offset  int    `json:"offset"`
		Limit   int    `json:"limit"`
	}
	_ = json.Unmarshal([]byte(call.Args), &args)
	switch call.Name {
	case agent.ToolSearch:
		return qaStepMeta{
			verb:    i18n.T("检索"),
			active:  i18n.T("正在检索"),
			subject: "“" + args.Query + "”",
			scope:   cleanScopeLabel(args.Path),
		}
	case agent.ToolGrep:
		return qaStepMeta{
			verb:    i18n.T("搜索"),
			active:  i18n.T("正在搜索"),
			subject: "“" + args.Pattern + "”",
			scope:   cleanScopeLabel(args.Path),
		}
	case agent.ToolRead:
		if args.Offset > 0 {
			args.Start = args.Offset
			if args.Limit > 0 {
				args.End = args.Offset + args.Limit - 1
			}
		}
		title := vault.Title(args.Path)
		scope := ""
		from := max(args.Start, 1)
		to := from
		if args.Start > 0 || args.End > 0 {
			if args.End > 0 {
				to = max(args.End, from)
				scope = humanLines(from, to)
			} else {
				scope = fmt.Sprintf(i18n.T("第 %d 行起"), from)
			}
		} else if dir := docFolderBreadcrumb(args.Path); dir != "" {
			scope = dir
		}
		return qaStepMeta{
			verb:    i18n.T("阅读"),
			active:  i18n.T("正在阅读"),
			subject: title,
			scope:   scope,
			docPath: args.Path,
			from:    from,
			to:      to,
		}
	case "edit", "write", "patch":
		return qaStepMeta{verb: i18n.T("修改"), active: i18n.T("正在准备修改"), subject: vault.Title(args.Path), docPath: args.Path}
	case agent.ToolGlob:
		sub := args.Pattern
		if sub == "" {
			sub = i18n.T("全部文档")
		}
		return qaStepMeta{
			verb:    i18n.T("浏览"),
			active:  i18n.T("正在浏览"),
			subject: sub,
		}
	case agent.ToolOutline:
		return qaStepMeta{
			verb:    i18n.T("目录"),
			active:  i18n.T("正在查看目录"),
			subject: vault.Title(args.Path),
			scope:   docFolderBreadcrumb(args.Path),
			docPath: args.Path,
			from:    1,
			to:      1,
		}
	case agent.ToolLinks:
		return qaStepMeta{
			verb:    i18n.T("双链"),
			active:  i18n.T("正在追踪链接"),
			subject: vault.Title(args.Path),
			scope:   docFolderBreadcrumb(args.Path),
			docPath: args.Path,
			from:    1,
			to:      1,
		}
	}
	return qaStepMeta{verb: call.Name, active: call.Name, subject: call.Args}
}

func cleanScopeLabel(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" || p == "." {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(p), ".md") || strings.HasSuffix(strings.ToLower(p), ".arcmd") {
		return vault.Title(p)
	}
	return strings.ReplaceAll(p, "/", " / ")
}

func docFolderBreadcrumb(rel string) string {
	dir := path.Dir(strings.Trim(rel, "/"))
	if dir == "" || dir == "." || dir == "/" {
		return ""
	}
	return strings.ReplaceAll(dir, "/", " / ")
}

func humanLines(from, to int) string {
	from = max(from, 1)
	if to > from {
		return fmt.Sprintf(i18n.T("第 %d–%d 行"), from, to)
	}
	return fmt.Sprintf(i18n.T("第 %d 行"), from)
}

var (
	countRe      = regexp.MustCompile(`^(\d+) (relevant passages|matches|documents)`)
	searchHeadRe = regexp.MustCompile(`^\[\d+\] ([^\(\n]+) \((.+), L(\d+)(?:-(\d+))?\)$`)
	grepHitRe    = regexp.MustCompile(`^([^:\n]+):(\d+):(.*)$`)
	linkOutRe    = regexp.MustCompile(`^\s*\[\[([^\]]+)\]\] → (.+)$`)
	linkInRe     = regexp.MustCompile(`^\s*([^:\n]+):(\d+):(.*)$`)
)

// stepResult is a short account of a call's result.
func stepResult(st *qaStep) string {
	switch {
	case !st.done:
		return ""
	case st.failed:
		return i18n.T("失败")
	case strings.HasPrefix(st.result, "No matches"),
		strings.HasPrefix(st.result, "No documents"),
		strings.HasPrefix(st.result, "No relevant passages"):
		return i18n.T("无结果")
	}
	if m := countRe.FindStringSubmatch(st.result); m != nil {
		switch m[2] {
		case "relevant passages":
			return fmt.Sprintf(i18n.T("召回 %s 段"), m[1])
		case "matches":
			return fmt.Sprintf(i18n.T("找到 %s 处"), m[1])
		default:
			return fmt.Sprintf(i18n.T("%s 篇"), m[1])
		}
	}
	switch st.call.Name {
	case agent.ToolRead:
		lines := strings.Count(st.result, "→")
		if lines > 0 {
			return fmt.Sprintf(i18n.T("已读 %d 行"), lines)
		}
	case agent.ToolOutline:
		if strings.Contains(st.result, "(no headings)") {
			return i18n.T("无标题")
		}
		n := max(0, strings.Count(strings.TrimSpace(st.result), "\n"))
		if n > 0 {
			return fmt.Sprintf(i18n.T("%d 节"), n)
		}
	}
	return ""
}

// ensureHits parses up to 5 clickable preview items for search/grep/glob/links steps.
func (st *qaStep) ensureHits() []qaStepHit {
	if !st.done || st.failed {
		return nil
	}
	if st.hitsParsed {
		return st.hits
	}
	st.hitsParsed = true
	lines := strings.Split(st.result, "\n")
	const maxPreview = 5

	switch st.call.Name {
	case agent.ToolSearch:
		for i := 0; i < len(lines) && len(st.hits) < maxPreview; i++ {
			m := searchHeadRe.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			from, _ := strconv.Atoi(m[3])
			to := from
			if m[4] != "" {
				to, _ = strconv.Atoi(m[4])
			}
			snippet := ""
			for j := i + 1; j < len(lines) && j <= i+5; j++ {
				if _, after, ok := strings.Cut(lines[j], "→"); ok {
					t := cleanPreviewSnippet(after)
					if t != "" {
						snippet = t
						break
					}
				} else {
					break
				}
			}
			st.hits = append(st.hits, qaStepHit{
				path: strings.TrimSpace(m[1]),
				line: max(from, 1),
				head: m[2],
				loc:  humanLines(from, to),
				text: snippet,
			})
		}
	case agent.ToolGrep:
		for _, l := range lines[min(1, len(lines)):] {
			if len(st.hits) >= maxPreview {
				break
			}
			if l == "--" || strings.HasPrefix(l, "(") || strings.HasPrefix(l, "Could not read") {
				continue
			}
			m := grepHitRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			ln, _ := strconv.Atoi(m[2])
			p := strings.TrimSpace(m[1])
			st.hits = append(st.hits, qaStepHit{
				path: p,
				line: max(ln, 1),
				head: vault.Title(p),
				loc:  humanLines(ln, ln),
				text: cleanPreviewSnippet(m[3]),
			})
		}
	case agent.ToolGlob:
		for _, l := range lines[min(1, len(lines)):] {
			if len(st.hits) >= maxPreview {
				break
			}
			parts := strings.Split(l, "\t")
			if len(parts) < 3 {
				continue
			}
			p := strings.TrimSpace(parts[0])
			title := strings.TrimSpace(parts[1])
			if title == "" {
				title = vault.Title(p)
			}
			cnt := strings.TrimSuffix(strings.TrimSpace(parts[2]), " lines")
			loc := ""
			if cnt != "" {
				loc = fmt.Sprintf(i18n.T("%s 行"), cnt)
			}
			st.hits = append(st.hits, qaStepHit{
				path: p,
				line: 1,
				head: title,
				loc:  loc,
				text: docFolderBreadcrumb(p),
			})
		}
	case agent.ToolLinks:
		for _, l := range lines {
			if len(st.hits) >= maxPreview {
				break
			}
			if m := linkOutRe.FindStringSubmatch(l); m != nil {
				p := strings.TrimSpace(m[2])
				st.hits = append(st.hits, qaStepHit{
					path: p,
					line: 1,
					head: vault.Title(p),
					loc:  i18n.T("引用文档"),
				})
				continue
			}
			if m := linkInRe.FindStringSubmatch(l); m != nil && strings.HasSuffix(strings.ToLower(strings.TrimSpace(m[1])), ".md") {
				p := strings.TrimSpace(m[1])
				ln, _ := strconv.Atoi(m[2])
				st.hits = append(st.hits, qaStepHit{
					path: p,
					line: max(ln, 1),
					head: vault.Title(p),
					loc:  humanLines(ln, ln),
					text: cleanPreviewSnippet(m[3]),
				})
			}
		}
	}
	return st.hits
}

func cleanPreviewSnippet(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "#> ")
	for _, pfx := range []string{"- [ ] ", "- [x] ", "- [X] ", "- ", "* "} {
		if strings.HasPrefix(s, pfx) {
			s = strings.TrimSpace(s[len(pfx):])
			break
		}
	}
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(s)
}

// qaErrorText says what went wrong with a question in the user's words,
// the service's own message after it.
func qaErrorText(err error) string {
	var auth *chatgpt.Error
	if errors.As(err, &auth) {
		return chatGPTErrorText(err)
	}
	var api *agent.APIError
	switch {
	case errors.As(err, &api):
		if api.Protocol == agent.ChatGPT || strings.HasPrefix(api.Code, "subscription_sharing_") || strings.HasPrefix(api.Code, "chatpass_") {
			text := i18n.T("ChatGPT 套餐请求未获准，请检查账号权限或稍后重试")
			switch api.Code {
			case "subscription_sharing_usage_limit_exceeded":
				text = i18n.T("已达到 ChatGPT 套餐或此应用的用量限制；请打开「管理用量」查看")
			case "subscription_sharing_user_not_eligible":
				text = i18n.T("此 ChatGPT 账号或工作区暂不支持套餐使用；请检查账号资格")
			case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable":
				text = i18n.T("ChatGPT 用量服务暂时不可用；连接已保留，请稍后重试")
			case "subscription_sharing_unsupported_capability":
				text = i18n.T("ChatGPT 套餐暂不支持此次请求的模型、附件或工具")
			}
			if api.RequestID != "" {
				text += "\n" + i18n.T("请求编号：") + api.RequestID
			}
			if api.Code != "" {
				text += "\n" + api.Code
			}
			if api.Param != "" {
				text += " (" + api.Param + ")"
			}
			return text
		}
		head := fmt.Sprintf(i18n.T("模型服务出错（%d）"), api.Status)
		switch {
		case api.Status == 401 || api.Status == 403:
			head = fmt.Sprintf(i18n.T("API Key 无效或没有权限（%d）"), api.Status)
		case api.Status == 404:
			head = i18n.T("找不到这个模型或服务地址（404）")
		case api.Status == 429:
			head = i18n.T("请求太频繁或额度已用完（429）")
		}
		if api.Message != "" {
			head += "：" + api.Message
		}
		return head
	case errors.Is(err, agent.ErrStreamCut):
		return i18n.T("回答中途断开了，请重试")
	case strings.HasPrefix(err.Error(), "cannot reach the model service"):
		return i18n.T("无法连接模型服务，请检查网络与服务地址")
	}
	return errText(err)
}

// qaActiveSpace is the space OneBot works in: the file browser's opened
// files while it is shown, the active space otherwise.
func (a *App) qaActiveSpace() *space.Space {
	if a.files != nil {
		return a.files.locations
	}
	return a.state.ActiveSpace()
}

// qaSpaceByID finds a space OneBot refers to, the file browser's included.
func (a *App) qaSpaceByID(id string) *space.Space {
	if a.files != nil && id == a.files.locations.ID {
		return a.files.locations
	}
	return a.state.Get(id)
}
