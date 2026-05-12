package agent

import (
	"time"

	"github.com/cago-frame/agents/provider"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// PartialState records why an assistant message stopped short of its final
// form. The zero value (PartialNone) means the message is finalized.
type PartialState string

const (
	PartialNone       PartialState = ""
	PartialStreaming  PartialState = "streaming"
	PartialCancelled  PartialState = "canceled"
	PartialErrored    PartialState = "errored"
	PartialTokenLimit PartialState = "tokens_limit"
	PartialTimeout    PartialState = "timeout"
)

// Usage is reused from provider for round-trip safety.
type Usage = provider.Usage

type Message struct {
	Role          Role
	Content       []ContentBlock
	CreatedAt     time.Time
	PartialReason PartialState
	// PartialDetail is the human-readable detail that pairs with PartialReason.
	// Empty when PartialReason is PartialNone or when the detail is not
	// meaningful for that state. Typical contents:
	//   - PartialErrored:    err.Error() of the cause
	//   - PartialCancelled:  cancel reason supplied by the caller
	//   - PartialTimeout:    deadline message
	//   - PartialTokenLimit: provider-supplied truncation note (optional)
	// Like PartialReason, PartialDetail is metadata about the message — it is
	// never replayed into LLM input.
	PartialDetail string
	Usage         *Usage // assistant only; nil otherwise
}
