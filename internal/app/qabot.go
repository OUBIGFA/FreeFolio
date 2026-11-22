package app

import (
	"math"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/agent"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

const (
	qaOrbBoxSize       = 26
	qaOrbCanonicalSize = 20
	qaOrbMorphDuration = 240 * time.Millisecond
	qaOrbCyclePeriod   = 4500 * time.Millisecond
	qaOrbReducedTime   = 0.6
)

// qaOrbState keeps per-turn Thinking Orb playback and crossfade state.
type qaOrbState struct {
	initialized bool
	origin      time.Time
	baseOffset  int
	manualStep  int
	style       qaOrbStyle
	prevStyle   qaOrbStyle
	morphAt     time.Time
	morphing    bool
}

// orbStyle picks a semantic Thinking Orb style for the turn's live phase,
// combining Working (orbits), Solving (rubik), Thinking (ribbon),
// Agent thinking (ring), and Connecting (web) so long runs stay varied.
func (t *qaTurn) orbStyle(now time.Time) qaOrbStyle {
	if t.retrying {
		return orbConnecting
	}
	if t.thinking {
		if (t.orb.baseOffset+len(t.steps))%2 == 0 {
			return orbThinking
		}
		return orbAgentThinking
	}
	for i := len(t.steps) - 1; i >= 0; i-- {
		st := t.steps[i]
		if st.done {
			continue
		}
		switch st.call.Name {
		case agent.ToolSearch, agent.ToolGrep, agent.ToolGlob:
			if (t.orb.baseOffset+i)%2 == 0 {
				return orbWorking
			}
			return orbSolving
		case agent.ToolRead, agent.ToolOutline:
			if (t.orb.baseOffset+i)%2 == 0 {
				return orbAgentThinking
			}
			return orbThinking
		case agent.ToolLinks:
			return orbConnecting
		case "edit", "write", "patch":
			return orbSolving
		}
	}
	// Initial waiting or between steps: cycle through the four primary styles
	// (Working, Solving, Thinking, Agent thinking) every 4.5s.
	cycle := [4]qaOrbStyle{orbWorking, orbSolving, orbThinking, orbAgentThinking}
	slot := t.orb.baseOffset
	if !t.orb.origin.IsZero() && !now.Before(t.orb.origin) {
		slot += int(now.Sub(t.orb.origin) / qaOrbCyclePeriod)
	}
	return cycle[slot%len(cycle)]
}

func qaOrbColor(white, alpha float64, dark bool) ui.Color {
	w := min(1.0, max(0.0, white))
	if dark {
		w = 1 - w
	}
	g := uint8(math.Round(w * 255))
	a := float32(min(1.0, max(0.0, alpha)))
	return ui.RGBA(g, g, g, a)
}

func paintOrbFrame(p *ui.Painter, r ui.Rect, style qaOrbStyle, tSec float64, opacity float64, dark bool, canonicalSize int) {
	if opacity < 0.01 {
		return
	}
	preset := resolveOrbPreset(style, canonicalSize)
	// Inline 26px tuning: keep Working's orbital rings rich and Connecting's
	// constellation edges visible at compact size, while canonical presets stay
	// verbatim for upstream golden-vector parity.
	opts := preset.opts
	if canonicalSize <= 20 {
		switch style {
		case orbWorking:
			opts.orbitN = 5
			opts.ghostN = 16
			opts.ghostR = 1.45
			opts.partR = 2.2
			opts.partRDepth = 2.8
		case orbConnecting:
			opts.nodeN = 16
			opts.thr = 0.95
			opts.signals = 2
			opts.nodeR = 1.65
			opts.nodeRDepth = 2.1
			opts.lineW = 0.75
		}
	}
	frame := orbComputeFrame(style, float64(canonicalSize), tSec*preset.speed, opts)
	sx := float64(r.W) / float64(canonicalSize)
	sy := float64(r.H) / float64(canonicalSize)
	scale := min(sx, sy)
	ox := float64(r.X) + (float64(r.W)-float64(canonicalSize)*scale)/2
	oy := float64(r.Y) + (float64(r.H)-float64(canonicalSize)*scale)/2

	for _, l := range frame.lines {
		col := qaOrbColor(l.white, l.a*opacity, dark)
		if col.A == 0 {
			continue
		}
		p.Line(
			float32(ox+l.x1*scale), float32(oy+l.y1*scale),
			float32(ox+l.x2*scale), float32(oy+l.y2*scale),
			float32(l.w*scale), col,
		)
	}
	for _, d := range frame.dots {
		col := qaOrbColor(d.white, d.a*opacity, dark)
		if col.A == 0 {
			continue
		}
		rad := float32(d.r * scale)
		cx := float32(ox + d.x*scale)
		cy := float32(oy + d.y*scale)
		p.Fill(ui.Rect{X: cx - rad, Y: cy - rad, W: rad * 2, H: rad * 2}, col, rad)
	}
}

// qaThinkingOrb renders Thinking Orbs natively via MyGo vector primitives.
// Styles adapt to OneBot's live phase and smoothly crossfade; clicking cycles
// through Working, Solving, Thinking, Agent thinking, and Connecting.
func qaThinkingOrb(c *ui.Context, s *qaOrbState, desired qaOrbStyle) ui.Element {
	now := c.Now()
	if !s.initialized {
		s.initialized = true
		s.origin = now
		s.style = desired
	}
	reduced := c.Preferences().ReduceMotion
	box := ui.Box(c.Key("qa-thinking-orb")).Size(qaOrbBoxSize, qaOrbBoxSize).Shrink(0).Radius(6).
		Focusable().Role(ui.RoleButton).Label(i18n.T("切换思考动效")).Tooltip(i18n.T("切换思考动效")).Cursor(ui.CursorPointer)
	if box.Focused() {
		box.Border(1, c.Theme().Accent)
	}
	if box.Clicked() {
		s.manualStep++
		next := s.style.next()
		if !reduced {
			s.prevStyle = s.style
			s.morphAt = now
			s.morphing = true
		}
		s.style = next
	} else {
		target := desired
		if s.manualStep > 0 {
			target = qaOrbStyle((int(desired) + s.manualStep) % int(orbStyleCount))
		}
		if target != s.style {
			if !reduced {
				s.prevStyle = s.style
				s.morphAt = now
				s.morphing = true
			}
			s.style = target
		}
	}

	box.Draw(func(p *ui.Painter, r ui.Rect) {
		dark := p.Theme().Dark
		if reduced {
			paintOrbFrame(p, r, s.style, qaOrbReducedTime, 1, dark, qaOrbCanonicalSize)
			return
		}
		pNow := p.Now()
		tSec := max(0.0, pNow.Sub(s.origin).Seconds())
		// Subtle 0.6s phase offset matches upstream's representative start frame.
		tSec += qaOrbReducedTime

		if s.morphing {
			f := pNow.Sub(s.morphAt).Seconds() / qaOrbMorphDuration.Seconds()
			if f >= 1 {
				s.morphing = false
			} else {
				f = max(0, f)
				ease := f * f * (3 - 2*f)
				paintOrbFrame(p, r, s.prevStyle, tSec, 1-ease, dark, qaOrbCanonicalSize)
				paintOrbFrame(p, r, s.style, tSec, ease, dark, qaOrbCanonicalSize)
				p.AnimationFrame()
				return
			}
		}
		paintOrbFrame(p, r, s.style, tSec, 1, dark, qaOrbCanonicalSize)
		p.AnimationFrame()
	})
	return box
}
