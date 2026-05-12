package codex

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
	"github.com/cago-frame/agents/provider"
)

type appThread struct {
	ID  string `json:"id"`
	Cwd string `json:"cwd"`
}

type appThreadStartResponse struct {
	Thread appThread `json:"thread"`
}

type appThreadResumeResponse struct {
	Thread appThread `json:"thread"`
}

type appTurnStartResponse struct {
	Turn appTurn `json:"turn"`
}

type appTurn struct {
	ID     string        `json:"id"`
	Status appStatus     `json:"status"`
	Error  *appTurnError `json:"error"`
}

type appTurnError struct {
	Message           string `json:"message"`
	AdditionalDetails string `json:"additionalDetails"`
}

type appNotification struct {
	ThreadID string          `json:"threadId"`
	TurnID   string          `json:"turnId"`
	Thread   *appThread      `json:"thread"`
	Turn     *appTurn        `json:"turn"`
	Item     *appThreadItem  `json:"item"`
	ItemID   string          `json:"itemId"`
	Delta    string          `json:"delta"`
	Usage    *appTokenUsage  `json:"tokenUsage"`
	Status   json.RawMessage `json:"status"`
	// turn/plan/updated payload. Kept in the generic notification shape so the
	// backend can surface Codex plan updates without adding a framework-level
	// event kind.
	Explanation *string       `json:"explanation"`
	Plan        []appPlanStep `json:"plan"`
	// item/fileChange/patchUpdated payload.
	Changes []appFileUpdateChange `json:"changes"`
	Raw     json.RawMessage       `json:"-"`
}

type appThreadItem struct {
	Type appItemType `json:"type"`
	ID   string      `json:"id"`

	Content json.RawMessage `json:"content,omitempty"`

	Text  string `json:"text,omitempty"`
	Phase string `json:"phase,omitempty"`

	Command          string    `json:"command,omitempty"`
	Cwd              string    `json:"cwd,omitempty"`
	Status           appStatus `json:"status,omitempty"`
	AggregatedOutput string    `json:"aggregatedOutput,omitempty"`
	ExitCode         *int      `json:"exitCode,omitempty"`

	Server    string          `json:"server,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Namespace string          `json:"namespace,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`

	SenderThreadID    string                         `json:"senderThreadId,omitempty"`
	ReceiverThreadIDs []string                       `json:"receiverThreadIds,omitempty"`
	AgentStates       map[string]appCollabAgentState `json:"agentsStates,omitempty"`
	Model             *string                        `json:"model,omitempty"`
	Prompt            *string                        `json:"prompt,omitempty"`
	ReasoningEffort   *string                        `json:"reasoningEffort,omitempty"`

	Success      *bool           `json:"success,omitempty"`
	ContentItems json.RawMessage `json:"contentItems,omitempty"`

	Summary []string              `json:"summary,omitempty"`
	Changes []appFileUpdateChange `json:"changes,omitempty"`
}

type appPlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type appCollabAgentState struct {
	Status  string  `json:"status"`
	Message *string `json:"message"`
}

type appFileUpdateChange struct {
	Path string             `json:"path"`
	Kind appPatchChangeKind `json:"kind"`
	Diff string             `json:"diff"`
}

type appPatchChangeKind struct {
	Type     string  `json:"type"`
	MovePath *string `json:"move_path"`
}

type appTokenUsage struct {
	Total appTokenBreakdown `json:"total"`
	Last  appTokenBreakdown `json:"last"`
}

type appTokenBreakdown struct {
	TotalTokens           int `json:"totalTokens"`
	InputTokens           int `json:"inputTokens"`
	CachedInputTokens     int `json:"cachedInputTokens"`
	OutputTokens          int `json:"outputTokens"`
	ReasoningOutputTokens int `json:"reasoningOutputTokens"`
}

func parseNotification(_ appMethod, params json.RawMessage) (appNotification, error) {
	var n appNotification
	if len(params) > 0 {
		if err := json.Unmarshal(params, &n); err != nil {
			return appNotification{}, err
		}
	}
	n.Raw = append(json.RawMessage(nil), params...)
	return n, nil
}

func userInput(text string) []map[string]any {
	return []map[string]any{{
		"type":          "text",
		"text":          text,
		"text_elements": []any{},
	}}
}

func appUsageToProvider(u *appTokenUsage) provider.Usage {
	if u == nil {
		return provider.Usage{}
	}
	last := u.Last
	return provider.Usage{
		PromptTokens:     last.InputTokens,
		CompletionTokens: last.OutputTokens,
		ReasoningTokens:  last.ReasoningOutputTokens,
		CachedTokens:     last.CachedInputTokens,
		TotalTokens:      last.TotalTokens,
	}
}

func appStopForTurn(turn *appTurn) runtime.StopReason {
	if turn == nil {
		return runtime.StopEndTurn
	}
	switch turn.Status {
	case appStatusCompleted:
		return runtime.StopEndTurn
	case appStatusInterrupted:
		return runtime.StopCancelled
	case appStatusFailed:
		return runtime.StopError
	default:
		return runtime.StopEndTurn
	}
}

func appTurnErr(turn *appTurn) error {
	if turn == nil || turn.Error == nil {
		return nil
	}
	if turn.Error.AdditionalDetails != "" {
		return fmt.Errorf("codex: %s: %s", turn.Error.Message, turn.Error.AdditionalDetails)
	}
	return fmt.Errorf("codex: %s", turn.Error.Message)
}

func appMessageFromItem(item *appThreadItem, raw json.RawMessage) runtime.Message {
	return runtime.Message{
		Role:      runtime.RoleAssistant,
		Origin:    runtime.OriginModel,
		Text:      item.Text,
		Persist:   true,
		CreatedAt: time.Now(),
		Raw:       raw,
	}
}

func toolNameForItem(item *appThreadItem) string {
	if item == nil {
		return ""
	}
	switch item.Type {
	case appItemCommandExecution:
		return string(appToolCommandExecution)
	case appItemFileChange:
		return string(appToolFileChange)
	case appItemMCPToolCall:
		if item.Server == "" {
			return item.Tool
		}
		return mcpToolNamePrefix + item.Server + "." + item.Tool
	case appItemDynamicToolCall:
		if item.Namespace == "" {
			return item.Tool
		}
		return item.Namespace + "." + item.Tool
	case appItemCollabAgentTool:
		return subagentToolNamePrefix + item.Tool
	default:
		return string(item.Type)
	}
}

// toolSourceForItem 分类 codex item 到 runtime.ToolSource。
//   - mcpToolCall → MCP
//   - commandExecution / fileChange / dynamicToolCall / collabAgentToolCall → Builtin
//     (codex app-server 的原生控制面工具,统一标 Builtin;subagent 的 collab 调用也是
//     codex 内部协调,不算 MCP)
//   - 其它 → Unknown
func toolSourceForItem(item *appThreadItem) runtime.ToolSource {
	if item == nil {
		return runtime.ToolSourceUnknown
	}
	switch item.Type {
	case appItemMCPToolCall:
		return runtime.ToolSourceMCP
	case appItemCommandExecution, appItemFileChange, appItemDynamicToolCall, appItemCollabAgentTool:
		return runtime.ToolSourceBuiltin
	default:
		return runtime.ToolSourceUnknown
	}
}

func toolInputForItem(item *appThreadItem) json.RawMessage {
	if item == nil {
		return nil
	}
	switch item.Type {
	case appItemCommandExecution:
		return mustRaw(map[string]any{"command": item.Command, "cwd": item.Cwd})
	case appItemMCPToolCall, appItemDynamicToolCall:
		return item.Arguments
	default:
		data, _ := json.Marshal(item)
		return data
	}
}

func toolResponseForItem(item *appThreadItem) json.RawMessage {
	if item == nil {
		return nil
	}
	switch item.Type {
	case appItemCommandExecution:
		return mustRaw(map[string]any{
			"output":   item.AggregatedOutput,
			"status":   item.Status,
			"exitCode": item.ExitCode,
		})
	case appItemMCPToolCall:
		if item.Result != nil {
			return item.Result
		}
	case appItemDynamicToolCall:
		if item.ContentItems != nil {
			return item.ContentItems
		}
	}
	data, _ := json.Marshal(item)
	return data
}

func toolErrForItem(item *appThreadItem) error {
	if item == nil {
		return nil
	}
	if item.Error != nil && item.Error.Message != "" {
		return fmt.Errorf("%s", item.Error.Message)
	}
	switch item.Type {
	case appItemCommandExecution:
		if item.ExitCode != nil && *item.ExitCode != 0 {
			return fmt.Errorf("command exited with %d", *item.ExitCode)
		}
		if item.Status == appStatusFailed || item.Status == appStatusDeclined {
			return fmt.Errorf("command status %s", item.Status)
		}
	case appItemMCPToolCall, appItemDynamicToolCall, appItemCollabAgentTool:
		if item.Status == appStatusFailed {
			return fmt.Errorf("tool status failed")
		}
	}
	return nil
}

func mustRaw(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
