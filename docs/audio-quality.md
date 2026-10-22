# Audio quality: resampler, parametric EQ, AutoEQ, crossfeed, speed, DSD

What the engine does to the sound between the decoder and the output, and the settings that change it. All of it lives in the service (`internal/engine`, persisted in `data/state.json` as part of `dsp`).

## Resampler

Every song is converted to the output rate (48 kHz by default, or the one chosen in Settings → Output rate) by `internal/resample`. There are two modes:

| Mode | How | Cost (PC, 1 s of 44.1 → 48 kHz) | Quality |
|---|---|---|---|
| `linear` | straight-line interpolation | 0.74 ms | rolls off the top octave a little and lets ultrasonic content alias back down (a 30 kHz tone going 96 → 48 kHz comes back at full level) |
| `quality` | Kaiser-windowed sinc, polyphase (`sinc.go`) | 3.9 ms (8.6 ms for 96 → 48 kHz) | flat to ±0.1 dB up to 45 % of the lower rate; 107–131 dB rejection from 55 % (measured in the tests); the 50–55 % transition only reaches above 20 kHz |

The sinc kernel has 32 taps per side at the lower rate (more when downsampling), β = 10, and every phase is normalised to unity DC gain. Rational ratios with up to 1024 phases (44.1 ↔ 48 kHz is 160/147, 44.1 → 96 kHz is 320/147) get one exact coefficient row per phase; any other ratio uses 512 rows and interpolates between the two nearest. Coefficients are computed once per rate pair and shared. `Reader.Pending()` stays exact in both modes (the engine subtracts it to report the position), and reading in chunks of any size gives bit-identical output.

Setting `resampler`:

- `auto` (default): `quality` on the desktop, `linear` on handhelds. The service decides at startup from the platform name (`platform.Detect`: `desktop` vs Knulli/muOS/ROCKNIX/ArkOS/EmuELEC).
- `linear` / `quality`: forced.

A change applies from the next song (the current one keeps its resampler, so nothing skips). `Status.resampler` says which one the playing song uses (empty when the file is already at the output rate).

On the handheld CPU (Cortex-A53) the quality mode is roughly 20–30× slower than on a desktop core, i.e. around 10 % of one core while playing, against ~2 % for linear; that is why `auto` keeps linear there. To measure it on the console:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o resample.test ./internal/resample
# copy to the console, then:
./resample.test -test.run x -test.bench . -test.benchtime 20x
```

(one benchmark op = one second of audio).

## Parametric EQ

Besides the 10-band graphic EQ (`eq`, presets), there is a parametric EQ with up to 10 bands (`engine.PEQMax`; AutoEq profiles use 10). `eq_mode` picks which one plays: `graphic` (default) or `parametric`. Both keep their settings, so switching back and forth loses nothing.

Each band (`engine.PEQBand`, JSON):

```json
{"type": "peak", "freq": 1000, "gain": 3, "q": 1}
```

| Field | Values |
|---|---|
| `type` | `peak`, `lowshelf`, `highshelf`, `lowpass`, `highpass` (`engine.PEQTypes`) |
| `freq` | 20–20000 Hz |
| `gain` | −15 to +15 dB (ignored by lowpass/highpass) |
| `q` | 0.3–10 (0 means 0.707) |

Filters are RBJ "Audio EQ Cookbook" biquads, the same as the graphic EQ. Bands above 45 % of the output rate are skipped, and so are peak/shelf bands at 0 dB (no cost). `peq_preamp_db` (−24 to +12) is applied before the bands, together with the same automatic headroom as the graphic EQ (80 % of the biggest boost); the limiter after the EQ still catches what goes over. Out-of-range values are clamped and unknown types dropped (`DSP.Normalize`).

## AutoEQ

`internal/autoeq` brings in headphone corrections from [AutoEq](https://github.com/jaakkopasanen/AutoEq) (free, thousands of models). The UI (`ui/autoequi.go`, from the EQ tile's menu → *AutoEQ*) downloads `results/INDEX.md` from the repository (about 850 KB; cached a week in `data/cache/autoeq-index.md`, and used even if stale when offline), searches it by words (names starting with the query first), and for the chosen entry fetches `<folder>/<model> ParametricEQ.txt`. That file is the Equalizer APO format:

```
Preamp: -6.1 dB
Filter 1: ON LSC Fc 105 Hz Gain 6.4 dB Q 0.70
Filter 2: ON PK Fc 8800 Hz Gain 5.1 dB Q 1.42
```

`autoeq.ParseParametric` maps `PK`/`PEQ` to peak, `LSC`/`LS` to lowshelf, `HSC`/`HS` to highshelf, `LP`/`HP` to the pass filters, skips `OFF` filters and keeps at most `PEQMax`. The result goes to the parametric EQ (`eqmode parametric`, `peq`, `peqpreamp`) and the editor opens. `.txt` files in an `eq/` folder next to the binary are offered in the same menu, for offline use.

## Crossfeed

`engine/crossfeed.go`, after the EQ: each channel gets a little of the other through a one-pole low-pass (which also delays it slightly), like hearing speakers, so hard-panned records tire less on headphones. Three bs2b-style levels in `DSP.Crossfeed`: `light` (700 Hz, −9.5 dB), `medium` (700 Hz, −6 dB), `strong` (650 Hz, −4.5 dB); the output is scaled by 1/(1+g) so the summed lows don't get louder. Off (`""`) costs nothing.

## Speed (audiobooks and podcasts)

`engine/stretch.go` changes the playback speed without changing the pitch, with WSOLA: 32 ms windows (1536 frames at 48 kHz) with half overlap and a Hann window; each window is taken `speed` times further along the input than along the output, shifted up to ±10 ms to where it best matches what was just played (normalized cross-correlation on a decimated mono mix). It runs after the resampler, at the output rate, only when the speed isn't 1, so music never pays for it. The engine resets it on seeks and on song changes, and `Status.Position` accounts for what it holds (`stretcher.Pending`) and for the output buffer at that speed. The UI sets it per track: `ui.book_speed` (0.75× to 2×) for audiobooks and podcasts, 1× for everything else.

## Service commands

```
eqmode graphic|parametric
peq '<JSON array of bands>'      # replaces all bands, at most 10
peqpreamp <dB>
resampler auto|linear|quality
crossfeed off|light|medium|strong
speed <0.5..3>                   # not saved; the UI sends it per track
```

They answer with the status like the other DSP commands (`replaygain`, `preamp`, `eq`, `crossfade`), and the result is saved in the service state (except `speed`, which is reported as `status.speed`). The UI reads the current values from `status.dsp` (`eq_mode`, `peq`, `peq_preamp_db`, `resampler`) and the active resampler from `status.resampler`.

## DSD: converted to PCM, no DoP

DSF and DFF files are decoded to PCM (`internal/decode/dsd.go`) and go through the same path as everything else. DSD over PCM (DoP), sending the DSD bits untouched to a DAC that understands them, is not implemented. Why:

- **No bit-perfect path.** The output is `oto`, which on Linux opens ALSA's `default`/`plug:default` (or PulseAudio/PipeWire) in float32 and lets the sound server mix and convert. DoP needs 24-bit words at exactly 176.4 kHz (DSD64) or 352.8 kHz (DSD128) to reach the DAC unchanged; any resampling, mixing, dithering or volume destroys the markers.
- **The device is taken.** A DoP stream would have to open the hardware device (`hw:X,Y`) directly. `oto` keeps its own PCM handle open for the life of the process, and on the desktop PipeWire/PulseAudio holds the card, so the open would fail with "device busy" unless `oto` is torn down first.
- **The engine is one pull pipeline.** `oto` pulls float frames from `Engine.Read`, where ReplayGain, EQ, crossfade, the limiter, volume and the meter all happen. A DoP song needs a second, push-style output path with none of those (volume must stay at 0 dB, or be done in the DAC), and its own position, seek, pause and gapless handling.
- **Nothing to verify against.** The handhelds' built-in codecs don't support DoP, and it needs an external USB DAC that does. A DAC without DoP plays the stream as low-level noise, so it would have to be opt-in.

What it would take (Linux only): a raw-DSD mode in the DSF/DFF decoder; a DoP packer (16 DSD bits per 24-bit word, MSB first, the 0x05/0xFA marker alternating every frame, left/right interleaved); an ALSA `hw:` writer through `purego`/libasound (`S32_LE` or `S24_3LE`, 176.4/352.8 kHz, the same library `oto` already loads); a way to suspend and close `oto`'s device while a DSD file plays and reopen it after; and a setting `dsd_output: pcm|dop` that falls back to PCM conversion whenever the hardware open fails. That is a few days of work plus a DoP DAC to test with.
