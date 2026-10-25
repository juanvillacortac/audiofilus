# MPD

The service (not the interface) can be an **MPD server** on the local network (`internal/mpd`, wired in `internal/daemon/mpd.go`). It's off by default. MPD clients (mpc, ncmpcpp, Cantata, M.A.L.P., Rigelian, MPDroid…) and other Audiofilus control the Audiofilus that runs it. They see its queue and library, and can send it songs by URL. Audiofilus is also an MPD **client**: "Send to" finds MPD servers next to Chromecasts and DLNA renderers, and while sending to one, its library can be browsed and played.

```json
"mpd": { "on": true, "port": 6600, "password": "", "name": "audiofilus (living room)" }
```

- `port` 0 or empty means 6600. `name` empty means the device name (see [DLNA](dlna.md)), plus ` :<port>` when the port isn't 6600, so two Audiofilus on one computer don't share a name.
- The interface has a tile in Settings → Online (on the phones too: on iOS it is announced and found through the system's Bonjour, see [iOS](ios.md#local-network)). It opens a menu with the switch, the password (on-screen keyboard) and the address to type in an MPD app (`192.168.1.5:6600`).
- The interface edits `audiofilus.json` and sends `mpd apply`. From a terminal, `audiofilus mpd on|off`, `audiofilus mpd port <n>`, `audiofilus mpd password [<password>]` and `audiofilus mpd name <name>` change the file and apply it. `audiofilus mpd` prints the state as JSON (`on`, `port`, `name`, `password`, `error`).

## Network

- **TCP** on the port, MPD's text protocol, announced as version 0.23.5. Without a password anyone on the network can control it; with one, only `password`, `ping`, `commands`, `tagtypes` and `binarylimit` work before it's given.
- **mDNS** (`internal/mdns`): `_mpd._tcp` with TXT `app=audiofilus` (and `password=1` if it asks for one), announced at start (three times, 1, 2 and 4 s apart), with a goodbye (TTL 0) when turned off, and answers to queries: multicast ones, and legacy unicast ones (from a port other than 5353) straight to the asker. The SRV host is `<hostname>-audiofilus-<port>.local`, not the computer's own name (which Avahi already owns), and its A record is the address on the asker's network. Docker, VM bridges and point-to-point VPN interfaces are left out.
- Port 5353 is shared (`SO_REUSEADDR`): several Audiofilus on one computer, and Avahi, can listen at once. Two Go details matter here. `ListenMulticastUDP` turns off `IP_MULTICAST_LOOP`, so without turning it back on, programs on the same computer (Avahi, another Audiofilus) never saw the answers. Avahi also drops mDNS packets whose IP TTL isn't 255.
- Battery: nothing polls while no client is connected. With clients connected, the server reads the engine's state every 400 ms (and right away when the engine reports a change) to answer `idle`.

## What the server does

- **Playback and state:** `status`, `currentsong`, `stats`, `play`/`playid`, `pause`, `stop`, `next`, `previous`, `seek`/`seekid`/`seekcur`, `setvol`/`volume`/`getvol` (the engine's volume, 0–100), `random`, `repeat`, `single`.
  - `repeat` alone is "repeat all"; `repeat` with `single` is "repeat one".
  - `single` without `repeat` is "stop after this song".
  - `consume`, `crossfade`, `replay_gain_mode` and the outputs are accepted and do nothing.
- **Queue:** `add`/`addid` (a song or a whole directory, at a position, or relative to the current one with `+N`/`-N`), `delete`/`deleteid`, `move`/`moveid`, `swap`/`swapid`, `shuffle`, `clear`, `playlistinfo`/`playlistid`/`playlist`, `plchanges`/`plchangesposid`, `playlistfind`/`playlistsearch`.
  - The queue is the engine's, in play order (with shuffle on, the shuffled order).
  - Ids are stable while a song stays in the queue (`queue.IDs`).
  - The engine's `QueueVersion` is MPD's playlist version. `plchanges` sends the whole queue if it changed: what changed isn't kept.
- **Library:** `lsinfo`, `listall`/`listallinfo`, `listfiles`, `find`/`search` (with `sort` and `window`), `findadd`/`searchadd`/`searchaddpl`, `list` (with `group`, and the old `list album <artist>`), `count`, `update`.
  - Filters come in both syntaxes: old pairs (`find artist X album Y`) and 0.21+ expressions with `==`, `!=`, `contains`, `starts_with`, `=~`, `!~`, `base`, `AND` and `!`. `find` is exact; `search` ignores case and matches substrings.
- **Covers:** `albumart` and `readpicture`, in chunks (`binarylimit`).
  - `albumart` sends the loose cover: `<file>.jpg`, then `cover.jpg`, `folder.jpg`, `front.jpg` (and `.png`) in the song's folder, then the embedded one.
  - `readpicture` sends the embedded one.
- **Stored playlists:** `listplaylists`, `listplaylist`/`listplaylistinfo`, `load`, `save` (`create`, `append`, `replace`), `rm`, `rename`, `playlistadd`, `playlistclear`, `playlistdelete`, `playlistmove`.
  - They are the interface's: the `.m3u` in the music folders and `data/playlists/*.m3u8`. New ones go in `data/playlists`.
- **Protocol:** `idle`/`noidle` (`player`, `mixer`, `options`, `playlist`, `database`, `stored_playlist`), command lists (`command_list_begin`, `command_list_ok_begin`), `commands`, `tagtypes`, `urlhandlers`, `decoders`, `outputs`, `ping`, `close`.

### The library

The library is `data/library.json`, the one the interface scanned. It's re-read when it changes; the service never rescans, so `update` only re-reads the file.

- A song's URI is its path relative to the music folder, with `/`. With several music folders, each is a top-level directory named after the folder.
- Tracks inside a file (CUE sheets, libgme) are `<file>/track0001`, as in MPD.
- Remote songs (Subsonic, Jellyfin) aren't in it.
- Tags: `Artist`, `AlbumArtist`, `Album`, `Title`, `Track`, `Genre`, `Date` (the year). `AlbumArtist` falls back to `Artist` for filters and grouping. Durations aren't in the cache, so songs show one only while playing.

### Songs by URL

`add http(s)://…` puts the URL in the engine's queue as `mpd://u/<n>`, and `addtagid`/`cleartagid` set its tags. The engine opens it through `OpenRemote`, which reaches the server's `internal/fetch` downloader, the same one the DLNA renderer uses.

- The file is downloaded whole to `data/cache/mpd` before it plays, so it can seek.
- Something without a length (a radio) plays live.
- Files are deleted when their song leaves the queue.
- **Cover:** `addtagid <id> AudiofilusCover <url>` (`mpd.CoverTag`, an Audiofilus-only tag; a real mpd rejects it, so Audiofilus only sends it to another Audiofilus) gives the URL of the song's cover, which is downloaded next to it (`fetch.Cover`). Without one, the cover embedded in the downloaded song is used. Either way the service reports it as `FullStatus.Cover`, and Now Playing shows it with its colors.
- A seek sent while the song is still downloading is remembered by the engine (`waitSeek`) and applied when the song arrives.

### Receiving

The server remembers the client that last changed the queue or what plays (`add`, `addid`, `clear`, `play`…) and the queue version it left. While that client is still connected and the queue is still that one, the service reports `Receiving` (its address) in its status; a DLNA renderer song reports `DLNA`. Now Playing shows `← <address>` in its title, and its Start menu has:

- **Stop receiving** (`audiofilus release`): empties the queue, so the music stops. An Audiofilus that was sending sees the empty queue and goes on playing on its own.
- **Turn off MPD server**: the same, and the server goes off (it can be turned back on in Settings → Online → MPD server).

## Audiofilus as a client

`internal/mpd` also has the client (`mpd.Dial`, `Cmd`, `Picture`, `Songs`, `Map`). The tests use it, and so does `cast`:

- **Discovery:** `cast.Discover` also browses `_mpd._tcp` (`mdns.Browse`). It reports each server's name, `Model` "Audiofilus" or "MPD", and whether it asks for a password. Its own server is left out of the list (`cast.IsOwnMPD`). For a server with a password, the interface asks for it before sending, and remembers it while the app runs.
- **Sending** (`cast/mpd.go`):
  - Load is `clear`, then `addid <url>` with `addtagid` for title, artist and album, then `playid` (and `seekid` when resuming mid-song).
  - To another Audiofilus (`Model` "Audiofilus"), the cover URL goes with `addtagid` too (`mpd.CoverTag`): the loose image (`cover.jpg`) or the embedded one, both served by the sending server. A real mpd only gets the tags it knows.
  - The next song is another `addid` after the current one. The server moves on by itself, gaplessly, and the session notices as it does with UPnP's `SetNext`.
  - State, position and volume come from `status`. The connection reconnects once if it drops.
- **Its library:** while sending to an MPD server, Now Playing → Start has "Library of <device>". It shows the server's albums (`list album group albumartist`), then an album's songs (`find` by album and album artist, or by album alone for an mpd without album artists).
  - Covers come from the server (`albumart`, then `readpicture`; `internal/ui/mpdcover.go`): an album's is the one of its first song (`find … window 0:1`). They show on each album's button, in the album's header and in Now Playing. The last 64 downloaded are kept in memory.
  - Picking a song plays that album on the device from that song.
  - These songs are `mpdlib://<host>/<uri>` in the send queue and go to the device by URI (`cast.Item.RemoteURI`), with nothing served from here.
  - Next, previous, seeking and the queue work as with local songs.
  - "Stop sending" doesn't resume them here, since they aren't on this computer.

## Testing with several instances on one computer

Each instance needs its own folder (`AUDIOFILUS_HOME`, which also gives it its own control socket) and its own port:

```sh
mkdir -p /tmp/a /tmp/b
echo '{"mpd":{"on":true,"port":6601,"name":"audiofilus A"}}' > /tmp/a/audiofilus.json
echo '{"mpd":{"on":true,"port":6602,"name":"audiofilus B"}}' > /tmp/b/audiofilus.json
AUDIOFILUS_HOME=/tmp/a audiofilus daemon &
AUDIOFILUS_HOME=/tmp/b audiofilus daemon &
avahi-browse -rt _mpd._tcp          # both, each with its own host and port
mpc -p 6601 status
```

The library is the `data/library.json` of each folder, which the interface writes when it scans (`AUDIOFILUS_HOME=/tmp/a audiofilus -music ~/Music`).
