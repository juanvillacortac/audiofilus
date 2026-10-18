# Control protocol

A client opens the Unix socket (see `config.Paths.Socket`), sends **one** JSON request on a single line, reads **one** response and closes. It is implemented in `internal/control`.

```json
→ {"cmd": "seek", "args": ["+10"]}
← {"ok": true, "data": { ...state... }}
← {"ok": false, "error": "usage: seek <seconds>|+<seconds>|-<seconds>"}
```

Almost every command responds with the full state (`daemon.FullStatus`):

```json
{
  "state": "playing",
  "track": {"path": "/.../Wasteland.mp3", "title": "Wasteland", "artist": "10 Years", "album": "The Autumn Effect"},
  "position": 64.2, "duration": 231.0,
  "index": 0, "count": 597,
  "shuffle": false, "repeat": "off", "volume": 1,
  "sleep": false, "awake": false, "platform": "desktop",
  "output": "wired",
  "pad": "Anbernic RG34XX-SP Controller (/dev/input/event1, mapping sdl)"
}
```

`state` is `playing`, `paused` or `stopped`. `position` already subtracts the audio that is still in the buffer.

## Commands

| Command | Arguments | What it does |
|---|---|---|
| `status` | — | Only returns the state |
| `play` | `[paths…]` | With no paths and a queue: resumes. Otherwise, replaces the queue with the files, folders or `.m3u` (or with `music_dirs`) and plays |
| `load` | `start files…` | Replaces the queue with those files (already in order) and plays from position `start`. The UI uses it when choosing an album or a song |
| `add` | `paths…` | Appends to the end of the queue |
| `playnext` | `files…` | Puts them right after the current song, in that order |
| `toggle` · `pause` · `resume` | — | Pause/resume |
| `next` · `prev` | — | `prev` restarts the song if it has played for more than 3 s, like Songo |
| `jump` | `n` | Plays position `n` of the queue |
| `seek` | `s` · `+s` · `-s` | Jumps to `s` seconds or moves `±s` |
| `shuffle` | `[on\|off\|toggle]` | With no argument, toggles |
| `repeat` | `off\|all\|one` | |
| `volume` | `0..2` | The audiofilus volume (1 is the original); the UI uses `volstep` |
| `volstep` | `+N` · `-N` | Raises or lowers N points on the single 0 to 200 scale: the system volume up to 100 and the audiofilus boost above that (when lowering, the boost goes first). Called by the volume buttons (through the `knulli-audio` wrapper) and by + and − in the UI |
| `queue` | — | Returns the list of paths in playback order (not the state) |
| `sleep` | `[on\|off\|toggle] [oled]` | The app's pseudo-sleep: gestures and, without `oled`, backlight off (see [knulli.md](knulli.md)) |
| `awake` | `on [seconds]` · `off` | The app's permission to prevent suspend while it is open (playing or not); it expires after 60 s if not renewed |
| `attach` | — | The UI opens it and keeps the connection open while it lives. When it closes (on exit or because the UI died) the service gets `detached`: if `background` was not requested, it pauses, saves the state and exits |
| `background` | — | The UI is about to close and the music must keep going: background mode. It holds until the next `attach` |
| `quit` | — | Stops the service, saves the state and undoes all the Knulli changes |

The CLI resolves relative paths against the current folder before sending them. Paths in `music_dirs` are resolved against the binary's folder.
