package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
	"github.com/cago-frame/agents/provider"
)

// ─── stream-json wire literals ────────────────────────────────────────────────

// frameType 是 Claude Code stream-json 顶层 "type" 字段的枚举值。
type frameType string

const (
	frameTypeSystem         frameType = "system"
	frameTypeStreamEvent    frameType = "stream_event"
	frameTypeAssistant      frameType = "assistant"
	frameTypeUser           frameType = "user"
	frameTypeResult         frameType = "result"
	frameTypeControlRequest frameType = "control_request"
)

// systemSubtypeInit 是 system.subtype 唯一会触发 SessionStart 的取值。
const systemSubtypeInit = "init"

// controlSubtypeCanUseTool 是 control_request.subtype 枚举值,表示 CLI 请求
// 工具调用权限(--permission-prompt-tool stdio 模式)。
const controlSubtypeCanUseTool = "can_use_tool"

// resultSubtype 是 result frame 的 subtype 枚举值,决定整轮 Stop reason。
type resultSubtype string

const (
	resultSubtypeSuccess              resultSubtype = "success"
	resultSubtypeErrorMaxTurns        resultSubtype = "error_max_turns"
	resultSubtypeErrorDuringExecution resultSubtype = "error_during_execution"
)

// streamDeltaType 是 stream_event.delta.type(--include-partial-messages 下的细粒度 delta)。
type streamDeltaType string

const (
	streamDeltaText     streamDeltaType = "text_delta"
	streamDeltaThinking streamDeltaType = "thinking_delta"
)

// contentPartType 是 assistant / user / transcript message 的 content[*].type 字段值。
type contentPartType string

const (
	contentPartText       contentPartType = "text"
	contentPartThinking   contentPartType = "thinking"
	contentPartToolUse    contentPartType = "tool_use"
	contentPartToolResult contentPartType = "tool_result"
)

// mcpToolPrefix 是 Claude Code 用来标识 MCP server 工具的 ToolName 前缀
// (full name = "mcp__<server>__<tool>")。
const mcpToolPrefix = "mcp__"

// ─── raw protocol types ───────────────────────────────────────────────────────

// rawEvent is the weak-typed top-level JSON object from Claude Code stream-json.
type rawEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// system init
	SessionID string `json:"session_id,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`

	// assistant / user message
	Message *rawMessage `json:"message,omitempty"`

	// stream_event (--include-partial-messages)
	Event           *rawStreamEvent `json:"event,omitempty"`
	ParentToolUseID string          `json:"parent_tool_use_id,omitempty"`

	// result
	Usage *rawUsage `json:"usage,omitempty"`
	Error string    `json:"error,omitempty"`

	// control_request (--permission-prompt-tool stdio)
	ID        string          `json:"id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
}

type rawStreamEvent struct {
	Type  string          `json:"type"`
	Index int             `json:"index,omitempty"`
	Delta *rawStreamDelta `json:"delta,omitempty"`
}

type rawStreamDelta struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type rawMessage struct {
	ID      string           `json:"id"`
	Content []rawContentPart `json:"content"`
}

type rawContentPart struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// parseLine parses one stream-json line into a rawEvent.
func parseLine(line string) (rawEvent, error) {
	var ev rawEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return rawEvent{}, fmt.Errorf("parse: %w", err)
	}
	if ev.Type == "" {
		return rawEvent{}, fmt.Errorf("parse: missing type field")
	}
	return ev, nil
}

// toProviderUsage converts a rawUsage pointer to provider.Usage.
func toProviderUsage(u *rawUsage) provider.Usage {
	if u == nil {
		return provider.Usage{}
	}
	return provider.Usage{
		PromptTokens:        u.InputTokens,
		CompletionTokens:    u.OutputTokens,
		CachedTokens:        u.CacheReadInputTokens,
		CacheCreationTokens: u.CacheCreationInputTokens,
		TotalTokens:         u.InputTokens + u.OutputTokens,
	}
}

// toolCallState tracks a pending tool_use call until the tool_result arrives.
type toolCallState struct {
	Name string
	// ParentID 来自 raw event 的 parent_tool_use_id;tool_result 出现时回填到 PostToolUse。
	ParentID string
	// Source 在 tool_use 时分类好,tool_result 时直接复用,避免再用名字查一遍。
	Source runtime.ToolSource
}

// translateState holds mutable parsing state across lines.
type translateState struct {
	sessionID     string
	partialActive bool
	toolCalls     map[string]toolCallState
}

// translateStream reads stream-json from r and emits runtime.Event values to out.
// registered is the set of MCP-bridge tool names; currently the prefix check
// in classifyToolSource() captures all "mcp__*" names regardless of which MCP
// server they came from, so the map is plumbed end-to-end but not consulted.
// Kept for forward-compat (e.g. distinguishing cago-bridge vs. user MCP servers
// later) and to avoid churning call sites.
//
// Caller must close out after this returns.
func translateStream(ctx context.Context, r io.Reader, out chan<- runtime.Event, registered map[string]struct{}) error {
	_ = registered // reserved: see doc comment above
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	emit := func(ev runtime.Event) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	st := &translateState{
		toolCalls: make(map[string]toolCallState),
	}

	lineNo := 0
	sentDone := false
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		raw, err := parseLine(line)
		if err != nil {
			truncated := line
			if len(truncated) > 512 {
				truncated = truncated[:512]
			}
			logger.Ctx(ctx).Warn("claudecode: translate parse warning",
				zap.Int("line_no", lineNo),
				zap.Error(err),
				zap.String("truncated", truncated),
			)
			continue
		}

		evs := translateRawEvent(raw, st)
		for _, ev := range evs {
			if !emit(ev) {
				return ctx.Err()
			}
			if ev.Kind == runtime.EventDone {
				sentDone = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if !sentDone {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// translateRawEvent is a pure function translating one rawEvent into zero or more runtime.Events.
func translateRawEvent(ev rawEvent, st *translateState) []runtime.Event {
	switch frameType(ev.Type) {
	case frameTypeSystem:
		if ev.Subtype == systemSubtypeInit {
			st.sessionID = ev.SessionID
			return []runtime.Event{{
				Kind:      runtime.EventSessionStart,
				SessionID: ev.SessionID,
				Cwd:       ev.Cwd,
			}}
		}

	case frameTypeStreamEvent:
		if ev.Event == nil || ev.Event.Delta == nil {
			return nil
		}
		switch streamDeltaType(ev.Event.Delta.Type) {
		case streamDeltaText:
			if ev.Event.Delta.Text == "" {
				return nil
			}
			st.partialActive = true
			return []runtime.Event{{Kind: runtime.EventTextDelta, Text: ev.Event.Delta.Text}}
		case streamDeltaThinking:
			if ev.Event.Delta.Thinking == "" {
				return nil
			}
			st.partialActive = true
			return []runtime.Event{{Kind: runtime.EventThinkingDelta, Text: ev.Event.Delta.Thinking}}
		}

	case frameTypeAssistant:
		if ev.Message == nil {
			return nil
		}
		return translateAssistant(ev, st)

	case frameTypeUser:
		if ev.Message == nil {
			return nil
		}
		return translateUser(ev, st)

	case frameTypeResult:
		return translateResult(ev)

	case frameTypeControlRequest:
		return translateControlRequest(ev)
	}
	return nil
}

// translateControlRequest handles the "control_request" frame
// (--permission-prompt-tool stdio mode).
func translateControlRequest(ev rawEvent) []runtime.Event {
	if ev.Subtype != controlSubtypeCanUseTool || ev.ID == "" {
		return nil
	}
	return []runtime.Event{{
		Kind:                runtime.EventPermissionRequest,
		PermissionRequestID: ev.ID,
		Tool: &runtime.ToolEvent{
			ID:    ev.ID,
			Name:  ev.ToolName,
			Input: ev.ToolInput,
		},
	}}
}

// classifyToolSource classifies a Claude Code tool name into ToolSource.
// 命名约定: MCP server 暴露的工具一律以 mcpToolPrefix 开头(claude CLI 自己的拼接规则);
// 其它(Bash / Read / Write / Edit / Task / WebFetch / Grep / Glob / TodoWrite / ...)
// 都是 CLI 内置工具。
func classifyToolSource(name string) runtime.ToolSource {
	if strings.HasPrefix(name, mcpToolPrefix) {
		return runtime.ToolSourceMCP
	}
	if name == "" {
		return runtime.ToolSourceUnknown
	}
	return runtime.ToolSourceBuiltin
}

// translateAssistant handles the "assistant" frame.
func translateAssistant(ev rawEvent, st *translateState) []runtime.Event {
	var out []runtime.Event
	var text string
	var hasThinking bool

	for _, part := range ev.Message.Content {
		switch contentPartType(part.Type) {
		case contentPartText:
			text += part.Text
			if !st.partialActive {
				out = append(out, runtime.Event{Kind: runtime.EventTextDelta, Text: part.Text})
			}
		case contentPartThinking:
			hasThinking = true
			if !st.partialActive {
				out = append(out, runtime.Event{Kind: runtime.EventThinkingDelta, Text: part.Thinking})
			}
		case contentPartToolUse:
			source := classifyToolSource(part.Name)
			st.toolCalls[part.ID] = toolCallState{Name: part.Name, ParentID: ev.ParentToolUseID, Source: source}
			out = append(out, runtime.Event{
				Kind: runtime.EventPreToolUse,
				Tool: &runtime.ToolEvent{
					ID:       part.ID,
					Name:     part.Name,
					Input:    part.Input,
					ParentID: ev.ParentToolUseID,
					Source:   source,
				},
			})
			out = append(out, runtime.Event{
				Kind: runtime.EventMessageEnd,
				Message: &runtime.Message{
					Role:    runtime.RoleAssistant,
					Origin:  runtime.OriginModel,
					Persist: true,
					ToolCalls: []runtime.ToolCall{{
						ID:    part.ID,
						Name:  part.Name,
						Input: part.Input,
					}},
					CreatedAt: time.Now(),
				},
			})
		}
	}

	if text != "" || hasThinking {
		out = append(out, runtime.Event{
			Kind: runtime.EventMessageEnd,
			Message: &runtime.Message{
				Role:      runtime.RoleAssistant,
				Origin:    runtime.OriginModel,
				Text:      text,
				Persist:   true,
				CreatedAt: time.Now(),
			},
		})
	}
	_ = provider.ThinkingBlock{} // keep provider import even if thinking blocks are not surfaced on runtime.Message
	return out
}

// translateUser handles the "user" frame (tool_result entries).
func translateUser(ev rawEvent, st *translateState) []runtime.Event {
	var out []runtime.Event
	for _, part := range ev.Message.Content {
		if contentPartType(part.Type) != contentPartToolResult {
			continue
		}
		state := st.toolCalls[part.ToolUseID]
		var resErrStr string
		var resErr error
		if part.IsError {
			resErr = fmt.Errorf("tool error: %v", part.Content)
			resErrStr = resErr.Error()
		}
		respBytes := encodeToolResultResponse(part.Content)
		resultText := stringifyToolResult(respBytes)
		out = append(out, runtime.Event{
			Kind: runtime.EventPostToolUse,
			Tool: &runtime.ToolEvent{
				ID:       part.ToolUseID,
				Name:     state.Name,
				Response: respBytes,
				Err:      resErr,
				ParentID: state.ParentID,
				Source:   state.Source,
			},
		})
		out = append(out, runtime.Event{
			Kind: runtime.EventMessageEnd,
			Message: &runtime.Message{
				Role:   runtime.RoleTool,
				Origin: runtime.OriginTool,
				Text:   resultText,
				ToolResult: &runtime.ToolResult{
					ToolCallID: part.ToolUseID,
					Output:     resultText,
					Err:        resErrStr,
				},
				Persist:   true,
				CreatedAt: time.Now(),
			},
		})
		delete(st.toolCalls, part.ToolUseID)
	}
	return out
}

// stringifyToolResult converts a tool_result content field (string or
// [{type:text,text:...}] blocks) to plain text.
func stringifyToolResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []rawContentPart
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if contentPartType(b.Type) == contentPartText {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	return string(raw)
}

func encodeToolResultResponse(content any) json.RawMessage {
	if content == nil {
		return nil
	}
	if s, ok := content.(string); ok {
		return json.RawMessage(s)
	}
	b, err := json.Marshal(content)
	if err != nil {
		return nil
	}
	return b
}

// translateResult handles the "result" frame.
func translateResult(ev rawEvent) []runtime.Event {
	usage := toProviderUsage(ev.Usage)
	switch resultSubtype(ev.Subtype) {
	case resultSubtypeSuccess:
		return []runtime.Event{
			{Kind: runtime.EventStop, Stop: runtime.StopEndTurn, Usage: usage},
			{Kind: runtime.EventDone, Stop: runtime.StopEndTurn, Usage: usage},
		}
	case resultSubtypeErrorMaxTurns:
		return []runtime.Event{
			{Kind: runtime.EventStop, Stop: runtime.StopMaxSteps, Usage: usage},
			{Kind: runtime.EventDone, Stop: runtime.StopMaxSteps, Usage: usage},
		}
	case resultSubtypeErrorDuringExecution:
		return []runtime.Event{
			{Kind: runtime.EventError, Err: fmt.Errorf("claude: %s", ev.Error)},
			{Kind: runtime.EventDone, Stop: runtime.StopError},
		}
	default:
		return []runtime.Event{
			{Kind: runtime.EventStop, Stop: runtime.StopEndTurn},
			{Kind: runtime.EventDone, Stop: runtime.StopEndTurn},
		}
	}
}
