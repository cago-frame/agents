package tool

import agent "github.com/cago-frame/agents/agent"

// TextResult returns a ToolResultBlock with a single TextBlock containing the given text.
func TextResult(text string) *agent.ToolResultBlock {
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: text}}}
}

// ErrorResult returns a ToolResultBlock marked as an error with a single TextBlock
// containing the given error message.
func ErrorResult(text string) *agent.ToolResultBlock {
	return &agent.ToolResultBlock{IsError: true, Content: []agent.ContentBlock{agent.TextBlock{Text: text}}}
}

// MultimodalResult returns a ToolResultBlock containing the given ContentBlocks.
// This allows combining text, images, and other content types in a single result.
func MultimodalResult(blocks ...agent.ContentBlock) *agent.ToolResultBlock {
	return &agent.ToolResultBlock{Content: append([]agent.ContentBlock(nil), blocks...)}
}
