# Online module archives

`internal/modsrc` searches and downloads music from four scene archives. None of them needs an API key.

| Source | How it's read | Search | Browse | By artist | Download |
|---|---|---|---|---|---|
| **The Mod Archive** (modarchive.org) | Its web pages (the XML API needs a key) | File name or title (up to 3 pages) | `random` (the 10 its search box suggests), `top` (top hits chart), `new` (latest uploads, first 100) | Artist roster search → the artist's modules (up to 3 pages) | `api.modarchive.org/downloads.php?moduleid=<id>`; the name comes from `Content-Disposition` |
| **Modland** (ftp.modland.com) | Its full index, `allmods.zip` (~6 MB, `<size>\t<path>` per line, ~520 000 files) | Every word in the path (case-insensitive), offline | `random` (50, reservoir-sampled) | The artist path component | `ftp.modland.com/pub/modules/<path>` |
| **VGMRips** (vgmrips.net) | Its web pages | Packs (the site returns at most 20) | `random` (5 pack pages), `top`, `new` | Composer page (`/packs/composer/<slug>`) | The pack zip; the playable tracks (`.vgz`) are extracted, sorted |
| **AMP** (amp.dascene.net) | Its web pages (`newresult.php` form) | Module titles | `new` (latest additions) | Handle search → the composer's modules | `downmod.php?index=<id>` redirects to a `.gz`, which is gunzipped |

## Behaviour

- **Only what Audiofilus can play** is listed (`Playable`, from the decoders' extension lists: libxmp modules and libgme console formats). SID, AHX, DBM and most Amiga-only formats are left out. VGMRips packs are always listed.
- **The Modland index** is downloaded once into `<data>/cache/modsrc/modland.txt` and refreshed after 30 days (if the refresh fails, the old one keeps working). Only playable lines are kept (~230 000, ~12 MB). Searches stream it line by line instead of loading it, which matters on handhelds. Paths are `<format>/<artist>/<file>` (sometimes `<format>/<artist>/<game>/<file>`); `Video Game Music` and `Ad Lib` have one more level (system or program) before the artist.
- **Downloads** (`Fetch`) go to `<dir>/<source>/...` and are reused if already there. Each file is written as `.part` and renamed, so an interrupted download never looks complete. Pack extraction keeps only file names (no folders), so nothing can be written outside the pack folder. Downloads are capped at 64 MB. LHA/LZX archives are rejected with a clear error.
- **Politeness:** every request carries `User-Agent: audiofilus (+https://github.com/juanvillacortac/audiofilus)`, and requests to the same host are spaced at least 400 ms apart.

## Limits

- The three web scrapers depend on the sites' HTML. The parsers are tested against trimmed copies of the real pages (`internal/modsrc/testdata`), and `AUDIOFILUS_LIVE=1 go test -run TestLive ./internal/modsrc` checks the real sites: search, browse, by artist, and a download that decodes.
- The Mod Archive's search matches whole words of the file name or title. Search results don't include the size or artist; a module page does.
- AMP has no random or top list; Modland has no top or new list (`ErrUnsupported`).
