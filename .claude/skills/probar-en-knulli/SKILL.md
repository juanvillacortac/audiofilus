---
name: probar-en-knulli
description: Instalar y probar audiofilus en la consola Knulli del autor (RG34XX-SP) por SSH con contraseña. Úsala para subir un binario, arrancar o parar el servicio en la consola, leer su log o comprobar que no quedaron montajes.
---

# Probar audiofilus en la consola Knulli

La consola es `root@knulli.local`. **Solo acepta contraseña** (la de Knulli por defecto) y `ssh-copy-id` no tiene efecto. No hay `sshpass`.

## Conectar sin escribir la contraseña

Usa `SSH_ASKPASS` con un script en tu carpeta temporal (scratchpad) que haga `echo` de la contraseña; no la guardes en el repo.

```sh
ASK=<scratchpad>/askpass.sh            # #!/bin/sh + echo <contraseña>; chmod +x
k() { SSH_ASKPASS=$ASK SSH_ASKPASS_REQUIRE=force DISPLAY=:0 \
      ssh -o StrictHostKeyChecking=accept-new -o PubkeyAuthentication=no root@knulli.local "$@"; }
kcp() { SSH_ASKPASS=$ASK SSH_ASKPASS_REQUIRE=force DISPLAY=:0 \
      scp -O -o PubkeyAuthentication=no "$@"; }   # -O: la consola no tiene sftp-server
```

Filtra el aviso de "post-quantum key exchange" de la salida. Si `knulli.local` no resuelve, el wifi de la consola está dormido: pide al autor que la despierte; no reintentes en bucle.

## Instalar

```sh
./build.sh                                   # dist/audiofilus-arm64
kcp dist/audiofilus-arm64 root@knulli.local:/userdata/audiofilus/audiofilus.new
k 'cd /userdata/audiofilus && mv audiofilus.new audiofilus && chmod +x audiofilus'
```

Sube con `.new` y luego `mv`: no corrompe un servicio que esté corriendo. La configuración (`audiofilus.json`, `data/`) vive en esa misma carpeta.

## Probar

```sh
k '/userdata/audiofilus/audiofilus play /userdata/MUSIC'     # arranca el servicio separado
k '/userdata/audiofilus/audiofilus status'
k 'tail -20 /userdata/audiofilus/data/audiofilus.log'
k '/userdata/audiofilus/audiofilus quit'
```

Después de `quit` o de un cierre, comprueba que no quedó nada tocado:

```sh
k 'grep -E "capability|power-button" /proc/mounts || echo limpio; ls /var/run/battery-saver/'
```

## Cuidado

- El modo bolsillo apaga la pantalla y congela EmulationStation: avisa al autor antes de activarlo, y deja siempre la salida (`pocket off` o Start).
- No instales en la consola algo que no pasó `go test ./...` y una prueba en el PC.
- `pgrep -f <texto>` también encuentra tu propio comando SSH si contiene ese texto; usa `pgrep -x audiofilus` o filtra.
- Knulli no trae `mountpoint`, `strings` ni `sftp-server`.
