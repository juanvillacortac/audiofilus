# Linux desktop and Omarchy

On a Linux desktop audiofilus behaves like any other app: it shows up in the launcher with its icon, the window manager recognizes its window, and the desktop's media controls drive it. Everything below works on any freedesktop desktop (GNOME, KDE, Hyprland...); [Omarchy](https://omarchy.org) is the one it's tested on.

## Installing

```sh
curl -fsSL https://juanvillacortac.github.io/audiofilus/install.sh | sh
```

`site/install.sh` (served by GitHub Pages) downloads the latest release for x86_64 or aarch64 into `~/.local/share/audiofilus` (`$AUDIOFILUS_DIR`), links both binaries into `~/.local/bin` (`$AUDIOFILUS_BIN`), and runs `audiofilus desktop install`. Each binary is copied next to its destination and renamed over it, so updating works while the service runs; `audiofilus.json` and `data/` in that folder are left alone.

## Launcher entry and icon

```sh
./audiofilus desktop install     # next to audiofilus-ui
./audiofilus desktop uninstall
```

`install` (`internal/desktop`) writes, for the binaries in the folder where `audiofilus` lives:

| File | What it is |
|---|---|
| `~/.local/share/applications/audiofilus.desktop` | Name "Audiofilus", `Exec` pointing at `audiofilus-ui`, `StartupWMClass=audiofilus`, and three actions (Play/Pause, Next track, Previous track) that call `audiofilus toggle|next|prev` |
| `~/.local/share/icons/hicolor/256x256/apps/audiofilus.png` | The icon: the logo's CD, pixel art scaled ×8 (`internal/desktop/icon.png`) |

`$XDG_DATA_HOME` replaces `~/.local/share` when set. If `update-desktop-database` and `gtk-update-icon-cache` exist, they're run; otherwise the desktop picks the files up by itself. Moving the binaries means running `install` again, since `Exec` is an absolute path.

The icon comes from the interface itself: `-shots` writes `icono.png` (the CD at 32×32, ×3), which is reduced to 1:1 and scaled ×8:

```sh
magick icono.png -crop 96x96+0+0 +repage -trim +repage -sample 33.3334% \
  -background none -gravity center -extent 32x32 -sample 800% internal/desktop/icon.png
```

## Window

The window's class (X11 `WM_CLASS`, which Hyprland shows as `class`) is `audiofilus`, set with `RunGameOptions.X11ClassName`, and its title is "Audiofilus". That matches `StartupWMClass`, so docks and bars group it under the launcher entry, and window rules can target it (`class:^audiofilus$`). The window icon is the same image (256 and 32 px).

## Media controls (MPRIS)

The service registers `org.mpris.MediaPlayer2.audiofilus` on the session bus (`internal/mpris`): Identity "Audiofilus", `DesktopEntry` "audiofilus" (so bars show the right name and icon), metadata with the cover, play/pause, next, previous, seek, shuffle, repeat and volume. On a desktop without a session bus (Windows, a bare console) it's skipped.

## Omarchy

- **Launcher:** after `audiofilus desktop install`, "Audiofilus" appears in the app launcher (Super + Space) with its icon.
- **Media keys and the bar:** Omarchy's media keys call `omarchy-shell media playPause|next|previous`, which talks MPRIS, so the keyboard's play/pause/next/previous keys and the bar's media widget control audiofilus with nothing to configure.
- **A shortcut to open it:** Omarchy binds Super + Shift + M to Spotify. To open (or focus) audiofilus instead, add to `~/.config/hypr/bindings.lua`:

  ```lua
  hl.unbind("SUPER + SHIFT + M")
  o.bind("SUPER + SHIFT + M", "Music", { launch = "/path/to/audiofilus-ui", focus = "^audiofilus$" })
  ```

  Then check it with `hyprctl reload` and `hyprctl configerrors`.
- **Colors:** the interface keeps its own colors, taken from the album cover, whatever the Omarchy theme.
