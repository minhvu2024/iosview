//go:build !windows

package main

// runWindow: native window only on Windows; elsewhere use a browser.
func runWindow(o winOpts) bool { return false }
