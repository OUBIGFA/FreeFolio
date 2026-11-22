# Thinking Orbs: native Go + MyGo port

Source: [Jakubantalik/thinking-orbs](https://github.com/Jakubantalik/thinking-orbs/tree/de85557ca220332586d070d8788c0e1d6e877a0d),
v0.3.1, commit `de85557ca220332586d070d8788c0e1d6e877a0d`, MIT © 2026 Jakub Antalik.

`internal/app/qaorb.go` ports the 3D dot-orb geometry engine (`src/engine/core.ts`,
`profiles.ts`, `orbits.ts`, `lattice.ts`, `ribbon.ts`, `web.ts`) and tuned presets
(`src/presets.ts`) for the five styles used by OneBot:

- `orbWorking` (`working` / `orbits`)
- `orbSolving` (`solving` / `rubik`)
- `orbThinking` (`composing` / `ribbon`)
- `orbAgentThinking` (`breathing` / `ring`)
- `orbConnecting` (`connecting` / `web`)

`internal/app/qabot.go` binds native MyGo vector drawing (`ui.Painter.Fill` and
`ui.Painter.Line`), automatic light/dark matte ink inversion, semantic phase
switching, periodic variety rotation, interactive click/keyboard cycling with
240 ms crossfades, reduced-motion static frames (`t = 0.6`), and automatic
frame-pause when hidden or clipped. No embedded bitmaps, JavaScript, or webview
are used at runtime.

Geometry parity is checked in `internal/app/qabot_test.go` against golden vectors
from upstream `spec/orbs-golden.json` (`1e-4` tolerance).
