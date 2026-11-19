//go:build windows

package app

import "syscall"

var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	procGetDpiForSystem        = user32.NewProc("GetDpiForSystem")
	procGetSystemMetricsForDpi = user32.NewProc("GetSystemMetricsForDpi")
)

// systemIconSize is the pixel size of a system icon metric at the
// system's DPI, which the notification area and the taskbar draw at:
// SM_CXSMICON is 16 px at 100% and 24 px at 150%, SM_CXICON twice that.
func systemIconSize(metric uintptr, points int) int {
	dpi := uintptr(96)
	if procGetDpiForSystem.Find() == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
			dpi = d
		}
	}
	if procGetSystemMetricsForDpi.Find() == nil {
		if n, _, _ := procGetSystemMetricsForDpi.Call(metric, dpi); n != 0 {
			return int(n)
		}
	}
	return points * int(dpi) / 96
}

// trayIcon is the logo at the notification area's icon size.
func trayIcon() []byte {
	const smCxSmIcon = 49
	return iconPNG(systemIconSize(smCxSmIcon, 16))
}

// windowIcon is the logo at the size of the window's large icon, which
// the taskbar and Alt+Tab show; Windows halves it for the small one.
func windowIcon() []byte {
	const smCxIcon = 11
	return iconPNG(systemIconSize(smCxIcon, 32))
}
