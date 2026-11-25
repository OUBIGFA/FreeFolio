//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// The taskbar groups a window with a pinned button by AppUserModelID. With
// no explicit ID the shell derives one from the executable's path, and a
// path reached through a junction (as a relocated AppData) derives another
// ID than the pinned shortcut's, so the running app shows as a second
// button. FreeFolio therefore gives its process an explicit ID and writes the
// same ID into the shortcuts that start it, as Chromium and Electron apps
// do; MyGo leaves the ID to the shell (internal/windows/jumplist.go).

// appUserModelID is FreeFolio's AppUserModelID, its identifier in mygo.json.
const appUserModelID = "io.github.oubigfa.freefolio"

var (
	ole32   = syscall.NewLazyDLL("ole32.dll")
	shell32 = syscall.NewLazyDLL("shell32.dll")

	procCoInitializeEx    = ole32.NewProc("CoInitializeEx")
	procCoUninitialize    = ole32.NewProc("CoUninitialize")
	procCoCreateInstance  = ole32.NewProc("CoCreateInstance")
	procPropVariantClear  = ole32.NewProc("PropVariantClear")
	procSetExplicitAppID  = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	procSHChangeNotify    = shell32.NewProc("SHChangeNotify")
	clsidShellLink        = comGUID(0x00021401, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46})
	iidIShellLinkW        = comGUID(0x000214f9, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46})
	iidIPersistFile       = comGUID(0x0000010b, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46})
	iidIPropertyStore     = comGUID(0x886d8eeb, 0x8cf2, 0x4446, [8]byte{0x8d, 0x02, 0xcd, 0xba, 0x1d, 0xbd, 0xcf, 0x99})
	pkeyAppUserModelID    = propertyKey{Format: comGUID(0x9f4c2855, 0x9f79, 0x4b39, [8]byte{0xa8, 0xd0, 0xe1, 0xd4, 0x2d, 0xe1, 0xd5, 0xf3}), ID: 5}
	errNoShortcutProperty = errors.New("the shortcut has no property store")
)

// Vtable indices, IUnknown's 0–2 included, and constants of the Windows SDK.
const (
	slGetPath        = 3 // IShellLinkW
	pfLoad           = 5 // IPersistFile
	pfSave           = 6
	psGetValue       = 5 // IPropertyStore
	psSetValue       = 6
	psCommit         = 7
	coinitApartment  = 0x2
	coinitNoOLE1DDE  = 0x4
	clsctxInproc     = 0x1
	stgmReadWrite    = 0x2
	vtLPWSTR         = 31
	shcneUpdateItem  = 0x00002000
	shcneAssocChange = 0x08000000
	shcnfPathW       = 0x0005
	shcnfIDList      = 0x0000
)

type comGUIDValue struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func comGUID(a uint32, b, c uint16, d [8]byte) comGUIDValue {
	return comGUIDValue{a, b, c, d}
}

type propertyKey struct {
	Format comGUIDValue
	ID     uint32
}

// propVariant is PROPVARIANT, 24 bytes on 64-bit Windows.
type propVariant struct {
	VT  uint16
	_   [3]uint16
	Val uint64
	_   uintptr
}

// applyAppUserModelID writes FreeFolio's ID into the Start menu and pinned
// taskbar shortcuts of this executable, then takes the ID for the process.
// The shortcuts come first, so that the window, which opens after, finds
// the pinned button carrying its ID. It must run before the first window.
func (a *App) applyAppUserModelID() {
	exe, err := os.Executable()
	if err == nil {
		_, err = syncShortcutAppIDs(shortcutDirs(), exe)
	}
	if err != nil {
		a.notify("无法更新 FreeFolio 快捷方式的任务栏标识；%v", err)
	}
	id, _ := syscall.UTF16PtrFromString(appUserModelID)
	if hr, _, _ := procSetExplicitAppID.Call(uintptr(unsafe.Pointer(id))); failedHR(hr) {
		a.notify("无法设置 FreeFolio 的任务栏标识；%v", hresult("SetCurrentProcessExplicitAppUserModelID", hr))
	}
}

// shortcutDirs are the folders whose shortcuts start the app from the
// Start menu and the taskbar. The desktop's do not group windows.
func shortcutDirs() []string {
	roaming := os.Getenv("APPDATA")
	if roaming == "" {
		return nil
	}
	return []string{
		filepath.Join(roaming, `Microsoft\Windows\Start Menu\Programs`),
		filepath.Join(roaming, `Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar`),
	}
}

// syncShortcutAppIDs sets the ID of every shortcut in dirs that starts
// the executable exe and carries another, and returns the shortcuts it
// changed. COM runs on a thread of its own, initialized for the shell's
// objects, which the caller waits for.
func syncShortcutAppIDs(dirs []string, exe string) ([]string, error) {
	self, err := os.Stat(exe)
	if err != nil {
		return nil, err
	}
	var links []string
	for _, dir := range dirs {
		found, _ := filepath.Glob(filepath.Join(dir, "*.lnk"))
		links = append(links, found...)
	}
	if len(links) == 0 {
		return nil, nil
	}
	var changed []string
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		hr, _, _ := procCoInitializeEx.Call(0, coinitApartment|coinitNoOLE1DDE)
		if failedHR(hr) {
			done <- hresult("CoInitializeEx", hr)
			return
		}
		defer procCoUninitialize.Call()
		var errs []error
		for _, link := range links {
			ok, err := setShortcutAppID(link, self)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(link), err))
			}
			if ok {
				changed = append(changed, link)
				p, _ := syscall.UTF16PtrFromString(link)
				procSHChangeNotify.Call(shcneUpdateItem, shcnfPathW, uintptr(unsafe.Pointer(p)), 0)
			}
		}
		// The taskbar reads the pinned buttons' IDs again on this notice.
		if len(changed) > 0 {
			procSHChangeNotify.Call(shcneAssocChange, shcnfIDList, 0, 0)
		}
		done <- errors.Join(errs...)
	}()
	err = <-done
	return changed, err
}

// setShortcutAppID gives the shortcut at link FreeFolio's ID when it starts
// the executable self, and reports whether it changed the file. Other
// shortcuts are left as they are.
func setShortcutAppID(link string, self os.FileInfo) (bool, error) {
	var sl uintptr
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInproc,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&sl))); failedHR(hr) {
		return false, hresult("CoCreateInstance", hr)
	}
	defer comRelease(sl)
	pf := comQuery(sl, &iidIPersistFile)
	if pf == 0 {
		return false, errors.New("the shortcut cannot be loaded")
	}
	defer comRelease(pf)
	path, _ := syscall.UTF16PtrFromString(link)
	if hr := comInvoke(pf, pfLoad, uintptr(unsafe.Pointer(path)), stgmReadWrite); failedHR(hr) {
		return false, hresult("IPersistFile::Load", hr)
	}
	var target [syscall.MAX_LONG_PATH]uint16
	if hr := comInvoke(sl, slGetPath, uintptr(unsafe.Pointer(&target[0])), uintptr(len(target)), 0, 0); failedHR(hr) {
		return false, hresult("IShellLinkW::GetPath", hr)
	}
	// SameFile sees through the junctions that make the paths differ.
	info, err := os.Stat(syscall.UTF16ToString(target[:]))
	if err != nil || !os.SameFile(info, self) {
		return false, nil
	}
	ps := comQuery(sl, &iidIPropertyStore)
	if ps == 0 {
		return false, errNoShortcutProperty
	}
	defer comRelease(ps)
	var cur propVariant
	if hr := comInvoke(ps, psGetValue, uintptr(unsafe.Pointer(&pkeyAppUserModelID)), uintptr(unsafe.Pointer(&cur))); failedHR(hr) {
		return false, hresult("IPropertyStore::GetValue", hr)
	}
	same := cur.VT == vtLPWSTR && wideString(uintptr(cur.Val)) == appUserModelID
	procPropVariantClear.Call(uintptr(unsafe.Pointer(&cur)))
	if same {
		return false, nil
	}
	id, _ := syscall.UTF16FromString(appUserModelID)
	// SetValue copies the string.
	v := propVariant{VT: vtLPWSTR, Val: uint64(uintptr(unsafe.Pointer(&id[0])))}
	hr := comInvoke(ps, psSetValue, uintptr(unsafe.Pointer(&pkeyAppUserModelID)), uintptr(unsafe.Pointer(&v)))
	runtime.KeepAlive(id)
	if failedHR(hr) {
		return false, hresult("IPropertyStore::SetValue", hr)
	}
	if hr := comInvoke(ps, psCommit); failedHR(hr) {
		return false, hresult("IPropertyStore::Commit", hr)
	}
	if hr := comInvoke(pf, pfSave, 0, 1); failedHR(hr) {
		return false, hresult("IPersistFile::Save", hr)
	}
	return true, nil
}

// nativePtr turns an address COM returned into a pointer without the
// uintptr conversion vet rejects; the memory is COM's, not Go's.
func nativePtr(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

// comInvoke calls method i of the COM object obj.
func comInvoke(obj uintptr, i int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(nativePtr(obj))
	fn := *(*uintptr)(unsafe.Add(nativePtr(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func comQuery(obj uintptr, iid *comGUIDValue) uintptr {
	var out uintptr
	if failedHR(comInvoke(obj, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))) {
		return 0
	}
	return out
}

func comRelease(obj uintptr) { comInvoke(obj, 2) }

// wideString reads a NUL-terminated UTF-16 string COM allocated.
func wideString(p uintptr) string {
	if p == 0 {
		return ""
	}
	var s []uint16
	for i := 0; ; i++ {
		c := *(*uint16)(unsafe.Add(nativePtr(p), 2*i))
		if c == 0 {
			return syscall.UTF16ToString(s)
		}
		s = append(s, c)
	}
}

func failedHR(hr uintptr) bool { return int32(uint32(hr)) < 0 }

func hresult(what string, hr uintptr) error {
	return fmt.Errorf("%s (HRESULT %#08x)", what, uint32(hr))
}
