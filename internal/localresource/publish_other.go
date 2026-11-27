//go:build !windows

package localresource

import (
	"os"
	"os/exec"
	"syscall"
)

type processGroup struct {
	pid int
}

func publishDownload(source, target string) error { return os.Link(source, target) }

func hideConsole(*exec.Cmd) {}

func configureComfyCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachProcessGroup(cmd *exec.Cmd) *processGroup {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return nil
	}
	return &processGroup{pid: cmd.Process.Pid}
}

func (g *processGroup) Kill() {
	if g == nil || g.pid <= 0 {
		return
	}
	_ = syscall.Kill(-g.pid, syscall.SIGKILL)
	g.pid = 0
}

func (g *processGroup) Close() {
	if g != nil {
		g.pid = 0
	}
}
