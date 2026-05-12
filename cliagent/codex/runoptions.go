package codex

// runOptCfg holds per-run overrides specific to the codex backend.
type runOptCfg struct {
	cwd       string
	sandbox   SandboxMode
	approval  ApprovalPolicy
	ephemeral *bool
	config    []string
	extraArgs []string
}

// RunOption is a per-run option for codex runs (Run / Stream / Send).
type RunOption func(*runOptCfg)

// RunCwd sets a per-run working directory override.
func RunCwd(path string) RunOption {
	return func(c *runOptCfg) { c.cwd = path }
}

// RunSandbox sets a per-run sandbox mode override.
func RunSandbox(mode SandboxMode) RunOption {
	return func(c *runOptCfg) { c.sandbox = mode }
}

// RunApproval sets a per-run approval policy override.
func RunApproval(policy ApprovalPolicy) RunOption {
	return func(c *runOptCfg) { c.approval = policy }
}

// RunEphemeral toggles ephemeral mode for a single run.
func RunEphemeral(v bool) RunOption {
	return func(c *runOptCfg) { c.ephemeral = &v }
}

// RunConfig appends a single Codex CLI config override (k=v form, TOML value).
func RunConfig(expr string) RunOption {
	return func(c *runOptCfg) {
		if expr != "" {
			c.config = append(c.config, expr)
		}
	}
}

// RunExtraArgs appends per-run extra argv.
func RunExtraArgs(args []string) RunOption {
	return func(c *runOptCfg) { c.extraArgs = append(c.extraArgs, args...) }
}
