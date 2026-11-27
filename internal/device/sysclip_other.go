//go:build !windows

package device

import "errors"

func sysWriteClipboard(string) error { return nil }

func WriteClipboardDIB([]byte) error { return errors.New("clipboard ảnh cần Windows") }
