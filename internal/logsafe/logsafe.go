// Package logsafe sanitizes values before structured logging to prevent
// log injection (CR/LF/ANSI escapes) from user-controlled inputs.
package logsafe

import "strings"

// Value neutralizes CR/LF first via strings.ReplaceAll (a CodeQL-recognized
// sanitizer, so taint flow is cut), then replaces remaining control chars.
func Value(s string) string {
	s = strings.ReplaceAll(s, "\r", "?")
	s = strings.ReplaceAll(s, "\n", "?")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}
