//go:build !windows

package localresource

import (
	"os"
	"os/exec"
)

func publishDownload(source, target string) error { return os.Link(source, target) }

func hideConsole(*exec.Cmd) {}
