# Test files

Short synthetic tones (no real music) with lyrics in their tags:

```sh
ffmpeg -f lavfi -i "sine=f=440:d=0.2" -ar 8000 tags.flac
metaflac --set-tag="LYRICS=[00:00.10]Tagged line" --set-tag="UNSYNCEDLYRICS=Plain tag" tags.flac
```

`tags.mp3` is the same tone as an MP3 with an ID3v2.3 tag written by hand (ffmpeg writes lyrics as TXXX): a USLT frame ("Unsynced words") and a SYLT frame in milliseconds ("First line" at 1 s, "Second line" at 2.5 s).

```sh
ffmpeg -f lavfi -i "sine=f=440:d=0.2" -ar 8000 -b:a 8k -id3v2_version 0 raw.mp3   # then prepend the tag
```
