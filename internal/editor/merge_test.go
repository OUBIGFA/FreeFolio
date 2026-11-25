package editor

import (
	"strings"
	"testing"
)

func TestMergeFilesPreservesMarkdownAndRejectsOverlap(t *testing.T) {
	b := "---\ncustom: value\n---\n\n# Title\n\nFirst paragraph.\n\nLast paragraph.\n"
	o := strings.Replace(b, "First paragraph.", "First paragraph locally edited.", 1)
	r := strings.Replace(b, "Last paragraph.", "Last paragraph remotely edited.", 1)
	m, ok := MergeFiles("note.md", []byte(b), []byte(o), []byte(r))
	if !ok || !strings.Contains(string(m), "custom: value") || !strings.Contains(string(m), "First paragraph locally edited.") || !strings.Contains(string(m), "Last paragraph remotely edited.") {
		t.Fatalf("merged %q, %v", m, ok)
	}
	r = strings.Replace(b, "First paragraph.", "First paragraph remotely edited.", 1)
	if _, ok = MergeFiles("note.md", []byte(b), []byte(o), []byte(r)); ok {
		t.Fatal("overlapping paragraph accepted")
	}
	if _, ok = MergeFiles("asset.png", []byte(b), []byte(o), []byte(r)); ok {
		t.Fatal("binary asset merged as text")
	}
}
