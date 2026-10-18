---
name: measure-power
description: Measure audiofilus CPU usage and the battery drain of the Knulli console (RG34XX-SP). Use it to compare pocket mode with Songo, verify an optimization, or answer "how much does it use".
---

# Measuring power

Record every new measurement in `docs/power.md`, with the exact situation (screen, governor, what was open) and the date.

## CPU of a process

`ps %CPU` is the average since the process started (it includes startup): it is useless here. Measure the tick difference over an interval:

```sh
P=$(pgrep -x audiofilus); HZ=$(getconf CLK_TCK)
a=$(awk '{print $14+$15}' /proc/$P/stat); sleep 20; b=$(awk '{print $14+$15}' /proc/$P/stat)
echo "$(( (b-a)*100/(20*HZ) ))% of one core"
```

To split it by thread, do the same with `/proc/$P/task/*/stat` and `comm`. To separate stages (decoding, resampling, converting): `audiofilus bench <file>`.

## Battery (on the console)

Indicator: `/sys/class/power_supply/axp2202-battery/` (`capacity`, `time_to_empty_now`, `status`). The battery is 3000 mAh, so 1 % ≈ 30 mAh.

- **Quick (2–3 min):** read `time_to_empty_now` every 10 s. mA ≈ `capacity/100 * 3000 / (time_to_empty_now/3600)`. It takes a while to settle and jumps around a lot; average it.
- **Reliable (30–60 min):** note the time of each `capacity` change. Minutes per 1 % → %/h. Start a logger with `nohup setsid` so it survives the SSH session.
- `status` must be `Discharging`. After unplugging the charger, the estimate stays frozen for a few minutes.
- The real backlight state is in `/sys/class/disp/disp/attr/sys` (`backlight(  0)` = off).

## Reference comparisons

See the table in `docs/power.md`: Songo in "Power Saver" ~255 mA, the Knulli menu with the screen off ~300–330 mA. For the comparison to be valid, measure in the same situation (screen off, same governor, music playing).

## Frames drawn

To check how many FPS a UI really draws: `timeout 5 strace -f -p <pid> -e trace=ioctl` and count `FBIOPAN_DISPLAY` on `/dev/fb0`. The panel's `fps:60.2` is not the application's.
