# macOS: the interface stops while a window menu is open

On macOS, hovering over the window's green button shows the tiling menu (Full Screen, Move & Resize, Fill & Arrange). Before this fix, every animation in the interface stopped while that menu was open: the visualizers, the scrolling titles, the progress bar. The music kept playing, because the service is a separate process, and everything moved again once the menu closed. The same happened with any other menu the app opens from its own window, such as the menu bar menus.

The fix is in the Ebitengine fork, not in Audiofilus. This document explains the cause, how other projects handle it, what the fix does, and what to check so it doesn't come back.

## Cause

Ebitengine's game loop runs on its own goroutine, but on every frame it asks the main thread to poll the system's events (`glfw.PollEvents`, called from `glfwBackend.update` in `internal/ui/ui_glfw.go`). The frame only goes on once that call returns.

On macOS, polling means `nextEventMatchingMask:` followed by `sendEvent:` (`platformPollEvents` in `internal/glfw/cocoa_window_darwin.go`). AppKit opens the green button's menu from inside `sendEvent:`, and a menu runs its own tracking loop: the main run loop spins in `NSEventTrackingRunLoopMode` until the menu closes. So `PollEvents` doesn't return, and the game loop waits for it without running any `Update` or `Draw`.

A live window resize works the same way. Ebitengine already handles that case: the framebuffer-size callback fires inside the resize loop and runs a frame from there (`forceUpdateFrameDuringPollEvents`, added for [ebiten#2615](https://github.com/hajimehoshi/ebiten/issues/2615)). A menu never changes the window's size, so that callback never fires and nothing gets drawn.

Calling the window delegate's `windowDidResize:` from Audiofilus doesn't work either. The fork's GLFW port only calls the size callback when the size actually changed (`updateWindowSize`), so the fix has to go into the fork itself.

## How other projects handle it

- **SDL** ([SDL#16703](https://github.com/libsdl-org/SDL/issues/16703), [PR #16722](https://github.com/libsdl-org/SDL/pull/16722)) uses `NSTimer`s that are added only to the run loop mode AppKit uses for its own loop. SDL has one timer for live resize, in the common modes and only while resizing, and since 3.4.22 a second one in `_NSMoveTimerRunLoopMode` for zoom animations. The comments explain why this costs nothing in normal use: a timer added only to a special mode never fires while the run loop is in its default mode. They also note that `NSEventTrackingRunLoopMode` is the mode menus track in, and that Flutter uses the same trick.
- **Ebitengine upstream** fixed only the resize case (commit `c9ba641`, July 2026). In #2615 the maintainer describes the loop stopping during system loops as usual behavior for GUI apps. There is no issue or fix for menus.
- **GLFW** has no fix. [glfw#408](https://github.com/glfw/glfw/issues/408) (event processing blocks during resizing) was closed as support, and [glfw#1611](https://github.com/glfw/glfw/issues/1611) (modal loop enter/exit events) has been open since 2020. Unity and other GLFW-based programs freeze the same way.

## The fix

The fix is one file in the fork, `internal/ui/ui_darwin.go`, about 70 lines:

1. When the window is created (`initializeWindowAfterCreation`), a repeating 60 Hz `NSTimer` is added to the main run loop **only in `NSEventTrackingRunLoopMode`**. The rest of the time the run loop is in its default mode, so the timer never fires and costs no CPU when no menu is open.
2. The timer calls a new method, `trackingLoopTick:`, on Ebitengine's existing window delegate class (`EbitengineWindowDelegate`). That method calls `forceUpdateFrameInTrackingLoop`.
3. `forceUpdateFrameInTrackingLoop` gets the current sizes the same way the game loop does (`layoutSizes`, `currentMonitor`). It then calls the existing `forceUpdateFrameDuringPollEvents`, so a menu goes through the same code as a live resize. That code already runs `Update` and `Draw` on another goroutine, keeps the main thread processing its calls, lets the clock decide how many ticks to run (so game time doesn't speed up), and refuses to run recursively or in single-thread mode.
4. It does nothing in these cases:
   - The main thread isn't polling events for the game loop, or a forced frame is already running. Then the tracking loop is inside some other main-thread operation, and querying the window there is unsafe.
   - The window is hidden or occluded. As in the game loop (`shouldPresentFrame`, ebiten#3405), presenting to such a window waits for a throttled display link, and that would make the menu lag.

The change:

```go
// In the method list of EbitengineWindowDelegate (initializePlatform):
{
	// The timer added in initializeWindowAfterCreation calls this.
	Cmd: sel_trackingLoopTick,
	Fn: func(id objc.ID, cmd objc.SEL, timer objc.ID) {
		if b, ok := u.runningBackend().(*glfwBackend); ok {
			b.forceUpdateFrameInTrackingLoop()
		}
	},
},

// At the end of initializeWindowAfterCreation, after setting the delegate:
// AppKit shows menus, like the one for hovering over the green button, in a tracking loop inside
// event polling, and the game loop cannot proceed until the menu closes. A timer only in the
// tracking loop's mode stays idle otherwise and renders frames meanwhile.
pool := cocoa.NSAutoreleasePool_new()
defer pool.Release()
timer := objc.ID(class_NSTimer).Send(sel_timerWithTimeInterval_target_selector_userInfo_repeats, 1.0/60, delegate, sel_trackingLoopTick, 0, true)
objc.ID(class_NSRunLoop).Send(sel_mainRunLoop).Send(sel_addTimer_forMode, timer, nsEventTrackingRunLoopMode.ID)

var (
	sel_addTimer_forMode                                       = objc.RegisterName("addTimer:forMode:")
	sel_mainRunLoop                                            = objc.RegisterName("mainRunLoop")
	sel_timerWithTimeInterval_target_selector_userInfo_repeats = objc.RegisterName("timerWithTimeInterval:target:selector:userInfo:repeats:")
	sel_trackingLoopTick                                       = objc.RegisterName("trackingLoopTick:")
)

// forceUpdateFrameInTrackingLoop runs one frame while an AppKit tracking loop blocks event polling.
//
// forceUpdateFrameInTrackingLoop must be called from the main thread.
func (u *glfwBackend) forceUpdateFrameInTrackingLoop() {
	// The tracking loop might run inside another main-thread operation, where the window must not be queried.
	if !u.pollingEvents || u.forcingFrame {
		return
	}

	// Like the game loop, skip a window that is not on the screen: presenting there waits for a
	// throttled display link (#3405), which would make the menu laggy.
	visible, err := u.isWindowVisible()
	if err != nil {
		u.setError(err)
		return
	}
	occluded, err := u.isWindowOccluded()
	if err != nil {
		u.setError(err)
		return
	}
	if !visible || occluded {
		return
	}

	outsideWidth, outsideHeight, screenWidth, screenHeight, err := u.layoutSizes()
	if err != nil {
		u.setError(err)
		return
	}
	m, err := u.currentMonitor()
	if err != nil {
		u.setError(err)
		return
	}
	if m == nil {
		return
	}
	u.forceUpdateFrameDuringPollEvents(outsideWidth, outsideHeight, screenWidth, screenHeight, m.DeviceScaleFactor())
}
```

The patch also adds these to the existing `var` blocks:

```go
class_NSRunLoop = objc.GetClass("NSRunLoop")
class_NSTimer   = objc.GetClass("NSTimer")

nsEventTrackingRunLoopMode = cocoa.NSString_alloc().InitWithUTF8String("NSEventTrackingRunLoopMode")
```

The file is built only for macOS (`//go:build !ios`), so iOS, Windows and Linux are not affected.

### What it doesn't cover, and why

- **Zoom animations** (`_NSMoveTimerRunLoopMode`, which SDL needed) already draw: the window size changes on every step, so the resize callback runs frames. Measured with and without the fix, both gave about 140 `Update`s in 2 s with no gap over 25 ms.
- **Modal dialogs** (`runModal`, `NSModalPanelRunLoopMode`) would still stop the loop. Audiofilus doesn't open any.

## How it was verified

A small test program opened an `NSMenu` on the main thread while Ebitengine was polling events, which is what the green button does. It called `popUpMenuPositioningItem:atLocation:inView:` through `performSelectorOnMainThread:`, closed the menu after 2 s with `cancelTracking` from a timer in the menu's mode, and counted frames:

| | `Update` / `Draw` in the 2 s the menu is open |
|---|---|
| Without the fix | 0 / 0 |
| With the fix | 119 / 119 (60 TPS) |

A 2 ms timer in the same mode measured how long the main thread was busy. It never went more than 4 ms without firing, so the menu stays responsive while frames are drawn. The real case was then checked by hand on macOS 26: with the pointer on the green button and its menu open, the interface keeps animating.

The fork's tests (`go test ./internal/ui ./internal/glfw`) pass, and `go vet` passes for darwin/arm64, darwin/amd64 and windows.

## Keeping the fix

Audiofilus doesn't contain Ebitengine. The `replace` in `go.mod` decides which commit of the fork every build uses, locally, in CI and in releases. The fix is only present when that commit contains it.

**Check the commit `go.mod` points to:**

```sh
grep -c trackingLoopTick "$(go list -m -f '{{.Dir}}' github.com/hajimehoshi/ebiten/v2)/internal/ui/ui_darwin.go"
```

Anything above 0 means the fix is in. 0, or a "no such file" error, means the build has the old behavior.

**When the fix has to go into a fork commit** (the first time, or after the fork was rebuilt from upstream without it):

1. Apply the change above on the fork's branch (`sdl2` in `juanvillacortac/ebiten`, or a branch in your own fork of it), commit and push.
2. Point Audiofilus at that commit:
   ```sh
   go mod edit -replace=github.com/hajimehoshi/ebiten/v2=github.com/<owner>/ebiten/v2@<branch-or-commit>
   go mod tidy   # turns the branch into a pseudo-version and updates go.sum
   ```
   The fork's `go.mod` keeps `module github.com/hajimehoshi/ebiten/v2`, which is what `replace` needs.
3. Run the usual checks (`gofmt -l .`, `go vet ./...`, `go test ./...`, the cgo-free arm64 build, `GOOS=windows go vet ./...`) and commit `go.mod` and `go.sum` together.

**Whenever the `replace` is moved to another fork commit** (dependency updates, a rebase of the fork onto newer Ebitengine), run the check above. No test in Audiofilus catches this regression, because it needs a real window and a real menu. After building, hover over the green button for a couple of seconds and make sure the interface keeps moving.

The fix belongs in the fork's main branch (`sdl2`), and ideally in upstream Ebitengine, since it applies to any Ebitengine game on macOS. Once it's there, updating the fork can't drop it.
