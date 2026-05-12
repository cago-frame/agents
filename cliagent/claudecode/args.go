package claudecode

import (
	"strconv"
	"strings"
)

// cliFlag 是 Claude Code CLI 的 argv flag 名。集中放这里以便和 CLI 契约逐字对齐;
// 用 typed string 让"这是 CLI 协议字面量"在调用点一眼可见。
type cliFlag string

// Claude Code CLI 当前在用的 argv flag。新增 flag 时同步加到这里,buildArgs +
// options.go 的 ExtraArgs helper 全部走该常量。
const (
	flagPrompt               cliFlag = "-p"
	flagOutputFormat         cliFlag = "--output-format"
	flagInputFormat          cliFlag = "--input-format"
	flagVerbose              cliFlag = "--verbose"
	flagModel                cliFlag = "--model"
	flagResume               cliFlag = "--resume"
	flagAppendSystemPrompt   cliFlag = "--append-system-prompt"
	flagPermissionMode       cliFlag = "--permission-mode"
	flagAllowedTools         cliFlag = "--allowedTools"
	flagDisallowedTools      cliFlag = "--disallowedTools"
	flagMaxTurns             cliFlag = "--max-turns"
	flagMCPConfig            cliFlag = "--mcp-config"
	flagSettings             cliFlag = "--settings"
	flagIncludePartialMsgs   cliFlag = "--include-partial-messages"
	flagDebug                cliFlag = "--debug"
	flagPermissionPromptTool cliFlag = "--permission-prompt-tool"
)

// formatStreamJSON 是 --output-format / --input-format 唯一接受的值。
const formatStreamJSON = "stream-json"

// runSpec holds the argv parameters for a claude CLI invocation. Used by buildArgs
// and by process.go to construct CLI arguments.
type runSpec struct {
	prompt                string
	model                 string
	cwd                   string
	env                   map[string]string
	systemPrompt          string
	permissionMode        PermissionMode
	interactivePermission bool
	allowedTools          []string
	disallowedTools       []string
	maxTurns              int
	resumeID              string
	mcpConfig             string
	settings              string
	extraArgs             []string
}

// buildArgs 把 runSpec 翻译为 claude CLI 的 argv（不含 binary 本身）。
func buildArgs(binary string, spec runSpec) []string {
	_ = binary // binary 由 exec.Command 持有，不进 argv
	// prompt 必须只走 stdin user frame；若同时把 spec.prompt 作为位置参数传给 claude，
	// CLI 会立即处理这条 prompt 并退出（不 emit system.init），触发 ErrInitTimeout。
	args := []string{
		string(flagPrompt),
		string(flagOutputFormat), formatStreamJSON,
		string(flagInputFormat), formatStreamJSON,
		string(flagVerbose),
		string(flagIncludePartialMsgs),
	}
	if spec.model != "" {
		args = append(args, string(flagModel), spec.model)
	}
	if spec.resumeID != "" {
		args = append(args, string(flagResume), spec.resumeID)
	}
	if spec.systemPrompt != "" {
		args = append(args, string(flagAppendSystemPrompt), spec.systemPrompt)
	}
	mode := spec.permissionMode
	if mode == "" {
		mode = PermissionModeAcceptEdits
	}
	args = append(args, string(flagPermissionMode), string(mode))
	if mode == PermissionModeDefault || spec.interactivePermission {
		args = append(args, string(flagPermissionPromptTool), "stdio")
	}
	if len(spec.allowedTools) > 0 {
		args = append(args, string(flagAllowedTools), strings.Join(spec.allowedTools, ","))
	}
	if len(spec.disallowedTools) > 0 {
		args = append(args, string(flagDisallowedTools), strings.Join(spec.disallowedTools, ","))
	}
	if spec.maxTurns > 0 {
		args = append(args, string(flagMaxTurns), strconv.Itoa(spec.maxTurns))
	}
	if spec.mcpConfig != "" {
		args = append(args, string(flagMCPConfig), spec.mcpConfig)
	}
	if spec.settings != "" {
		args = append(args, string(flagSettings), spec.settings)
	}
	if len(spec.extraArgs) > 0 {
		args = append(args, spec.extraArgs...)
	}
	return args
}
