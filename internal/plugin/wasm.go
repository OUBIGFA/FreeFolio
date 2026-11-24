package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// WASM limits: what one invocation of a plugin may use.
const (
	// MaxWASMModule bounds a plugin's .wasm file.
	MaxWASMModule = 48 << 20
	// wasmMemoryPages bounds a module's linear memory (64 KiB pages): 1 GiB.
	wasmMemoryPages = 16384
	// maxWASMOutput bounds what a plugin writes to standard output.
	maxWASMOutput = 32 << 20
	// WASMTimeout bounds one invocation.
	WASMTimeout = 60 * time.Second
)

// WASMHost runs wasm plugins with wazero: pure Go, no cgo.
//
// A plugin is a WASI command module. Each invocation instantiates it afresh
// and runs its _start with the Request as JSON on standard input; it writes
// the Response as JSON to standard output and exits. The module gets no
// file system, no network, no environment and no arguments beyond its
// name: everything it may see comes in the request, which FreeFolio fills
// only as the manifest's permissions allow. Its clock and random source
// are the system's, as WASI programs expect.
type WASMHost struct {
	once sync.Once
	rt   wazero.Runtime
	err  error
}

// NewWASMHost returns the host; the runtime starts on first use.
func NewWASMHost() *WASMHost { return &WASMHost{} }

// Runtime implements Host.
func (h *WASMHost) Runtime() Runtime { return RuntimeWASM }

func (h *WASMHost) runtime() (wazero.Runtime, error) {
	h.once.Do(func() {
		ctx := context.Background()
		cfg := wazero.NewRuntimeConfig().WithMemoryLimitPages(wasmMemoryPages).WithCloseOnContextDone(true)
		rt := wazero.NewRuntimeWithConfig(ctx, cfg)
		if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
			rt.Close(ctx)
			h.err = err
			return
		}
		h.rt = rt
	})
	return h.rt, h.err
}

// Close releases the runtime and every compiled module.
func (h *WASMHost) Close() error {
	if h.rt != nil {
		return h.rt.Close(context.Background())
	}
	return nil
}

// Start implements Host: it compiles the plugin's module.
func (h *WASMHost) Start(ctx context.Context, p *Installed) (Instance, error) {
	if p == nil || p.Manifest == nil || p.Manifest.Runtime != RuntimeWASM {
		return nil, errors.New("plugin: not a wasm plugin")
	}
	if !p.Enabled {
		return nil, fmt.Errorf("plugin: %s is disabled", p.Manifest.ID)
	}
	rt, err := h.runtime()
	if err != nil {
		return nil, fmt.Errorf("plugin: the wasm runtime did not start: %w", err)
	}
	bin, err := readModule(p)
	if err != nil {
		return nil, err
	}
	compiled, err := rt.CompileModule(ctx, bin)
	if err != nil {
		return nil, fmt.Errorf("plugin: %s: invalid wasm module: %w", p.Manifest.ID, err)
	}
	return &wasmInstance{rt: rt, compiled: compiled, manifest: p.Manifest}, nil
}

// readModule reads the plugin's main module, refusing a path that leaves the
// plugin's folder through a link.
func readModule(p *Installed) ([]byte, error) {
	dir, err := filepath.EvalSymlinks(p.Dir)
	if err != nil {
		return nil, err
	}
	full, err := filepath.EvalSymlinks(filepath.Join(dir, filepath.FromSlash(p.Manifest.Main)))
	if err != nil {
		return nil, fmt.Errorf("plugin: %s: %w", p.Manifest.ID, err)
	}
	if rel, err := filepath.Rel(dir, full); err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("plugin: %s: main leaves the plugin folder", p.Manifest.ID)
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxWASMModule {
		return nil, fmt.Errorf("plugin: %s: main must be a regular file under %d MB", p.Manifest.ID, MaxWASMModule>>20)
	}
	return os.ReadFile(full)
}

type wasmInstance struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	manifest *Manifest
	// mu runs one invocation at a time: plugins are single-threaded.
	mu     sync.Mutex
	closed bool
}

// limitedWriter keeps at most n bytes and reports overflow.
type limitedWriter struct {
	buf      bytes.Buffer
	n        int
	overflow bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.n {
		w.overflow = true
		p = p[:max(0, w.n-w.buf.Len())]
	}
	w.buf.Write(p)
	return len(p), nil
}

// Invoke implements Instance.
func (in *wasmInstance) Invoke(ctx context.Context, req Request) (*Response, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.closed {
		return nil, errors.New("plugin: the instance is closed")
	}
	if err := in.check(req); err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, WASMTimeout)
	defer cancel()
	stdout := &limitedWriter{n: maxWASMOutput}
	stderr := &limitedWriter{n: 16 << 10}
	cfg := wazero.NewModuleConfig().
		WithName("").
		WithArgs(in.manifest.ID).
		WithStdin(bytes.NewReader(body)).
		WithStdout(stdout).
		WithStderr(stderr).
		WithSysWalltime().
		WithSysNanotime()
	mod, err := in.rt.InstantiateModule(ctx, in.compiled, cfg)
	if mod != nil {
		defer mod.Close(context.Background())
	}
	if err != nil {
		var exit *sys.ExitError
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return nil, fmt.Errorf("plugin: %s did not answer within %s", in.manifest.ID, WASMTimeout)
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.As(err, &exit) && exit.ExitCode() == 0:
			// A command that exits 0 through proc_exit succeeded.
		default:
			return nil, fmt.Errorf("plugin: %s failed: %w%s", in.manifest.ID, err, stderrNote(stderr))
		}
	}
	if stdout.overflow {
		return nil, fmt.Errorf("plugin: %s answered more than %d MB", in.manifest.ID, maxWASMOutput>>20)
	}
	var resp Response
	if err := json.Unmarshal(bytes.TrimSpace(stdout.buf.Bytes()), &resp); err != nil {
		return nil, fmt.Errorf("plugin: %s answered no valid response: %w%s", in.manifest.ID, err, stderrNote(stderr))
	}
	return in.filter(req, &resp)
}

func stderrNote(w *limitedWriter) string {
	s := strings.TrimSpace(w.buf.String())
	if s == "" {
		return ""
	}
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return " (" + s + ")"
}

// check refuses requests the manifest does not allow.
func (in *wasmInstance) check(req Request) error {
	if req.Document != nil && !in.has(PermDocumentRead) {
		return fmt.Errorf("plugin: %s may not read the document", in.manifest.ID)
	}
	if req.Convert != nil {
		if !in.converts(req.Convert.Name) {
			return fmt.Errorf("plugin: %s does not read %s", in.manifest.ID, filepath.Ext(req.Convert.Name))
		}
	}
	return nil
}

// filter drops what the plugin may not ask for.
func (in *wasmInstance) filter(req Request, resp *Response) (*Response, error) {
	if len(resp.Edits) > 0 && (!in.has(PermDocumentWrite) || req.Document == nil) {
		return nil, fmt.Errorf("plugin: %s may not edit the document", in.manifest.ID)
	}
	if req.Document != nil {
		for _, e := range resp.Edits {
			if e.Range.Start < 0 || e.Range.End < e.Range.Start || e.Range.End > len(req.Document.Text) {
				return nil, fmt.Errorf("plugin: %s answered an edit outside the document", in.manifest.ID)
			}
		}
	}
	return resp, nil
}

func (in *wasmInstance) has(p Permission) bool {
	for _, q := range in.manifest.Permissions {
		if q == p {
			return true
		}
	}
	return false
}

func (in *wasmInstance) converts(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, f := range in.manifest.Contributes.Formats {
		for _, e := range f.Extensions {
			if e == ext {
				return true
			}
		}
	}
	return false
}

// Close implements Instance.
func (in *wasmInstance) Close() error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.closed {
		return nil
	}
	in.closed = true
	return in.compiled.Close(context.Background())
}

// Converters keeps one started instance per format plugin and converts
// files with them; it is what FreeFolio registers with the formats
// package. Instances start on first use, off the UI thread.
type Converters struct {
	host Host
	mu   sync.Mutex
	by   map[string]*Installed // extension → plugin
	live map[string]Instance   // plugin ID → instance
}

// NewConverters returns converters running plugins on host.
func NewConverters(host Host) *Converters {
	return &Converters{host: host, by: map[string]*Installed{}, live: map[string]Instance{}}
}

// Set replaces the plugins converting formats: the enabled wasm plugins
// of installed, closing instances of plugins no longer there.
func (c *Converters) Set(installed []*Installed) {
	c.mu.Lock()
	defer c.mu.Unlock()
	by := map[string]*Installed{}
	keep := map[string]bool{}
	for _, p := range installed {
		if !p.Enabled || p.Manifest.Runtime != c.host.Runtime() {
			continue
		}
		for _, f := range p.Manifest.Contributes.Formats {
			for _, e := range f.Extensions {
				if _, taken := by[e]; !taken {
					by[e] = p
					keep[p.Manifest.ID] = true
				}
			}
		}
	}
	for id, in := range c.live {
		if !keep[id] {
			in.Close()
			delete(c.live, id)
		}
	}
	c.by = by
}

// Convert reads data, the file name, into Markdown with the plugin
// contributing its extension.
func (c *Converters) Convert(ctx context.Context, name string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	c.mu.Lock()
	p := c.by[ext]
	var in Instance
	if p != nil {
		in = c.live[p.Manifest.ID]
	}
	c.mu.Unlock()
	if p == nil {
		return "", fmt.Errorf("plugin: no enabled plugin reads %s", ext)
	}
	if in == nil {
		started, err := c.host.Start(ctx, p)
		if err != nil {
			return "", err
		}
		c.mu.Lock()
		if prev := c.live[p.Manifest.ID]; prev != nil {
			started.Close()
			in = prev
		} else {
			c.live[p.Manifest.ID] = started
			in = started
		}
		c.mu.Unlock()
	}
	resp, err := in.Invoke(ctx, Request{Convert: &ConvertRequest{Name: filepath.Base(name), Data: data}})
	if err != nil {
		return "", err
	}
	return resp.Markdown, nil
}

// Close closes every instance.
func (c *Converters) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, in := range c.live {
		in.Close()
		delete(c.live, id)
	}
}

var _ io.Writer = (*limitedWriter)(nil)
