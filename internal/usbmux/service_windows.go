//go:build windows

package usbmux

import (
	"errors"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

const amdsName = "Apple Mobile Device Service"

// nudgeAndDiagnose inspects Apple Mobile Device Service when 127.0.0.1:27015
// refuses a connection, tries to start it if stopped (succeeds when elevated),
// and returns a human-readable Vietnamese explanation.
func nudgeAndDiagnose(seenBefore bool) string {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		if seenBefore {
			return "dịch vụ Apple Mobile Device đang khởi động lại hoặc quá tải — đang tự thử lại…"
		}
		return "đã cài Apple Devices/iTunes chưa?"
	}
	defer windows.CloseServiceHandle(scm)

	name, _ := syscall.UTF16PtrFromString(amdsName)
	sh, err := windows.OpenService(scm, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return "chưa cài Apple Devices hoặc iTunes trên máy tính này"
		}
		if seenBefore {
			return "dịch vụ Apple Mobile Device đang khởi động lại — đang tự thử lại…"
		}
		return "đã cài Apple Devices/iTunes chưa?"
	}
	defer windows.CloseServiceHandle(sh)

	var st windows.SERVICE_STATUS_PROCESS
	var needed uint32
	if err := windows.QueryServiceStatusEx(sh, windows.SC_STATUS_PROCESS_INFO, (*byte)(unsafe.Pointer(&st)), uint32(unsafe.Sizeof(st)), &needed); err != nil {
		return "dịch vụ Apple Mobile Device đang bận — đang tự thử lại…"
	}

	switch svc.State(st.CurrentState) {
	case svc.Stopped:
		if startH, e := windows.OpenService(scm, name, windows.SERVICE_START); e == nil {
			errStart := windows.StartService(startH, 0, nil)
			windows.CloseServiceHandle(startH)
			if errStart == nil {
				time.Sleep(500 * time.Millisecond)
				return "dịch vụ Apple Mobile Device bị dừng — đã tự khởi động lại, đang kết nối…"
			}
		}
		return "dịch vụ Apple Mobile Device đang bị DỪNG — mở Services (services.msc) bật lại Apple Mobile Device Service hoặc kiểm tra nguồn USB Hub"
	case svc.StartPending, svc.StopPending:
		return "dịch vụ Apple Mobile Device đang khởi động lại — đang tự chờ kết nối…"
	default:
		return "dịch vụ Apple Mobile Device đang quá tải kết nối — đang tự thử lại…"
	}
}
