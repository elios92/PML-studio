//go:build windows

package main

// Keep map diagnostics in the same asynchronous log as the rest of the editor.
func mapLogf(format string, args ...any) {
	diagLogf(format, args...)
}
