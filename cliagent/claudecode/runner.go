package claudecode

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// Runner is the public claudecode entry point.
//
// New(opts ...) builds a Runner; Runner.Session creates a Session; Session.Stream
// drives a single turn and returns a Stream of events. The implementation drives
// the local Claude Code CLI via the cliagent/internal/runtime package — no agent
// dependency.
type Runner struct {
	cfg backendCfg
	pm  *processManager
}

// New constructs a Runner from a slice of Options.
func New(opts ...Option) *Runner {
	cfg := defaultBackendCfg()
	for _, o := range opts {
		o(&cfg)
	}
	return &Runner{cfg: cfg, pm: newProcessManager(&cfg)}
}

// Session opens a multi-turn conversation. id is generated when not specified.
func (r *Runner) Session(opts ...SessionOption) *Session {
	so := sessionOptCfg{}
	for _, o := range opts {
		o(&so)
	}
	if so.id == "" {
		so.id = newSessionID()
	}
	rs := runtime.NewSession(so.id, so.store)
	return &Session{rt: rs, runner: r}
}

// Run starts an ephemeral session, runs one turn and returns the aggregate Result.
func (r *Runner) Run(ctx context.Context, prompt string, opts ...RunOption) (*Result, error) {
	sess := r.Session()
	return sess.Send(ctx, prompt, opts...)
}

// Text drives Run and returns just the final text.
func (r *Runner) Text(ctx context.Context, prompt string, opts ...RunOption) (string, error) {
	res, err := r.Run(ctx, prompt, opts...)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

// Stream starts an ephemeral session and returns the live Stream.
func (r *Runner) Stream(ctx context.Context, prompt string, opts ...RunOption) (*Stream, error) {
	sess := r.Session()
	return sess.Stream(ctx, prompt, opts...)
}

// Close releases all backend resources owned by the Runner.
func (r *Runner) Close(ctx context.Context) error { return r.pm.Close(ctx) }

// newSessionID returns a random hex-formatted session id.
func newSessionID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return "cc-" + hex.EncodeToString(buf[:])
}
