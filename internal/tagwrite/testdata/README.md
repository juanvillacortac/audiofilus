# Test audio for tagwrite

Half a second of a 440 Hz sine, generated with ffmpeg (no real music). To regenerate:

```sh
S="-f lavfi -i sine=frequency=440:duration=0.5:sample_rate=22050 -ac 1"
ffmpeg -v error -y $S -c:a libmp3lame -b:a 32k -id3v2_version 4 -metadata title="Tono v24" -metadata artist=audiofilus -metadata album=Pruebas -metadata track=2/9 -metadata date=2020 tone24.mp3
ffmpeg -v error -y $S -c:a libmp3lame -b:a 32k -id3v2_version 3 -metadata title="Tono v23" -metadata artist=audiofilus -metadata genre=Rock tone23.mp3
ffmpeg -v error -y $S -c:a libmp3lame -b:a 32k -id3v2_version 0 -write_id3v1 0 -map_metadata -1 bare.mp3
ffmpeg -v error -y $S -c:a flac -metadata title="Tono flac" -metadata artist=audiofilus -metadata tracknumber=3 tone.flac
ffmpeg -v error -y $S -c:a libvorbis -q:a 0 -metadata title="Tono vorbis" -metadata artist=audiofilus tone.ogg
ffmpeg -v error -y -f lavfi -i sine=frequency=440:duration=0.5:sample_rate=48000 -ac 1 -c:a libopus -b:a 16k -metadata title="Tono opus" -metadata artist=audiofilus tone.opus
ffmpeg -v error -y $S -c:a aac -b:a 24k -metadata title="Tono m4a" -metadata artist=audiofilus -metadata track=4/10 -movflags +faststart front.m4a
ffmpeg -v error -y $S -c:a aac -b:a 24k -metadata title="Tono m4a fin" -metadata artist=audiofilus back.m4a
ffmpeg -v error -y $S -c:a aac -b:a 24k -map_metadata -1 -movflags +frag_keyframe+empty_moov+default_base_moof+skip_trailer -frag_duration 200000 dash.m4a
ffmpeg -v error -y $S -c:a aac -b:a 24k -map_metadata -1 -movflags +frag_keyframe+empty_moov+skip_trailer -frag_duration 200000 fragabs.m4a
```

`dash.m4a` is fragmented like YouTube's DASH audio (each `tfhd` has default-base-is-moof, no `mfra`), so it can be shifted as is; `fragabs.m4a` is fragmented with absolute base offsets, which tagwrite must refuse. `front.m4a` has the moov before the mdat (so growing the tags shifts the chunk offsets), `back.m4a` after it. The cover images in the tests are generated noise PNGs.
