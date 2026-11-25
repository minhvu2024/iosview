//go:build !windows

package device

func sysWriteClipboard(string) error { return nil }
