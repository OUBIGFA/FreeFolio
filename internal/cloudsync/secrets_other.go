//go:build !windows

package cloudsync

// Unix credentials use the owner-only credential files shared with the app.
func protect(text string) (string, error)   { return text, nil }
func unprotect(text string) (string, error) { return text, nil }
