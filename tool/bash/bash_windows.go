//go:build windows

package bash

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

func defaultShell() string {
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	return "cmd.exe"
}

func defaultArgsForShell(shell string) []string {
	switch strings.ToLower(filepath.Base(shell)) {
	case "cmd", "cmd.exe":
		return []string{"/d", "/s", "/c"}
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return []string{"-NoProfile", "-Command"}
	default:
		return []string{"-c"}
	}
}

// procGroup tracks the process tree a command starts so cancellation can end all of it.
// Windows has no process groups to signal: killing the shell alone leaves its children
// (ping -t, scripts, servers) running and holding the inherited output pipes. The shell
// is placed in a job object right after it starts; kill terminates every process in it.
type procGroup struct {
	cmd *exec.Cmd

	mu  sync.Mutex
	job windows.Handle
}

// newProcGroup prepares cmd (before Start): no console window for the shell.
func newProcGroup(cmd *exec.Cmd) *procGroup {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return &procGroup{cmd: cmd}
}

// started assigns the freshly started shell to a new job object. Processes the shell
// creates from then on belong to the job. If the job cannot be set up, kill falls back
// to ending the shell process only (the pre-job behavior).
func (g *procGroup) started() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(g.cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer func() { _ = windows.CloseHandle(proc) }()
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	g.mu.Lock()
	g.job = job
	g.mu.Unlock()
}

// kill ends every process in the job (the shell and its descendants).
func (g *procGroup) kill() error {
	g.mu.Lock()
	job := g.job
	g.mu.Unlock()
	if job != 0 {
		return windows.TerminateJobObject(job, 1)
	}
	if g.cmd.Process == nil {
		return nil
	}
	return g.cmd.Process.Kill()
}

// release closes the job handle after cmd.Wait. The job is created without
// KILL_ON_JOB_CLOSE, so closing it leaves processes that outlived the shell untouched.
func (g *procGroup) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}
