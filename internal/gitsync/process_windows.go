package gitsync

import (
	"os/exec"
	"syscall"
)

func hideCommand(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
