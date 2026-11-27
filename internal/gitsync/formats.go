package gitsync

import (
	"path/filepath"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

// Structured note formats must remain valid after a textual Git merge. A
// syntactically successful merge is not sufficient to replace a whiteboard.
func validateMergedFile(path string, data []byte) error {
	low := strings.ToLower(path)
	if strings.HasSuffix(low, ".excalidraw") || strings.HasSuffix(low, ".excalidraw.svg") {
		_, e := whiteboard.Decode(string(data))
		return e
	}
	if strings.EqualFold(filepath.Ext(path), ".arcmd") {
		_, e := document.Decode(string(data))
		return e
	}
	return nil
}
