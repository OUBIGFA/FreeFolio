package app

import (
	"time"

	"github.com/OUBIGFA/FreeMD/internal/agent"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

const (
	qaLoaderBoxW         = 32
	qaLoaderBoxH         = 22
	qaTailChaseSize      = 20.0
	qaDotStreamSize      = 30.0
	qaMirageSize         = 32.0
	qaLoaderMorphDur     = 220 * time.Millisecond
	qaLoaderCyclePeriod  = 4500 * time.Millisecond
	qaLoaderReducedPhase = 0.55
)

// qaLoaderState keeps per-turn LDRS loader playback, crossfade, and Mirage bitmap state.
type qaLoaderState struct {
	initialized bool
	origin      time.Time
	baseOffset  int
	manualStep  int
	style       qaLoaderStyle
	prevStyle   qaLoaderStyle
	morphAt     time.Time
	morphing    bool
	mirage      ldrsMiragePainter
}

// loaderStyle picks an LDRS animation for OneBot's live phase, combining
// Tail Chase, Dot Stream, and Mirage so long runs stay varied without shifting layout.
func (t *qaTurn) loaderStyle(now time.Time) qaLoaderStyle {
	if t.retrying {
		return loaderTailChase
	}
	if t.thinking {
		return loaderMirage
	}
	for i := len(t.steps) - 1; i >= 0; i-- {
		st := t.steps[i]
		if st.done {
			continue
		}
		switch st.call.Name {
		case agent.ToolRead, agent.ToolOutline, agent.ToolLinks:
			return loaderDotStream
		case agent.ToolSearch, agent.ToolGrep, agent.ToolGlob, "edit", "write", "patch":
			if (t.loader.baseOffset+i)%2 == 0 {
				return loaderTailChase
			}
			return loaderMirage
		}
	}
	cycle := [3]qaLoaderStyle{loaderTailChase, loaderDotStream, loaderMirage}
	slot := t.loader.baseOffset
	if !t.loader.origin.IsZero() && !now.Before(t.loader.origin) {
		slot += int(now.Sub(t.loader.origin) / qaLoaderCyclePeriod)
	}
	return cycle[slot%len(cycle)]
}

func qaLoaderColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Accent
	}
	return t.Accent.Alpha(0.92)
}

func paintLoaderStyle(p *ui.Painter, box ui.Rect, s *qaLoaderState, style qaLoaderStyle, tSec, opacity float64, col ui.Color) {
	if opacity <= 0.01 {
		return
	}
	drawCol := col.Alpha(float32(opacity))
	switch style {
	case loaderTailChase:
		dots := ldrsTailChaseDots(qaTailChaseSize, tSec)
		ox := float64(box.X) + (float64(box.W)-qaTailChaseSize)/2
		oy := float64(box.Y) + (float64(box.H)-qaTailChaseSize)/2
		for _, d := range dots {
			rad := float32(d.r)
			cx := float32(ox + d.x)
			cy := float32(oy + d.y)
			p.Fill(ui.Rect{X: cx - rad, Y: cy - rad, W: rad * 2, H: rad * 2}, drawCol, rad)
		}
	case loaderDotStream:
		dots := ldrsDotStreamDots(qaDotStreamSize, tSec)
		h := qaDotStreamSize * 0.18
		ox := float64(box.X) + (float64(box.W)-qaDotStreamSize)/2
		oy := float64(box.Y) + (float64(box.H)-h)/2
		for _, d := range dots {
			if d.r <= 0.05 {
				continue
			}
			rad := float32(d.r)
			cx := float32(ox + d.x)
			cy := float32(oy + d.y)
			p.Fill(ui.Rect{X: cx - rad, Y: cy - rad, W: rad * 2, H: rad * 2}, drawCol, rad)
		}
	case loaderMirage:
		s.mirage.draw(p, box, qaMirageSize, tSec, col, opacity)
	}
}

// qaLoader renders the LDRS status loader natively via MyGo primitives.
// Styles adapt to OneBot's live phase and smoothly crossfade; clicking cycles
// through Tail Chase, Dot Stream, and Mirage.
func qaLoader(c *ui.Context, s *qaLoaderState, desired qaLoaderStyle) ui.Element {
	now := c.Now()
	if !s.initialized {
		s.initialized = true
		s.origin = now
		s.style = desired
	}
	reduced := c.Preferences().ReduceMotion
	box := ui.Box(c.Key("qa-thinking-loader")).Size(qaLoaderBoxW, qaLoaderBoxH).Shrink(0).Radius(6).
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
			target = qaLoaderStyle((int(desired) + s.manualStep) % int(loaderStyleCount))
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
		col := qaLoaderColor(p.Theme())
		if reduced {
			paintLoaderStyle(p, r, s, s.style, qaLoaderReducedPhase, 1, col)
			return
		}
		pNow := p.Now()
		tSec := max(0.0, pNow.Sub(s.origin).Seconds()) + qaLoaderReducedPhase

		if s.morphing {
			f := pNow.Sub(s.morphAt).Seconds() / qaLoaderMorphDur.Seconds()
			if f >= 1 {
				s.morphing = false
			} else {
				f = max(0.0, f)
				ease := f * f * (3 - 2*f)
				paintLoaderStyle(p, r, s, s.prevStyle, tSec, 1-ease, col)
				paintLoaderStyle(p, r, s, s.style, tSec, ease, col)
				p.AnimationFrame()
				return
			}
		}
		paintLoaderStyle(p, r, s, s.style, tSec, 1, col)
		p.AnimationFrame()
	})
	return box
}
