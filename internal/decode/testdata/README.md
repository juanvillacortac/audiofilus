# Test audio

Synthetic tones generated with ffmpeg; there is no real music in the repo. To regenerate them:

```sh
for fmt in mp3 flac ogg wav; do
  case $fmt in
    mp3)  c="-c:a libmp3lame -b:a 64k" ;;
    flac) c="-c:a flac -sample_fmt s16" ;;
    ogg)  c="-c:a libvorbis -q:a 0" ;;
    wav)  c="-c:a pcm_s16le" ;;
  esac
  ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=2:sample_rate=44100" -ac 2 $c \
    -metadata title="Prueba $fmt" -metadata artist=audiofilus tone.$fmt
done
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=1:sample_rate=48000" -ac 1 -c:a pcm_s24le mono24.wav
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=1:sample_rate=44100" -c:a libopus opus.ogg
```

Also (Opus, AIFF, and M4A with AAC and ALAC; the tracker module for the tests is generated inside the test itself):

```sh
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=2:sample_rate=48000" -ac 2 -c:a libopus -b:a 48k tone.opus
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=2:sample_rate=44100" -ac 2 -c:a pcm_s16be tone.aiff
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=2:sample_rate=44100" -ac 2 -c:a aac -b:a 64k tone.m4a
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=2:sample_rate=44100" -ac 2 -c:a alac alac.m4a
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=1:sample_rate=48000" -ac 1 -c:a alac -sample_fmt s32p alac24mono.m4a
```

`opus.ogg` is also used to check that, without the C libraries (`AUDIOFILUS_DECODER=go`), Opus is rejected with `ErrUnsupported`.

`chapters.m4b` and `chapters.mp3` are three seconds of tone with three chapters (One, Two, Three, a second each), for `TestChapters`. ffmpeg writes both kinds of MP4 chapters (Nero `chpl` and the QuickTime text track) and ID3 `CHAP` frames in the MP3:

```sh
printf ';FFMETADATA1\ntitle=Book\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=1000\ntitle=One\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=1000\nEND=2000\ntitle=Two\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=2000\nEND=3000\ntitle=Three\n' > ch.txt
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=3:sample_rate=22050" -i ch.txt -map_metadata 1 -map_chapters 1 -ac 1 -c:a aac -b:a 24k chapters.m4b
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=3:sample_rate=22050" -i ch.txt -map_metadata 1 -map_chapters 1 -ac 1 -c:a libmp3lame -b:a 32k -id3v2_version 3 chapters.mp3
```
