# iOS

audiofilus runs on iPhone and iPad as an unsigned app (`audiofilus-ios-unsigned.ipa`), built on GitHub Actions like Trancazo's: install it with [Sideloadly](https://sideloadly.io) or [AltStore](https://altstore.io), which sign it with your own Apple ID.

## How it's built

- `mobile/` is audiofilus as a Go package for [ebitenmobile](https://ebitengine.org/en/documents/mobile.html): `ebitenmobile bind -target ios` turns it into `ios/Mobile.xcframework`. The ebitenmobile binary is built from the same commit of the Ebitengine fork that `go.mod` uses (its `replace`).
- `ios/project.yml` is the Xcode project for XcodeGen (no `.xcodeproj` in the repo); `ios/Audiofilus/AppDelegate.swift` is the app.
- `.github/workflows/ios.yml` builds it on macOS (by hand from the Actions tab, or from `release.yml` on every release, which attaches the `.ipa`). The repo is public, so macOS minutes are free.

## Differences from the desktop and the handhelds

- **One process.** iOS doesn't let an app start processes: the audio service runs in a goroutine of the app (`ui.StartService`), and the interface talks to it over its socket as usual. The socket path is relative to the app's temporary folder (`AUDIOFILUS_SOCKET`), because Unix sockets can't take paths over 104 bytes and an app's container is close to that.
- **Background music.** `Info.plist` declares the `audio` background mode and the app sets the audio session to playback (`AppDelegate`): the music keeps going with the screen locked, the silent switch on or another app in front. The interface stops drawing while the app is in the background; the service doesn't.
- **Where the music is.** Settings and the library live in the app's Documents folder (`AUDIOFILUS_HOME`), which shows up in the Files app as *On My iPhone → Audiofilus*: anything copied into its `Music` folder plays (it's the default music folder). Settings → Folder opens the system's folder picker (`ui.PickFolder`, `MobileHostProtocol` in Swift) for iCloud Drive, *On My iPhone* or another app's folder; the app keeps a security-scoped bookmark so it can read that folder again on the next launch.
- **Quit** stops the music instead of closing (an iOS app doesn't close itself); "Go to background" isn't needed, leaving the app already does it.
- The view sits inside the safe area (no notch or home indicator over it), and the interface follows portrait and landscape.
- Formats: those decoded in pure Go (MP3, FLAC, Ogg Vorbis, WAV, AIFF, ALAC, MP4/M4A with ALAC). The C libraries (fdk-aac, opusfile, libxmp) aren't there; AAC through Apple's AudioToolbox is pending.

## Pending

- Lock screen and Control Center controls (`MPRemoteCommandCenter`, `MPNowPlayingInfoCenter`).
- AAC through AudioToolbox.
- Android (`ebitenmobile bind -target android`, the same `mobile/` package).
