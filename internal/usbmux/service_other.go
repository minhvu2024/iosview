//go:build !windows

package usbmux

func nudgeAndDiagnose(seenBefore bool) string {
	if seenBefore {
		return "usbmuxd đang khởi động lại hoặc quá tải — đang tự thử lại…"
	}
	return "usbmuxd chưa chạy"
}
