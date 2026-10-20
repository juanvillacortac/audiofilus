#!/bin/sh
# HELP: audiofilus: a pixel-art music player that keeps playing in the background, with radio, podcasts, servers and games.
# ICON: audiofilus
# GRID: audiofilus

# muOS application launcher (no PortMaster needed). muOS runs it as
# "mux_launch.sh <app folder>". It supports both launcher generations:
# Jacaranda and later have SETUP_APP; older releases use
# SETUP_SDL_ENVIRONMENT.
for f in /opt/muos/script/var/func.sh /opt/muos/script/var/stage.sh /opt/muos/script/var/retro.sh; do
	[ -f "$f" ] && . "$f"
done

APP_DIR="${1:-$(dirname "$0")}"
cd "$APP_DIR" || exit 1

command -v SETUP_STAGE_OVERLAY >/dev/null 2>&1 && SETUP_STAGE_OVERLAY
if command -v SETUP_APP >/dev/null 2>&1; then
	SETUP_APP "audiofilus" ""
else
	echo app >/tmp/act_go
	command -v SETUP_SDL_ENVIRONMENT >/dev/null 2>&1 && SETUP_SDL_ENVIRONMENT
fi

# The settings and the library cache live in this folder (audiofilus.json and
# data/); the service logs to data/audiofilus.log.
export AUDIOFILUS_HOME="$APP_DIR"
# The UI presents through SDL2 (KMS/DRM); SDL gives the gamepad mapping.
export EBITENGINE_UI_BACKEND=sdl2

chmod +x ./audiofilus.aarch64 2>/dev/null
./audiofilus.aarch64 >"$APP_DIR/log.txt" 2>&1 &
UI=$!
# muOS closes the app with "pgrep -f <name>", which would also match the music
# service (audiofilusd daemon): it gets the UI's PID instead, which it kills
# directly. The service then keeps playing only if "Background" was chosen.
command -v SET_VAR >/dev/null 2>&1 && SET_VAR "system" "foreground_process" "$UI"
wait "$UI"
