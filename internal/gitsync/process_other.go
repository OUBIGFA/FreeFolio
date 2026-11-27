//go:build !windows

package gitsync

import "os/exec"

func hideCommand(c *exec.Cmd) {}
