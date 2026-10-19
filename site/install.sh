#!/bin/sh
# Installs (or updates) audiofilus on a Linux desktop:
#   curl -fsSL https://juanvillacortac.github.io/audiofilus/install.sh | sh
#
# - Downloads the latest release for this machine (x86_64 or aarch64).
# - Puts it in ~/.local/share/audiofilus. Settings (audiofilus.json) and state
#   (data/) live there too and are kept on updates.
# - Links audiofilus (the player, the audio service and the command line, in
#   one binary) into ~/.local/bin.
# - Adds Audiofilus to the app launcher (audiofilus desktop install).
#
# Uninstall:
#   audiofilus quit; audiofilus desktop uninstall
#   rm -rf ~/.local/share/audiofilus ~/.local/bin/audiofilus
set -eu

repo=juanvillacortac/audiofilus
dest=${AUDIOFILUS_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/audiofilus}
bin=${AUDIOFILUS_BIN:-$HOME/.local/bin}

say() { printf '%s\n' "$*"; }
die() { printf 'audiofilus: %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = Linux ] || die "this installer is for Linux (Windows: download the zip from https://github.com/$repo/releases/latest)"
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "no build for $(uname -m)" ;;
esac
command -v tar >/dev/null || die "tar is needed"
if command -v curl >/dev/null; then
	fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null; then
	fetch() { wget -qO "$2" "$1"; }
else
	die "curl or wget is needed"
fi

name=audiofilus-linux-$arch
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
say "Downloading $name..."
fetch "https://github.com/$repo/releases/latest/download/$name.tar.gz" "$tmp/a.tar.gz" ||
	die "download failed"
tar -xzf "$tmp/a.tar.gz" -C "$tmp"

# The binary is copied next to its destination and then renamed over it: the
# service may be running, and a running binary can be replaced but not
# overwritten.
mkdir -p "$dest/licenses" "$bin"
cp "$tmp/$name/audiofilus" "$dest/.audiofilus.new"
chmod 755 "$dest/.audiofilus.new"
mv -f "$dest/.audiofilus.new" "$dest/audiofilus"
ln -sf "$dest/audiofilus" "$bin/audiofilus"
# Up to 0.2.x the interface was a second binary, audiofilus-ui.
rm -f "$dest/audiofilus-ui" "$bin/audiofilus-ui"
cp "$tmp/$name/licenses/"* "$dest/licenses/"

if "$dest/audiofilus" desktop install >/dev/null 2>&1; then
	say "Added Audiofilus to the app launcher."
fi
say "Installed in $dest"
case ":$PATH:" in
*":$bin:"*) ;;
*) say "Note: $bin is not in your PATH; open Audiofilus from the launcher or run $dest/audiofilus" ;;
esac
if pgrep -f '^audiofilusd daemon' >/dev/null 2>&1 || pgrep -f "^$dest/audiofilus daemon" >/dev/null 2>&1; then
	say "The audio service is running the old version: run 'audiofilus quit' to restart it on the next launch."
fi
