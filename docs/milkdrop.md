# MilkDrop visualizer

The **MilkDrop** visualizer, the last one on the Visualizer screen, plays MilkDrop 1 presets (`.milk` files). It is a reimplementation in pure Go plus Ebitengine: the equations are compiled to Go closures, and drawing uses `DrawTriangles` and two Kage shaders. It needs no cgo and no native libraries, so it builds and runs the same on the handhelds, the desktop and Windows. The code is in `internal/milkdrop`, and the UI glue is in `internal/ui/milkdrop.go`.

## Opening it

MilkDrop is in audiofilus 0.12.0 and newer.

1. Open the **Visualizer** screen from the screens dock (SELECT → Visualizer).
2. Press ← or → until the name at the top says **MilkDrop**. It is the last visualizer, so from the first one a single ← gets there. You can also press START and pick it from the list of visualizers.

## Controls

| Button | What it does |
|---|---|
| ← / → | Switch visualizer, as on the other visualizers |
| L1 / R1 | Previous / next preset, with a 2-second blend |
| A | Lock the current preset, so it stops changing on its own; press again to unlock |
| START | Menu: **Colors** (one button: A switches between *Preset* and *Album*), previous, next, random, lock, rescan presets, and the list of visualizers. Picking another visualizer there rebuilds the menu, so the MilkDrop options go away |

Unless it is locked, the visualizer moves on to the next preset after 20 seconds, on the first beat after that (or 6 seconds later if no beat comes). The preset's name shows for a moment whenever it changes.

On this screen, L1/R1 change the preset instead of the song. The song can still be changed from the dock, from Now Playing or with the system shortcuts.

## Background in Now Playing

With START → *Visualizer background* in Now Playing, MilkDrop (or whichever visualizer was last chosen) is drawn dimmed behind the player. There it always uses the album's colors, whatever the Colors button says, so it matches the cover and the rest of the screen.

## Colors: preset or album

START → **Colors** is one button that switches between two modes, shown under its name. **Preset** (the default) shows each preset as its author made it. **Album** recolors the final image with the theme of the album that is playing, the same colors the rest of the interface takes from the cover. The preset's brightness and shapes stay, and only the color changes:

- Brightness is mapped onto a gradient from the theme background, to its accent, to its second accent, to its text color.
- 15% of the preset's own color is kept, so the image doesn't look flat.

Switching modes fades over about half a second. When the album changes, the gradient follows the new theme over about a second. The choice is saved in `audiofilus.json` as `ui.milkdrop_colors` (`""` for the preset's colors, `"album"` for the album's).

## Presets

- **Built-in:** eight original presets written for audiofilus, under the same MIT license as the rest of the project (`internal/milkdrop/presets/*.milk`): Aurora Curtain, Ember Tunnel, Kaleido Bloom, Liquid Chrome, Neon Grid, Ripple Pond, Spectrum Fountain and Starfield Pulse.
- **Yours:** put `.milk` files in a `milkdrop` folder next to the binary, or in `$AUDIOFILUS_HOME/milkdrop` if that variable is set. Subfolders are fine, so a whole pack can be unzipped there as it is. Another folder can be picked in Settings → **MilkDrop presets** (the same folder browser as the music folder; saved as `ui.milkdrop_dir` in `audiofilus.json`, empty for the default; not on iPhone). Picking it while MilkDrop is open rescans at once.

The folder is scanned the first time the visualizer opens, not at startup. After adding files, use START → *Rescan presets*.

audiofilus doesn't bundle third-party presets, because their licenses vary. The classic packs are easy to find:

- The presets that came with Winamp's MilkDrop 1.04.
- The *projectM* preset collections (`projectM-presets-cream-of-the-crop` and the classic packs on GitHub).
- The MilkDrop community archives.

**MilkDrop 1 presets** (those without `warp_`/`comp_` shader lines) work best. MilkDrop 2 presets load too, but their HLSL shaders are ignored (see below), so many look different from the original.

## What is supported

**The file**
- `[preset00]` with `key=value` lines. Only the first `[presetNN]` section counts.
- `per_frame_init_N`, `per_frame_N` and `per_pixel_N` code (also spelled `per_vertex_N`).
- `wavecode_N_*` and `shapecode_N_*` values, and their code: `wave_N_init/per_frame/per_point M` and `shape_N_init/per_frame M`.
- Lines are joined in numeric order. A line that is missing its trailing `;` still works.

**The equations (an EEL subset)**
- Variables are case-insensitive and start at 0.
- Operators: `+ - * / % ^ | &`, comparisons, `&& || !`, `?:`, and `= += -= *= /= %=`. Statements are separated by `;`, and `( a; b )` is a sequence whose value is `b`.
- Division by zero gives 0, `%` is an integer remainder (also 0 when dividing by zero), and `==` compares within 0.00001.
- Functions: `sin cos tan asin acos atan atan2 sqrt pow exp log log10 abs sign min max sigmoid rand int floor ceil sqr invsqrt above below equal bnot band bor if exec2 exec3 loop while megabuf gmegabuf`, plus `$pi`, `$e` and `$phi`.
  - `if`, `band`, `bor`, `&&` and `||` only evaluate the branch they need.
  - `sqrt` and `log` take the absolute value, as in MilkDrop.
  - `rand(n)` returns an integer from 0 to n-1.
  - `megabuf` and `gmegabuf` hold one million cells each, allocated in blocks the first time a cell is written.
  - `loop` and `while` stop after 65,536 iterations.
- How variables pass between contexts:
  - `q1`–`q32` go from the per-frame code to the per-vertex code, the waves and the shapes, and reset every frame to the value they had after `per_frame_init`.
  - `t1`–`t8` go from a wave's or shape's per-frame code to its per-point code, and reset to the value they had after that wave's or shape's init.
  - `reg00`–`reg99` are global, shared by all contexts and kept across presets.
  - Other variables you create keep their values between frames.
- Code that fails to compile is skipped (and logged), and the rest of the preset still runs, as in MilkDrop.
- Running the code allocates no memory: only compiling does.

**Variables**
- Inputs:
  - `time fps frame progress`;
  - `bass mid treb` and `bass_att mid_att treb_att`;
  - `meshx meshy aspectx aspecty pixelsx pixelsy`;
  - per vertex: `x y rad ang`.
- Outputs:
  - motion: `zoom zoomexp rot warp cx cy dx dy sx sy decay gamma`;
  - echo: `echo_zoom echo_alpha echo_orient`;
  - the main wave: `wave_mode wave_r/g/b/a wave_x wave_y wave_mystery wave_thick wave_usedots additivewave wave_brighten`;
  - borders: `ob_size ob_r/g/b/a ib_size ib_r/g/b/a`;
  - motion vectors: `mv_x mv_y mv_dx mv_dy mv_l mv_r/g/b/a`;
  - effects: `darken_center invert brighten darken solarize`;
  - warp animation: `warpanimspeed warpscale`.
- The header values are read too: `fDecay`, `fGammaAdj`, `fVideoEcho*`, `nWaveMode`, `b*` flags, `fWaveScale`, `fModWaveAlpha*`, `fWarp*`, `fZoomExponent`, `fShader`, `nMotionVectors*`, and the others.
- Output variables start each frame at the preset's value, or MilkDrop's default when the preset doesn't set one.
- `bass`, `mid` and `treb` come from audiofilus's 16-band spectrum:
  - bass is 40–180 Hz, mid is 180 Hz–1.7 kHz and treble is above that;
  - each is divided by its own average over the last few seconds, so it hovers around 1 with steady music and is 0 in silence;
  - the `_att` versions are smoothed over about 0.35 s.

**Rendering**
- Warp:
  - the previous frame goes through a 32×24 mesh (MilkDrop's default), with the MilkDrop formula per vertex: zoom (with `zoomexp`), stretch, the animated warp, rotation about `cx`/`cy`, and translation;
  - the per-vertex code can change any of these per vertex;
  - the Kage warp shader samples bilinearly, wraps the texture or clamps it to the edge (`bTexWrap`), applies `decay`, and darkens the center if the preset asks for it.
- Drawn on top, in MilkDrop's order:
  1. motion vectors;
  2. up to 4 shapes, each with up to 32 instances (`num_inst`). Shapes have a center color and an edge color, an outline, additive blending, and texture: a textured shape samples the previous frame;
  3. up to 4 custom waves (up to 512 points, as a spectrum or a waveform, lines or dots, additive);
  4. the main wave, in modes 0–7;
  5. the outer and inner borders.
- Composite (a Kage shader): video echo (zoom, alpha, the four orientations), gamma, the `fShader` color tint, brighten, darken, solarize and invert, then the album colors when enabled.
- Blending: for 2 seconds after a change, both presets run. The meshes, the composite values and the alpha of everything each one draws are blended.
- Internal resolution:
  - it is the visualizer's area on screen, about 320×212 on a 4:3 handheld;
  - `AUDIOFILUS_MILKDROP_SCALE=2` renders at half that and scales it up, for very weak GPUs;
  - drawing runs at 30 fps, like the other visualizers.

## Fidelity limits

- **No real waveform.** The audiofilus service sends the UI a 16-band spectrum, not audio samples. The waves (main and custom, `value1`/`value2`) are therefore a sum of sines, one per band, each with its band's strength. They react to the music like an oscilloscope, but they are not the actual waveform. Spectrum waves (`bSpectrum=1`) use the real bands.
- **Main wave modes 0–7 are approximations** of MilkDrop 1's shapes (circle, x-y spirals, the horizontal scope, the rotating "explosive" pattern, the angled line, two lines). Wave smoothing is not applied.
- **No MilkDrop 2 shaders.** `warp_`/`comp_` HLSL is skipped, and so is the blur variables' effect (`b1n`, `blur1` and so on): MilkDrop 2's own shaders aren't run.
- **No sprites, no custom textures, no `.jpg`/`.png` from preset folders, and no text or titles.**
- **Other limits:**
  - `bRedBlueStereo` and mesh sizes other than 32×24 are not supported.
  - The `meshx`/`meshy` variables report the fixed mesh.
  - Custom waves ignore `smoothing`, and `sep` only offsets the second channel.
  - Shapes beyond 32 instances are dropped.
  - Signs that MilkDrop documents ambiguously were chosen to match the docs: positive `dy` moves the image up, positive `rot` turns it counter-clockwise, and per-pixel `y` and `cy` count from the top.
- **8-bit feedback.** The feedback runs in 8-bit textures. A small bias in the warp shader makes faint trails fade fully to black (in MilkDrop on some cards, trails "stick" at low levels).

## Performance

CPU time per frame, measured on the PC (Ryzen desktop), at 320×240 with the 32×24 mesh:

| | Time per frame |
|---|---|
| `go test -bench Update ./internal/milkdrop` (CPU side only) | 0.07–0.22 ms, 0 allocations |
| `audiofilus -bench milk -music <dir>` (the full UI frame, GPU included) | mean 0.58 ms, p95 0.97 ms |

The UI frame is 1.07 ms for Lake and 0.21 ms for Tunnel. The bench's KB/frame figure is mostly font loading in the background.

The costly part on the CPU is the per-vertex code. On the GPU, it is the warp pass: four texture reads per pixel at the internal resolution.

## Tests

```sh
go test ./internal/milkdrop                                # parser, evaluator, mesh math, q/t passing, frames without GPU, no allocations
go test -tags milkdropgpu -run GPU ./internal/milkdrop     # opens a window: colors survive, trails, fading to black
go test -run xxx -bench . ./internal/milkdrop              # CPU cost per preset
audiofilus -bench milk -music <dir>                        # the whole UI frame
audiofilus -shots <s> -frames <f> -music <dir>             # <f>/visual/milk/<preset>[-album]/: three presets in both color modes
```
