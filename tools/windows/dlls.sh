#!/bin/sh
# Baja de MSYS2 (mingw64) las DLL que audiofilus carga en Windows con purego
# y deja junto al .exe del paquete: libmpg123 (MP3), libfdk-aac (AAC: radios,
# HLS, M4A), libopusfile (Opus), libxmp (módulos tracker) y libgme (música de
# consolas), con lo que ellas necesitan. Sin ellas Windows solo toca lo que
# está en Go puro (ver docs/architecture.md#windows).
#   tools/windows/dlls.sh <carpeta de salida>
# deja <salida>/*.dll, <salida>/licenses/<paquete>/ (los textos de cada
# licencia) y <salida>/licenses/windows-dlls-SOURCE.txt (qué paquete y
# versión de MSYS2 es cada una).
#
# Las dependencias salen de la base de datos de MSYS2 (%DEPENDS%) y después
# se comprueban las DLL que importa cada una (objdump): si falta alguna que
# no sea del sistema, falla. fdk-aac ya no está en MSYS2 y se compila con zig
# (fdk-aac.sh). Necesita curl, tar con zstd, python3, objdump, cmake y zig.
set -eu
mkdir -p "${1:?usage: tools/windows/dlls.sh <output dir>}"
out=$(cd "$1" && pwd)
repo=https://repo.msys2.org/mingw/mingw64
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl -sSfL "$repo/mingw64.db" -o "$work/db.tar.zst"
mkdir "$work/db"
tar --zstd -xf "$work/db.tar.zst" -C "$work/db"

# Los paquetes de las DLL que se cargan, y los que piden (recursivo): una
# línea "nombre archivo versión" por paquete.
python3 - "$work/db" > "$work/pkgs.txt" <<'EOF'
import os, re, sys
db = sys.argv[1]
info = {}
for d in os.listdir(db):
    f = os.path.join(db, d, "desc")
    if not os.path.exists(f):
        continue
    fields, key = {}, None
    for line in open(f, encoding="utf-8"):
        line = line.rstrip("\n")
        if line.startswith("%") and line.endswith("%"):
            key = line.strip("%"); fields[key] = []
        elif line and key:
            fields[key].append(line)
    name = fields["NAME"][0]
    info[name] = fields
    for p in fields.get("PROVIDES", []):
        info.setdefault(re.split(r"[<>=]", p)[0], fields)
want = ["mingw-w64-x86_64-mpg123", "mingw-w64-x86_64-opusfile",
        "mingw-w64-x86_64-libxmp", "mingw-w64-x86_64-libgme"]
seen, todo = {}, list(want)
while todo:
    n = re.split(r"[<>=]", todo.pop())[0]
    if n not in info:
        sys.exit("msys2: no package " + n)
    f = info[n]
    real = f["NAME"][0]
    if real in seen:
        continue
    seen[real] = f
    todo += f.get("DEPENDS", [])
for n, f in sorted(seen.items()):
    print(n, f["FILENAME"][0], f["VERSION"][0])
EOF

# Cada paquete en su carpeta (para saber de cuál sale cada DLL) y todos
# juntos en root.
mkdir "$work/root" "$work/pkg"
while read -r name file version; do
  curl -sSfL "$repo/$file" -o "$work/pkg.tar.zst"
  mkdir "$work/pkg/$name"
  tar --zstd -xf "$work/pkg.tar.zst" -C "$work/pkg/$name" 2>/dev/null
  cp -r "$work/pkg/$name/." "$work/root/"
done < "$work/pkgs.txt"
bin="$work/root/mingw64/bin"

# Desde las que carga audiofilus, las que importan (sin las de Windows).
system='^(kernel32|user32|advapi32|msvcrt|ws2_32|shell32|ole32|oleaut32|gdi32|winmm|bcrypt|crypt32|shlwapi|secur32|dbghelp|ntdll|api-ms-win-.*|ucrtbase)\.dll$'
need="libmpg123-0.dll libopusfile-0.dll libxmp.dll libgme.dll"
done=""
while [ -n "$need" ]; do
  set -- $need
  dll=$1; shift; need="$*"
  case " $done " in *" $dll "*) continue ;; esac
  [ -f "$bin/$dll" ] || { echo "missing $dll" >&2; exit 1; }
  done="$done $dll"
  for imp in $(objdump -p "$bin/$dll" | sed -n 's/^.*DLL Name: //p'); do
    lower=$(printf %s "$imp" | tr 'A-Z' 'a-z')
    if printf %s "$lower" | grep -Eq "$system"; then continue; fi
    real=$(ls "$bin" | grep -ix "$imp" || true)
    [ -n "$real" ] || { echo "$dll imports $imp, not found in the packages" >&2; exit 1; }
    need="$need $real"
  done
done
for dll in $done; do
  cp "$bin/$dll" "$out/"
  echo "$out/$dll"
done

# Las licencias de los paquetes que aportan alguna DLL, y de dónde sale cada una.
mkdir -p "$out/licenses"
{
  echo "DLLs from MSYS2 ($repo), unmodified:"
  while read -r name file version; do
    ships=""
    for dll in $done; do
      [ -f "$work/pkg/$name/mingw64/bin/$dll" ] && ships="$ships $dll"
    done
    [ -n "$ships" ] || continue
    lic=$(ls -d "$work/pkg/$name/mingw64/share/licenses/"* 2>/dev/null | head -1 || true)
    if [ -n "$lic" ]; then
      mkdir -p "$out/licenses/$(basename "$lic")"
      cp -r "$lic/." "$out/licenses/$(basename "$lic")/"
    elif [ "$name" = mingw-w64-x86_64-libxmp ]; then
      # Su paquete no trae la licencia (MIT): la de su código, de esa versión.
      mkdir -p "$out/licenses/libxmp"
      curl -sSfL "https://raw.githubusercontent.com/libxmp/libxmp/libxmp-${version%-*}/docs/COPYING" -o "$out/licenses/libxmp/COPYING"
    else
      echo "no license text for $name" >&2
      exit 1
    fi
    echo "$name $version ($file):$ships"
  done < "$work/pkgs.txt"
  echo "Sources: https://packages.msys2.org/ and each project's upstream."
} > "$out/licenses/windows-dlls-SOURCE.txt"

# fdk-aac (AAC) no está en MSYS2: se compila aquí con zig, con libc++ dentro.
"$(dirname "$0")/fdk-aac.sh" "$out"
