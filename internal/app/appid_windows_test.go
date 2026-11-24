//go:build windows

package app

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"unsafe"
)

// makeShortcut saves a shortcut at link that starts target.
func makeShortcut(t *testing.T, link, target string) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitializeEx.Call(0, coinitApartment|coinitNoOLE1DDE)
	defer procCoUninitialize.Call()
	var sl uintptr
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInproc,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&sl))); failedHR(hr) {
		t.Fatal(hresult("CoCreateInstance", hr))
	}
	defer comRelease(sl)
	const slSetPath = 20
	p, _ := syscall.UTF16PtrFromString(target)
	if hr := comInvoke(sl, slSetPath, uintptr(unsafe.Pointer(p))); failedHR(hr) {
		t.Fatal(hresult("SetPath", hr))
	}
	pf := comQuery(sl, &iidIPersistFile)
	defer comRelease(pf)
	l, _ := syscall.UTF16PtrFromString(link)
	if hr := comInvoke(pf, pfSave, uintptr(unsafe.Pointer(l)), 1); failedHR(hr) {
		t.Fatal(hresult("Save", hr))
	}
}

// Only the shortcuts starting this executable take FreeFolio's ID, once.
func TestSyncShortcutAppIDs(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "FreeFolio.exe")
	other := filepath.Join(dir, "Other.exe")
	for _, f := range []string{exe, other} {
		if err := os.WriteFile(f, []byte("MZ"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mine, theirs := filepath.Join(dir, "FreeFolio.lnk"), filepath.Join(dir, "Other.lnk")
	makeShortcut(t, mine, exe)
	makeShortcut(t, theirs, other)
	before, _ := os.ReadFile(theirs)

	changed, err := syncShortcutAppIDs([]string{dir}, exe)
	if err != nil || !slices.Equal(changed, []string{mine}) {
		t.Fatalf("changed %v, %v", changed, err)
	}
	if after, _ := os.ReadFile(theirs); string(after) != string(before) {
		t.Fatal("another app's shortcut changed")
	}
	if changed, err := syncShortcutAppIDs([]string{dir}, exe); err != nil || len(changed) != 0 {
		t.Fatalf("second run changed %v, %v", changed, err)
	}
}
