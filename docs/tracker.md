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

- **Song:** rows; each row has a chain number (or empty) per channel. Playback walks the rows; each channel plays its own chain and moves on independently, like LSDJ. A channel with no chain in a row rests until the channels that do have one move to the next row, so rows stay aligned (LSDJ would stop that channel); a row with no chains at all rests for one phrase. Up to 256 rows.
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

## LSDJ import

`internal/tracker/lsdj.go` reads LSDJ songs: `ImportLSDSNG` for a `.lsdsng` (8-byte name, version byte, then the compressed blocks back to back) and `ImportLSDJSav` for a whole `.sav` (128 KB of cartridge RAM: the open song at 0x0000, the header at 0x8000 with the 32 project names, the `jk` mark, the active project and the block allocation table, and the 0x200-byte blocks from 0x8200). It returns every saved song, plus the open one (named with a `*`) when it differs from its saved copy. The format follows [liblsdj](https://github.com/stijnfrishert/liblsdj) (MIT): the block compression (`0xC0` runs, `0xE0` specials for the default wave/instrument, block jumps and end), the 0x8000-byte song memory map (`song_offsets.h`) and the instrument bytes (`instrument*.c`).

Since the tracker mirrors LSDJ, most of it maps 1:1, in Game Boy mode: song rows (trailing empty rows dropped), chains with transposes, phrases (LSDJ's C-3, the lowest Game Boy note, is our note 25, i.e. C-2), instruments, tables, grooves, the 256 waves and the tempo (values under 40 mean 256+). Commands keep their letter (codes shift by one from format 8, when B was added); `H` keeps only the step (no loop count), `M` merges the left/right volumes into one, `F` and `P` keep their raw argument (LSDJ counts in its own pitch steps, so the effect is close, not identical), and `B` plus the Arduinoboy commands are dropped. Instruments: pulse keeps envelope (the NRx2 byte; from format 11 the ADSR is approximated by its initial level and decay), length, sweep, duty, pan, table and transpose; wave instruments use the first wave of their synth and the wave volume; kits (sampled drums) become a short noise hit. Song formats older than 4 (LSDJ before ~3.x) are refused with an error. Tested against the sample `.lsdsng`/`.sav` files of liblsdj (LSDJ 6.2 to 8.8.8), which aren't in this repo.

## MOD / XM

`internal/tracker/mod.go`, `xm.go` and `modexport.go`. Both formats go through one intermediate form (`modSong`: patterns of rows × channels, each cell a note, instrument, volume and one effect).

**Import** (`ImportMOD`: ProTracker `M.K.`/`M!K!`/`FLT4`/`4CHN`, `6CHN`, `8CHN`/`FLT8`/`CD81`/`OKTA`, `xxCH`; `ImportXM`: FastTracker 2) gives a Free-mode song. Each pattern becomes, per channel, phrases of 16 steps and one chain of them (identical phrases and chains are shared); each order position is a song row. A `Bxx`/`Dxx` jump ends the pattern there (an `H` in every channel), and so does an XM pattern whose row count isn't a multiple of 16. Timing is exact: a module row lasts speed ticks of 2.5/BPM s and a tracker step groove ticks of 60/(BPM·24) s, the same thing, so the module's BPM is the song's and each speed becomes a one-value groove (`G`, placed one step early since the player applies a groove from the next step; a speed change on the first row of a pattern other than the first still lands one row late). Each module sample becomes a sample instrument (`Song.Samples`, 8 or 16 bit, forward or ping-pong loops) at full instrument volume: the module's per-note volume (the sample's default, `Cxx`, the XM volume column, and `Axy`/`EAx`/`EBx`/`5xy`/`6xy` and XM column slides, applied per row) goes in the step volume. MOD pitch: period 428 is C-4, sampled at 7093789.2/(2·428) ≈ 8287 Hz, with the sample finetune. XM: note numbers match (C-0 = 1); the sample's relative note and finetune set its root and rate (8363 Hz at C-4); an instrument keeps only the sample mapped to C-4; its volume envelope is approximated with the ADSR (peak point, sustain point, fadeout as release); sample panning sets the instrument pan; up to 32 channels are read and the first 8 kept. Effects: `0xy`→`C`, `1xx`/`2xx`→`P` (converted to cents per tick, linear or Amiga), `3xx` (and `5xy`, the XM column porta)→`L` with the ticks to reach the note, `4xy`/`6xy`→`V`, `8xx` and the XM column pan→`O`, `E9x`→`R`, `ECx`→`K` (`EC0`→`OFF`), `EDx`→`D`, `Fxx`→`G`/`T`, XM `Kxx`→`OFF`. Dropped: `9xx` sample offset, `E6x` pattern loops, tremolo, global volume, envelope position, fine portamentos, arpeggio/vibrato memory nuances. Checked locally (not in the repo) against several MOD and XM files rendered by libopenmpt: the song lengths match to the millisecond and the loudness envelopes correlate 0.9–1.0 on most files (lower where heavy per-tick volume slides or hard panning dominate).

**Export** (`ExportXM`, `ExportMOD`): the song is flattened (`flatten`): each song row becomes a pattern as long as its longest chain (hops followed, chain transposes applied, notes above B-7 moved down an octave for XM; MOD keeps its three octaves, C-3 to B-5 here, folding the rest). Speed changes (each step's groove value, so swing becomes alternating `Fxx`) and `T` go in `Fxx` effects: in XM on extra control channels (the channel count is padded to even for FT2), in MOD in the first free effect slot, with a `D00` ending each partial 64-row pattern and the BPM in the first row. Instruments: sample instruments export their sample (MOD: converted to 8 bits, with notes shifted and the finetune set so they sound at the right pitch); pulse, wave, synth and noise instruments are rendered to a looped single cycle (or 8192 samples of their LFSR) that sounds at the right pitch. XM gets a volume envelope from the Game Boy envelope (start, direction, speed) or the ADSR (fadeout from the release); MOD only the volume. Commands back to effects: `C`→`0xy`, `P`→`1xx`/`2xx`, `L`→`3xx`, `V`→`4xy`, `K`→`ECx`, `D`→`EDx`, `R`→`E9x`, `O`→`8xx`, `OFF`→key off (XM) or `EC0` (MOD); one effect per cell, the step volume in the XM volume column (MOD: `Cxx` when the effect is free). Lost: tables, the other commands, per-channel grooves, and channels 5–8 in MOD (`ExportMOD` returns an error if they are used). The exported files load in libopenmpt with the same duration as their source.

## MIDI

`internal/midi` talks MIDI without cgo on Linux and Windows, and through CoreMIDI (cgo, like the rest of the iOS-only code) on iOS:

| System | Ports | How |
|---|---|---|
| Linux handhelds (Knulli, muOS, ROCKNIX, ArkOS, EmuELEC) and desktop | USB MIDI class devices (keyboards, controllers, sequencers) | ALSA raw MIDI character devices `/dev/snd/midiC<card>D<dev>`, read and written as files. Names come from `/proc/asound/card<N>/midi<D>` (or the card id). Knulli runs the UI as root; on other systems the user must be in the `audio` group. |
| Windows | Every MIDI input, paired with the output of the same name | `winmm.dll` through `golang.org/x/sys/windows` (`midiInOpen` with a callback, `midiOutShortMsg`). |
| iOS / iPadOS | CoreMIDI sources, paired with the destination of the same name (USB through the camera adapter, already-paired Bluetooth MIDI) | `MIDIClientCreate`, `MIDIInputPortCreate`, `MIDISend`. |

`midi.Watch` re-scans every couple of seconds, so a keyboard plugged in with the app open shows up. The byte parser handles running status, realtime messages interleaved anywhere and skips sysex; note-on with velocity 0 is a note-off.

**Clock.** The tracker runs at 24 ticks per beat, exactly MIDI clock's 24 pulses per quarter note, so one pulse is one tick:

- **Follow an external clock** (`Player.SetExternalClock`): every `0xF8` pulse advances the sequencer one tick (`ExternalTick`); `0xFA` Start plays the song from row 0, `0xFB` Continue resumes, `0xFC` Stop stops. Audio keeps rendering between pulses; the tempo shown is measured from the pulse intervals (`ExternalBPM`). Pulses take effect in the next audio block, so they are heard one output buffer later (a constant 50–100 ms).
- **Send our clock** (`Player.ClockOut`): a goroutine sends Start, pulses at the song tempo (including `T` changes) and Stop. It runs in real time while the audio is rendered ahead, so the receiving device runs ahead of what you hear by the output latency.

**Notes.** Incoming notes map to tracker notes as MIDI note − 11 (MIDI 60, middle C, is C-4); velocity maps to volume 0–F. The UI uses them to preview the current instrument and to write notes into the phrase.

Limits: no song position pointer (`0xF2`), no MIDI out of the tracker's own notes yet, and the iOS backend is only built by the iOS CI (it can't be compiled on Linux). Wired USB MIDI on iOS needs no permission; showing the Bluetooth MIDI pairing screen would need `NSBluetoothAlwaysUsageDescription` in the Info.plist, which the app doesn't do (Bluetooth devices paired elsewhere still appear).
