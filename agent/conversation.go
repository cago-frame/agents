package agent

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// ConversationReader is the read-only view of a Conversation.
// Hooks and tool implementations receive this interface, not *Conversation,
// so they cannot accidentally mutate history.
type ConversationReader interface {
	ID() string
	Len() int
	Messages() []Message
	MessageAt(index int) (Message, error)
	BranchedFrom() (BranchInfo, bool)
}

type BranchInfo struct {
	ParentConvID string
	ParentIndex  int
}

type ConvOption func(*convConfig)

type convConfig struct {
	id           string
	branchedFrom *BranchInfo
}

func WithConvID(id string) ConvOption {
	return func(c *convConfig) { c.id = id }
}

func WithBranchedFrom(p BranchInfo) ConvOption {
	return func(c *convConfig) { c.branchedFrom = &p }
}

type Conversation struct {
	mu           sync.RWMutex
	id           string
	messages     []Message
	branchedFrom *BranchInfo

	// populated lazily on first Watch()
	watchMu   sync.Mutex
	watchSubs []*watchSubscription
	version   int64

	// runnerLock is a separate mutex from c.mu so that Watch / read methods
	// remain non-blocking while a Runner is active.
	runnerLock   sync.Mutex
	runnerActive bool
}

func NewConversation(opts ...ConvOption) *Conversation {
	cfg := &convConfig{}
	for _, o := range opts {
		o(cfg)
	}
	id := cfg.id
	if id == "" {
		id = newConvID()
	}
	return &Conversation{
		id:           id,
		branchedFrom: cfg.branchedFrom,
	}
}

func LoadConversation(id string, msgs []Message, opts ...ConvOption) *Conversation {
	c := NewConversation(append([]ConvOption{WithConvID(id)}, opts...)...)
	c.messages = append(c.messages, msgs...)
	return c
}

func (c *Conversation) ID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.id
}

func (c *Conversation) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.messages)
}

func (c *Conversation) MessageAt(index int) (Message, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if index < 0 || index >= len(c.messages) {
		return Message{}, ErrIndexOutOfRange
	}
	return cloneMessage(c.messages[index]), nil
}

func (c *Conversation) Messages() []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Message, len(c.messages))
	for i, m := range c.messages {
		out[i] = cloneMessage(m)
	}
	return out
}

func (c *Conversation) Append(msg Message) int {
	c.mu.Lock()
	c.messages = append(c.messages, cloneMessage(msg))
	idx := len(c.messages) - 1
	c.mu.Unlock()

	c.broadcast(Change{
		Kind:    ChangeAppended,
		Index:   idx,
		Message: clonePtr(&msg),
	})
	return idx
}

func (c *Conversation) Truncate(index int) error {
	c.mu.Lock()
	if index < 0 || index > len(c.messages) {
		c.mu.Unlock()
		return ErrIndexOutOfRange
	}
	if index == len(c.messages) {
		c.mu.Unlock()
		return nil // no-op
	}
	c.messages = c.messages[:index]
	c.mu.Unlock()

	c.broadcast(Change{Kind: ChangeTruncated, Index: index})
	return nil
}

func (c *Conversation) BranchedFrom() (BranchInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.branchedFrom == nil {
		return BranchInfo{}, false
	}
	return *c.branchedFrom, true
}

func (c *Conversation) BranchFrom(index int) (*Conversation, error) {
	c.mu.RLock()
	if index < 0 || index > len(c.messages) {
		c.mu.RUnlock()
		return nil, ErrIndexOutOfRange
	}
	parentID := c.id
	cloned := make([]Message, index)
	for i := 0; i < index; i++ {
		cloned[i] = cloneMessage(c.messages[i])
	}
	c.mu.RUnlock()

	return &Conversation{
		id:       newConvID(),
		messages: cloned,
		branchedFrom: &BranchInfo{
			ParentConvID: parentID,
			ParentIndex:  index,
		},
	}, nil
}

// cloneMessage performs a shallow copy of slices/maps that callers might mutate.
// ContentBlock values are immutable by convention so no deep-copy is needed for them.
func cloneMessage(m Message) Message {
	cp := m
	if m.Content != nil {
		cp.Content = append([]ContentBlock(nil), m.Content...)
	}
	if m.Usage != nil {
		u := *m.Usage
		cp.Usage = &u
	}
	return cp
}

func clonePtr(m *Message) *Message {
	cm := cloneMessage(*m)
	return &cm
}

func newConvID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return "conv-" + hex.EncodeToString(buf[:])
}

func (c *Conversation) appendDeltaToPartial(index int, delta string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.messages) {
		return
	}
	msg := &c.messages[index]
	// Per spec §4.2: streaming content growth does NOT emit a Change.
	if n := len(msg.Content); n > 0 {
		if tb, ok := msg.Content[n-1].(TextBlock); ok {
			msg.Content[n-1] = TextBlock{Text: tb.Text + delta}
			return
		}
	}
	msg.Content = append(msg.Content, TextBlock{Text: delta})
}

// appendThinkingDeltaToPartial extends the trailing ThinkingBlock if the most
// recent block is one, otherwise appends a new ThinkingBlock. Signature is
// taken from the latest non-empty value (Anthropic emits it once at the end).
func (c *Conversation) appendThinkingDeltaToPartial(index int, delta, signature string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.messages) {
		return
	}
	msg := &c.messages[index]
	if n := len(msg.Content); n > 0 {
		if tb, ok := msg.Content[n-1].(ThinkingBlock); ok {
			sig := tb.Signature
			if signature != "" {
				sig = signature
			}
			msg.Content[n-1] = ThinkingBlock{Text: tb.Text + delta, Signature: sig}
			return
		}
	}
	msg.Content = append(msg.Content, ThinkingBlock{Text: delta, Signature: signature})
}

// upsertPartialToolUse replaces the ToolUseBlock with matching ID, or appends
// it when not yet present. Used to mirror tool-call streaming state into the
// partial assistant message so callers can render in-progress
// tool invocations even before the args JSON is complete.
//
// IDs are required for matching; deltas without an ID are no-ops.
func (c *Conversation) upsertPartialToolUse(index int, tu ToolUseBlock) {
	if tu.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.messages) {
		return
	}
	msg := &c.messages[index]
	for i, b := range msg.Content {
		if existing, ok := b.(ToolUseBlock); ok && existing.ID == tu.ID {
			msg.Content[i] = tu
			return
		}
	}
	msg.Content = append(msg.Content, tu)
}

func (c *Conversation) finalizePartial(index int, reason PartialState, detail string, usage *Usage) {
	c.mu.Lock()
	if index < 0 || index >= len(c.messages) {
		c.mu.Unlock()
		return
	}
	msg := &c.messages[index]
	msg.PartialReason = reason
	msg.PartialDetail = detail
	if usage != nil {
		u := *usage
		msg.Usage = &u
	}
	snapshot := cloneMessage(*msg)
	c.mu.Unlock()
	c.broadcast(Change{Kind: ChangeFinalized, Index: index, Message: &snapshot})
}

func (c *Conversation) appendToolUsesToPartial(index int, uses []ToolUseBlock) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.messages) {
		return
	}
	msg := &c.messages[index]
	for _, u := range uses {
		// If this tool_use was already streamed in via upsertPartialToolUse
		// (matching ID), replace in place to upgrade RawArgs-only state to
		// fully-parsed Input.
		replaced := false
		if u.ID != "" {
			for i, b := range msg.Content {
				if existing, ok := b.(ToolUseBlock); ok && existing.ID == u.ID {
					msg.Content[i] = u
					replaced = true
					break
				}
			}
		}
		if !replaced {
			msg.Content = append(msg.Content, u)
		}
	}
}

func (c *Conversation) acquireRunner() error {
	c.runnerLock.Lock()
	defer c.runnerLock.Unlock()
	if c.runnerActive {
		return ErrConversationBusy
	}
	c.runnerActive = true
	return nil
}

func (c *Conversation) releaseRunner() {
	c.runnerLock.Lock()
	c.runnerActive = false
	c.runnerLock.Unlock()
}
