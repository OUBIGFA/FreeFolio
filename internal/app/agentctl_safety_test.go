package app

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/vault"
)

func TestControlEditPreservesReadOnlyAndExternalChanges(t *testing.T) {
	for _, mode := range []string{"read-only", "external-change"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := newTestApp(t)
			d := a.doc
			before := d.ed.Text()
			if mode == "read-only" {
				d.ed.SetReadOnly(true)
			} else if err := vault.Write(d.space.Root, d.rel, "# External replacement\n"); err != nil {
				t.Fatal(err)
			}
			disk, _ := vault.Read(d.space.Root, d.rel)
			err := a.commitMarkdownForControl(d.space, d.rel, "# Agent replacement\n")
			if err == nil {
				t.Fatal("control edit bypassed a document write guard")
			}
			if d.ed.Text() != before {
				t.Fatal("rejected edit changed the live document")
			}
			after, _ := vault.Read(d.space.Root, d.rel)
			if after != disk {
				t.Fatal("rejected edit changed the file")
			}
		})
	}
}

func TestControlReadsUnsavedSource(t *testing.T) {
	a, _ := newTestApp(t)
	d := a.doc
	d.toggleMode()
	d.preview = "# Pending source\n\nUnsaved control text\n"
	text, dirty, err := a.readMarkdownForControl(d.space, d.rel)
	if err != nil || !dirty || !strings.Contains(text, "Unsaved control text") {
		t.Fatalf("control did not read current source: dirty=%v err=%v text=%q", dirty, err, text)
	}
}
