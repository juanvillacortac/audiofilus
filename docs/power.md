# Power

The project's goal is to use less battery than Songo #5 in the pocket. These are the measurements so far and how they were taken.

## Baseline: the RG34XX-SP with Songo #5 (October 2026)

The battery is 3000 mAh. "Time remaining" is `time_to_empty_now` from the battery indicator; the percentage drop is more reliable than that estimate (see below).

| Situation | Draw | % per hour |
|---|---|---|
| Songo, screen on (brightness 33) | ~630 mA | ~21 % |
| Songo, screen off ("Bright Fade"), CPU fixed at 1.5 GHz | ~460 mA | ~15 % |
| Same with `balanced` (`schedutil`) | ~370 mA | ~12 % |
| Same, limiting Songo to 10 FPS in sleep ("Power Saver") | **~255 mA** | **~8.5 %** |
| Knulli menu, screen off, nothing open | ~300–330 mA | ~10–11 % |

What was left of Songo in sleep: ~38 % of one core (drawing at 10 FPS 16 %, SDL audio 11 %, decoding 7 %).

## audiofilus on the PC (Ryzen 7 5700U)

| Measurement | CPU of one core |
|---|---|
| Service playing MP3, steady (20 s) | 2.4 % |
| `bench`: MP3 decoding only | 1.45 % |
| `bench`: + resampling to 48 kHz + conversion to bytes | ~1.5 % |

Almost all the cost is `go-mp3`; the rest (~1 %) is the `oto` mixer and sending to PulseAudio.

## audiofilus on the console (RG34XX-SP, `schedutil` at 1.5 GHz)

| Measurement | CPU of one core |
|---|---|
| `bench`: decoding MP3 (`go-mp3`) | **18 %** |
| Service playing, steady (20 s) | **25 %** |

`go-mp3` costs ~2.5 times what FFmpeg cost in Songo (7 %): that is the thing to optimize. At 480 MHz (`powersave`) it would be ~55 % of one core. Battery draw in pocket mode was not measured (the test was cut short).

## audiofilus on the console with libmpg123 (October 18, 2026)

Decoding MP3 with libmpg123 costs 0.6 % of one core at 1.5 GHz (go-mp3: 18 %); see the formats table in [architecture.md](architecture.md#formats).

App sleep with the music **paused**, screen off (`backlight(0)`), wifi on (connected over SSH), quick estimate with `time_to_empty_now` over 3 min:

| Governor | Draw | CPU (service + interface) |
|---|---|---|
| `performance` (fixed 1.5 GHz) | ~295 mA | ~1 % + 0.4 % of one core |
| `powersave` (480 MHz) | **~270 mA** | |

Since then, sleep sets `powersave` by default (`sleep.governor`). audiofilus's CPU barely matters anymore: what's left is the system baseline with the screen off (the Knulli menu in that situation draws ~300–330 mA with `performance`) and the wifi. Still to measure: playing and without wifi, with the reliable method (`capacity` drop).

### Playing in sleep (October 18, 2026)

With MP3 playing, sleep, `powersave` and wifi on, ~260 mA according to `time_to_empty_now` (8.9 h at 77 %), almost the same as Songo (~255 mA). Per process, 30 s:

| Process | CPU of one core at 480 MHz |
|---|---|
| Service (decode, resample, oto) | ~8–10 % |
| PortMaster's `gptokeyb` | ~5 % |
| Interface | ~1 % |
| `knulli-audio getSystemVolume` (every 5 s) | short-lived processes: they don't show up in the table, but the console total was ~47 % of one core versus ~30 % adding up processes |

Changes: in sleep the service no longer reads the system volume (nobody sees it and it can't be changed from outside), and it freezes the interface's `gptokeyb` with SIGSTOP (it only provides PortMaster's Select+Start shortcut) and resumes it on wake. Still to do: measure after these changes and profile the service: `touch data/daemon-cpuprofile` records 20 s into `data/daemon-cpu.prof` (the interface one, `data/cpuprofile`, is useless in sleep because the interface barely runs).

Measured afterwards (same conditions, 60 s): the whole console went from ~47 % to **~18 % of one core**; the service, ~8 %; the interface, ~1.7 %; `gptokeyb`, 0 (frozen). The service profile in sleep: ~30 % in C calls (mpg123 and sending to PulseAudio), **16 % in the meter** (level and bass, which nobody looks at in sleep) and the rest in oto's mixing. Since then the whole meter (not just the spectrum) is only computed with the app open and awake. Still to do: measure with the reliable method (`capacity` drop) and compare with Songo.

## How to measure

See the [`measure-power`](../.claude/skills/measure-power/SKILL.md) skill. In short:

- **CPU of a process:** difference in `utime+stime` in `/proc/<pid>/stat` over 20 s or more, divided by `CLK_TCK` and the elapsed time. `ps %CPU` is the average since the process started and includes startup; it is useless here.
- **Battery, quick:** `time_to_empty_now` every 10 s for 2–3 min. It moves slowly after a change and jumps a lot between readings.
- **Battery, reliable:** how long `capacity` takes to drop 1 % (1 % ≈ 30 mAh), ideally with several points over an hour. The percentage rises while charging, and the estimate freezes for a few minutes after unplugging the charger.
- **Frames actually drawn:** count `FBIOPAN_DISPLAY` on `/dev/fb0` with `strace -f -p <pid> -e trace=ioctl`. The `fps:60.2` in `/sys/class/disp/disp/attr/sys` is the panel refresh rate, not the application's.
