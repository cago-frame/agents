package agent

import (
	"time"

	"github.com/cago-frame/agents/agent/blocks"
)

// DisplayMessage is the UI-facing projection of a canonical Message. It is
// the dual of provider.Message (which BuildRequest emits for the LLM):
// where BuildRequest filters by blocks.ToLLM, RenderForDisplay filters by
// blocks.ToUI and applies UI-specific transforms (DisplayText takes
// precedence over plain text, ToolUse partial state becomes an explicit
// ToolStatus, etc.).
//
// UI code should consume DisplayMessage rather than peek at raw
// ContentBlocks. That keeps "what do I render for a streaming
// ToolUseBlock?" / "do I show this MetadataBlock?" / "how do I order
// thinking vs text?" in one place instead of scattered across every
// frontend.
type DisplayMessage struct {
	Role      Role
	Segments  []DisplaySegment
	Partial   PartialState
	CreatedAt time.Time
	Usage     *Usage
}

// DisplaySegment is the sealed interface for variants in a DisplayMessage.
// Each segment is already deduplicated and resolved (e.g. a UI-only
// DisplayTextBlock has replaced the plain TextBlock it shadowed).
type DisplaySegment interface {
	displaySegment()
}

// DisplayText is the user-visible text of a message. When a DisplayTextBlock
// exists in the canonical Message it takes precedence over plain TextBlock —
// that's the whole point of having it. SourceLLM tells the UI whether the
// model would see the same string (useful for "raw" / "expanded" toggles).
type DisplayText struct {
	Text      string
	SourceLLM bool // true if this text was also sent to the LLM (TextBlock / SummaryBlock); false for UI-only forms
}

func (DisplayText) displaySegment() {}

// DisplayThinking is a chain-of-thought block. UIs typically render
// collapsed.
type DisplayThinking struct {
	Text string
}

func (DisplayThinking) displaySegment() {}

// ToolStatus is the human-readable lifecycle for a tool call as projected
// from ToolUseBlock.State (plus the presence of a paired ToolResultBlock).
type ToolStatus uint8

const (
	// ToolStatusPending means args are still streaming or didn't parse.
	ToolStatusPending ToolStatus = iota
	// ToolStatusReady means args parsed; no paired result has been observed
	// yet (dispatch may be in flight).
	ToolStatusReady
	// ToolStatusSuccess means a paired ToolResultBlock arrived without
	// IsError set.
	ToolStatusSuccess
	// ToolStatusError means a paired ToolResultBlock arrived with IsError.
	ToolStatusError
)

// DisplayToolCall fuses a ToolUseBlock with its paired ToolResultBlock (when
// available) so UI code does not have to walk across messages. Args is the
// parsed Input when ready, or the raw streaming buffer when not.
type DisplayToolCall struct {
	ID     string
	Name   string
	Args   any // parsed map[string]any (Ready) or RawArgs string (Pending)
	Status ToolStatus
	Result []DisplaySegment // projected from the paired ToolResultBlock, if any
}

func (DisplayToolCall) displaySegment() {}

// DisplayImage is the UI projection of an ImageBlock.
type DisplayImage struct {
	MediaType string
	URL       string
	Inline    []byte
}

func (DisplayImage) displaySegment() {}

// DisplayRef is the UI projection of a RefBlock (file / asset / tool ref).
type DisplayRef struct {
	Kind  string
	ID    string
	Label string
}

func (DisplayRef) displaySegment() {}

// DisplayNotice is the UI projection of a NoticeBlock (system notice).
type DisplayNotice struct {
	Level string
	Text  string
}

func (DisplayNotice) displaySegment() {}

// DisplaySummary is the UI projection of a SummaryBlock. UIs typically
// render it inline with a "summarized" badge so users can tell history was
// compacted.
type DisplaySummary struct {
	Text string
}

func (DisplaySummary) displaySegment() {}

// RenderForDisplay projects a single Message into DisplayMessage. It is a
// pure function: same input, same output, no I/O. UIs may call it
// per-message on every change (it is cheap) or once per Conversation
// snapshot.
//
// Pairing of ToolUseBlock with ToolResultBlock happens within the same
// Message — that mirrors how Anthropic/OpenAI lay out tool turns and how
// the runner records them. For cross-message pairing (rare; only when a
// caller hand-builds history), use RenderConversationForDisplay.
//
// Extensibility note: this projection knows how to render the built-in
// block variants. Third-party blocks registered via
// [github.com/cago-frame/agents/agent/blocks.Register] are silently
// dropped if their Audience includes ToUI but they are not in the
// switch below — callers integrating custom blocks should write their
// own projection over [Message] rather than retro-fitting cases here.
func RenderForDisplay(m Message) DisplayMessage {
	out := DisplayMessage{
		Role:      m.Role,
		Partial:   m.PartialReason,
		CreatedAt: m.CreatedAt,
		Usage:     m.Usage,
	}

	var pendingText string
	var displayText string
	hasDisplay := false

	flushText := func() {
		if hasDisplay {
			out.Segments = append(out.Segments, DisplayText{Text: displayText, SourceLLM: false})
			displayText = ""
			hasDisplay = false
			pendingText = ""
			return
		}
		if pendingText != "" {
			out.Segments = append(out.Segments, DisplayText{Text: pendingText, SourceLLM: true})
			pendingText = ""
		}
	}

	for _, b := range m.Content {
		if !b.Audience().Has(blocks.ToUI) {
			continue
		}
		switch v := b.(type) {
		case TextBlock:
			if pendingText != "" {
				pendingText += "\n"
			}
			pendingText += v.Text
		case DisplayTextBlock:
			if displayText != "" {
				displayText += "\n"
			}
			displayText += v.Text
			hasDisplay = true
		case SummaryBlock:
			flushText()
			out.Segments = append(out.Segments, DisplaySummary{Text: v.Text})
		case ThinkingBlock:
			flushText()
			out.Segments = append(out.Segments, DisplayThinking{Text: v.Text})
		case ToolUseBlock:
			flushText()
			tc := projectToolUse(v)
			tc.Result = pairedToolResultSegments(v.ID, m.Content)
			if len(tc.Result) > 0 && tc.Status == ToolStatusReady {
				tc.Status = ToolStatusSuccess
				if paired := findToolResult(v.ID, m.Content); paired != nil && paired.IsError {
					tc.Status = ToolStatusError
				}
			}
			out.Segments = append(out.Segments, tc)
		case ToolResultBlock:
			// Rendered inline as part of the paired DisplayToolCall above.
			// Standalone ToolResultBlocks (no matching ToolUseBlock in this
			// message) are surfaced so UI can still see them.
			if !hasMatchingToolUse(v.ToolUseID, m.Content) {
				flushText()
				out.Segments = append(out.Segments, DisplayToolCall{
					ID:     v.ToolUseID,
					Status: statusForResult(v),
					Result: projectResultContent(v.Content),
				})
			}
		case ImageBlock:
			flushText()
			out.Segments = append(out.Segments, DisplayImage{
				MediaType: v.MediaType,
				URL:       v.Source.URL,
				Inline:    v.Source.Inline,
			})
		case RefBlock:
			flushText()
			out.Segments = append(out.Segments, DisplayRef{Kind: v.Kind, ID: v.ID, Label: v.Label})
		case NoticeBlock:
			flushText()
			out.Segments = append(out.Segments, DisplayNotice{Level: v.Level, Text: v.Text})
		}
	}
	flushText()
	return out
}

// RenderConversationForDisplay is the multi-message counterpart that also
// links ToolResultBlocks living in a later (Role=Tool) message back to the
// ToolUseBlock that introduced them — the layout the runner actually
// produces.
func RenderConversationForDisplay(msgs []Message) []DisplayMessage {
	results := collectResults(msgs)
	out := make([]DisplayMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == RoleTool {
			// Tool messages are folded into their owning assistant message
			// via the results map; skip the standalone projection unless the
			// owning use is missing (which is a runner bug, but stay
			// defensive).
			if hasOwner(m, msgs) {
				continue
			}
		}
		dm := RenderForDisplay(m)
		for i, seg := range dm.Segments {
			tc, ok := seg.(DisplayToolCall)
			if !ok || len(tc.Result) > 0 {
				continue
			}
			if paired, found := results[tc.ID]; found {
				tc.Result = projectResultContent(paired.Content)
				if tc.Status == ToolStatusReady {
					if paired.IsError {
						tc.Status = ToolStatusError
					} else {
						tc.Status = ToolStatusSuccess
					}
				}
				dm.Segments[i] = tc
			}
		}
		out = append(out, dm)
	}
	return out
}

func projectToolUse(v ToolUseBlock) DisplayToolCall {
	tc := DisplayToolCall{ID: v.ID, Name: v.Name}
	switch v.State {
	case ToolUseReady:
		if v.Input != nil {
			tc.Args = v.Input
			tc.Status = ToolStatusReady
		} else {
			tc.Args = v.RawArgs
			tc.Status = ToolStatusPending
		}
	default:
		// Streaming or Malformed: surface RawArgs verbatim so UI can render
		// the partial JSON / show a "parsing failed" hint.
		tc.Args = v.RawArgs
		tc.Status = ToolStatusPending
	}
	return tc
}

func pairedToolResultSegments(useID string, contents []ContentBlock) []DisplaySegment {
	tr := findToolResult(useID, contents)
	if tr == nil {
		return nil
	}
	return projectResultContent(tr.Content)
}

func findToolResult(useID string, contents []ContentBlock) *ToolResultBlock {
	for _, c := range contents {
		if tr, ok := c.(ToolResultBlock); ok && tr.ToolUseID == useID {
			return &tr
		}
	}
	return nil
}

func hasMatchingToolUse(useID string, contents []ContentBlock) bool {
	for _, c := range contents {
		if tu, ok := c.(ToolUseBlock); ok && tu.ID == useID {
			return true
		}
	}
	return false
}

func statusForResult(r ToolResultBlock) ToolStatus {
	if r.IsError {
		return ToolStatusError
	}
	return ToolStatusSuccess
}

func projectResultContent(content []ContentBlock) []DisplaySegment {
	var out []DisplaySegment
	for _, c := range content {
		if !c.Audience().Has(blocks.ToUI) {
			continue
		}
		switch v := c.(type) {
		case TextBlock:
			out = append(out, DisplayText{Text: v.Text, SourceLLM: true})
		case DisplayTextBlock:
			out = append(out, DisplayText{Text: v.Text, SourceLLM: false})
		case ImageBlock:
			out = append(out, DisplayImage{MediaType: v.MediaType, URL: v.Source.URL, Inline: v.Source.Inline})
		case RefBlock:
			out = append(out, DisplayRef{Kind: v.Kind, ID: v.ID, Label: v.Label})
		case NoticeBlock:
			out = append(out, DisplayNotice{Level: v.Level, Text: v.Text})
		}
	}
	return out
}

func collectResults(msgs []Message) map[string]ToolResultBlock {
	out := map[string]ToolResultBlock{}
	for _, m := range msgs {
		for _, c := range m.Content {
			if tr, ok := c.(ToolResultBlock); ok {
				out[tr.ToolUseID] = tr
			}
		}
	}
	return out
}

func hasOwner(toolMsg Message, all []Message) bool {
	for _, c := range toolMsg.Content {
		tr, ok := c.(ToolResultBlock)
		if !ok {
			continue
		}
		for _, m := range all {
			for _, b := range m.Content {
				if tu, ok := b.(ToolUseBlock); ok && tu.ID == tr.ToolUseID {
					return true
				}
			}
		}
	}
	return false
}
