#!/bin/bash
# PortMaster launcher for audiofilus: opens the UI, which starts the audio
# service (audiofilus daemon) from its own folder. The service outlives the UI
# only when "Go to background" was chosen; otherwise the music stops on exit.

XDG_DATA_HOME=${XDG_DATA_HOME:-$HOME/.local/share}

if [ -d "/opt/system/Tools/PortMaster/" ]; then
  controlfolder="/opt/system/Tools/PortMaster"
elif [ -d "/opt/tools/PortMaster/" ]; then
  controlfolder="/opt/tools/PortMaster"
elif [ -d "$XDG_DATA_HOME/PortMaster/" ]; then
  controlfolder="$XDG_DATA_HOME/PortMaster"
else
  controlfolder="/roms/ports/PortMaster"
fi

source $controlfolder/control.txt
[ -f "${controlfolder}/mod_${CFW_NAME}.txt" ] && source "${controlfolder}/mod_${CFW_NAME}.txt"
get_controls

GAMEDIR="/$directory/ports/audiofilus"
BINARY="audiofilus-ui"

cd "$GAMEDIR"
> "$GAMEDIR/log.txt" && exec > >(tee "$GAMEDIR/log.txt") 2>&1

# The settings and the library cache live next to the binaries
# (audiofilus.json and data/); the service logs to data/audiofilus.log.
export AUDIOFILUS_HOME="$GAMEDIR"

# ArkOS and dArkOS have no PulseAudio: audio goes through ALSA, which takes the
# output device from ~/.asoundrc, and some ports remove it.
if [[ "${CFW_NAME^^}" == *"ARKOS"* ]]; then
  if [ ! -f ~/.asoundrc ] && [ -f ~/.asoundrcbak ]; then
    $ESUDO cp ~/.asoundrcbak ~/.asoundrc
    $ESUDO chmod ugo+rw ~/.asoundrc
    sleep 0.5
  fi
fi

# The UI presents through SDL2 (KMS/DRM) and reads the gamepad with
# PortMaster's mapping for this device; so does the service, which inherits
# this environment.
export EBITENGINE_UI_BACKEND=sdl2
export SDL_GAMECONTROLLERCONFIG="$sdl_controllerconfig"
echo "SDL_GAMECONTROLLERCONFIG=$SDL_GAMECONTROLLERCONFIG"

# gptokeyb only provides PortMaster's exit hotkey (Select+Start).
$GPTOKEYB "$BINARY" &
pm_platform_helper "$GAMEDIR/$BINARY"
./"$BINARY" "$@"

pm_finish
