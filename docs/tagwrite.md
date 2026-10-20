# Writing tags (`internal/tagwrite`)

Pure Go reader and writer for the tags of MP3, FLAC, Ogg Vorbis, Ogg Opus and M4A/MP4 files, for the tag editor, the MusicBrainz lookup, cover fetching and ReplayGain analysis.

```go
type Tags struct {
    Title, Artist, Album, AlbumArtist, Genre, Composer, Comment string
    Year, Track, TrackTotal, Disc, DiscTotal                    int
    ReplayGain *Gain   // nil: unchanged
    Cover      *Cover  // nil: unchanged (front cover)
    Clear      []string
}
type Gain struct { TrackGain, TrackPeak, AlbumGain, AlbumPeak float64; HasAlbum bool }
type Cover struct { MIME string; Data []byte }

func Read(path string) (Tags, error)
func Write(path string, t Tags) error
func Supported(path string) bool // by extension: .mp3 .flac .ogg .oga .opus .m4a .m4b .mp4
```

- **Write only changes what you pass.** Empty strings, zeros and nil pointers leave the file's value alone; to delete a field name it in `Clear` (`title`, `artist`, `album`, `albumartist`, `genre`, `composer`, `comment`, `year`, `track`, `tracktotal`, `disc`, `disctotal`, `replaygain`, `cover`). A `Gain` without `HasAlbum` keeps the album gain already in the file. If nothing changes, the file isn't touched.
- **Atomic:** the new file is written next to the original (same permissions) and renamed over it; on any error the original is untouched.
- **Byte-exact audio:** MP3 frames, FLAC frames, MP4 `mdat` and Ogg audio pages (data and granule positions) are copied unchanged; tests hash them before and after.
- **Unknown data is kept:** other ID3 frames (byte for byte), other FLAC blocks, other MP4 atoms, other Vorbis comments. Only the entries of the changed fields are replaced, in place.
- **Read** uses the same parsers, so `Read` returns exactly what `Write` would round-trip.

## Per format

- **MP3:** ID3v2.3 and 2.4 are read and written back in their own version (a file without a tag gets 2.4). 2.4 text is UTF-8; 2.3 text is Latin-1 when possible, else UTF-16 with BOM. Year is `TDRC` (2.4) or `TYER` (2.3); track and disc are `TRCK`/`TPOS` as `n/total`; ReplayGain is `TXXX:REPLAYGAIN_*`; the cover is an `APIC` of type 3 (if the file has a single picture, that one is replaced whatever its type). Numeric genres (`(17)`) are read as names (the 80 standard ID3v1 genres). An ID3v2.2 tag is converted to 2.4 keeping only the frames it understands (text, comment, picture). The extended header and tag-level unsynchronisation are dropped on rewrite. If the new tag fits in the old one (with its padding) the size stays the same; otherwise it gets 1 KB of padding. ID3v1/APE tags at the end are left alone.
- **FLAC:** `VORBIS_COMMENT` (created if missing) and `PICTURE` blocks; the other blocks are kept. If the new metadata fits in the old space (padding included) the audio doesn't move; otherwise 4 KB of padding is added. An ID3v2 tag in front of `fLaC` is kept.
- **Ogg Vorbis / Opus:** the comment packet is rebuilt and the header pages re-paginated (the Vorbis setup packet is copied as is; the last header packet ends its page). Audio pages keep their data and granule positions; their sequence numbers are shifted if the number of header pages changed and every CRC is recomputed. The cover is a `METADATA_BLOCK_PICTURE`. In **Opus** the gains are written as `R128_TRACK_GAIN`/`R128_ALBUM_GAIN` (Q7.8 dB relative to −23 LUFS, i.e. ReplayGain − 5 dB, as the Opus spec requires) and the peaks as `REPLAYGAIN_*_PEAK`; `REPLAYGAIN_*_GAIN` is not written in Opus files. Only a single logical stream is supported (no multiplexed or chained streams before the audio).
- **M4A/MP4:** `moov/udta/meta/ilst` (created if missing): `©nam ©ART ©alb aART ©gen ©wrt ©cmt ©day trkn disk covr` and `----:com.apple.iTunes:REPLAYGAIN_*`. If the `moov` is after the `mdat`, it just changes size. If it's before, a following `free` atom absorbs the growth (or takes the slack when it shrinks); otherwise all `stco`/`co64` chunk offsets are shifted and 1 KB of `free` is left for next time. Fragmented files (`moof`) are only written when the audio doesn't need to move.

## Not supported

WAV and AIFF (their ID3 chunk) are not written. Compressed or encrypted ID3v2.4 frames are kept but not read. `stco` offsets that would overflow 32 bits return an error rather than converting to `co64`.
