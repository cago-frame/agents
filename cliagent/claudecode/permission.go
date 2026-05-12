package claudecode

// PermissionMode 描述 Claude Code CLI 的 --permission-mode 枚举值。
// 用于 WithPermissionMode 选项。
type PermissionMode string

// Claude Code CLI 的 --permission-mode 枚举值。
const (
	// PermissionModeDefault 每次调用工具均询问用户（-p 非交互下会阻塞/报错）。
	PermissionModeDefault PermissionMode = "default"
	// PermissionModeAcceptEdits 自动通过只读与 Edit/Write；Bash/WebFetch 等仍需权限。
	PermissionModeAcceptEdits PermissionMode = "acceptEdits"
	// PermissionModeBypassPermissions 跳过所有权限询问（脚本/自动化场景推荐）。
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
	// PermissionModePlan 进入 plan 模式，仅收集方案不实际执行写操作。
	PermissionModePlan PermissionMode = "plan"
)
