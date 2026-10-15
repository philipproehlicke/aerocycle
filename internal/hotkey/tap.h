#ifndef AEROCYCLE_TAP_H
#define AEROCYCLE_TAP_H

#include <stdbool.h>

// Returns 1 if Accessibility (event-tap) permission is currently granted,
// otherwise 0. The prompt variant asks macOS to show the permission dialog.
int ac_accessibility_trusted(int prompt);

// Creates the CFMachPort event tap. Returns 0 on success, non-zero on failure
// (failure almost always means Accessibility permission is missing).
int ac_tap_create(void);

// Runs the tap's run loop on the calling thread. Blocks until the loop stops.
void ac_tap_run(void);

// Enables/disables the tap.
void ac_tap_set_enabled(int enabled);

#endif // AEROCYCLE_TAP_H
