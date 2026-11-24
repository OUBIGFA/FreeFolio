# LDRS: native Go + MyGo loaders

Source: [GriffinJohnston/ldrs](https://github.com/GriffinJohnston/ldrs/tree/f759f65beb8ab4f4a1f010438072180693ca01ec),
commit `f759f65beb8ab4f4a1f010438072180693ca01ec`, MIT © 2022 Griffin Johnston.

`internal/app/qaloader.go` and `internal/app/qabot.go` port three LDRS loaders
into native Go + MyGo for OneBot's live status indicator:

- **Tail Chase** (`src/elements/tailChase.ts`, `tailChase.scss`): 6 staggered dots
  orbiting inside a continuously rotating frame (`speed = 1.5s`, container `1.8×`,
  dot diameter `0.17 × size`, CSS `ease-in-out` burst over `0%–65%`).
- **Dot Stream** (`src/elements/dotStream.ts`, `dotStream.scss`): 5 dots streaming
  linearly left-to-right while scaling `0 → 1 → 0` (`speed = 2.5s`, dot diameter
  `0.18 × size`, evenly staggered by `0.2 × speed`).
- **Mirage** (`src/elements/mirage.ts`, `mirage.scss`): 5 streaming dots (`0.23 × size`)
  with the signature `#uib-jelly-ooze` SVG filter (`feGaussianBlur` at `size / 20`
  plus `feColorMatrix` alpha transfer `18a - 7`) evaluated natively in Go into a
  reusable 2× supersampled RGBA bitmap.

All three loaders adapt automatically to FreeFolio's active light/dark theme accent
color, support semantic phase selection, periodic variety rotation, interactive
click/keyboard cycling with 220 ms crossfades, reduced-motion static frames, and
automatic frame-pause when hidden or clipped. No JavaScript or webview is used.
