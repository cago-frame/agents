package coding

import (
	"testing"
)

func TestGeneralPurposeTools_NoDispatchNoTask(t *testing.T) {
	tools := generalPurposeTools(".", nil, nil)
	for _, tt := range tools {
		switch tt.Name() {
		case "task_create", "task_list", "task_get", "task_update", "task_delete":
			t.Errorf("GP must not have %s", tt.Name())
		case "subagent":
			t.Errorf("GP must not have subagent")
		}
	}
	want := map[string]bool{
		"read": true, "write": true, "edit": true,
		"bash": true, "bash_output": true, "kill_shell": true,
		"grep": true, "find": true, "ls": true,
	}
	got := map[string]bool{}
	for _, tt := range tools {
		got[tt.Name()] = true
	}
	for n := range want {
		if !got[n] {
			t.Errorf("missing %s in GP tools", n)
		}
	}
}
