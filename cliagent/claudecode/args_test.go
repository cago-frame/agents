package claudecode

import (
	"reflect"
	"testing"
)

func TestBuildArgs_Minimal(t *testing.T) {
	got := buildArgs("claude", runSpec{prompt: "hi"})
	want := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--permission-mode", string(PermissionModeAcceptEdits),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestBuildArgs_FullOptions(t *testing.T) {
	spec := runSpec{
		prompt:          "p",
		model:           "claude-sonnet-4-6",
		systemPrompt:    "sys",
		permissionMode:  PermissionModeBypassPermissions,
		allowedTools:    []string{"Read", "Grep"},
		disallowedTools: []string{"Bash"},
		maxTurns:        7,
		resumeID:        "uuid-1",
		mcpConfig:       `{"mcpServers":{}}`,
		settings:        `{"hooks":{}}`,
		extraArgs:       []string{"--debug"},
	}
	got := buildArgs("claude", spec)
	want := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--model", "claude-sonnet-4-6",
		"--resume", "uuid-1",
		"--append-system-prompt", "sys",
		"--permission-mode", string(PermissionModeBypassPermissions),
		"--allowedTools", "Read,Grep",
		"--disallowedTools", "Bash",
		"--max-turns", "7",
		"--mcp-config", `{"mcpServers":{}}`,
		"--settings", `{"hooks":{}}`,
		"--debug",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

// prompt 必须只通过 stdin user frame 传，不能作为位置参数。
// 否则 claude 会立即处理 prompt 后退出而不 emit system.init。
func TestBuildArgs_PromptNotPositional(t *testing.T) {
	args := buildArgs("claude", runSpec{prompt: "hi"})
	if !containsPair(args, "--input-format", "stream-json") {
		t.Fatalf("argv missing --input-format stream-json: %v", args)
	}
	if !containsPair(args, "--output-format", "stream-json") {
		t.Fatalf("argv missing --output-format stream-json: %v", args)
	}
	for _, a := range args {
		if a == "hi" {
			t.Fatalf("argv must not include prompt as positional arg: %v", args)
		}
	}
}

// containsPair 检查 args 里有连续的 "flag" "value"。
func containsPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestBuildArgs_DefaultPermissionAddsPromptTool(t *testing.T) {
	args := buildArgs("claude", runSpec{
		prompt:         "hi",
		permissionMode: PermissionModeDefault,
	})
	if !containsPair(args, "--permission-prompt-tool", "stdio") {
		t.Fatalf("expected --permission-prompt-tool stdio for default mode: %v", args)
	}
}

func TestBuildArgs_AcceptEditsNoPromptTool(t *testing.T) {
	args := buildArgs("claude", runSpec{
		prompt:         "hi",
		permissionMode: PermissionModeAcceptEdits,
	})
	for _, a := range args {
		if a == "--permission-prompt-tool" {
			t.Fatalf("should not have --permission-prompt-tool for acceptEdits: %v", args)
		}
	}
}

func TestBuildArgs_InteractivePermissionFlag(t *testing.T) {
	args := buildArgs("claude", runSpec{
		prompt:                "hi",
		permissionMode:        PermissionModeBypassPermissions,
		interactivePermission: true,
	})
	if !containsPair(args, "--permission-prompt-tool", "stdio") {
		t.Fatalf("expected --permission-prompt-tool stdio with interactivePermission: %v", args)
	}
	if !containsPair(args, "--permission-mode", string(PermissionModeBypassPermissions)) {
		t.Fatalf("expected --permission-mode bypassPermissions: %v", args)
	}
}
