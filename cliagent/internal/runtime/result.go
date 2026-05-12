package runtime

import "github.com/cago-frame/agents/provider"

type Result struct {
	Text     string
	Stop     StopReason
	ThreadID string
	State    State
	Usage    provider.Usage
	History  []Message
}
