package app

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBytes(t *testing.T, root, rel string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A document's pictures named without a folder are looked for across the
// space. The window reads the space once for all the pictures of a frame;
// that listing must find what a walk for each name finds, and must not
// outlive the frame, so a picture added since is found by the next one.
func TestFramePictureNamesAgreeWithTheWalk(t *testing.T) {
	a, tt := newTestApp(t)
	r := root(a)
	for _, rel := range []string{
		"deep/er/photo.png", "z/photo.png", "a/PHOTO.png", ".hidden/photo.png", "node_modules/photo.png",
		"only/Upper.PNG", "x/dup.png", "y/dup.png", "x/y/dup.png", ".dot.png", "folder.png/inside.txt",
	} {
		writeBytes(t, r, rel)
	}
	tt.Frame()
	for _, name := range []string{"photo.png", "PHOTO.png", "Photo.PNG", "upper.png", "Upper.PNG", "dup.png", ".dot.png", "folder.png", "none.png", "inside.txt"} {
		want, wantOK := findSpaceAttachment(r, name)
		got, ok := a.findAttachment(r, name)
		if got != want || ok != wantOK {
			t.Errorf("findAttachment(%q) = %q, %v; the walk finds %q, %v", name, got, ok, want, wantOK)
		}
	}
	if got, ok := a.findAttachment(r, "photo.png"); !ok || got != filepath.Join(r, "z", "photo.png") {
		t.Errorf("photo.png found at %q, %v: want the exact name nearest the root", got, ok)
	}
	if _, ok := a.findAttachment(r, "late.png"); ok {
		t.Fatal("found a picture that is not there")
	}
	writeBytes(t, r, "later/late.png")
	if _, ok := a.findAttachment(r, "late.png"); ok {
		t.Fatal("the frame's listing was read again within the frame")
	}
	tt.Frame()
	if got, ok := a.findAttachment(r, "late.png"); !ok || got != filepath.Join(r, "later", "late.png") {
		t.Fatalf("the next frame did not find the picture added: %q, %v", got, ok)
	}
	// What invalidates the resolved pictures drops the listing at once.
	writeBytes(t, r, "later/again.png")
	a.invalidateResolvedImages()
	if _, ok := a.findAttachment(r, "again.png"); !ok {
		t.Fatal("the listing outlived invalidateResolvedImages")
	}
}
