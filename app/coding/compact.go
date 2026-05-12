package coding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
)

// CompactionResult describes one successful compaction.
type CompactionResult struct {
	Before  int
	After   int
	Trigger string
}

// CompactOptions configures Compact.
type CompactOptions struct {
	Hint       string    // forwarded to summarizer prompt
	Trigger    string    // "auto" | "manual" | "slash"; defaults to "manual"
	Now        time.Time // tests fix it
	KeepRecent int       // recent messages to retain verbatim; default 6
}

type CompactOption func(*CompactOptions)

func WithCompactHint(h string) CompactOption    { return func(o *CompactOptions) { o.Hint = h } }
func WithCompactTrigger(t string) CompactOption { return func(o *CompactOptions) { o.Trigger = t } }
func WithCompactKeepRecent(n int) CompactOption { return func(o *CompactOptions) { o.KeepRecent = n } }

// CompactSpec controls which provider/model the summarizer LLM uses.
// nil Provider = reuse parent provider; empty Model = parent model.
type CompactSpec struct {
	Provider provider.Provider
	Model    string
}

// compactStrategy implements compactor.Strategy. It is the single source of
// truth for both auto (TurnEnd hook) and manual (System.Compact / /compact) paths.
type compactStrategy struct {
	parentProv  provider.Provider
	parentModel string
	override    *CompactSpec

	threshold  int // 0 = no auto-trigger; only manual fires
	keepRecent int // default 6
	nowFn      func() time.Time

	// lastTrigger is set by manual paths before invoking Compact; auto path leaves "auto".
	lastTrigger atomic.Value // string
	lastHint    atomic.Value // string
}

func newCompactStrategy(parentProv provider.Provider, parentModel string, spec *CompactSpec, threshold int) *compactStrategy {
	s := &compactStrategy{
		parentProv:  parentProv,
		parentModel: parentModel,
		override:    spec,
		threshold:   threshold,
		keepRecent:  6,
		nowFn:       time.Now,
	}
	s.lastTrigger.Store("auto")
	s.lastHint.Store("")
	return s
}

// ShouldCompact gates the auto (TurnEnd) path. It returns true only when
// (a) a non-zero threshold is configured, (b) the most recent assistant
// Usage.PromptTokens exceeds it, AND (c) the precondition that Compact would
// otherwise gate on (history long enough after KeepRecent peel-back) holds.
// The precondition gate avoids producing errSkipCompact errors from the auto
// path, which compactor.WithStrategy would surface verbatim.
func (s *compactStrategy) ShouldCompact(conv *agent.Conversation) bool {
	if s.threshold <= 0 {
		return false
	}
	msgs := conv.Messages()
	tokenHit := false
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Usage != nil && msgs[i].Usage.PromptTokens >= s.threshold {
			tokenHit = true
			break
		}
	}
	if !tokenHit {
		return false
	}
	keep := s.keepRecent
	if keep <= 0 {
		keep = 6
	}
	startKeep := len(msgs) - keep
	if startKeep <= 0 {
		return false
	}
	for startKeep > 0 && isToolMessage(msgs[startKeep]) {
		startKeep--
	}
	return startKeep > 0
}

func (s *compactStrategy) Compact(ctx context.Context, conv *agent.Conversation) (agent.Message, error) {
	keep := s.keepRecent
	if keep <= 0 {
		keep = 6
	}
	msgs := conv.Messages()
	startKeep := len(msgs) - keep
	if startKeep <= 0 {
		return agent.Message{}, errSkipCompact
	}
	// Walk back over consecutive tool messages so we don't split a tool-call/tool-result pair.
	for startKeep > 0 && isToolMessage(msgs[startKeep]) {
		startKeep--
	}
	if startKeep <= 0 {
		return agent.Message{}, errSkipCompact
	}
	older := msgs[:startKeep]

	var priorSummary string
	if len(older) > 0 && isCompactionSummary(older[0]) {
		priorSummary = textOf(older[0])
		older = older[1:]
	}

	hint, _ := s.lastHint.Load().(string)
	summaryText, err := s.summarize(ctx, older, priorSummary, hint)
	if err != nil {
		return agent.Message{}, fmt.Errorf("summarize: %w", err)
	}
	return agent.Message{
		Role:      agent.RoleSystem,
		Content:   []agent.ContentBlock{agent.SummaryBlock{Text: summaryText}},
		CreatedAt: s.nowFn(),
	}, nil
}

// errSkipCompact is sentinel: returned when history is too short to compact.
// The manual path (compactor.compact) checks for this and returns nil result.
// The auto path (compactor.WithStrategy) is gated by ShouldCompact's matching
// precondition, so it never reaches Compact under skip conditions.
var errSkipCompact = errors.New("coding: skip compaction")

// isToolMessage returns true for the tool-result frames added by Runner after
// PostToolUse or for assistant frames whose only content is ToolUseBlock.
func isToolMessage(m agent.Message) bool {
	if m.Role == agent.RoleTool {
		return true
	}
	if m.Role == agent.RoleAssistant {
		for _, b := range m.Content {
			if _, ok := b.(agent.ToolUseBlock); ok {
				continue
			}
			return false
		}
		return len(m.Content) > 0
	}
	return false
}

// isCompactionSummary identifies an earlier compaction summary so the
// iterate-prior-summary path can fold it back into the next summary. A
// compaction summary is a RoleSystem message whose first block is a
// SummaryBlock — no marker prefix conventions, no string-sniffing.
func isCompactionSummary(m agent.Message) bool {
	if m.Role != agent.RoleSystem || len(m.Content) == 0 {
		return false
	}
	_, ok := m.Content[0].(agent.SummaryBlock)
	return ok
}

// textOf reads the text payload of a compaction-summary message. Falls back
// to TextBlock for messages produced outside this compactor (e.g. legacy
// transcripts being imported).
func textOf(m agent.Message) string {
	if len(m.Content) == 0 {
		return ""
	}
	switch v := m.Content[0].(type) {
	case agent.SummaryBlock:
		return v.Text
	case agent.TextBlock:
		return v.Text
	}
	return ""
}

func (s *compactStrategy) summarize(ctx context.Context, older []agent.Message, prior, hint string) (string, error) {
	prov := s.parentProv
	model := s.parentModel
	if s.override != nil {
		if s.override.Provider != nil {
			prov = s.override.Provider
		}
		if s.override.Model != "" {
			model = s.override.Model
		}
	}
	if prov == nil {
		return "", errors.New("compactor: no summarizer provider")
	}

	system := CompactionSummarizerPrompt
	if prior != "" {
		system = CompactionSummarizerIteratePrompt
	}
	if h := strings.TrimSpace(hint); h != "" {
		system += "\n\n# User Hint\n" + h
	}

	var convo strings.Builder
	if prior != "" {
		convo.WriteString("Previous Summary:\n")
		convo.WriteString(prior)
		convo.WriteString("\n\n")
	}
	convo.WriteString("Conversation to summarize:\n")
	for _, m := range older {
		convo.WriteString("[")
		convo.WriteString(string(m.Role))
		convo.WriteString("] ")
		for _, blk := range m.Content {
			switch v := blk.(type) {
			case agent.TextBlock:
				convo.WriteString(v.Text)
			case agent.SummaryBlock:
				convo.WriteString(v.Text)
			case agent.ToolUseBlock:
				convo.WriteString("(tool_call: ")
				convo.WriteString(v.Name)
				convo.WriteString(")")
			case agent.ToolResultBlock:
				convo.WriteString("(tool_result")
				for _, sub := range v.Content {
					if tb, ok := sub.(agent.TextBlock); ok {
						snippet := tb.Text
						if len(snippet) > 500 {
							snippet = snippet[:500] + "...[truncated]"
						}
						convo.WriteString(": ")
						convo.WriteString(snippet)
					}
				}
				convo.WriteString(")")
			}
		}
		convo.WriteString("\n")
	}

	resp, err := prov.ChatCompletion(ctx, &provider.CompletionRequest{
		Model: model,
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: system},
			{Role: provider.RoleUser, Content: convo.String()},
		},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// compactor wraps compactStrategy with the manual-compaction surface used by
// System.Compact and the /compact slash. It is **not** registered with the agent;
// only the Strategy is. Manual paths bypass ShouldCompact and apply Compact +
// history rewrite + result aggregation directly.
type compactor struct {
	strategy *compactStrategy
}

func newCompactor(parentProv provider.Provider, parentModel string, spec *CompactSpec, threshold int) *compactor {
	return &compactor{strategy: newCompactStrategy(parentProv, parentModel, spec, threshold)}
}

func (c *compactor) compact(ctx context.Context, conv *agent.Conversation, opts CompactOptions) (*CompactionResult, error) {
	if opts.KeepRecent <= 0 {
		opts.KeepRecent = 6
	}
	if opts.Trigger == "" {
		opts.Trigger = "manual"
	}
	c.strategy.lastTrigger.Store(opts.Trigger)
	c.strategy.lastHint.Store(opts.Hint)
	prevKeep := c.strategy.keepRecent
	c.strategy.keepRecent = opts.KeepRecent
	defer func() { c.strategy.keepRecent = prevKeep }()
	if !opts.Now.IsZero() {
		prev := c.strategy.nowFn
		c.strategy.nowFn = func() time.Time { return opts.Now }
		defer func() { c.strategy.nowFn = prev }()
	}

	summary, err := c.strategy.Compact(ctx, conv)
	if errors.Is(err, errSkipCompact) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	before := conv.Len()
	if err := conv.Truncate(0); err != nil {
		return nil, fmt.Errorf("truncate: %w", err)
	}
	conv.Append(summary)
	return &CompactionResult{Before: before, After: conv.Len(), Trigger: opts.Trigger}, nil
}
