//go:build darwin && !ios

package tray

import (
	"errors"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/juanvillacortac/audiofilus/internal/decode"
	"github.com/juanvillacortac/audiofilus/internal/tags"
)

// The media keys and Now Playing on macOS. MPRemoteCommandCenter hands the
// keyboard's ⏮ ⏯ ⏭, the headphones' buttons and Control Center's to the app
// that last filled MPNowPlayingInfoCenter, so the service fills it on the
// AppKit main thread the tray already runs. No permission is needed.

// commands maps each MPRemoteCommand to its order for base.media.
var commands = map[objc.ID]string{}

// setupNowPlaying registers the remote commands with target (main thread).
func setupNowPlaying(target objc.ID) error {
	if _, err := purego.Dlopen("/System/Library/Frameworks/MediaPlayer.framework/MediaPlayer", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return err
	}
	center := objc.ID(objc.GetClass("MPRemoteCommandCenter")).Send(sel("sharedCommandCenter"))
	if center == 0 {
		return errors.New("no MPRemoteCommandCenter")
	}
	for prop, cmd := range map[string]string{
		"playCommand":                   "resume",
		"pauseCommand":                  "pause",
		"togglePlayPauseCommand":        "toggle",
		"nextTrackCommand":              "next",
		"previousTrackCommand":          "prev",
		"changePlaybackPositionCommand": "seek",
	} {
		c := center.Send(sel(prop))
		c.Send(sel("addTarget:action:"), target, sel("remoteCommand:"))
		commands[c] = cmd
	}
	return nil
}

// remoteCommand does a key or button press off the main thread; 0 is
// MPRemoteCommandHandlerStatusSuccess.
func remoteCommand(_ objc.ID, _ objc.SEL, ev objc.ID) int {
	cmd := commands[ev.Send(sel("command"))]
	var pos float64
	if cmd == "seek" {
		pos = objc.Send[float64](ev, sel("positionTime"))
		// Play, pause and track changes publish through the engine's hooks; a
		// seek doesn't, and lands once the buffer is flushed.
		time.AfterFunc(150*time.Millisecond, cur.Update)
	}
	go cur.media(cmd, pos)
	return 0
}

// nowPlayingUpdate puts what plays in Now Playing (main thread, from Update).
func nowPlayingUpdate(_ objc.ID, _ objc.SEL, _ objc.ID) {
	st := cur.eng.Status()
	state := 3 // MPNowPlayingPlaybackStateStopped
	switch st.State {
	case "playing":
		state = 1
	case "paused":
		state = 2
	}
	var info objc.ID
	if st.Track.Path != "" {
		info = objc.ID(objc.GetClass("NSMutableDictionary")).Send(sel("dictionary"))
		put := func(key string, v objc.ID) { info.Send(sel("setObject:forKey:"), v, nsString(key)) }
		num := func(f float64) objc.ID { return objc.ID(objc.GetClass("NSNumber")).Send(sel("numberWithDouble:"), f) }
		// The keys are MediaPlayer's constants (MPMediaItemPropertyTitle...), by value.
		put("title", nsString(title(st.Track)))
		if st.Track.Artist != "" {
			put("artist", nsString(st.Track.Artist))
		}
		if st.Track.Album != "" {
			put("albumTitle", nsString(st.Track.Album))
		}
		if st.Duration > 0 {
			put("playbackDuration", num(st.Duration))
		}
		rate := 0.0
		if state == 1 {
			rate = 1
		}
		put("MPNowPlayingInfoPropertyElapsedPlaybackTime", num(st.Position))
		put("MPNowPlayingInfoPropertyPlaybackRate", num(rate))
		put("MPNowPlayingInfoPropertyMediaType", objc.ID(objc.GetClass("NSNumber")).Send(sel("numberWithInteger:"), 1)) // audio
		if a := artwork(st.Track.Path); a != 0 {
			put("artwork", a)
		}
	}
	center := objc.ID(objc.GetClass("MPNowPlayingInfoCenter")).Send(sel("defaultCenter"))
	center.Send(sel("setNowPlayingInfo:"), info)
	center.Send(sel("setPlaybackState:"), state)
}

// art is the cover of the last song as an MPMediaItemArtwork and its image,
// and the one before: Now Playing may still hold the previous info, so that
// one is released a song later. Main thread only.
var art struct {
	path             string
	obj, img         objc.ID
	prevObj, prevImg objc.ID
}

// artwork is the cover embedded in path's tags (like MPRIS), or 0.
func artwork(path string) objc.ID {
	if path == art.path {
		return art.obj
	}
	for _, o := range []objc.ID{art.prevObj, art.prevImg} {
		if o != 0 {
			o.Send(sel("release"))
		}
	}
	art.prevObj, art.prevImg = art.obj, art.img
	art.path, art.obj, art.img = path, 0, 0
	b := cover(path)
	if len(b) == 0 {
		return 0
	}
	data := objc.ID(objc.GetClass("NSData")).Send(sel("dataWithBytes:length:"), unsafe.Pointer(&b[0]), len(b))
	runtime.KeepAlive(b)
	img := objc.ID(objc.GetClass("NSImage")).Send(sel("alloc")).Send(sel("initWithData:"), data)
	if img == 0 {
		return 0
	}
	// The handler gets a CGSize: two doubles, in the same registers as two
	// float64 arguments on arm64 and amd64.
	block := objc.NewBlock(func(_ objc.Block, _, _ float64) objc.ID { return img })
	obj := objc.ID(objc.GetClass("MPMediaItemArtwork")).Send(sel("alloc")).
		Send(sel("initWithBoundsSize:requestHandler:"), objc.Send[nsSize](img, sel("size")), block)
	block.Release() // the artwork keeps its own copy
	art.obj, art.img = obj, img
	return obj
}

// cover is the picture in path's tags.
func cover(path string) []byte {
	f, err := os.Open(decode.FileOf(path))
	if err != nil {
		return nil
	}
	defer f.Close()
	m, err := tags.ReadFrom(f)
	if err != nil || m.Picture() == nil {
		return nil
	}
	return m.Picture().Data
}

// goString copies an NSString into Go.
func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	p := objc.Send[*byte](s, sel("UTF8String"))
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

// OpenDocuments sends the files opened from the Finder to open, in the
// interface: macOS delivers them as an "odoc" Apple event, not as arguments.
// The handler goes in at applicationWillFinishLaunching, as Apple asks, to
// catch the event that launched the app, without touching Ebitengine's
// delegate. Call it before ebiten.RunGame. (With only the service running,
// the event goes to it: openURLs.)
func OpenDocuments(open func(urls []string)) {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return
	}
	const aevt, odoc, direct, furl = 0x61657674, 0x6f646f63, 0x2d2d2d2d, 0x6675726c
	cls, err := objc.RegisterClass("AudiofilusOpener", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: sel("willFinishLaunching:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
			objc.ID(objc.GetClass("NSAppleEventManager")).Send(sel("sharedAppleEventManager")).
				Send(sel("setEventHandler:andSelector:forEventClass:andEventID:"), self, sel("openDocuments:withReply:"), uint32(aevt), uint32(odoc))
		}},
		{Cmd: sel("openDocuments:withReply:"), Fn: func(_ objc.ID, _ objc.SEL, ev, _ objc.ID) {
			list := ev.Send(sel("paramDescriptorForKeyword:"), uint32(direct))
			n := objc.Send[int](list, sel("numberOfItems"))
			var urls []string
			for i := 1; i <= n; i++ { // Apple event lists count from 1
				d := list.Send(sel("descriptorAtIndex:"), i).Send(sel("coerceToDescriptorType:"), uint32(furl))
				if s := goString(d.Send(sel("stringValue"))); s != "" {
					urls = append(urls, s)
				}
			}
			if len(urls) > 0 {
				go open(urls)
			}
		}},
	})
	if err != nil {
		return
	}
	objc.ID(objc.GetClass("NSNotificationCenter")).Send(sel("defaultCenter")).
		Send(sel("addObserver:selector:name:object:"), objc.ID(cls).Send(sel("new")), sel("willFinishLaunching:"),
			nsString("NSApplicationWillFinishLaunchingNotification"), objc.ID(0))
}

// openURLs plays the files opened from the Finder while only the service
// runs (it's the app macOS sees then): as "audiofilus <files>".
func openURLs(_ objc.ID, _ objc.SEL, _ objc.ID, urls objc.ID) {
	var files []string
	for i := 0; i < objc.Send[int](urls, sel("count")); i++ {
		if p := goString(urls.Send(sel("objectAtIndex:"), i).Send(sel("path"))); p != "" {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		return
	}
	go func() {
		// Opening a file with the app closed starts the interface, which starts
		// this service, and macOS may hand the event here instead: wait for
		// that interface rather than open a second one.
		// shortcut: a service started from the CLI waits up to 10 s too, fine for a first open.
		for time.Since(started) < 10*time.Second && !cur.opt.UIOpen() {
			time.Sleep(250 * time.Millisecond)
		}
		cur.open(files...)
	}()
}
