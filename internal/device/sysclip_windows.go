//go:build windows

package device

import (
	"errors"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	createClipWindow  = user32.NewProc("CreateWindowExW")
	destroyClipWindow = user32.NewProc("DestroyWindow")
	openClipboard     = user32.NewProc("OpenClipboard")
	closeClipboard    = user32.NewProc("CloseClipboard")
	emptyClipboard    = user32.NewProc("EmptyClipboard")
	setClipboardData  = user32.NewProc("SetClipboardData")
	globalAlloc       = kernel32.NewProc("GlobalAlloc")
	globalFree        = kernel32.NewProc("GlobalFree")
	globalLock        = kernel32.NewProc("GlobalLock")
	globalUnlock      = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// sysWriteClipboard writes text directly to the Windows system clipboard so
// copies succeed even when the UI window loses focus or lacks a user gesture.
func sysWriteClipboard(text string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	u16 := utf16.Encode([]rune(text))
	u16 = append(u16, 0)
	size := uintptr(len(u16) * 2)

	hMem, _, err := globalAlloc.Call(gmemMoveable, size)
	if hMem == 0 {
		return err
	}
	ptr, _, err := globalLock.Call(hMem)
	if ptr == 0 {
		globalFree.Call(hMem)
		return err
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(u16))
	copy(dst, u16)
	globalUnlock.Call(hMem)

	// The clipboard owner must be a window on this thread. Use the predefined
	// STATIC class to create an invisible owner even in headless mode.
	class, _ := syscall.UTF16PtrFromString("STATIC")
	hwnd, _, err := createClipWindow.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		globalFree.Call(hMem)
		return err
	}
	defer destroyClipWindow.Call(hwnd)

	var opened bool
	for i := 0; i < 10; i++ {
		if r, _, _ := openClipboard.Call(hwnd); r != 0 {
			opened = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !opened {
		globalFree.Call(hMem)
		return errors.New("không mở được clipboard Windows")
	}
	defer closeClipboard.Call()

	if r, _, err := emptyClipboard.Call(); r == 0 {
		globalFree.Call(hMem)
		return err
	}

	if r, _, err := setClipboardData.Call(cfUnicodeText, hMem); r == 0 {
		globalFree.Call(hMem)
		return err
	}
	return nil
}
