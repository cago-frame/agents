//go:build !windows

package bash

import "testing"

func TestDefaultShellUnix(t *testing.T) {
	if got := defaultShell(); got != "/bin/sh" {
		t.Fatalf("defaultShell() = %q", got)
	}
	if got := defaultArgsForShell(defaultShell()); len(got) != 1 || got[0] != "-c" {
		t.Fatalf("defaultArgsForShell(defaultShell()) = %#v", got)
	}
}
