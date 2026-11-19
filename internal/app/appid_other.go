//go:build !windows

package app

// applyAppUserModelID is Windows's: elsewhere the system groups windows by
// the app bundle or desktop entry.
func (a *App) applyAppUserModelID() {}
