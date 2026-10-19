//go:build !windows

package main

// runWindow: native window only on Windows; elsewhere use a browser.
func runWindow(url, version string) bool { return false }
