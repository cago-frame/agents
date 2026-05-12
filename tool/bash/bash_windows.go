//go:build windows

package bash

import "os/exec"

func setProcessGroup(_ *exec.Cmd) {}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
