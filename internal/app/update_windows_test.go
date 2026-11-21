package app

import (
	"runtime"
	"testing"
)

func TestWindowsUpdatesWorkWithoutPackagedBuildMetadata(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("published Windows installers target x64")
	}
	// A plain go test binary has none of MyGo's release metadata. It must still
	// be able to check the same release source as the packaged application.
	service := newUpdateService(t.TempDir())
	if !service.Enabled() {
		t.Fatal("ordinary Windows builds must not be classified as unable to update")
	}
}
