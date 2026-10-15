// Package hotkey installs a CoreGraphics event tap that understands
// right-Command combos. AeroSpace's own bindings cannot distinguish the
// left from the right Command key, so we do it ourselves: on every
// flags-changed event we track which Command key is down, and we consume
// right-Command+X key-down events that a handler claims.
package hotkey

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include "tap.h"
*/
import "C"

import (
	"fmt"
	"runtime"
)

// Handler is invoked for a key-down while right-Command is held. name is the
// canonical key name ("d", "1", ...). Return true to consume the event.
type Handler func(name string, isRepeat bool) bool

var handler Handler

// SetHandler registers the single global handler. Call before Install.
func SetHandler(h Handler) { handler = h }

// AccessibilityTrusted reports whether the process is trusted for the
// Accessibility (event tap) permission. If prompt is true, macOS is asked to
// show its permission dialog when the process is not yet trusted.
func AccessibilityTrusted(prompt bool) bool {
	return C.ac_accessibility_trusted(C.int(boolToInt(prompt))) != 0
}

// Install creates the event tap. It fails if Accessibility permission is
// missing.
func Install() error {
	if rc := C.ac_tap_create(); rc != 0 {
		if !AccessibilityTrusted(false) {
			return fmt.Errorf("accessibility permission is not granted " +
				"(System Settings > Privacy & Security > Accessibility: enable this binary)")
		}
		return fmt.Errorf("CGEventTapCreate failed (code %d)", int(rc))
	}
	return nil
}

// Run blocks, running the event tap's run loop on a dedicated OS thread.
func Run() {
	runtime.LockOSThread()
	C.ac_tap_run()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
