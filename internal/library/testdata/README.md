# Test files

Short synthetic tones (no real music) with ReplayGain tags:

```sh
ffmpeg -f lavfi -i "sine=f=440:d=0.2" -ar 8000 rg.flac
metaflac --set-tag="REPLAYGAIN_TRACK_GAIN=-6.54 dB" --set-tag="REPLAYGAIN_TRACK_PEAK=0.988" --set-tag="REPLAYGAIN_ALBUM_GAIN=-7.10 dB" rg.flac
ffmpeg -f lavfi -i "sine=f=440:d=0.2" -ar 8000 -b:a 8k -id3v2_version 3 -metadata "REPLAYGAIN_TRACK_GAIN=+2.30 dB" -metadata "REPLAYGAIN_ALBUM_GAIN=-1.00 dB" rg.mp3
ffmpeg -f lavfi -i "sine=f=440:d=0.2" -c:a libopus -b:a 16k -metadata R128_TRACK_GAIN=-1280 rg.opus
```
