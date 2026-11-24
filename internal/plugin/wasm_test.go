package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// guest is a WASI plugin: it echoes a conversion request as Markdown,
// loops forever on the command "spin", fails on "fail" and edits on "edit".
const guest = `package main

import (
	"encoding/json"
	"os"
	"strings"
)

func main() {
	var req struct {
		Command string ` + "`json:\"command\"`" + `
		Convert *struct {
			Name string ` + "`json:\"name\"`" + `
			Data []byte ` + "`json:\"data\"`" + `
		} ` + "`json:\"convert\"`" + `
	}
	json.NewDecoder(os.Stdin).Decode(&req)
	switch req.Command {
	case "spin":
		for {
		}
	case "fail":
		os.Stderr.WriteString("boom")
		os.Exit(3)
	case "edit":
		os.Stdout.WriteString(` + "`{\"edits\":[{\"range\":{\"start\":0,\"end\":0},\"text\":\"x\"}]}`" + `)
		return
	}
	md := ""
	if req.Convert != nil {
		md = "# " + req.Convert.Name + "\n\n" + strings.ToUpper(string(req.Convert.Data))
	}
	json.NewEncoder(os.Stdout).Encode(map[string]string{"markdown": md})
}
`

// buildGuest compiles guest for wasip1 into a plugin folder.
func buildGuest(t *testing.T) *Installed {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a wasm module")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain")
	}
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "go.mod"), []byte("module guest\n\ngo 1.24\n"), 0o644)
	os.WriteFile(filepath.Join(src, "main.go"), []byte(guest), 0o644)
	dir := t.TempDir()
	cmd := exec.Command(goBin, "build", "-o", filepath.Join(dir, "plugin.wasm"), ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0", "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build wasip1: %v\n%s", err, out)
	}
	m := &Manifest{ID: "dev.test.echo", Name: "Echo", Version: "1.0.0", Runtime: RuntimeWASM, Main: "plugin.wasm",
		Contributes: Contributes{Formats: []FileFormat{{Name: "Echo", Extensions: []string{".echo"}}}}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	return &Installed{Manifest: m, Dir: dir, Enabled: true}
}

func TestWASMHost(t *testing.T) {
	p := buildGuest(t)
	host := NewWASMHost()
	defer host.Close()
	ctx := context.Background()
	in, err := host.Start(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	resp, err := in.Invoke(ctx, Request{Convert: &ConvertRequest{Name: "a.echo", Data: []byte("hello")}})
	if err != nil || resp.Markdown != "# a.echo\n\nHELLO" {
		t.Fatalf("convert = %+v, %v", resp, err)
	}
	// A second call gets a fresh instance and still works.
	if resp, err := in.Invoke(ctx, Request{Convert: &ConvertRequest{Name: "b.echo", Data: []byte("x")}}); err != nil || !strings.Contains(resp.Markdown, "b.echo") {
		t.Fatalf("second convert = %+v, %v", resp, err)
	}
	if _, err := in.Invoke(ctx, Request{Convert: &ConvertRequest{Name: "a.docx"}}); err == nil {
		t.Fatal("an extension the plugin does not declare was converted")
	}
	if _, err := in.Invoke(ctx, Request{Document: &DocumentContext{Text: "secret"}}); err == nil {
		t.Fatal("the document went to a plugin without document:read")
	}
	if _, err := in.Invoke(ctx, Request{Command: "edit"}); err == nil {
		t.Fatal("an edit from a plugin without document:write was accepted")
	}
	if _, err := in.Invoke(ctx, Request{Command: "fail"}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("failure = %v", err)
	}
	short, cancel := context.WithTimeout(ctx, 300_000_000)
	defer cancel()
	if _, err := in.Invoke(short, Request{Command: "spin"}); err == nil {
		t.Fatal("a looping plugin was not stopped")
	}
}

func TestConverters(t *testing.T) {
	p := buildGuest(t)
	host := NewWASMHost()
	defer host.Close()
	c := NewConverters(host)
	defer c.Close()
	c.Set([]*Installed{p})
	md, err := c.Convert(context.Background(), "dir/z.ECHO", []byte("ok"))
	if err != nil || md != "# z.ECHO\n\nOK" {
		t.Fatalf("Convert = %q, %v", md, err)
	}
	p.Enabled = false
	c.Set([]*Installed{p})
	if _, err := c.Convert(context.Background(), "z.echo", nil); err == nil {
		t.Fatal("a disabled plugin converted")
	}
}

func TestManifestFormats(t *testing.T) {
	base := func() *Manifest {
		return &Manifest{ID: "dev.test.hwp", Name: "HWP", Version: "1.0.0", Runtime: RuntimeWASM, Main: "p.wasm"}
	}
	m := base()
	m.Contributes.Formats = []FileFormat{{Name: "HWP", Extensions: []string{".hwp", ".hwpx"}}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{"hwp"}, {".HWP"}, {".hwp", ".hwp"}, {}} {
		m := base()
		m.Contributes.Formats = []FileFormat{{Name: "HWP", Extensions: bad}}
		if m.Validate() == nil {
			t.Errorf("extensions %v accepted", bad)
		}
	}
	m = base()
	m.Runtime, m.Main = RuntimeProcess, "p.exe"
	m.Contributes.Formats = []FileFormat{{Name: "HWP", Extensions: []string{".hwp"}}}
	if m.Validate() == nil {
		t.Error("a process plugin contributed formats")
	}
}
