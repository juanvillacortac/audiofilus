# Tracker

The tracker (`internal/tracker`, screens in `internal/ui/tracker*.go`) is a hybrid of **LSDJ** (the Game Boy tracker: song → chains → phrases, instruments, tables, grooves, its commands) and **PICO-8** (an instrument and a command on every note, ready-made waveforms, a piano roll to draw notes). Author's decisions, October 20, 2026: hybrid structure; two sound modes per song; WAV export, MOD/XM import and export, LSDJ import and MIDI sync.

## Song modes

Each song has a mode, chosen when it's created and changeable in its settings:

| Mode | Channels | What each channel can play |
|---|---|---|
| **Game Boy** (`gb`) | 4: PU1, PU2, WAV, NOI | PU1/PU2: pulse instruments (PU1 also has the hardware sweep). WAV: wave instruments (the 32×4-bit wave RAM, its synth, the PICO-8 waveforms as wave presets) and sample kits, as LSDJ does. NOI: noise instruments. Everything goes through a model of the Game Boy APU: frequencies quantized to its 11-bit periods, its volume envelope, sweep, length and LFSR. |
| **Free** (`free`) | 8 | Any instrument on any channel: pulse, wave, noise, synth (sine, triangle, saw, square with PWM, organ, phantom, noise; ADSR), sample. No hardware quantization. |

Switching Game Boy → Free loses nothing. Free → Game Boy reports what doesn't fit (channels 5–8, an instrument on a channel of another type) and leaves it muted until fixed.

## Structure

- **Song:** rows; each row has a chain number (or empty) per channel. Playback walks the rows; each channel plays its own chain and moves on independently, like LSDJ. Up to 256 rows.
- **Chain** (256): up to 16 steps, each a phrase number and a transpose (signed semitones).
- **Phrase** (256): 16 steps, each with a **note** (C-0..B-9, or `---` empty, or `OFF`/kill), an **instrument**, a **volume** (0–F or none) and **two commands** (letter + hex argument). One phrase is 16 sixteenth notes by default; the groove decides how many ticks each step lasts.
- **Instrument** (64): type, volume and envelope, its type's parameters, an optional table, panning, automatic length, vibrato and transpose on or off.
- **Table** (32): 16 rows run one per tick (or per the table's own groove) while a note plays: volume, transpose and two commands per row; `H` hops (loops).
- **Groove** (32): ticks per step, alternated (6/6 straight, 8/4 swing…). Song tempo in BPM: 24 ticks per beat at groove 6/6, as in LSDJ.
- **Wave** (256): 32 samples of 4 bits. Presets: the PICO-8 waveforms (triangle, tilted saw, saw, square, pulse, organ, noise-ish, phantom).
- **Sample** (WAV files, kept next to the song): a kit's slots or a single sample, with root note and loop.

## Commands

Two per phrase step and per table row, `letter + hex byte`:

| | LSDJ | Meaning here |
|---|---|---|
| A | table | Start table xx (FF stops it) |
| C | chord | Arpeggio: note, +x, +y semitones |
| D | delay | Start the step x ticks late |
| E | envelope | Volume envelope override (initial volume x, step y) |
| F | finetune | Fine pitch / wave synth frame |
| G | groove | Switch to groove xx |
| H | hop | Jump (tables, chains) |
| I | (PICO-8 fade in) | Fade in over xx ticks |
| K | kill | Cut the note after xx ticks |
| L | slide | Slide to this note in xx ticks (legato) |
| M | master | Master volume |
| O | output | Panning: L, R, both, none |
| P | pitch | Pitch bend per tick (signed) |
| R | retrig | Retrigger every y ticks, volume change x |
| S | sweep/shape | PU1 sweep, noise shape |
| T | tempo | Song tempo |
| U | (PICO-8 fade out) | Fade out over xx ticks |
| V | vibrato | Speed x, depth y |
| W | wave/duty | Pulse duty or wave number |
| X | (PICO-8 drop) | Pitch drops to silence over xx ticks |
| Z | random | Randomizes the next command's argument |

Songs made with the old tracker (patterns of 16 steps) are converted on load: each pattern becomes one phrase per channel, the order becomes song rows with one chain per phrase, and the old effects map to `C` (arpeggio), `W` (duty) and `P` (bend).

## Formats

- **WAV:** render the song into the music folder (as before).
- **MOD / XM:** export (each channel's notes with sampled instruments rendered from the synth) and import (patterns to phrases and chains, samples to sample instruments).
- **LSDJ:** import `.lsdsng` (and songs from a `.sav`): its compressed blocks, then its song, chains, phrases, instruments, tables, grooves and waves map almost one to one.
- **MIDI sync:** send or follow MIDI clock (start, stop, 24 pulses per beat) on desktop systems with a MIDI port.
