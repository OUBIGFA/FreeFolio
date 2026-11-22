package app

import (
	"bytes"
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/agent"
	"github.com/egoist/mygo/ui"
)

func TestThinkingOrbMatchesUpstreamGoldenVectors(t *testing.T) {
	// Reference vectors extracted verbatim from upstream thinking-orbs v0.3.1
	// spec/orbs-golden.json (commit de85557ca220332586d070d8788c0e1d6e877a0d).
	type vec struct {
		name      string
		style     qaOrbStyle
		size      int
		t         float64
		dotCount  int
		lineCount int
		firstDot  [6]float64
		lastDot   [6]float64
		firstLine [7]float64
	}
	cases := []vec{
		{
			name: "working-64-0.6", style: orbWorking, size: 64, t: 0.6,
			dotCount: 516, lineCount: 0,
			firstDot: [6]float64{32.34438, 30.683937, -24.13443, 0.356187, 0.72, 0.200238},
			lastDot:  [6]float64{31.65562, 33.316063, 24.13443, 0.356187, 0.72, 0.499762},
		},
		{
			name: "working-20-3.3", style: orbWorking, size: 20, t: 3.3,
			dotCount: 39, lineCount: 0,
			firstDot: [6]float64{9.799297, 10.124795, -7.550283, 0.567387, 0.299946, 1},
			lastDot:  [6]float64{11.100429, 10.065749, 7.473109, 0.425401, 0.72, 0.498394},
		},
		{
			name: "solving-64-0.6", style: orbSolving, size: 64, t: 0.6,
			dotCount: 138, lineCount: 0,
			firstDot: [6]float64{32.999536, 35.206761, -0.991773, 0.3, 0.617779, 1},
			lastDot:  [6]float64{34.395262, 28.752372, 0.988104, 0.951566, 0.083212, 1},
		},
		{
			name: "solving-20-3.3", style: orbSolving, size: 20, t: 3.3,
			dotCount: 30, lineCount: 0,
			firstDot: [6]float64{8.017374, 7.144441, -0.905688, 0.366774, 0.454536, 1},
			lastDot:  [6]float64{11.717005, 8.646124, 0.963792, 0.849134, 0.089776, 1},
		},
		{
			name: "thinking-ribbon-64-3.3", style: orbThinking, size: 64, t: 3.3,
			dotCount: 566, lineCount: 0,
			firstDot: [6]float64{37.322389, 30.655967, -24.348868, 0.31661, 0.78, 0.102693},
			lastDot:  [6]float64{32, 25.854149, 24.19153, 0.699834, 0.266773, 0.990764},
		},
		{
			name: "thinking-ribbon-20-0.6", style: orbThinking, size: 20, t: 0.6,
			dotCount: 208, lineCount: 0,
			firstDot: [6]float64{7.795005, 12.112063, -7.177547, 0.3, 0.682444, 0.42394},
			lastDot:  [6]float64{10, 6.759833, 7.095162, 0.431603, 0.27988, 0.972891},
		},
		{
			name: "agent-thinking-ring-64-0.6", style: orbAgentThinking, size: 64, t: 0.6,
			dotCount: 484, lineCount: 0,
			firstDot: [6]float64{41.791591, 53.440594, -8.838984, 0.467922, 0.557908, 0.593762},
			lastDot:  [6]float64{55.395251, 28.636271, 8.863436, 0.638987, 0.401877, 0.806532},
		},
		{
			name: "agent-thinking-ring-20-3.3", style: orbAgentThinking, size: 20, t: 3.3,
			dotCount: 120, lineCount: 0,
			firstDot: [6]float64{9.201663, 17.595667, -2.004845, 0.414845, 0.536547, 0.622891},
			lastDot:  [6]float64{16.910423, 13.076719, 1.985655, 0.519031, 0.423994, 0.776371},
		},
		{
			name: "connecting-web-64-0.6", style: orbConnecting, size: 64, t: 0.6,
			dotCount: 48, lineCount: 81,
			firstDot:  [6]float64{23.99695, 34.67833, -0.944099, 0.662966, 0.537422, 1},
			lastDot:   [6]float64{31.723599, 32.180593, 0.999917, 1.490098, 0.100019, 1},
			firstLine: [7]float64{40.743047, 12.360858, 25.929776, 13.006921, 0.42, 0.137696, 0.6},
		},
	}

	const tol = 1e-4
	checkDot := func(t *testing.T, label string, got orbDot, want [6]float64) {
		t.Helper()
		vals := [6]float64{got.x, got.y, got.z, got.r, got.white, got.a}
		for i := range 6 {
			if math.Abs(vals[i]-want[i]) > tol {
				t.Fatalf("%s[%d] = %.6f, want %.6f", label, i, vals[i], want[i])
			}
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preset := resolveOrbPreset(tc.style, tc.size)
			frame := orbComputeFrame(tc.style, float64(tc.size), tc.t, preset.opts)
			if len(frame.dots) != tc.dotCount || len(frame.lines) != tc.lineCount {
				t.Fatalf("counts = (%d dots, %d lines), want (%d, %d)", len(frame.dots), len(frame.lines), tc.dotCount, tc.lineCount)
			}
			checkDot(t, "firstDot", frame.dots[0], tc.firstDot)
			checkDot(t, "lastDot", frame.dots[len(frame.dots)-1], tc.lastDot)
			if tc.lineCount > 0 {
				l := frame.lines[0]
				vals := [7]float64{l.x1, l.y1, l.x2, l.y2, l.white, l.a, l.w}
				for i := range 7 {
					if math.Abs(vals[i]-tc.firstLine[i]) > tol {
						t.Fatalf("firstLine[%d] = %.6f, want %.6f", i, vals[i], tc.firstLine[i])
					}
				}
			}
		})
	}
}

func TestThinkingOrbClickCycleCrossfadeAndReducedMotion(t *testing.T) {
	var s qaOrbState
	hidden, clipped := false, false
	builds := 0
	desired := orbWorking
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		c.Root().Background(white)
		ui.Column(c).Fill().Clip().Padding(40).Children(func() {
			if clipped {
				ui.Box(c).Height(400).Shrink(0)
			}
			qaThinkingOrb(c, &s, desired).Hidden(hidden)
		})
	}, 200, 200)

	label := "切换思考动效"
	r, ok := tt.Find(label)
	if !ok || r.W != qaOrbBoxSize || r.H != qaOrbBoxSize {
		t.Fatalf("orb box must be %d×%d: %+v", qaOrbBoxSize, qaOrbBoxSize, r)
	}
	if s.style != orbWorking {
		t.Fatalf("initial style = %v, want orbWorking", s.style)
	}

	// Clicking cycles to Solving and starts a smooth crossfade.
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if s.style != orbSolving || !s.morphing || s.prevStyle != orbWorking {
		t.Fatalf("click did not crossfade to Solving: %+v", s)
	}
	tt.Advance(qaOrbMorphDuration + 20*time.Millisecond)
	if s.morphing {
		t.Fatal("crossfade did not finish")
	}

	// Keyboard activation cycles to Thinking.
	tt.Key(0, ui.KeyEnter)
	if s.style != orbThinking {
		t.Fatalf("Enter did not advance style to Thinking: %v", s.style)
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
		t.Fatal("reduced-motion orb did not rest")
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if s.style != orbAgentThinking || tt.FramePending() {
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
			t.Fatalf("%s orb kept requesting frames", kind)
		}
	}
}

func TestThinkingOrbFollowsWaitingTurnAndVariesByPhase(t *testing.T) {
	a, tt := qaApp(t, &scripted{})
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Advance(slide)
	s := a.qaSession(a.state.ActiveSpace().ID)
	turn := &qaTurn{question: "查找空间里的任务", running: true}
	s.turns = append(s.turns, turn)
	stopped := false
	s.cancel = func() { stopped = true }
	tt.Frame()
	if !turn.orb.initialized || !tt.HasText("正在查找…") || !tt.HasText("停止") {
		t.Fatal("waiting orb missing")
	}
	if turn.orb.style != orbWorking {
		t.Fatalf("initial waiting style = %v, want orbWorking", turn.orb.style)
	}

	// Auto-cycles across the 4 primary styles during a long wait.
	tt.Advance(qaOrbCyclePeriod + 50*time.Millisecond)
	tt.Frame()
	if turn.orb.style != orbSolving {
		t.Fatalf("auto-cycle after period = %v, want orbSolving", turn.orb.style)
	}

	// Thinking phase shifts to Thinking (ribbon) or Agent thinking (ring).
	turn.apply(agent.Event{Kind: agent.ReasoningDelta, Text: "reasoning"})
	tt.Frame()
	if !tt.HasText("思考中…") || (turn.orb.style != orbThinking && turn.orb.style != orbAgentThinking) {
		t.Fatalf("reasoning status/orb unexpected: style=%v", turn.orb.style)
	}

	// Streaming answer replaces waiting status card.
	turn.apply(agent.Event{Kind: agent.TextDelta, Text: "正在整理结果"})
	tt.Frame()
	qaAdvance(tt, 700*time.Millisecond)
	if tt.HasText("思考中…") || !tt.HasText("正在整理结果") {
		t.Fatal("streaming did not replace status")
	}

	// Tool call round restores waiting status with phase-matched orb.
	turn.apply(agent.Event{Kind: agent.StepDone, Message: agent.Message{Calls: []agent.ToolCall{{Name: agent.ToolRead}}}})
	turn.apply(agent.Event{Kind: agent.ToolStart, Call: agent.ToolCall{ID: "r1", Name: agent.ToolRead, Args: `{"path":"Welcome.md"}`}})
	tt.Frame()
	if !tt.HasText("正在阅读 Welcome") || (turn.orb.style != orbAgentThinking && turn.orb.style != orbThinking) {
		t.Fatalf("tool round orb unexpected: style=%v texts=%q", turn.orb.style, tt.Texts())
	}

	if err := tt.Click("停止"); err != nil || !stopped {
		t.Fatal("stop did not cancel")
	}
	a.finishQA(s, turn, nil, context.Canceled)
	tt.Frame()
	if tt.HasText("正在查找…") || !tt.HasText("已停止") {
		t.Fatal("stopped turn retained its orb")
	}
}

func TestThinkingOrbThemeColorsAndCrossfadeOpacity(t *testing.T) {
	lightNear := qaOrbColor(0, 1, 1, false)
	lightFar := qaOrbColor(1, 0.2, 1, false)
	if lightNear.R < 20 || lightNear.R >= lightFar.R {
		t.Fatalf("light theme matte graphite range invalid: near=%+v far=%+v", lightNear, lightFar)
	}
	darkNear := qaOrbColor(0, 1, 1, true)
	darkFar := qaOrbColor(1, 0.2, 1, true)
	if darkNear.R <= darkFar.R || darkNear.R < 220 {
		t.Fatalf("dark theme matte silver inversion invalid: near=%+v far=%+v", darkNear, darkFar)
	}
	if faded := qaOrbColor(0.2, 0.8, 0, false); faded.A != 0 {
		t.Fatalf("zero crossfade opacity should produce transparent color: %+v", faded)
	}
}

func TestQAOrbStylesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEMD_SNAPSHOT to inspect Thinking Orbs styles")
	styles := []struct {
		name  string
		style qaOrbStyle
	}{
		{"Working", orbWorking},
		{"Solving", orbSolving},
		{"Thinking", orbThinking},
		{"Agent thinking", orbAgentThinking},
		{"Connecting", orbConnecting},
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
							var st qaOrbState
							qaThinkingOrb(c, &st, item.style)
							ui.Text(c, item.name+" · 正在查找…").FontSize(12.5).FontWeight(500).TextColor(th.Text.Alpha(0.85))
						})
					}
				})
			}, 340, 300)
			tt.SetDark(dark)
			tt.SetScale(2)
			tt.Frame()
			savePNG(t, filepath.Join(dir, "qa-orbs-"+mode+".png"), tt)
		})
	}
}
