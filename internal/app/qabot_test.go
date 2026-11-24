package app

import (
	"bytes"
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/egoist/mygo/ui"
)

func TestLDRSGeometryAndMirageOoze(t *testing.T) {
	t.Run("TailChase", func(t *testing.T) {
		const size = 20.0
		dots := ldrsTailChaseDots(size, 0.3)
		wantR := (size * 0.17) / 2
		orbitR := (size - size*0.17) / 2
		cx, cy := size/2, size/2
		for i, d := range dots {
			if math.Abs(d.r-wantR) > 1e-6 {
				t.Fatalf("dot[%d].r = %v, want %v", i, d.r, wantR)
			}
			dist := math.Hypot(d.x-cx, d.y-cy)
			if math.Abs(dist-orbitR) > 1e-6 {
				t.Fatalf("dot[%d] orbit radius = %v, want %v", i, dist, orbitR)
			}
		}
		// Staggered burst means leading and trailing dots separate on the circle.
		if math.Hypot(dots[0].x-dots[1].x, dots[0].y-dots[1].y) < 0.5 {
			t.Fatalf("tail chase dots collapsed together: %+v", dots)
		}
	})

	t.Run("DotStream", func(t *testing.T) {
		const size = 30.0
		dots := ldrsDotStreamDots(size, 0.625)
		wantCy := (size * 0.18) / 2
		maxR := (size * 0.18) / 2
		for i, d := range dots {
			if d.x < 0 || d.x > size {
				t.Fatalf("dot[%d].x out of bounds: %v", i, d.x)
			}
			if math.Abs(d.y-wantCy) > 1e-6 {
				t.Fatalf("dot[%d].y = %v, want %v", i, d.y, wantCy)
			}
			// Symmetric 0 -> 1 -> 0 scale profile peaking at x = size/2.
			p := d.x / size
			wantScale := 2 * p
			if p > 0.5 {
				wantScale = 2 * (1 - p)
			}
			if math.Abs(d.r-maxR*wantScale) > 1e-6 {
				t.Fatalf("dot[%d].r = %v, want %v", i, d.r, maxR*wantScale)
			}
		}
	})

	t.Run("MirageOoze", func(t *testing.T) {
		var painter ldrsMiragePainter
		col := ui.RGB(20, 110, 220)
		bmp, pad, logicalW, logicalH := painter.render(32.0, 0.75, col, 1.0)
		if bmp == nil || logicalW <= 32.0 || logicalH <= 32.0*0.23 || pad <= 0 {
			t.Fatalf("unexpected mirage bitmap metadata: bmp=%v pad=%v w=%v h=%v", bmp, pad, logicalW, logicalH)
		}
		pix := painter.pixels.Pix
		nonZero := 0
		for i := 0; i < len(pix); i += 4 {
			r, g, b, a := pix[i], pix[i+1], pix[i+2], pix[i+3]
			if r > a || g > a || b > a {
				t.Fatalf("pixel %d is not premultiplied RGBA: (%d,%d,%d,%d)", i/4, r, g, b, a)
			}
			if a > 0 {
				nonZero++
			}
		}
		if nonZero < 40 {
			t.Fatalf("mirage rendered too few opaque pixels: %d", nonZero)
		}

		// When two adjacent dots sit close together, the jelly-ooze filter forms a
		// visible liquid bridge at the midpoint even outside both crisp circles.
		dots := ldrsMirageDots(32.0, 1.0)
		midX := (dots[0].x + dots[1].x) / 2
		midY := (dots[0].y + dots[1].y) / 2
		px := int(math.Round((midX+pad)*2 - 0.5))
		py := int(math.Round((midY+pad)*2 - 0.5))
		idx := py*painter.pixels.Stride + px*4
		if painter.pixels.Pix[idx+3] == 0 {
			t.Fatalf("mirage midpoint (%d,%d) lacks liquid ooze bridge between dots %+v and %+v", px, py, dots[0], dots[1])
		}
	})
}

func TestQALoaderClickCycleCrossfadeAndReducedMotion(t *testing.T) {
	var s qaLoaderState
	hidden, clipped := false, false
	builds := 0
	desired := loaderTailChase
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		c.Root().Background(white)
		ui.Column(c).Fill().Clip().Padding(40).Children(func() {
			if clipped {
				ui.Box(c).Height(400).Shrink(0)
			}
			qaLoader(c, &s, desired).Hidden(hidden)
		})
	}, 200, 200)

	label := "切换思考动效"
	r, ok := tt.Find(label)
	if !ok || r.W != qaLoaderBoxW || r.H != qaLoaderBoxH {
		t.Fatalf("loader box must be %d×%d: %+v", qaLoaderBoxW, qaLoaderBoxH, r)
	}
	if s.style != loaderTailChase {
		t.Fatalf("initial style = %v, want loaderTailChase", s.style)
	}

	// Clicking cycles to Dot Stream and starts a smooth crossfade.
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if s.style != loaderDotStream || !s.morphing || s.prevStyle != loaderTailChase {
		t.Fatalf("click did not crossfade to Dot Stream: %+v", s)
	}
	tt.Advance(qaLoaderMorphDur + 20*time.Millisecond)
	if s.morphing {
		t.Fatal("crossfade did not finish")
	}

	// Keyboard activation cycles to Mirage.
	tt.Key(0, ui.KeyEnter)
	if s.style != loaderMirage {
		t.Fatalf("Enter did not advance style to Mirage: %v", s.style)
	}

	// Reduced motion rests on a deterministic frame without scheduling animation.
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	before := bytes.Clone(tt.Image().Pix)
	built := builds
	for range 10 {
		tt.Advance(time.Second / 60)
	}
	if !bytes.Equal(before, tt.Image().Pix) || tt.FramePending() || builds != built {
		t.Fatal("reduced-motion loader did not rest")
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if s.style != loaderTailChase || tt.FramePending() {
		t.Fatalf("reduced-motion click failed: style=%v pending=%v", s.style, tt.FramePending())
	}

	// Live animation repaints without rebuilding the UI tree.
	tt.SetPreferences(ui.Preferences{})
	tt.Advance(time.Second / 60)
	built = builds
	for range 10 {
		tt.Advance(time.Second / 60)
	}
	if builds != built || !tt.FramePending() {
		t.Fatal("animation must repaint without rebuilding the view")
	}

	// Hidden or clipped offscreen stops requesting frames.
	for _, kind := range []string{"hidden", "clipped"} {
		hidden, clipped = kind == "hidden", kind == "clipped"
		tt.Frame()
		if tt.FramePending() {
			t.Fatalf("%s loader kept requesting frames", kind)
		}
	}
}

func TestQALoaderFollowsWaitingTurnAndVariesByPhase(t *testing.T) {
	a, tt := qaApp(t, &scripted{})
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Advance(slide)
	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{question: "查找空间里的任务", running: true}
	s.turns = append(s.turns, turn)
	stopped := false
	s.cancel = func() { stopped = true }
	tt.Frame()
	if !turn.loader.initialized || !tt.HasText("正在查找…") || !tt.HasText("停止") {
		t.Fatal("waiting loader missing")
	}
	if turn.loader.style != loaderTailChase {
		t.Fatalf("initial waiting style = %v, want loaderTailChase", turn.loader.style)
	}

	// Auto-cycles across the 3 LDRS styles during a long wait.
	tt.Advance(qaLoaderCyclePeriod + 50*time.Millisecond)
	tt.Frame()
	if turn.loader.style != loaderDotStream {
		t.Fatalf("auto-cycle after period = %v, want loaderDotStream", turn.loader.style)
	}

	// Thinking phase shifts to Mirage.
	turn.apply(agent.Event{Kind: agent.ReasoningDelta, Text: "reasoning"})
	tt.Frame()
	if !tt.HasText("思考中…") || turn.loader.style != loaderMirage {
		t.Fatalf("reasoning status/loader unexpected: style=%v", turn.loader.style)
	}

	// Streaming answer replaces waiting status card.
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "正在整理结果"})
	tt.Frame()
	qaAdvance(tt, 700*time.Millisecond)
	if tt.HasText("思考中…") || !tt.HasText("正在整理结果") {
		t.Fatal("streaming did not replace status")
	}

	// Tool call round restores waiting status with phase-matched loader.
	turn.apply(agent.Event{Kind: agent.StepDone, Message: agent.Message{Calls: []agent.ToolCall{{Name: agent.ToolRead}}}})
	turn.apply(agent.Event{Kind: agent.ToolStart, Call: agent.ToolCall{ID: "r1", Name: agent.ToolRead, Args: `{"path":"Welcome.md"}`}})
	tt.Frame()
	if !tt.HasText("正在阅读 Welcome") || turn.loader.style != loaderDotStream {
		t.Fatalf("tool round loader unexpected: style=%v texts=%q", turn.loader.style, tt.Texts())
	}

	if err := tt.Click("停止"); err != nil || !stopped {
		t.Fatal("stop did not cancel")
	}
	a.finishQA(s, turn, nil, context.Canceled)
	tt.Frame()
	if tt.HasText("正在查找…") || !tt.HasText("已停止") {
		t.Fatal("stopped turn retained its loader")
	}
}

func TestQALoaderStylesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect LDRS styles")
	styles := []struct {
		name  string
		style qaLoaderStyle
	}{
		{"Tail Chase", loaderTailChase},
		{"Dot Stream", loaderDotStream},
		{"Mirage", loaderMirage},
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		t.Run(mode, func(t *testing.T) {
			tt := ui.NewTester(func(c *ui.Context) {
				th := c.Theme()
				c.Root().Background(th.Background)
				ui.Column(c).Padding(16).Gap(10).Children(func() {
					for i, item := range styles {
						ui.Row(c.Key(i)).Gap(10).Padding(8, 12).Radius(11).AlignItems(ui.Center).
							Background(th.Text.Alpha(0.032)).Border(1, th.Border.Alpha(0.32)).Children(func() {
							var st qaLoaderState
							qaLoader(c, &st, item.style)
							ui.Text(c, item.name+" · 正在查找…").FontSize(12.5).FontWeight(500).TextColor(th.Text.Alpha(0.85))
						})
					}
				})
			}, 340, 210)
			tt.SetDark(dark)
			tt.SetScale(2)
			tt.Frame()
			savePNG(t, filepath.Join(dir, "qa-loaders-"+mode+".png"), tt)
		})
	}
}
