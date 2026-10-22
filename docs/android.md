# Android

audiofilus runs on Android phones, tablets and Android handhelds (Retroid, AYN, Anbernic's Android models) as an app built from the same Go code as the iOS one. Work in progress: tested on the emulator (Android 16, x86_64), not yet on a real device, and not published yet.

## How it's built

```sh
mise install                       # zig, the Android SDK (ANDROID_HOME) and JDK 21 (mise.toml)
mise exec -- sdkmanager "ndk;28.2.13676358" "platforms;android-36"   # once, inside the SDK
mise exec -- android/libs.sh       # the C decoders for the APK (android/app/src/main/jniLibs)
mise exec -- android/build.sh      # debug APK; "release" for the release one
```

- `android/build.sh` builds `ebitenmobile` from the same commit of the Ebitengine fork that `go.mod` uses (like the iOS workflow), turns `mobile/` into `android/app/libs/mobile.aar` (`ebitenmobile bind -target android/arm64,android/amd64 -javapkg dev.audiofilus`) and runs Gradle. The app version comes from `VERSION` (`0.15.0`), and `versionCode` from it (`1500`).
- `android/libs.sh` compiles **libfdk-aac** (AAC: M4A, M4B, AAC radio) with the NDK and puts it, with the NDK's `libc++_shared.so`, in `jniLibs`. audiofilus loads it with purego by name (`libfdk-aac.so`), which works because the APK extracts its native libraries (`useLegacyPackaging`). fdk-aac's Android build includes the platform's private `log/log.h`, so the script gives it a stub. MP3, FLAC, Ogg Vorbis, WAV, AIFF and ALAC decode in Go; Opus, tracker modules and console music aren't there yet.
- `android/app` is a plain Gradle project in Kotlin, with framework APIs only (no androidx).
- Release signing: `AUDIOFILUS_KEYSTORE`, `AUDIOFILUS_KEYSTORE_PASSWORD`, `AUDIOFILUS_KEY_ALIAS` and `AUDIOFILUS_KEY_PASSWORD` in the environment; without them the release APK is signed with the debug key. The release key (alias `audiofilus`, certificate SHA-256 `16:6B:68:99:…:8B:85`) lives with the author, outside the repo, and in the repository secrets `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD`. Losing it means updates can't be installed over the existing app.
- `.github/workflows/android.yml` builds the signed `audiofilus.apk` (by hand from the Actions tab, or from `release.yml` on every release, which attaches it).
- The launcher icon is adaptive pixel art made by `tools/android/icon.sh` (background, foreground and monochrome layers, drawn at 72 px for 108 dp).

## Differences from the handhelds (shared with iOS)

The same as [iOS](ios.md): one process (the audio service in a goroutine, `ui.StartService`), one audio context shared with the interface, the system keyboard, the safe area, and lock screen controls fed by `daemon.NowPlayingHook`. What is Android's own (`android/app/src/main/kotlin/...`):

- **Background music.** `PlaybackService` is a `mediaPlayback` foreground service with the playback notification, started when something plays and taken out of the foreground on pause (the notification stays). Without it Android would kill the process, and the Go service with it, after leaving the app.
- **Media session.** `Media` keeps an `android.media.session.MediaSession`: the notification's controls, the lock screen, headphone and Bluetooth buttons call `Mobile.play`, `pause`, `next`, `previous` and `seekTo`.
- **Where the music is.** Google Play doesn't allow "all files access" for a music player, so the app asks for `READ_MEDIA_AUDIO` (and `READ_MEDIA_IMAGES`, for loose cover images; `READ_EXTERNAL_STORAGE` before Android 13). With it, audio files are readable by their normal paths, which is what the Go code needs. The first time it's granted, the music folder is the phone's `Music`. Settings → Folder opens the system folder picker, and the chosen tree (`primary:Music/Rock`, `1A2B-3C4D:Music`) is turned into its path (`/storage/emulated/0/Music/Rock`, `/storage/1A2B-3C4D/Music`). Files that aren't media (`.lrc`, `.m3u`) are only visible in the app's own folder.
- **The app's folder** (`audiofilus.json`, `data/`, the log `data/audiofilus.log`) is `Android/data/io.github.juanvillacortac.audiofilus/files`, reachable from a PC over USB. The socket goes in the cache folder (short path).
- **Back gesture.** Ebitengine only reads Android's Back key as a gamepad's Back button, so the activity catches the system Back (`OnBackInvokedCallback` on Android 13+, `onBackPressed` before) and calls `Mobile.back`: it counts as B in the interface (`App.BackGesture`), except in Now Playing with no menu open, where the app goes to the background (`moveTaskToBack`) and the music keeps playing.
- **Gamepads.** With a gamepad connected (at start or plugged in later) the interface is the gamepad one, as on a handheld; touching the screen switches it to the touch one (`Input.padsSeen`, `internal/ui/input.go`). This applies to iOS too.
- **Emulator testing.** `adb shell input tap` presses and releases within one frame, which Ebitengine never sees; use a held tap: `adb shell input swipe X Y X Y 150`.
