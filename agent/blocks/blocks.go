// Package blocks owns the canonical ContentBlock interface and every block
// variant flowing through a Message. Each block declares both its on-wire
// Type() (the JSON discriminator used by the registry) and its Audience()
// (which projections — UI / LLM — include it).
//
// Audience is the single mechanism that resolves the recurring "UI shows
// one thing, LLM sees another" divergence: the
// canonical Message is unchanged, but agent.BuildRequest / agent.RenderForDisplay
// each filter by AudienceMask and produce a different view. There is no
// separate "side channel" — every block, including ones
// previously expressed as MetadataBlock{Key: ...}, is a first-class typed
// value with a well-defined audience.
package blocks

// ContentBlock is the sealed interface implemented by every message block.
// New variants must:
//   - return a stable Type() string (used by the registry for JSON decode);
//   - declare an Audience() mask;
//   - register themselves in init() via blocks.RegisterFactory.
type ContentBlock interface {
	Type() string
	Audience() AudienceMask
}

// TextBlock is plain UTF-8 text. Visible to all consumers.
type TextBlock struct {
	Text string `json:"text"`
}

func (TextBlock) Type() string           { return "text" }
func (TextBlock) Audience() AudienceMask { return ToAll }

// BlobSource is either a URL reference or inline bytes; mutually exclusive.
type BlobSource struct {
	URL    string `json:"url,omitempty"`
	Inline []byte `json:"inline,omitempty"`
}

// ImageBlock carries image data alongside text. Visible to all consumers.
type ImageBlock struct {
	MediaType string     `json:"media_type"`
	Source    BlobSource `json:"source"`
}

func (ImageBlock) Type() string           { return "image" }
func (ImageBlock) Audience() AudienceMask { return ToAll }

// ToolUseState discriminates the lifecycle of a streamed ToolUseBlock.
//
// Zero value is ToolUseReady, so plain literals like
// ToolUseBlock{ID: ..., Name: ..., Input: ...} are usable directly.
type ToolUseState uint8

const (
	// ToolUseReady means Input is fully parsed and the call can be dispatched
	// or replayed to the LLM verbatim.
	ToolUseReady ToolUseState = iota
	// ToolUseStreaming means the args buffer is still growing; Input may be
	// nil or partial. UIs may render the partial RawArgs; BuildRequest skips
	// it (a malformed tool_call without a paired result would be rejected).
	ToolUseStreaming
	// ToolUseMalformed means the stream finished but RawArgs failed to parse.
	// BuildRequest skips it for the same reason as ToolUseStreaming.
	ToolUseMalformed
)

// ToolUseBlock is an assistant-side tool invocation.
//
// RawArgs holds the raw streaming JSON buffer so partial assistant messages
// remain renderable mid-stream. When State == ToolUseReady, Input is the
// parsed form of RawArgs and downstream consumers may use it directly.
type ToolUseBlock struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Input   map[string]any `json:"input,omitempty"`
	RawArgs string         `json:"raw_args,omitempty"`
	State   ToolUseState   `json:"state,omitempty"`
}

func (ToolUseBlock) Type() string           { return "tool_use" }
func (ToolUseBlock) Audience() AudienceMask { return ToAll }

// ToolResultBlock is the tool-side reply to a ToolUseBlock.
// Content is a heterogeneous slice; JSON round-trip is handled by custom
// MarshalJSON / UnmarshalJSON below so nested ContentBlocks keep their types.
type ToolResultBlock struct {
	ToolUseID string         `json:"tool_use_id"`
	Content   []ContentBlock `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

func (ToolResultBlock) Type() string           { return "tool_result" }
func (ToolResultBlock) Audience() AudienceMask { return ToAll }

// ThinkingBlock is the model's chain-of-thought. Replayed to the LLM
// (Anthropic requires the Signature for round-trip); UIs hide it by default.
type ThinkingBlock struct {
	Text      string `json:"text"`
	Signature string `json:"signature,omitempty"`
}

func (ThinkingBlock) Type() string           { return "thinking" }
func (ThinkingBlock) Audience() AudienceMask { return ToLLM }

// DisplayTextBlock is a UI-only text variant. Used when the form a user
// types (or that we want the human to see) diverges from what the LLM
// receives — e.g. an "@srv1 status" mention whose body is expanded for
// the model. Stripped before BuildRequest reaches the chat request.
type DisplayTextBlock struct {
	Text string `json:"text"`
}

func (DisplayTextBlock) Type() string           { return "display_text" }
func (DisplayTextBlock) Audience() AudienceMask { return ToUI }

// RefBlock points to an external artifact (file path, asset id, tool ref).
// UI renders it as a pill / link; the LLM never sees it directly (the
// upstream code is expected to inline whatever the model needs as TextBlock).
type RefBlock struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

func (RefBlock) Type() string           { return "ref" }
func (RefBlock) Audience() AudienceMask { return ToUI }

// NoticeBlock is a UI-only system notice (e.g. "compaction in progress",
// "retrying"). Kept for UI replay, never sent to the LLM.
type NoticeBlock struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

func (NoticeBlock) Type() string           { return "notice" }
func (NoticeBlock) Audience() AudienceMask { return ToUI }

// SummaryBlock is the compactor's output: a condensed summary that
// replaces truncated history. Travels through both built-in projections so
// the LLM sees the summary and UI can render it specially.
type SummaryBlock struct {
	Text string `json:"text"`
}

func (SummaryBlock) Type() string           { return "summary" }
func (SummaryBlock) Audience() AudienceMask { return ToAll }

func init() {
	RegisterFactory[TextBlock]()
	RegisterFactory[ImageBlock]()
	RegisterFactory[ToolUseBlock]()
	RegisterFactory[ToolResultBlock]()
	RegisterFactory[ThinkingBlock]()
	RegisterFactory[DisplayTextBlock]()
	RegisterFactory[RefBlock]()
	RegisterFactory[NoticeBlock]()
	RegisterFactory[SummaryBlock]()
}
