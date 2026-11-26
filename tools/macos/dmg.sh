#!/bin/sh
# Builds Audiofilus-macos.dmg on a Mac (either kind): a universal
# Audiofilus.app, for Apple Silicon and Intel in one, with the C decoders in
# Contents/Frameworks and their licenses in Contents/Resources/licenses,
# ad-hoc signed, next to an Applications link. Also Audiofilus-macos.zip, the
# same app for Settings → Update (internal/update), which unpacks it in Go.
#   tools/macos/libs.sh build/macos   # the decoders, with zig, once
#   tools/macos/dmg.sh <out dir>
# Every Mach-O file has both parts (lipo), so each Mac runs its own, without
# Rosetta on Apple Silicon. The decoders only link to the system's libraries,
# so no Homebrew is needed.
set -eu
cd "$(dirname "$0")/../.."
out=$1
libs=build/macos
if [ ! -d "$libs/aarch64" ] || [ ! -d "$libs/x86_64" ]; then
  echo "$libs missing: build the decoders first with tools/macos/libs.sh $libs" >&2
  exit 1
fi
version=${VERSION:-$(git describe --tags --exact-match 2>/dev/null || echo dev)}
pkg=github.com/juanvillacortac/audiofilus/internal
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$out"

# The icon, from the iOS one (1024 px).
set=$work/icon.iconset
mkdir -p "$set"
src=ios/Audiofilus/Assets.xcassets/AppIcon.appiconset/icon-1024.png
for s in 16 32 128 256 512; do
  sips -z $s $s "$src" --out "$set/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) "$src" --out "$set/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$set" -o "$work/icon.icns"

short=${version#v}
[ "$short" = dev ] && short=0.0.0

app=$work/dmg/Audiofilus.app
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources/licenses" "$app/Contents/Frameworks"
for arch in arm64 amd64; do
  CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath \
    -ldflags="-s -w -X $pkg/version.Version=$version -X $pkg/scrobble.DefaultLastFMKey=${LASTFM_KEY:-} -X $pkg/scrobble.DefaultLastFMSecret=${LASTFM_SECRET:-}" \
    -o "$work/audiofilus-$arch" ./cmd/audiofilus
done
lipo -create "$work/audiofilus-arm64" "$work/audiofilus-amd64" -output "$app/Contents/MacOS/audiofilus"
for f in "$libs/aarch64"/*.dylib; do
  lib=$(basename "$f")
  lipo -create "$f" "$libs/x86_64/$lib" -output "$app/Contents/Frameworks/$lib"
done
cp "$libs/licenses"/* "$app/Contents/Resources/licenses/"
cp "$work/icon.icns" "$app/Contents/Resources/icon.icns"

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

# Without resource forks or extended attributes: in the zip they'd be ._ files,
# which the updater would write into the app, breaking its signature.
ditto -c -k --norsrc --noextattr --keepParent "$app" "$out/Audiofilus-macos.zip"
echo "$out/Audiofilus-macos.zip"

ln -s /Applications "$work/dmg/Applications"
dmg=$out/Audiofilus-macos.dmg
hdiutil create -volname Audiofilus -srcfolder "$work/dmg" -ov -format UDZO "$dmg" >/dev/null
echo "$dmg"
