// Package hooks implements the PreToolUse, PostToolUse, and SessionStart
// hook handlers for ctx-saver.  The hooks are invoked by the AI agent runtime
// (Codex CLI, VS Code Copilot, …) as external commands and communicate via
// stdin/stdout JSON.
package hooks

import "time"

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
}

// ── Codex CLI output envelope ──────────────────────────────────────────────

// CodexHookOutput is the JSON object written to stdout by all hooks when
// targeting Codex CLI.
type CodexHookOutput struct {
	HookSpecificOutput CodexSpecificOutput `json:"hookSpecificOutput"`
}

// CodexSpecificOutput carries the per-event-type payload inside the envelope.
type CodexSpecificOutput struct {
	HookEventName    string `json:"hookEventName"`
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
