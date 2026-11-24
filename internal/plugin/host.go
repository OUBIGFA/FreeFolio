package plugin

import "context"

// Host runs plugins of one Runtime. FreeFolio has none yet: the WASM host
// (wazero, pure Go, no cgo) and the process host (JSON-RPC over stdio) are
// planned; see docs/plugins.md. Until a host is registered for a plugin's
// runtime, its commands show as unavailable.
type Host interface {
	Runtime() Runtime
	// Start loads an installed plugin. The host enforces the manifest's
	// permissions on every call the plugin makes back into FreeFolio.
	Start(ctx context.Context, p *Installed) (Instance, error)
}

// Instance is a running plugin.
type Instance interface {
	// Invoke runs one of the plugin's commands, or renders one of its
	// panels (Request.Panel), and returns what FreeFolio should do.
	Invoke(ctx context.Context, req Request) (*Response, error)
	Close() error
}

// Request is what FreeFolio tells a plugin when invoking it. Fields the
// plugin's permissions do not cover are left empty.
type Request struct {
	Command string `json:"command,omitempty"`
	Panel   string `json:"panel,omitempty"`
	// Document is the open document: its path in the space and its text
	// (document:read).
	Document *DocumentContext `json:"document,omitempty"`
	// Convert asks a format plugin for the Markdown of a file of one of
	// its contributed formats; the answer is Response.Markdown. The file's
	// bytes go with the request: the plugin needs no file access.
	Convert *ConvertRequest `json:"convert,omitempty"`
}

// ConvertRequest is a file to read into Markdown.
type ConvertRequest struct {
	Name string `json:"name"` // the file's base name, for its extension
	Data []byte `json:"data"` // base64 in JSON
}

// DocumentContext is the open document as a plugin sees it.
type DocumentContext struct {
	Path string `json:"path"`
	Text string `json:"text"`
	// Selection is in bytes of Text; Start == End is the caret.
	Selection Range `json:"selection"`
}

// Range is a range of bytes of a text.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Response is what a plugin asks FreeFolio to do.
type Response struct {
	// Edits change the open document (document:write), as one undo step.
	Edits []Edit `json:"edits,omitempty"`
	// Markdown is shown in the panel invoked, or in a sheet for a command.
	Markdown string `json:"markdown,omitempty"`
	// Toast is a short message to show.
	Toast string `json:"toast,omitempty"`
}

// Edit replaces Range of the open document with Text.
type Edit struct {
	Range Range  `json:"range"`
	Text  string `json:"text"`
}
