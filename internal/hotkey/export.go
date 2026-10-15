package hotkey

import "C" // required for cgo export (no C preamble here)

import "aerocycle/internal/keymap"

//export acHandleKey
func acHandleKey(keycode C.int, rightCmd C.int, leftCmd C.int, isRepeat C.int) C.int {
	if handler == nil {
		return 0
	}
	name, ok := keymap.Name(int(keycode))
	if !ok {
		return 0
	}
	if handler(name, isRepeat != 0) {
		return 1
	}
	return 0
}
