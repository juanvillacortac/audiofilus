#!/bin/sh
# Builds Audiofilus-macos-<arch>.dmg on a Mac: Audiofilus.app with the C
# decoders from Homebrew in Contents/Frameworks (rewired with dylibbundler so
# they don't need Homebrew), ad-hoc signed, next to an Applications link.
#   tools/macos/dmg.sh <out dir>
# It builds for the Mac's own architecture, since the libraries are Homebrew's.
# Needs: brew install mpg123 opusfile libxmp game-music-emu dylibbundler
# fdk-aac (M4A, AAC), libavshim (WMA) and libvgmshim (every VGM chip) aren't in
# Homebrew: they go in when they were built first, for this Mac, with
#   tools/fdk-aac/build.sh build/fdk-aac macos
#   tools/ffmpeg/build.sh build/ffmpeg macos
#   tools/libvgm/build.sh build/libvgm macos
set -eu
cd "$(dirname "$0")/../.."
out=$1
arch=$(go env GOARCH)
version=${VERSION:-$(git describe --tags --exact-match 2>/dev/null || echo dev)}
pkg=github.com/juanvillacortac/audiofilus/internal
work=$(mktemp -d)
app=$work/dmg/Audiofilus.app
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$app/Contents/Frameworks" "$out"

CGO_ENABLED=0 go build -trimpath \
  -ldflags="-s -w -X $pkg/version.Version=$version -X $pkg/scrobble.DefaultLastFMKey=${LASTFM_KEY:-} -X $pkg/scrobble.DefaultLastFMSecret=${LASTFM_SECRET:-}" \
  -o "$app/Contents/MacOS/audiofilus" ./cmd/audiofilus

# The decoders, under the names internal/decode looks for, and whatever they
# link to (libogg, libopus...), with paths relative to the app.
prefix=$(brew --prefix)
for lib in libmpg123.0 libopusfile.0 libxmp.4 libgme.0; do
  if [ ! -f "$prefix/lib/$lib.dylib" ]; then
    echo "$lib missing in Homebrew: the app goes without it" >&2
    continue
  fi
  cp "$prefix/lib/$lib.dylib" "$app/Contents/Frameworks/"
  chmod u+w "$app/Contents/Frameworks/$lib.dylib"
  install_name_tool -id "@rpath/$lib.dylib" "$app/Contents/Frameworks/$lib.dylib"
  dylibbundler -of -b -cd -x "$app/Contents/Frameworks/$lib.dylib" \
    -d "$app/Contents/Frameworks" -p @loader_path/
done

# The ones built by tools/: they only link to the system's libraries, so they
# need no rewiring. Their licenses go in Resources/licenses, as in the Windows
# zip (libvgm's GPL asks for it).
lic=$app/Contents/Resources/licenses
for d in fdk-aac:libfdk-aac.2 ffmpeg:libavshim libvgm:libvgmshim; do
  dir=build/${d%%:*} lib=${d#*:}
  if [ ! -f "$dir/macos/$lib.dylib" ]; then
    echo "$dir/macos/$lib.dylib missing: the app goes without it (tools/${d%%:*}/build.sh $dir macos)" >&2
    continue
  fi
  cp "$dir/macos/$lib.dylib" "$app/Contents/Frameworks/"
  mkdir -p "$lic"
  cp "$dir"/*-LICENSE.txt "$dir"/*-SOURCE.txt "$lic/"
done
[ -f build/libvgm/macos/libvgmshim.dylib ] && cp tools/libvgm/GPL-2.0.txt "$lic/"

# The icon, from the iOS one (1024 px).
set=$work/icon.iconset
mkdir -p "$set"
src=ios/Audiofilus/Assets.xcassets/AppIcon.appiconset/icon-1024.png
for s in 16 32 128 256 512; do
  sips -z $s $s "$src" --out "$set/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) "$src" --out "$set/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$set" -o "$app/Contents/Resources/icon.icns"

short=${version#v}
[ "$short" = dev ] && short=0.0.0
cat >"$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Audiofilus</string>
  <key>CFBundleDisplayName</key><string>Audiofilus</string>
  <key>CFBundleIdentifier</key><string>io.github.juanvillacortac.audiofilus</string>
  <key>CFBundleExecutable</key><string>audiofilus</string>
  <key>CFBundleIconFile</key><string>icon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$short</string>
  <key>CFBundleVersion</key><string>$short</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.music</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>CFBundleDocumentTypes</key>
  <array>
    <dict>
      <key>CFBundleTypeName</key><string>Audio</string>
      <key>CFBundleTypeRole</key><string>Viewer</string>
      <key>LSHandlerRank</key><string>Alternate</string>
      <key>LSItemContentTypes</key>
      <array>
        <string>public.audio</string>
        <string>public.folder</string>
        <string>public.m3u-playlist</string>
      </array>
    </dict>
  </array>
</dict>
</plist>
EOF
plutil -lint "$app/Contents/Info.plist" >/dev/null

# Ad-hoc: no Apple account, but Apple Silicon won't run unsigned code.
codesign --force --deep --sign - "$app"

ln -s /Applications "$work/dmg/Applications"
dmg=$out/Audiofilus-macos-$arch.dmg
hdiutil create -volname Audiofilus -srcfolder "$work/dmg" -ov -format UDZO "$dmg" >/dev/null
rm -rf "$work"
echo "$dmg"
