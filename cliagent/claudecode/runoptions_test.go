package claudecode

import "testing"

func TestRunOptions_Cwd(t *testing.T) {
	var c runOptCfg
	RunCwd("/foo")(&c)
	if c.cwd != "/foo" {
		t.Fatalf("cwd = %q", c.cwd)
	}
}

func TestRunOptions_Permission(t *testing.T) {
	var c runOptCfg
	RunPermission(PermissionModeBypassPermissions)(&c)
	if c.permission != PermissionModeBypassPermissions {
		t.Fatalf("permission = %v", c.permission)
	}
}

func TestRunOptions_ExtraArgs(t *testing.T) {
	var c runOptCfg
	RunExtraArgs("--foo", "bar")(&c)
	if len(c.extraArgs) != 2 || c.extraArgs[0] != "--foo" {
		t.Fatalf("extraArgs = %v", c.extraArgs)
	}
}

func TestRunOptions_Multiple(t *testing.T) {
	var c runOptCfg
	RunCwd("/tmp")(&c)
	RunPermission(PermissionModeAcceptEdits)(&c)
	if c.cwd != "/tmp" {
		t.Fatalf("cwd = %q", c.cwd)
	}
	if c.permission != PermissionModeAcceptEdits {
		t.Fatalf("permission = %v", c.permission)
	}
}
