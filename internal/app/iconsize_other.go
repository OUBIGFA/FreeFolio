//go:build !windows

package app

import "github.com/OUBIGFA/FreeFolio/resources"

// trayIcon is the logo at 16 points on a Retina menu bar, as MyGo asks.
func trayIcon() []byte { return iconPNG(32) }

// windowIcon is the whole logo, which macOS and Linux shrink smoothly.
func windowIcon() []byte { return resources.Icon }
