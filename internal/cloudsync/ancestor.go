package cloudsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
)

func mergeablePath(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".arcmd"
}

// Content-addressed ancestors are local sync state, never document attachments.
// A missing pre-upgrade ancestor simply retains the explicit conflict path.
func readAncestor(dir, hash, password string) ([]byte, bool, error) {
	if len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
		return nil, false, errors.New("invalid sync ancestor hash")
	}
	raw, e := os.ReadFile(filepath.Join(dir, hash))
	if errors.Is(e, os.ErrNotExist) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	data, e := openBackup(raw, password)
	if e != nil {
		return nil, false, e
	}
	if hashBytes(data) != hash {
		return nil, false, fmt.Errorf("sync ancestor checksum mismatch: %s", hash)
	}
	return data, true, nil
}

func saveAncestor(dir string, data []byte, c Config) error {
	hash := hashBytes(data)
	file := filepath.Join(dir, hash)
	if existing, e := os.ReadFile(file); e == nil {
		var envelope sealedBackup
		sealed := json.Unmarshal(existing, &envelope) == nil && envelope.Format == sealedFormat
		if sealed == c.Encrypt {
			_, _, e = readAncestor(dir, hash, c.EncryptPassword)
			return e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	// Encryption can be enabled after a plaintext baseline was recorded.
	payload := data
	if c.Encrypt {
		var e error
		payload, e = sealBackup(data, c.EncryptPassword)
		if e != nil {
			return e
		}
	}
	return fsutil.WriteFileAtomic(file, payload, 0o600)
}
