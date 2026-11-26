#!/bin/sh
# Builds Audiofilus-macos-arm64.dmg and Audiofilus-macos-amd64.dmg on a Mac
# (either kind): Audiofilus.app with the C decoders in Contents/Frameworks and
# their licenses in Contents/Resources/licenses, ad-hoc signed, next to an
# Applications link.
#   tools/macos/libs.sh build/macos   # the decoders, with zig, once
#   tools/macos/dmg.sh <out dir>
# The decoders only link to the system's libraries, so both apps come out of
# the same Mac, and no Homebrew is needed.
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

for arch in arm64 amd64; do
  # The decoders' folders use the other names for the same thing.
  larch=$([ "$arch" = arm64 ] && echo aarch64 || echo x86_64)
  app=$work/$arch/Audiofilus.app
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources/licenses" "$app/Contents/Frameworks"

  CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath \
    -ldflags="-s -w -X $pkg/version.Version=$version -X $pkg/scrobble.DefaultLastFMKey=${LASTFM_KEY:-} -X $pkg/scrobble.DefaultLastFMSecret=${LASTFM_SECRET:-}" \
    -o "$app/Contents/MacOS/audiofilus" ./cmd/audiofilus
  cp "$libs/$larch"/*.dylib "$app/Contents/Frameworks/"
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

  ln -s /Applications "$work/$arch/Applications"
  dmg=$out/Audiofilus-macos-$arch.dmg
  hdiutil create -volname Audiofilus -srcfolder "$work/$arch" -ov -format UDZO "$dmg" >/dev/null
  echo "$dmg"
done
