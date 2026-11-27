package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

// OneBot reads and edits the open whiteboard through these tools, as
// Dim0's board-aware agent does (MIT, by design only): it sees the
// elements and their connections, and creates cards, lines and AI nodes
// that are ordinary undoable edits of the board.

const boardAgentSystem = "The open document is a whiteboard. Use the board_* tools to read it and to add or change cards, connections and AI nodes on it; every change can be undone by the user. Element IDs come from board_read. Do not describe edits you did not make."

func schema(props string, required ...string) json.RawMessage {
	req, _ := json.Marshal(required)
	return json.RawMessage(`{"type":"object","properties":{` + props + `},"required":` + string(req) + `}`)
}

// boardAgentTools are the tools on the board document d. Each runs on the
// main thread through deliver and waits for it.
func (a *App) boardAgentTools(d *document, deliver func(func())) []agent.Tool {
	onMain := func(fn func() (string, error)) (string, error) {
		type result struct {
			out string
			err error
		}
		ch := make(chan result, 1)
		deliver(func() {
			if d.closed.Load() || d.board == nil {
				ch <- result{err: errors.New("the whiteboard was closed")}
				return
			}
			out, err := fn()
			ch <- result{out, err}
		})
		r := <-ch
		return r.out, r.err
	}
	options := func() editor.BoardOptions { return a.boardOptionsWith(nil, d) }
	parse := func(raw json.RawMessage, v any) error {
		if err := json.Unmarshal(raw, v); err != nil {
			return fmt.Errorf("invalid arguments: %w", err)
		}
		return nil
	}
	tool := func(name, desc string, params json.RawMessage, mutates bool, run func(json.RawMessage) (string, error)) agent.Tool {
		return agent.Tool{Spec: agent.ToolSpec{Name: name, Description: desc, Parameters: params}, Mutates: mutates, Run: func(_ context.Context, args json.RawMessage) (string, error) {
			return onMain(func() (string, error) { return run(args) })
		}}
	}
	// place is a free spot right of an element, or near the view's center.
	place := func(near string, w, h float64) whiteboard.Point {
		b := d.board
		if e := b.Scene.ElementByID(near); e != nil {
			return whiteboard.Point{e.X + e.W + 100, e.Y}
		}
		c := b.ViewCenter()
		return whiteboard.Point{c[0] - w/2, c[1] - h/2}
	}
	return []agent.Tool{
		tool("board_read", "List the whiteboard's elements (ID, type, position, size, text, link, AI node settings and result) and its connections. scope is \"all\" (default) or \"selection\".", schema(`"scope":{"type":"string","enum":["all","selection"]}`), false, func(raw json.RawMessage) (string, error) {
			var args struct{ Scope string }
			if err := parse(raw, &args); err != nil {
				return "", err
			}
			return boardListing(d.board, args.Scope == "selection"), nil
		}),
		tool("board_add_card", "Add a card with text: kind \"note\" (sticky note, default), \"card\" (rectangle) or \"text\". Place it right of the element near, or at x/y. Returns its ID.", schema(`"text":{"type":"string"},"kind":{"type":"string","enum":["note","card","text"]},"near":{"type":"string"},"x":{"type":"number"},"y":{"type":"number"}`, "text"), true, func(raw json.RawMessage) (string, error) {
			var args struct {
				Text, Kind, Near string
				X, Y             *float64
			}
			if err := parse(raw, &args); err != nil {
				return "", err
			}
			b := d.board
			o := options()
			at := place(args.Near, 240, 180)
			if args.X != nil && args.Y != nil {
				at = whiteboard.Point{*args.X, *args.Y}
			}
			var id string
			b.Edit(o, func() {
				label := whiteboard.NewElement(whiteboard.KindText, at[0], at[1], 0, 0)
				label.Text, label.OriginalText = args.Text, args.Text
				if args.Kind == "text" {
					b.Scene.Elements = append(b.Scene.Elements, label)
					b.Scene.LayoutText(label.ID, editor.BoardMeasure)
					id = label.ID
					return
				}
				kind := whiteboard.KindNote
				if args.Kind == "card" {
					kind = whiteboard.KindRectangle
				}
				box := whiteboard.NewElement(kind, at[0], at[1], 240, 180)
				label.FontSize = 16
				b.Scene.Elements = append(b.Scene.Elements, box, label)
				_ = b.Scene.BindText(label.ID, box.ID, editor.BoardMeasure)
				id = box.ID
			})
			return "created " + id, nil
		}),
		tool("board_set_text", "Replace the text of a text element, or of a card's or note's label.", schema(`"id":{"type":"string"},"text":{"type":"string"}`, "id", "text"), true, func(raw json.RawMessage) (string, error) {
			var args struct{ ID, Text string }
			if err := parse(raw, &args); err != nil {
				return "", err
			}
			b := d.board
			e := b.Scene.ElementByID(args.ID)
			if e == nil || e.IsDeleted {
				return "", fmt.Errorf("no element %s", args.ID)
			}
			if e.Locked {
				return "", errors.New("the element is locked")
			}
			target := e
			if e.Kind != whiteboard.KindText {
				target = b.Scene.ElementByID(e.BoundTextID())
			}
			b.Edit(options(), func() {
				if target == nil {
					t := whiteboard.NewElement(whiteboard.KindText, e.X, e.Y, 0, 0)
					t.Text, t.OriginalText = args.Text, args.Text
					b.Scene.Elements = append(b.Scene.Elements, t)
					if err := b.Scene.BindText(t.ID, e.ID, editor.BoardMeasure); err != nil {
						b.Scene.Elements = b.Scene.Elements[:len(b.Scene.Elements)-1]
					}
					return
				}
				b.Scene.SetText(target.ID, args.Text, editor.BoardMeasure)
			})
			return "updated " + args.ID, nil
		}),
		tool("board_connect", "Connect one element to another. Into an AI node it links the source to an input slot: label picks the slot (\"prompt\" replaces the node's instruction, \"text\", \"image\", \"mask\"); without a label the source is material, put in the first free text or picture slot. Between other elements it draws an arrow with label as its text.", schema(`"from":{"type":"string"},"to":{"type":"string"},"label":{"type":"string"}`, "from", "to"), true, func(raw json.RawMessage) (string, error) {
			var args struct{ From, To, Label string }
			if err := parse(raw, &args); err != nil {
				return "", err
			}
			if err := d.board.Connect(options(), args.From, args.To, args.Label); err != nil {
				return "", err
			}
			return "connected", nil
		}),
		tool("board_add_ai_node", "Add an AI node: role text (writes text), image (generates or edits a picture), video, concat (joins texts by template) or note (reads a [[document]]). inputs are element IDs connected into it in order. It uses the model the user last chose for the role. Returns its ID.", schema(`"role":{"type":"string","enum":["text","image","video","concat","note"]},"instruction":{"type":"string"},"title":{"type":"string"},"note":{"type":"string"},"inputs":{"type":"array","items":{"type":"string"}},"near":{"type":"string"}`, "role"), true, func(raw json.RawMessage) (string, error) {
			var args struct {
				Role, Instruction, Title, Note, Near string
				Inputs                               []string
			}
			if err := parse(raw, &args); err != nil {
				return "", err
			}
			role := whiteboard.AIRole(args.Role)
			if !slices.Contains(whiteboard.AIRoles, role) {
				return "", fmt.Errorf("unknown role %q", args.Role)
			}
			b, o := d.board, options()
			id := b.AddAINode(role, b.ViewCenter(), o)
			if id == "" {
				return "", errors.New("the whiteboard is read-only")
			}
			e := b.Scene.ElementByID(id)
			n := whiteboard.AINodeOf(e)
			n.Instruction, n.Title, n.Note = args.Instruction, args.Title, strings.Trim(args.Note, "[]")
			b.Edit(o, func() { _ = whiteboard.SetAINode(e, n) })
			for _, in := range args.Inputs {
				if err := b.Connect(o, in, id, ""); err != nil {
					return "created " + id + "; could not connect " + in + ": " + err.Error(), nil
				}
			}
			return "created " + id, nil
		}),
		tool("board_run_ai", "Start running AI nodes (all when ids is empty); upstream also runs the nodes they read. Results appear on the board when ready; read the board later to see them.", schema(`"ids":{"type":"array","items":{"type":"string"}},"upstream":{"type":"boolean"}`), true, func(raw json.RawMessage) (string, error) {
			var args struct {
				IDs      []string
				Upstream bool
			}
			if err := parse(raw, &args); err != nil {
				return "", err
			}
			b := d.board
			b.RunAI(args.IDs, args.Upstream, false, options())
			if !b.AIRunning() {
				if err := b.LastError(); err != nil {
					return "", err
				}
				return "finished", nil
			}
			return "started", nil
		}),
		tool("board_arrange", "Line up elements: layout \"row\" (left to right) or \"column\" (top to bottom), evenly spaced.", schema(`"ids":{"type":"array","items":{"type":"string"}},"layout":{"type":"string","enum":["row","column"]}`, "ids", "layout"), true, func(raw json.RawMessage) (string, error) {
			var args struct {
				IDs    []string
				Layout string
			}
			if err := parse(raw, &args); err != nil {
				return "", err
			}
			b := d.board
			b.Edit(options(), func() {
				// From the top-left of the elements, each after the last.
				x, y := math.Inf(1), math.Inf(1)
				for _, id := range args.IDs {
					if e := b.Scene.ElementByID(id); e != nil {
						x, y = math.Min(x, e.X), math.Min(y, e.Y)
					}
				}
				for _, id := range args.IDs {
					e := b.Scene.ElementByID(id)
					if e == nil {
						continue
					}
					b.Scene.Translate(b.Scene.ExpandSelection([]string{id}, true), whiteboard.Point{x - e.X, y - e.Y})
					if args.Layout == "column" {
						y += e.H + 60
					} else {
						x += e.W + 80
					}
				}
			})
			return "arranged", nil
		}),
		tool("board_delete", "Delete elements by ID (the user can undo it).", schema(`"ids":{"type":"array","items":{"type":"string"}}`, "ids"), true, func(raw json.RawMessage) (string, error) {
			var args struct{ IDs []string }
			if err := parse(raw, &args); err != nil {
				return "", err
			}
			b := d.board
			b.Edit(options(), func() { b.Scene.Delete(args.IDs) })
			return fmt.Sprintf("deleted %d", len(args.IDs)), nil
		}),
	}
}

// boardListing describes a board to the model, one element per line.
func boardListing(b *editor.Board, selection bool) string {
	s := b.Scene
	chosen := map[string]bool{}
	for _, id := range b.SelectedIDs() {
		chosen[id] = true
	}
	var lines []string
	for _, e := range s.Elements {
		if e.IsDeleted || e.Kind == whiteboard.KindText && e.ContainerID != "" || e.IsLinear() || selection && !chosen[e.ID] {
			continue
		}
		line := fmt.Sprintf("%s | %s | at %.0f,%.0f size %.0fx%.0f", e.ID, e.Kind, e.X, e.Y, e.W, e.H)
		if n := whiteboard.AINodeOf(e); n != nil {
			line = fmt.Sprintf("%s | ai-%s node | at %.0f,%.0f | model %s | instruction %q", e.ID, n.Role, e.X, e.Y, n.Model, n.Instruction)
			if out := n.Output(); out != nil {
				if out.Kind == "text" {
					line += fmt.Sprintf(" | result %q", clip(out.Text, 600))
				} else {
					line += " | result " + out.Kind + " " + out.Path
				}
			}
			if n.Error != "" {
				line += " | error " + n.Error
			}
		} else if t := s.ElementText(e); t != "" {
			line += fmt.Sprintf(" | text %q", clip(t, 600))
		}
		if e.IsFrame() && e.Name != "" {
			line += fmt.Sprintf(" | name %q", e.Name)
		}
		if e.Link != "" {
			line += " | link " + e.Link
		}
		lines = append(lines, line)
	}
	for _, a := range s.Elements {
		if a.IsDeleted || !a.IsLinear() || a.StartBinding == nil || a.EndBinding == nil {
			continue
		}
		if selection && !chosen[a.StartBinding.ElementID] && !chosen[a.EndBinding.ElementID] {
			continue
		}
		line := "connection " + a.StartBinding.ElementID + " -> " + a.EndBinding.ElementID
		if label := s.ElementText(s.ElementByID(a.BoundTextID())); label != "" {
			line += fmt.Sprintf(" label %q", label)
		}
		lines = append(lines, line)
	}
	// Node links are not arrows; they are listed by their input slot.
	for _, e := range s.Elements {
		n := whiteboard.AINodeOf(e)
		if n == nil || e.IsDeleted {
			continue
		}
		for _, sl := range whiteboard.AISlots(n) {
			from := s.AILinkSource(n, sl.Name)
			if from == nil || selection && !chosen[from.ID] && !chosen[e.ID] {
				continue
			}
			lines = append(lines, fmt.Sprintf("link %s -> %s slot %s", from.ID, e.ID, sl.Name))
		}
	}
	if len(lines) == 0 {
		return "The whiteboard is empty."
	}
	return strings.Join(lines, "\n")
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
