# Smart playlists

`internal/smart` evaluates rule-based playlists over the local library, the listening history (`data/history.jsonl`, `internal/history`) and the marks (likes and ratings, `data/favorites.json`). It doesn't depend on the UI: the UI converts its songs to `smart.Track` with whatever it knows (missing fields stay zero and never match rules about them).

## Definition

Saved as a JSON array in `data/smart.json` (`smart.Load` / `smart.Save`; with no file, `Load` returns the presets).

```json
{
  "name": "Rock 90s",
  "match": "all",
  "rules": [
    {"field": "genre", "op": "is", "value": "Rock"},
    {"field": "year", "op": "between", "value": "1990", "value2": "1999"}
  ],
  "sort": {"field": "plays", "desc": true},
  "limit": 100,
  "plays_days": 0,
  "live": true
}
```

- `match`: `all` (default) or `any`. No rules means every song.
- `sort.field`: any field, or `random` (stable for the whole day, seeded by the day and the playlist name). Empty keeps library order.
- `limit`: 0 is unlimited.
- `plays_days`: counts only listens from the last N days in the `plays` field (0: all time).
- `live`: a hint for the UI to re-evaluate on open rather than keep a snapshot.

## Fields and operations

| Kind | Fields | Ops |
|---|---|---|
| Text (case-insensitive) | title, artist, album, albumartist (falls back to artist), genre, format (extension if unset), folder, path | is, isnot, contains, notcontains, startswith |
| Number | year, track, duration (s), bitrate (kbps), plays, rating (0–5) | is, isnot, gt, lt, between |
| Date | added, lastplayed | inlast N (days), notinlast N, gt / lt / between (YYYY-MM-DD, inclusive days) |
| Bool | liked | true, false |

A song never played has no `lastplayed`: it doesn't match `inlast`, `gt`, `lt` or `between`, and it does match `notinlast`. `smart.Fields()` lists fields with their kind and ops for building a rule editor; `smart.Validate` reports unknown fields, ops and unparsable values (`Evaluate` skips bad rules).

## Presets

`smart.Presets()`: Most played (30 days), Recently added (30 days), Never played (random), Liked, Top rated (rating > 3), Forgotten favorites (more than 4 plays, none in 90 days), Random mix (today).

## API

```go
paths := smart.Evaluate(def, tracks, history.Load(dataDir), history.LoadMarks(dataDir), time.Now())
```

Performance: 50,000 songs and 100,000 listens with three rules and a sort take about 32 ms on a desktop CPU (`go test -bench Evaluate ./internal/smart`).

## Server playlist editing

Not part of this package, but related: `internal/remote` edits Subsonic and Jellyfin playlists (`PlaylistEditor`, and on `Store` by server slug and song paths): `CreatePlaylist`, `AddToPlaylist`, `RemoveFromPlaylist` (by position), `MovePlaylistItem`, `RenamePlaylist`, `DeletePlaylist`. Subsonic moves rewrite the whole list (`createPlaylist` with `playlistId`); Jellyfin uses each entry's `PlaylistItemId`, and renaming needs Jellyfin 10.9 or newer (`POST /Playlists/{id}`). After an edit, re-read `Playlists` to refresh the cached catalog.
