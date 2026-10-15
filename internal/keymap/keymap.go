// Package keymap maps macOS virtual keycodes to canonical key names.
package keymap

import "sort"

// ANSI layout. AeroSpace's default key mapping is qwerty, so we key off the
// physical US layout, which is what the event tap reports.
var byCode = map[int]string{
	0: "a", 1: "s", 2: "d", 3: "f", 4: "h", 5: "g", 6: "z", 7: "x",
	8: "c", 9: "v", 11: "b", 12: "q", 13: "w", 14: "e", 15: "r",
	16: "y", 17: "t", 31: "o", 32: "u", 34: "i", 35: "p", 37: "l",
	38: "j", 40: "k", 45: "n", 46: "m",
	18: "1", 19: "2", 20: "3", 21: "4", 22: "6", 23: "5",
	25: "9", 26: "7", 28: "8", 29: "0",
}

// Name returns the canonical key name for a virtual keycode.
func Name(code int) (string, bool) {
	n, ok := byCode[code]
	return n, ok
}

// Valid reports whether name is a bindable key.
func Valid(name string) bool {
	for _, n := range byCode {
		if n == name {
			return true
		}
	}
	return false
}

// Names returns every bindable key name, sorted.
func Names() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(byCode))
	for _, n := range byCode {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
