//go:build !windows

package bash

import (
	"os/exec"
	"syscall"
)

func defaultShell() string {
	return "/bin/sh"
}

func defaultArgsForShell(_ string) []string {
	return []string{"-c"}
}

// procGroup tracks the process tree a command starts so cancellation can end all of it.
// On unix the command leads its own process group and kill signals the whole group.
type procGroup struct {
	cmd *exec.Cmd
}

// newProcGroup prepares cmd (before Start) to lead its own process group.
func newProcGroup(cmd *exec.Cmd) *procGroup {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return &procGroup{cmd: cmd}
}

// started is called right after cmd.Start; nothing to do on unix.
func (g *procGroup) started() {}

// kill ends the whole process group.
func (g *procGroup) kill() error {
	if g.cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(g.cmd.Process.Pid)
	if err != nil {
		return g.cmd.Process.Kill()
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// release frees per-command resources after cmd.Wait; nothing to do on unix.
func (g *procGroup) release() {}
