# DLNA / UPnP

The service (not the interface) can be a **DLNA media server** and a **DLNA media renderer** on the local network (`internal/dlna`, wired in `internal/daemon/dlna.go`). Both are off by default.

```json
"dlna": { "server": true, "renderer": true, "name": "audiofilus (living room)" }
```

- `name` empty means the device name (`platform.DeviceName`): the hostname as it is, or, on a handheld that keeps its system's default hostname (`KNULLI`, `ROCKNIX`, `muos`…), the console model: PortMaster's `DEVICE_NAME` (the launcher passes it as `AUDIOFILUS_DEVICE`), muOS's `board/name`, Knulli's `batocera.board` or the device tree model, skipping generic SoC names like `sun50iw9`. The phone apps pass their own (`mobile.SetDeviceName`): on Android the name given in the phone's settings, or the model; on iOS `UIDevice.name`.
- The interface edits `audiofilus.json` and sends `dlna apply` so the service re-reads it. From a terminal, `audiofilus dlna server on|off`, `audiofilus dlna renderer on|off` and `audiofilus dlna name <name>` change the file and apply it; `audiofilus dlna` prints the state as JSON (`server`, `renderer`, `name`, `url`, `subscribers`, `error`).

## Network

- **SSDP** on 239.255.255.250:1900: `ssdp:alive` at start and every 15 min (max-age 1800), `ssdp:byebye` when turned off, and answers to `M-SEARCH` (after the random MX delay). The listener joins the group on every IPv4 interface that is up, and every LOCATION uses the address of the interface the request came from.
- **HTTP** on a free port: device and service descriptions, SOAP control, GENA events, the media and the covers.
- The UUIDs are stored in `data/dlna-server.uuid` and `data/dlna-renderer.uuid`, so the device keeps its identity across restarts.
- Battery: the SSDP listener is a socket blocked on read and the announcer is a timer; nothing polls. The service never keeps the console awake because of DLNA.

## Media server

ContentDirectory with `Browse` (`BrowseMetadata`, `BrowseDirectChildren`, paging) and a simple `Search` (title, artist, album contains; albums or artists by class), and ConnectionManager.

- The library is the one the interface scanned: `data/library.json` (re-read when it changes; the service never rescans). Remote songs (Subsonic, Jellyfin) are not served.
- Root: **Artists** → albums → tracks, **Albums**, **Songs**, **Playlists** (the `.m3u` in the music folders and `data/playlists`), **Folders** (from each music folder).
- Songs are served with `Range`, `transferMode.dlna.org: Streaming` and `contentFeatures.dlna.org` (`DLNA.ORG_OP=01`). MP3, FLAC, Ogg, Opus, AAC/M4A, WAV and AIFF go as they are. What TVs don't know (DSD, modules, console music, CUE tracks) is decoded and served as 16-bit WAV, seekable by byte range when the length is known.
- Covers (`albumArtURI`): the embedded picture, `<file>.jpg` next to the song, or `cover.jpg`/`folder.jpg`/`front.jpg` (and `.png`) in its folder.

## Media renderer

AVTransport (`SetAVTransportURI`, `SetNextAVTransportURI`, `Play`, `Pause`, `Stop`, `Seek` by `REL_TIME`/`ABS_TIME`, `Next`, `Previous`, `GetPositionInfo`, `GetTransportInfo`, `GetMediaInfo`, …), RenderingControl (volume 0–100 and mute) and ConnectionManager (sink protocols for what Audiofilus decodes, including `audio/L16`), with **LastChange** events over GENA, which most control points (BubbleUPnP, Windows' "Cast to device") rely on.

- A URL becomes the queue as `dlna://r/<n>`. The engine opens it through `OpenRemote`: the file is downloaded whole to `data/cache/dlna` first (it plays as soon as it's there; a song over home Wi-Fi takes a moment), because the decoders seek in the file. A URL without `Content-Length` (or with ICY headers) is treated as a live stream and played like a radio station. `audio/L16` is saved as WAV.
- Title, artist and album come from the DIDL-Lite metadata the control point sends.
- Only the current and next URLs are kept in the cache; everything is deleted when the renderer stops or the service starts.
- If music from the interface replaced the renderer's, the renderer reports `STOPPED`; `Play` brings its URL back.

## Testing

`go test ./internal/dlna` covers SOAP for each action, DIDL-Lite (escaping, paging), Range and WAV transcoding, SSDP messages, the renderer state machine with a fake engine, GENA notifications and downloads. To try it on a real network, enable both in a scratch home and search with any UPnP tool (or an `M-SEARCH` from a script); BubbleUPnP on Android sees both devices.
