package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOneBotAttachmentsKeepTextAndRejectBinary(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.md")
	os.WriteFile(file, []byte("中文参考"), 0600)
	part, err := oneBotAttachment(file)
	if err != nil || part.Type != "text" || !strings.Contains(part.Text, "中文参考") {
		t.Fatalf("text %+v %v", part, err)
	}
	file = filepath.Join(dir, "binary.exe")
	os.WriteFile(file, []byte{0, 1, 2, 3}, 0600)
	if _, err = oneBotAttachment(file); err == nil {
		t.Fatal("binary accepted as text")
	}
	file = filepath.Join(dir, "page.pdf")
	os.WriteFile(file, []byte("%PDF-test"), 0600)
	part, err = oneBotAttachment(file)
	if err != nil || part.Type != "file" || !strings.HasPrefix(part.Data, "data:application/pdf;base64,") {
		t.Fatalf("PDF %+v %v", part, err)
	}
}
