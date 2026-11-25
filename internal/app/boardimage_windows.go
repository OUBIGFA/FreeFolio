//go:build windows

package app

import (
	"unsafe"

	"github.com/egoist/mygo"
)

const boardImageOutsidePressSupported = true

var (
	boardImageKeyState   = user32.NewProc("GetAsyncKeyState")
	boardImageCursorPos  = user32.NewProc("GetCursorPos")
	boardImageWindowRect = user32.NewProc("GetWindowRect")
)

func watchBoardImageOutsidePress(handle windowHandle, dismiss func()) {
	win, ok := handle.(*mygo.Window)
	if !ok {
		return
	}
	win.OnBlur(func() {
		// Focus loss alone also includes Alt+Tab and system dialogs. Require
		// a mouse press outside the whole native window, including its frame.
		pressed := false
		for _, button := range []uintptr{1, 2, 4} {
			state, _, _ := boardImageKeyState.Call(button)
			pressed = pressed || state&0x8000 != 0
		}
		if !pressed {
			return
		}
		var point struct{ x, y int32 }
		var rect struct{ left, top, right, bottom int32 }
		if ok, _, _ := boardImageCursorPos.Call(uintptr(unsafe.Pointer(&point))); ok == 0 {
			return
		}
		if ok, _, _ := boardImageWindowRect.Call(win.NativeHandle(), uintptr(unsafe.Pointer(&rect))); ok == 0 {
			return
		}
		if point.x < rect.left || point.x >= rect.right || point.y < rect.top || point.y >= rect.bottom {
			dismiss()
		}
	})
}
