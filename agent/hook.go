package agent

import "context"

// Decision is used by lifecycle hooks (UserPromptSubmit) to express a
// disposition. Tool-dispatch decisions live on *ToolContext methods
// (Next / AbortWithDeny / AbortWithError) instead.
type Decision int

const (
	DecisionPass Decision = iota
	DecisionApprove
	DecisionDeny
)

// UserPromptSubmit: runs at the start of a Send/Resend turn; can deny or rewrite text.
type UserPromptInput struct {
	Text string
	Conv ConversationReader
}

type UserPromptOutput struct {
	Decision     Decision
	DenyReason   string
	ModifiedText string
}

type UserPromptHook func(ctx context.Context, in *UserPromptInput) (*UserPromptOutput, error)

// TurnEnd: notification at end of every turn; commonly used to attach compaction.
type TurnEndInput struct {
	Conv       ConversationReader
	StopReason StopReason
	Usage      *Usage
}

type TurnEndOutput struct {
	// EmittedEvents are forwarded by the runner after EventTurnEnd has been
	// emitted, in the order returned. Use this to surface plugin-driven
	// events such as EventCompacted from a TurnEnd hook without giving
	// plugins direct access to the runner's event sink.
	EmittedEvents []Event
}

type TurnEndHook func(ctx context.Context, in *TurnEndInput) (*TurnEndOutput, error)
