package agent

type SendOption func(*sendConfig)

type sendConfig struct {
	text         string
	extraBlocks  []ContentBlock
	customBlocks []ContentBlock
}

func WithImage(img ImageBlock) SendOption {
	return func(c *sendConfig) { c.extraBlocks = append(c.extraBlocks, img) }
}

// WithBlocks sets the user message content explicitly. Mutually exclusive
// with non-empty text passed to Send.
func WithBlocks(blocks ...ContentBlock) SendOption {
	return func(c *sendConfig) {
		c.customBlocks = append([]ContentBlock(nil), blocks...)
	}
}

// WithSendDisplay attaches a DisplayTextBlock to the user message being
// sent. The block is kept in the Conversation message and visible to UI
// projections (and Hooks via ConversationReader), but its Audience
// excludes blocks.ToLLM so BuildRequest never forwards it to the model.
// Use this when the chat UI wants to show the user's raw input
// (e.g. "@srv1 status") while the LLM receives a different body (the
// mention-expanded form passed as the `text` argument to Send).
//
// Multiple WithSendDisplay calls each append a separate block; consumers
// typically render the first one.
func WithSendDisplay(displayText string) SendOption {
	return func(c *sendConfig) {
		c.extraBlocks = append(c.extraBlocks, DisplayTextBlock{Text: displayText})
	}
}

// buildUserContent assembles the final content list per the rules in spec §4.3.
// Returns ErrIncompatibleOption if non-empty text and WithBlocks both used.
func buildUserContent(text string, opts []SendOption) ([]ContentBlock, error) {
	cfg := &sendConfig{text: text}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.customBlocks != nil && cfg.text != "" {
		return nil, ErrIncompatibleOption
	}
	if cfg.customBlocks != nil {
		return cfg.customBlocks, nil
	}
	var out []ContentBlock
	if cfg.text != "" {
		out = append(out, TextBlock{Text: cfg.text})
	}
	out = append(out, cfg.extraBlocks...)
	return out, nil
}
