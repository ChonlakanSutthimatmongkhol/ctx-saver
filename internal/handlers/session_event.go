package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// mcpSessionID groups all MCP tool calls in one server lifetime under a single ID.
// Prefixed "mcp-" to distinguish from hook-driven session IDs in analytics.
var mcpSessionID = newMCPSessionID()

func newMCPSessionID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "mcp-unknown"
	}
	return "mcp-" + hex.EncodeToString(b)
}

// recordToolCall persists one MCP tool invocation as a session_event.
// Errors are logged but not returned so telemetry never fails a tool call.
func recordToolCall(
	ctx context.Context,
	st store.Store,
	projectPath, toolName, inputPreview, outputPreview, summary string,
) {
	if st == nil {
		return
	}
	event := &store.SessionEvent{
		SessionID:   mcpSessionID,
		ProjectPath: projectPath,
		EventType:   "mcp_tool_call",
		ToolName:    toolName,
		ToolInput:   truncatePreview(inputPreview, 1024),
		ToolOutput:  truncatePreview(outputPreview, 512),
		Summary:     truncatePreview(summary, 200),
		CreatedAt:   time.Now(),
	}
	if err := st.SaveSessionEvent(ctx, event); err != nil {
		slog.Warn("recordToolCall: SaveSessionEvent failed", "tool", toolName, "error", err)
	}
}

// truncatePreview returns s truncated to max bytes with a "…" suffix when cut.
// UTF-8 safe: walks back to the nearest rune boundary before cutting.
func truncatePreview(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && (s[max]&0xC0) == 0x80 {
		max--
	}
	return s[:max] + "…"
}
