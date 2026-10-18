# Audios de prueba

Tonos sintéticos generados con ffmpeg; no hay música real en el repo. Para regenerarlos:

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

Además (Opus, AIFF y M4A con AAC y ALAC; el módulo tracker de los tests se genera en el propio test):

```sh
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=2:sample_rate=48000" -ac 2 -c:a libopus -b:a 48k tone.opus
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=2:sample_rate=44100" -ac 2 -c:a pcm_s16be tone.aiff
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=2:sample_rate=44100" -ac 2 -c:a aac -b:a 64k tone.m4a
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=2:sample_rate=44100" -ac 2 -c:a alac alac.m4a
ffmpeg -v error -y -f lavfi -i "sine=frequency=440:duration=1:sample_rate=48000" -ac 1 -c:a alac -sample_fmt s32p alac24mono.m4a
```

`opus.ogg` sirve además para comprobar que, sin las bibliotecas en C (`AUDIOFILUS_DECODER=go`), Opus se rechaza con `ErrUnsupported`.
