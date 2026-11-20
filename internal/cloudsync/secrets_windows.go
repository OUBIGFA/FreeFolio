//go:build windows

package cloudsync

import (
	"encoding/base64"
	"unsafe"

	"golang.org/x/sys/windows"
)

// protect seals credentials for the current Windows user without showing UI.
func protect(text string) (string, error) {
	sealed, err := dpapi([]byte(text), true)
	return base64.StdEncoding.EncodeToString(sealed), err
}

func unprotect(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	plain, err := dpapi(raw, false)
	return string(plain), err
}

func dpapi(data []byte, seal bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if seal {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
