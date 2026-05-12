package agent

import (
	"context"

	"github.com/cago-frame/agents/provider"
)

// PreparedRequest returns a dry-run CompletionRequest that would be sent to the
// provider if a turn were run with the given text. It does not modify the
// conversation. If text is empty, returns a request built from the conversation
// as-is.
func (r *Runner) PreparedRequest(ctx context.Context, text string) (*provider.CompletionRequest, error) {
	if r.isClosed() {
		return nil, ErrRunnerClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	msgs := r.conv.Messages()
	if text != "" {
		msgs = append(msgs, Message{Role: RoleUser, Content: []ContentBlock{TextBlock{Text: text}}})
	}
	spec := RequestSpec{
		System:   r.agent.cfg.system,
		Extra:    r.agent.cfg.extraSystem,
		Messages: msgs,
		Tools:    r.agent.cfg.tools,
		Model:    r.agent.cfg.model,
		Thinking: r.agent.cfg.thinking,
	}
	return BuildRequest(spec), nil
}
