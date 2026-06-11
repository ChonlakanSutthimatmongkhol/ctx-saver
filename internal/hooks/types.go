// Package hooks implements the PreToolUse, PostToolUse, and SessionStart
// hook handlers for ctx-saver.  The hooks are invoked by the AI agent runtime
// (Codex CLI, VS Code Copilot, …) as external commands and communicate via
// stdin/stdout JSON.
package hooks

import (
	"encoding/json"
	"time"
)

// ── Shared input fields ────────────────────────────────────────────────────

// HookInput is the common envelope received on stdin for all hook events.
// Platforms may omit fields that are not relevant to a given event type.
type HookInput struct {
	// SessionID uniquely identifies the current agent session.
	SessionID string `json:"session_id"`

	// Cwd is the working directory at the time the hook fires.
	// This is used to locate the per-project SQLite database.
	Cwd string `json:"cwd"`

	// ToolName is the name of the tool being invoked (PreToolUse / PostToolUse).
	ToolName string `json:"tool_name"`

	// ToolInput is the raw JSON input for the tool (PreToolUse / PostToolUse).
	ToolInput map[string]any `json:"tool_input"`

	// ToolOutput is the raw tool output (PostToolUse only).
	// Can be a string or an array of content blocks.
	ToolOutput any `json:"tool_output"`

	// HookEventName is echoed back in the output for some platforms.
	HookEventName string `json:"hook_event_name"`

	// GitHub Copilot sends camelCase fields and encodes toolArgs as a JSON
	// string. These fields are folded into the canonical fields by normalize.
	CopilotSessionID string             `json:"sessionId"`
	CopilotToolName  string             `json:"toolName"`
	CopilotToolArgs  string             `json:"toolArgs"`
	CopilotResult    *CopilotToolResult `json:"toolResult"`
	TimestampMs      int64              `json:"timestamp"`
	Source           string             `json:"source"`
	InitialPrompt    string             `json:"initialPrompt"`
}

// CopilotToolResult is the result envelope sent by GitHub Copilot postToolUse.
type CopilotToolResult struct {
	ResultType       string `json:"resultType"`
	TextResultForLlm string `json:"textResultForLlm"`
}

// HostFormat identifies the hook protocol used by the agent host.
type HostFormat int

const (
	HostClaudeCodex HostFormat = iota
	HostCopilot
)

// normalize folds GitHub Copilot fields into the canonical hook fields.
func (in *HookInput) normalize() HostFormat {
	isCopilot := in.CopilotToolName != "" || in.CopilotToolArgs != "" ||
		in.CopilotResult != nil || (in.Source != "" && in.HookEventName == "")
	if !isCopilot {
		return HostClaudeCodex
	}
	if in.ToolName == "" {
		in.ToolName = in.CopilotToolName
	}
	if in.SessionID == "" {
		in.SessionID = in.CopilotSessionID
	}
	if in.ToolInput == nil && in.CopilotToolArgs != "" {
		var toolInput map[string]any
		if err := json.Unmarshal([]byte(in.CopilotToolArgs), &toolInput); err == nil {
			in.ToolInput = toolInput
		}
	}
	if in.ToolOutput == nil && in.CopilotResult != nil {
		in.ToolOutput = in.CopilotResult.TextResultForLlm
	}
	return HostCopilot
}

// ── Codex CLI output envelope ──────────────────────────────────────────────

// CodexHookOutput is the JSON object written to stdout by all hooks when
// targeting Codex CLI.
type CodexHookOutput struct {
	HookSpecificOutput CodexSpecificOutput `json:"hookSpecificOutput"`
}

// CodexSpecificOutput carries the per-event-type payload inside the envelope.
type CodexSpecificOutput struct {
	HookEventName string `json:"hookEventName"`
	// PermissionDecision is set to "deny" by PreToolUse when a tool call
	// must be blocked.  It is omitted for allow decisions.
	PermissionDecision string `json:"permissionDecision,omitempty"`
	// AdditionalContext is injected into the model's system prompt by
	// SessionStart.
	AdditionalContext string `json:"additionalContext,omitempty"`
}

// ── Session event ──────────────────────────────────────────────────────────

// Event is a normalised representation of one PostToolUse capture.
type Event struct {
	SessionID   string
	ProjectPath string
	EventType   string
	ToolName    string
	ToolInput   string // JSON
	ToolOutput  string // text
	Summary     string
	CreatedAt   time.Time
}
