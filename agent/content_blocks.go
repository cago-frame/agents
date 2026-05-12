package agent

import "github.com/cago-frame/agents/agent/blocks"

// ContentBlock is the canonical interface implemented by every Message
// content variant. Each block declares both its on-wire Type() (the JSON
// discriminator used by the blocks registry) and its Audience() (which
// projections — UI / LLM — include it).
//
// Audience is the single mechanism that keeps the downstream
// projections in sync: agent.BuildRequest filters by blocks.ToLLM,
// and agent.RenderForDisplay filters by blocks.ToUI. There is no separate
// side-channel mechanism — every block, including ones previously expressed
// as MetadataBlock, is a first-class typed value with a well-defined audience.
type ContentBlock = blocks.ContentBlock

// Re-exports of the canonical block types. The concrete definitions live
// in agent/blocks alongside the registry; aliases here keep call sites
// terse (agent.TextBlock vs blocks.TextBlock) without duplicating the
// type identity.
type (
	TextBlock        = blocks.TextBlock
	BlobSource       = blocks.BlobSource
	ImageBlock       = blocks.ImageBlock
	ToolUseBlock     = blocks.ToolUseBlock
	ToolResultBlock  = blocks.ToolResultBlock
	ThinkingBlock    = blocks.ThinkingBlock
	DisplayTextBlock = blocks.DisplayTextBlock
	RefBlock         = blocks.RefBlock
	NoticeBlock      = blocks.NoticeBlock
	SummaryBlock     = blocks.SummaryBlock
)

// ToolUseState re-exports the lifecycle discriminator for ToolUseBlock.
type ToolUseState = blocks.ToolUseState

// Re-exported ToolUseState constants.
const (
	ToolUseReady     = blocks.ToolUseReady
	ToolUseStreaming = blocks.ToolUseStreaming
	ToolUseMalformed = blocks.ToolUseMalformed
)

// AudienceMask re-exports the bitmask type from agent/blocks.
type AudienceMask = blocks.AudienceMask

// Audience constants are re-exported for convenience.
const (
	AudienceUI  = blocks.ToUI
	AudienceLLM = blocks.ToLLM
	AudienceAll = blocks.ToAll
)
