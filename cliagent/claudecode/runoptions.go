package claudecode

// runOptCfg holds per-run overrides specific to the claudecode backend.
type runOptCfg struct {
	cwd                   string
	permission            PermissionMode
	interactivePermission bool
	extraArgs             []string
}

// RunOption is a per-run option for claudecode runs (Run / Stream / Send).
type RunOption func(*runOptCfg)

// RunCwd sets a per-run working directory override.
func RunCwd(path string) RunOption {
	return func(c *runOptCfg) { c.cwd = path }
}

// RunPermission sets a per-run --permission-mode override.
func RunPermission(m PermissionMode) RunOption {
	return func(c *runOptCfg) { c.permission = m }
}

// RunExtraArgs appends per-run extra argv.
func RunExtraArgs(args ...string) RunOption {
	return func(c *runOptCfg) { c.extraArgs = append(c.extraArgs, args...) }
}

// RunInteractivePermission enables per-run --permission-prompt-tool stdio.
func RunInteractivePermission() RunOption {
	return func(c *runOptCfg) { c.interactivePermission = true }
}
