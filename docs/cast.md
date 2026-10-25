# Casting (internal/cast)

`internal/cast` sends music from audiofilus to another device on the local network: DLNA/UPnP speakers and TVs (MediaRenderer), Chromecasts, and MPD servers (another audiofilus, mpd; see [mpd.md](mpd.md)). Pure Go, no cgo, no new dependencies.

## Pieces

- **Discovery** (`Discover(ctx, timeout)`): SSDP `M-SEARCH` for `urn:schemas-upnp-org:device:MediaRenderer:1` plus the device description XML (AVTransport, RenderingControl and ConnectionManager control URLs; nested devices and `URLBase` handled), and mDNS PTR queries (`internal/mdns`, legacy unicast from an ephemeral port; PTR/SRV/TXT/A parsed with name compression) for `_googlecast._tcp.local` (`fn=` is the name, `md=` the model) and `_mpd._tcp.local`. All run at once; `Known()` returns the last result.
- **UPnP control** (`upnp.go`): SOAP to AVTransport (`SetAVTransportURI` with DIDL-Lite metadata: title, artist, album, cover, `protocolInfo` with the MIME type and duration; `Play`, `Pause`, `Stop`, `Seek` REL_TIME, `GetTransportInfo`, `GetPositionInfo`, `SetNextAVTransportURI` for gapless when supported) and RenderingControl (`SetVolume`/`GetVolume`).
- **Chromecast** (`castv2.go`): CASTV2 over TLS to port 8009 (self-signed certificate accepted). `CastMessage` protobuf is hand-encoded (fields 1–6), framed with a 4-byte big-endian length. CONNECT, PING/PONG heartbeat every 5 s, `LAUNCH` of the Default Media Receiver (`CC1AD845`), CONNECT to its transport, `LOAD` (`MusicTrackMediaMetadata`, metadataType 3, with cover image), `PLAY`/`PAUSE`/`SEEK`/`STOP`, `SET_VOLUME`, `GET_STATUS`. A dropped connection is redialled on the next call.
- **MPD** (`mpd.go`): `clear`, `addid <url>` with `addtagid` tags, `playid`/`seekid`; the next song is queued behind the current one so the server moves on gaplessly; `status` for state, position and volume. `Item.RemoteURI` plays a song from the server's own library by its URI.
- **Media server** (`serve.go`, `NewServer(addr)`): files are published under random tokens (`/m/<token>.<ext>`, covers under `/c/`); nothing else is served, and tokens expire after a day. The URL uses the LAN IP of the interface that routes to the device. MP3, FLAC, AAC/M4A, WAV and Ogg go as they are, through `http.ServeContent` (Range, HEAD). Everything else — modules, console music, DSD, Opus in some cases, CUE tracks and other virtual paths — is decoded with `internal/decode`, resampled to 44.1 or 48 kHz and streamed as 16-bit WAV of exact size, so Range requests work too (the byte offset is mapped to a frame and the decoder seeks). Songs without a known length stream without Range. DLNA headers (`transferMode`, `contentFeatures`) are set for picky renderers. http(s) items (servers, radio) are passed through untouched.
- **Session** (`session.go`): `NewSession(srv, dev)`, then `Cast(ctx, item, start)`, `Pause`, `Resume`, `Seek`, `SetVolume`, `Queue(ctx, next)`, `Status()`, `Current()`, `Events()`, `Close(ctx)`. It polls the device every second. When a song ends it plays the queued one (UPnP devices with `SetNextAVTransportURI` switch by themselves, without a gap) and emits `Advanced`; with nothing queued it emits `Finished`. Three failed polls in a row emit `Failed`. `ForceTranscode` sends everything as WAV.

## For the UI

```go
devs, _ := cast.Discover(ctx, 3*time.Second)
srv, _ := cast.NewServer("")          // one per process; keep it open
s, _ := cast.NewSession(srv, devs[i])
_ = s.Cast(ctx, cast.Item{Path: song.Path, Title: song.Title, Artist: song.Artist, Album: song.Album, Cover: coverPath, Duration: dur}, pos)
_ = s.Queue(ctx, nextItem)            // after each Cast/Advanced, the next song of the queue
for e := range s.Events() { /* Advanced: show e.Item; Finished; Failed: e.Err; Released */ }
s.Close(ctx)                          // stop the device; resume locally if wanted
```

Local playback should be paused while casting; on `Close` the UI decides whether to resume locally from `s.Status().Position`.

- **Controls the service reads** (the sleep gestures with the screen off, the FN hotkey, headset buttons, MPRIS media keys) would only touch the paused local engine. So while casting, the UI sends `remote on` (and `remote off` when it stops, or the service drops it when the UI detaches). The service then queues those commands (`toggle`, `next`, `prev`, `seek -10`, `pause`, `resume`) in `FullStatus.RemoteSeq`/`RemoteActs` instead, and the UI forwards them to the device through the same path as its own buttons (`castRemote`).
- **Released:** another audiofilus that chose "Stop receiving" empties its queue: as MPD, its queue goes empty; as a DLNA renderer, it forgets what it was given (`NO_MEDIA_PRESENT`). Either one, after something was sent, makes `Status` return `cast.ErrReleased`. The session emits `Released` and the UI resumes playing locally.

## Verified

- Unit tests: description and SSDP parsing, mDNS parsing (with compressed names), a fake UPnP renderer over HTTP (SOAP calls, DIDL escaping, status, volume, unsupported `SetNext`), CastMessage round trip, a fake TLS Chromecast (LAUNCH → LOAD on the transport → PAUSE → status), direct Range serving, the transcoded WAV (exact size, header, and a Range starting mid-frame matching the full download), the session's advance/finish logic. `go test -race` passes.
- On the author's LAN, discovery found a Chromecast built into a TV ("JVC 2K ISDB TV") and a CASTV2 TLS connection got its `RECEIVER_STATUS` (no app launched, no audio sent). No UPnP renderer was on that network, so UPnP was only tested against the fake renderer.

## Limits

- Chromecast queues are not used: the session loads the next song when one ends (a short gap).
- Some UPnP renderers only accept specific `protocolInfo` values; `ForceTranscode` (WAV) is the fallback. `GetProtocolInfo` is not consulted yet.
- Firewalls must allow the device to reach the media server's port (random by default; pass a fixed `addr` to `NewServer` if needed).
- mDNS uses legacy unicast queries; a network that drops multicast hides both kinds of device.
