//go:build !windows

package app

// The portable window API reports focus loss but not its mouse/keyboard cause.
// Keep explicit closing on these platforms rather than treating Alt+Tab as a click.
const boardImageOutsidePressSupported = false

func watchBoardImageOutsidePress(windowHandle, func()) {}
