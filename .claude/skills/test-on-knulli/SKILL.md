---
name: test-on-knulli
description: Install and test audiofilus on the author's Knulli console (RG34XX-SP) over SSH with a password. Use it to upload a binary, start or stop the service on the console, read its log, or check that no mounts were left behind.
---

# Testing audiofilus on the Knulli console

The console is `root@knulli.local`. **It only accepts a password** (the Knulli default) and `ssh-copy-id` has no effect. There is no `sshpass`.

## Connecting without typing the password

Use `SSH_ASKPASS` with a script in your temporary folder (scratchpad) that `echo`es the password. Do not store it in the repo.

```sh
ASK=<scratchpad>/askpass.sh            # #!/bin/sh + echo <password>; chmod +x
k() { SSH_ASKPASS=$ASK SSH_ASKPASS_REQUIRE=force DISPLAY=:0 \
      ssh -o StrictHostKeyChecking=accept-new -o PubkeyAuthentication=no root@knulli.local "$@"; }
kcp() { SSH_ASKPASS=$ASK SSH_ASKPASS_REQUIRE=force DISPLAY=:0 \
      scp -O -o PubkeyAuthentication=no "$@"; }   # -O: the console has no sftp-server
```

Filter the "post-quantum key exchange" warning out of the output. If `knulli.local` does not resolve, the console's wifi is asleep: ask the author to wake it up. Do not retry in a loop.

## Installing

The port lives in `/userdata/roms/ports/audiofilus` (installed through PortMaster), with the console's binaries named `audiofilus.aarch64` and `audiofilus-ui.aarch64` and the launcher at `/userdata/roms/ports/Audiofilus.sh`.

```sh
./build.sh                                   # dist/audiofilus-arm64 and dist/audiofilus-ui-arm64
D=/userdata/roms/ports/audiofilus
kcp dist/audiofilus-arm64 root@knulli.local:$D/audiofilus.aarch64.new
kcp dist/audiofilus-ui-arm64 root@knulli.local:$D/audiofilus-ui.aarch64.new
k "cd $D && mv -f audiofilus.aarch64.new audiofilus.aarch64 && mv -f audiofilus-ui.aarch64.new audiofilus-ui.aarch64 && chmod +x audiofilus.aarch64 audiofilus-ui.aarch64"
```

Check the upload with `md5sum` on both sides. If the service is running, `./audiofilus quit` makes the next launch use the new binary.

Upload as `.new` and then `mv`: this does not corrupt a running service. The configuration (`audiofilus.json`, `data/`) lives in that same folder.

## Testing

```sh
k '/userdata/roms/ports/audiofilus/audiofilus.aarch64 play /userdata/MUSIC'     # starts the detached service
k '/userdata/roms/ports/audiofilus/audiofilus.aarch64 status'
k 'tail -20 /userdata/roms/ports/audiofilus/data/audiofilus.log'
k '/userdata/roms/ports/audiofilus/audiofilus.aarch64 quit'
```

After `quit` or a crash, check that nothing was left modified:

```sh
k 'grep -E "capability|power-button" /proc/mounts || echo limpio; ls /var/run/battery-saver/'
```

## Be careful

- Pocket mode turns off the screen and freezes EmulationStation: warn the author before turning it on, and always leave the way out (`pocket off` or Start).
- Do not install anything on the console that has not passed `go test ./...` and a test on the PC.
- `pgrep -f <text>` also matches your own SSH command if it contains that text. Use `pgrep -x audiofilus` or filter.
- Knulli does not ship `mountpoint`, `strings` or `sftp-server`.
