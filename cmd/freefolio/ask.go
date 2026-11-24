package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/app"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/spacefs"
)

const askUsage = `usage: freefolio ask [flags] "question"

Asks a question about a space's documents in the terminal, printing each
search and read the agent makes, the answer, and whether its citations
point at real lines: for trying out and evaluating the agent.

The model is the one set in Settings › AI, or given by flags; the API key
comes from FREEFOLIO_AI_KEY (the app's own key stays in its local config folder).

`

// ask runs "freefolio ask"; it returns the process's exit code.
func ask(args []string, cfg app.Config, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, askUsage); fs.PrintDefaults() }
	spaceArg := fs.String("space", "", "space name, ID or folder (default: the active space)")
	base := fs.String("base-url", os.Getenv("FREEFOLIO_AI_BASE_URL"), "Chat Completions API root (default: settings)")
	model := fs.String("model", os.Getenv("FREEFOLIO_AI_MODEL"), "model name (default: settings)")
	steps := fs.Int("max-steps", agent.DefaultMaxSteps, "most model steps for the question")
	verbose := fs.Bool("v", false, "print each tool's whole result")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	question := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if question == "" {
		fs.Usage()
		return 2
	}
	p, err := prefs.Store{Path: filepath.Join(cfg.ConfigDir, "prefs.json")}.Load()
	if err != nil {
		fmt.Fprintln(stderr, "freefolio:", err)
		return 1
	}
	settings := agent.Settings{BaseURL: *base, Model: *model, APIKey: os.Getenv("FREEFOLIO_AI_KEY")}
	// Flags override the selected OneBot connection; the key still comes only
	// from the environment so the CLI never reads the GUI's secret store.
	if c := p.AI.Current(); c != nil {
		settings.ConnectionID, settings.Protocol, settings.Vendor, settings.Parameters = c.ID, c.Protocol, c.Vendor, c.Parameters
		if settings.BaseURL == "" {
			settings.BaseURL = strings.TrimSpace(c.BaseURL)
		}
		if settings.Model == "" {
			settings.Model = c.Model
		}
	}
	if err := settings.Check(); err != nil {
		fmt.Fprintln(stderr, "freefolio: cannot ask:", err, "(set them in Settings › AI, or with -base-url, -model and FREEFOLIO_AI_KEY)")
		return 1
	}
	sp, err := findSpace(cfg, *spaceArg)
	if err != nil {
		fmt.Fprintln(stderr, "freefolio:", err)
		return 1
	}
	view := spacefs.New(sp.Root, app.QAPage)
	if sp.IsExternal() {
		files := sp.ExternalFiles
		if files == nil {
			files = map[string]string{}
		}
		view.SetFiles(files)
	}
	overview, err := view.Overview(6000)
	if err != nil {
		fmt.Fprintln(stderr, "freefolio: cannot read the space:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ag := &agent.Agent{
		Provider: settings.Provider(),
		System:   agent.Prompt(agent.Context{Space: sp.Name, Overview: overview, Now: time.Now()}),
		Tools:    agent.SpaceTools(view),
		MaxSteps: *steps,
		Retries:  2,
	}
	fmt.Fprintf(stderr, "space %q (%s), model %s\n\n", sp.Name, sp.Root, settings.Model)
	started := time.Now()
	var answer strings.Builder
	var input, output int
	added, err := ag.Run(ctx, []agent.Message{{Role: agent.User, Text: question}}, func(e agent.Event) {
		switch e.Kind {
		case agent.TextDelta:
			answer.WriteString(e.Text)
			fmt.Fprint(stdout, e.Text)
		case agent.StepDone:
			input += e.Usage.Input
			output += e.Usage.Output
			if len(e.Message.Calls) > 0 && answer.Len() > 0 {
				fmt.Fprintln(stdout)
				answer.Reset()
			}
		case agent.ToolStart:
			fmt.Fprintf(stderr, "→ %s %s\n", e.Call.Name, e.Call.Args)
		case agent.ToolDone:
			result := e.Result
			if !*verbose {
				result, _, _ = strings.Cut(result, "\n")
			}
			fmt.Fprintf(stderr, "  ← %s\n", strings.ReplaceAll(result, "\n", "\n    "))
		case agent.Retrying:
			fmt.Fprintln(stderr, "  (retrying:", e.Text+")")
		}
	})
	fmt.Fprintln(stdout)
	if err != nil {
		fmt.Fprintln(stderr, "freefolio:", err)
		return 1
	}
	final := ""
	if n := len(added); n > 0 {
		final = added[n-1].Text
	}
	calls, modelSteps := 0, 0
	for _, m := range added {
		if m.Role == agent.Assistant {
			modelSteps++
			calls += len(m.Calls)
		}
	}
	fmt.Fprintf(stderr, "\n%d tool calls, %d model steps, %s", calls, modelSteps, time.Since(started).Round(100*time.Millisecond))
	if input+output > 0 {
		fmt.Fprintf(stderr, ", %d tokens in, %d out", input, output)
	}
	fmt.Fprintln(stderr)
	bad := 0
	for _, c := range agent.Citations(final) {
		page, err := view.Page(c.Path)
		switch {
		case err != nil:
			fmt.Fprintf(stderr, "✗ [%s#L%d] %v\n", c.Path, c.From, err)
			bad++
		case c.From < 1 || c.To > len(page.Lines):
			fmt.Fprintf(stderr, "✗ [%s#L%d-L%d] past the document's %d lines\n", c.Path, c.From, c.To, len(page.Lines))
			bad++
		}
	}
	if bad > 0 {
		return 3
	}
	return 0
}

// findSpace is the space named by arg: a folder, a space's name or ID, or
// the active space when arg is empty.
func findSpace(cfg app.Config, arg string) (*space.Space, error) {
	if arg != "" {
		if info, err := os.Stat(arg); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(arg)
			return &space.Space{Name: filepath.Base(abs), Root: abs}, nil
		}
	}
	st, err := space.NewStore(filepath.Join(cfg.ConfigDir, "spaces.json")).Load()
	if err != nil {
		return nil, err
	}
	if arg == "" {
		if sp := st.ActiveSpace(); sp != nil {
			return sp, nil
		}
		return nil, fmt.Errorf("there is no space yet; give a folder with -space")
	}
	for _, sp := range st.Spaces {
		if sp.ID == arg || strings.EqualFold(sp.Name, arg) {
			return sp, nil
		}
	}
	return nil, fmt.Errorf("no space named %q", arg)
}
