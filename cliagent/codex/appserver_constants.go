package codex

const (
	codexBackendName = "codex"
	codexBinaryName  = "codex"
)

const (
	appServerCommand     = "app-server"
	appServerListenFlag  = "--listen"
	appServerListenStdio = "stdio://"
	appServerConfigFlag  = "--config"
)

const (
	appClientName    = "cago-agents"
	appClientTitle   = "Cago Agents"
	appClientVersion = "0.0.0"
)

const (
	mcpBridgeServerName        = "cago-bridge"
	mcpDefaultToolApprovalMode = "approve"
	mcpToolNamePrefix          = "mcp."
	subagentToolNamePrefix     = "subagent."
)

type appMethod string

const (
	appMethodInitialize  appMethod = "initialize"
	appMethodInitialized appMethod = "initialized"

	appMethodThreadStart   appMethod = "thread/start"
	appMethodThreadResume  appMethod = "thread/resume"
	appMethodThreadStarted appMethod = "thread/started"

	appMethodTurnStart       appMethod = "turn/start"
	appMethodTurnSteer       appMethod = "turn/steer"
	appMethodTurnInterrupt   appMethod = "turn/interrupt"
	appMethodTurnCompleted   appMethod = "turn/completed"
	appMethodTurnPlanUpdated appMethod = "turn/plan/updated"

	appMethodItemStarted                   appMethod = "item/started"
	appMethodItemCompleted                 appMethod = "item/completed"
	appMethodItemAgentMessageDelta         appMethod = "item/agentMessage/delta"
	appMethodItemReasoningTextDelta        appMethod = "item/reasoning/textDelta"
	appMethodItemReasoningSummaryTextDelta appMethod = "item/reasoning/summaryTextDelta"
	appMethodItemFileChangePatchUpdated    appMethod = "item/fileChange/patchUpdated"
	appMethodItemCommandApprovalRequest    appMethod = "item/commandExecution/requestApproval"
	appMethodItemFileApprovalRequest       appMethod = "item/fileChange/requestApproval"
	appMethodItemPermissionsRequest        appMethod = "item/permissions/requestApproval"
	appMethodItemToolRequestUserInput      appMethod = "item/tool/requestUserInput"
	appMethodItemToolCall                  appMethod = "item/tool/call"

	appMethodMCPServerElicitationRequest appMethod = "mcpServer/elicitation/request"
	appMethodThreadTokenUsageUpdated     appMethod = "thread/tokenUsage/updated"
	appMethodError                       appMethod = "error"
)

type appItemType string

const (
	appItemUserMessage      appItemType = "userMessage"
	appItemAgentMessage     appItemType = "agentMessage"
	appItemReasoning        appItemType = "reasoning"
	appItemPlan             appItemType = "plan"
	appItemCommandExecution appItemType = "commandExecution"
	appItemFileChange       appItemType = "fileChange"
	appItemMCPToolCall      appItemType = "mcpToolCall"
	appItemDynamicToolCall  appItemType = "dynamicToolCall"
	appItemCollabAgentTool  appItemType = "collabAgentToolCall"
)

type appStatus string

const (
	appStatusInProgress  appStatus = "inProgress"
	appStatusCompleted   appStatus = "completed"
	appStatusInterrupted appStatus = "interrupted"
	appStatusFailed      appStatus = "failed"
	appStatusDeclined    appStatus = "declined"
)

type appToolName string

const (
	appToolCommandExecution appToolName = "command_execution"
	appToolFileChange       appToolName = "file_change"
	appToolUpdatePlan       appToolName = "update_plan"
)

type appApprovalDecision string

const (
	appApprovalAccept  appApprovalDecision = "accept"
	appApprovalDecline appApprovalDecision = "decline"
)

type appElicitationAction string

const (
	appElicitationCancel appElicitationAction = "cancel"
)
