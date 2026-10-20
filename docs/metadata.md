# Metadata and loudness

Two packages feed the library tools (tag editing, missing covers, ReplayGain for untagged songs). They only **return data**; writing it into files is `internal/tagwrite`'s job, and the UI decides when to call them.

## `internal/musicbrainz`

MusicBrainz WS/2 (JSON, no key), the Cover Art Archive and, as a fallback for covers, the iTunes Search API (no key).

- **Politeness.** One request per second to MusicBrainz (`Client.Interval`, the waits honour the context), and the User-Agent `audiofilus/<version> ( https://github.com/juanvillacortac/audiofilus )` on every request. CAA and iTunes are not rate limited (they're only hit once per album).
- **API.**
  - `New() *Client`
  - `(*Client).SearchRelease(ctx, artist, album, trackCount) ([]Release, error)`: up to 25 releases, ranked by MusicBrainz score plus how well the track count matches the local album (exact +0.3, ±1 +0.1, otherwise −0.2) and a small bonus for primary type "Album". Search results carry no tracks.
  - `(*Client).GetRelease(ctx, mbid) (Release, error)`: media and tracks (disc, number, title, artist, length, recording id), date/year, country, label, top genres (release, then release group, then free tags), release-group id.
  - `(*Client).LookupRecording(ctx, artist, title, durationSec) ([]Recording, error)`: single songs; with a duration, recordings within ±10 s come first, then those with no length.
  - `MatchAlbum(local []LocalTrack, rel Release) ([]TrackMatch, confidence float64)`: pairs each local song with a release track. Score = title similarity (0.6; case, accents, punctuation and anything in parentheses or brackets ignored; Levenshtein ratio) + track number (0.25, disc-aware) + duration (0.15, full within 3 s, nothing past 30 s), renormalised over what is known. Greedy assignment from the best pairs, each track used once; pairs under 0.35 stay unmatched (`Track == nil`). The confidence is the mean score, for the UI to warn before overwriting tags.
  - `(*Client).FrontCover(ctx, releaseID, groupID, size)`: CAA `front-250/500/1200` (0 = original) for the release, falling back to the release group. Returns the bytes and the image MIME type.
  - `(*Client).ITunesCover(ctx, artist, album, size)`: first iTunes album whose artist matches, at `size`×`size`.
  - `(*Client).Cover(ctx, releaseID, groupID, artist, album, size)`: CAA, then iTunes.
  - `ErrNotFound` when nothing matches.

## `internal/loudness`

EBU R128 / ITU-R BS.1770-4 integrated loudness and ReplayGain 2.0.

- K-weighting computed for the source's sample rate (the libebur128 derivation; at 48 kHz it reproduces the standard's coefficients), 400 ms blocks every 100 ms, absolute gate −70 LUFS, relative gate −10 LU. A mono file decoded as two identical channels is measured as one channel, as BS.1770 says (3 dB less than the same signal on two different channels).
- **Peak:** true peak by 4× (below 96 kHz) or 2× oversampling with a windowed-sinc polyphase interpolator (12 taps per phase), or the plain sample peak with `Options{SamplePeak: true}`. To keep it cheap, it only interpolates where the two neighbouring samples could reach the current peak (+3 dB margin, the band-limited worst case).
- **ReplayGain 2.0:** `Gain = -18 - LUFS` dB, `Peak` linear. Silence (nothing above the absolute gate, or shorter than one 400 ms block) gives `Silent`, `LUFS = -Inf`, `Gain = 0`.
- **Memory:** constant apart from one float64 pair per 100 ms block (about 560 KB for an hour), needed for the relative gate and for album gain.
- **API.**
  - `NewAnalyzer(rate, opts) *Analyzer`, `(*Analyzer).Add(stereoInterleaved []float32)`, `(*Analyzer).Result() Result`.
  - `Measure(ctx, decode.Source, opts, progress) (*Analyzer, error)` and `File(ctx, path, opts, progress)`: read a whole source (cancellable; `progress(doneFrames, totalFrames)`).
  - `Album(tracks ...*Analyzer) Result`: album loudness gated over all tracks' blocks together (as if played back to back), album peak = highest track peak.
  - `Result{LUFS, Gain, Peak, TruePeak, Seconds, Silent}`.
- **Verified:** EBU Tech 3341 case 1 (stereo 1 kHz at −23 dBFS → −23.0 LUFS at 48, 44.1 and 11.025 kHz), the BS.1770 48 kHz coefficients to 1e-8, gating and album tests; against `ffmpeg -af ebur128=peak=true+sample` on two real MP3s: −9.82 vs −9.8 LUFS and −6.49 vs −6.5 LUFS, peaks within 0.05 dB.
- **Speed** (analysis plus MP3 decoding, real songs): about 110 ms per minute of audio on a desktop (80 ms with sample peak); on the RG34XX-SP (H700) about 1.4 s per minute (0.86 s with sample peak), so a 4-minute song takes 5–6 s. Run it in the background, one song at a time.
