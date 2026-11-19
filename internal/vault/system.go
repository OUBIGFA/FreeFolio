package vault

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// SystemDir is the hidden folder of a space where FreeMD keeps its own
// files (versions, templates). The walk skips it as it skips every hidden
// folder.
const SystemDir = ".freemd"

// legacySystemDir is the name the folder had before the rebrand, which
// MigrateSystemDir renames.
const legacySystemDir = ".arcmd"

// MigrateSystemDir renames a space's legacy system folder to SystemDir so
// earlier versions and templates stay with their documents. A space that
// already has SystemDir keeps both untouched: merging two histories could
// overwrite versions, and the legacy folder still holds them for the user.
func MigrateSystemDir(root string) error {
	if root == "" {
		return nil
	}
	legacy := filepath.Join(root, legacySystemDir)
	info, err := os.Lstat(legacy)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// A legacy document named ".arcmd" or a link is not the folder.
	if !info.IsDir() {
		return nil
	}
	target := filepath.Join(root, SystemDir)
	if _, err := os.Lstat(target); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(legacy, target)
}
