package coding

import (
	"testing"
)

func TestGeneralPurposeTools_NoDispatchNoTodo(t *testing.T) {
	tools := generalPurposeTools(".", nil, nil)
	for _, tt := range tools {
		switch tt.Name() {
		case "todo_write":
			t.Errorf("GP must not have todo_write")
		case "dispatch_subagent":
			t.Errorf("GP must not have dispatch_subagent")
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
