#include "tap.h"

#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdint.h>

// Implemented in Go (see export.go).
extern int acHandleKey(int keycode, int rightCmd, int leftCmd, int isRepeat);

// macOS virtual keycodes for the modifier keys.
#define KC_LEFT_CMD  55
#define KC_RIGHT_CMD 54

static CFMachPortRef gTap = NULL;
static CFRunLoopSourceRef gSrc = NULL;

// We track the two Command keys separately. The device-independent event flags
// cannot tell left from right, so we toggle on each flags-changed event that
// names a specific side. Toggling (rather than reading the flag) is correct even
// when both Command keys are held at once.
static bool gRightCmd = false;
static bool gLeftCmd = false;

int ac_accessibility_trusted(int prompt) {
    const void *keys[] = { kAXTrustedCheckOptionPrompt };
    const void *vals[] = { prompt ? kCFBooleanTrue : kCFBooleanFalse };
    CFDictionaryRef opts = CFDictionaryCreate(
        kCFAllocatorDefault, keys, vals, 1,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    Boolean trusted = AXIsProcessTrustedWithOptions(opts);
    CFRelease(opts);
    return trusted ? 1 : 0;
}

static CGEventRef acTapCallback(CGEventTapProxy proxy, CGEventType type,
                                CGEventRef event, void *refcon) {
    if (type == kCGEventTapDisabledByTimeout ||
        type == kCGEventTapDisabledByUserInput) {
        // The system disables taps that are too slow; just turn it back on.
        if (gTap) {
            CGEventTapEnable(gTap, true);
        }
        return event;
    }

    if (type == kCGEventFlagsChanged) {
        int64_t kc = CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
        if (kc == KC_RIGHT_CMD) {
            gRightCmd = !gRightCmd;
        } else if (kc == KC_LEFT_CMD) {
            gLeftCmd = !gLeftCmd;
        }
        return event;
    }

    if (type == kCGEventKeyDown) {
        if (gRightCmd) {
            int64_t kc = CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
            int repeat = (int)CGEventGetIntegerValueField(event, kCGKeyboardEventAutorepeat);
            if (acHandleKey((int)kc, 1, gLeftCmd ? 1 : 0, repeat)) {
                return NULL; // consume: the focused app must not see this combo
            }
        }
    }

    return event;
}

int ac_tap_create(void) {
    CGEventMask mask =
        CGEventMaskBit(kCGEventFlagsChanged) | CGEventMaskBit(kCGEventKeyDown);

    gTap = CGEventTapCreate(kCGHIDEventTap, kCGHeadInsertEventTap,
                            kCGEventTapOptionDefault, mask, acTapCallback, NULL);
    if (!gTap) {
        return 1;
    }
    gSrc = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, gTap, 0);
    if (!gSrc) {
        return 2;
    }
    return 0;
}

void ac_tap_run(void) {
    if (!gTap || !gSrc) {
        return;
    }
    CFRunLoopAddSource(CFRunLoopGetCurrent(), gSrc, kCFRunLoopCommonModes);
    CGEventTapEnable(gTap, true);
    CFRunLoopRun();
}

void ac_tap_set_enabled(int enabled) {
    if (gTap) {
        CGEventTapEnable(gTap, enabled ? true : false);
    }
}
