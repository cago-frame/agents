package codex

type runSpec struct {
	binary   string
	prompt   string
	resumeID string

	model    string
	cwd      string
	env      map[string]string
	sandbox  SandboxMode
	approval ApprovalPolicy

	bypass    bool
	ephemeral bool

	config    []string
	extraArgs []string

	mcpServerName string
	mcpURL        string
	mcpTokenEnv   string
}
