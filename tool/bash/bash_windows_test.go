//go:build windows

package bash

import (
	"os/exec"
	"testing"
)

func TestDefaultShellWindows(t *testing.T) {
	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)

	if got := defaultShell(); got != `C:\Windows\System32\cmd.exe` {
		t.Fatalf("defaultShell() = %q", got)
	}
	if got := defaultArgsForShell(defaultShell()); len(got) != 3 || got[0] != "/d" || got[1] != "/s" || got[2] != "/c" {
		t.Fatalf("defaultArgsForShell(defaultShell()) = %#v", got)
	}
}

func TestDefaultShellWindowsFallback(t *testing.T) {
	t.Setenv("COMSPEC", "")

	if got := defaultShell(); got != "cmd.exe" {
		t.Fatalf("defaultShell() = %q", got)
	}
}

func TestDefaultArgsForShellWindows(t *testing.T) {
	tests := []struct {
		shell string
		want  []string
	}{
		{shell: `C:\Windows\System32\cmd.exe`, want: []string{"/d", "/s", "/c"}},
		{shell: "powershell.exe", want: []string{"-NoProfile", "-Command"}},
		{shell: "pwsh.exe", want: []string{"-NoProfile", "-Command"}},
		{shell: `C:\Program Files\Git\bin\bash.exe`, want: []string{"-c"}},
	}

	for _, tt := range tests {
		got := defaultArgsForShell(tt.shell)
		if len(got) != len(tt.want) {
			t.Fatalf("defaultArgsForShell(%q) = %#v", tt.shell, got)
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Fatalf("defaultArgsForShell(%q) = %#v", tt.shell, got)
			}
		}
	}
}

func TestSetProcessGroupDisablesConsoleWindow(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit 0")

	setProcessGroup(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
}
