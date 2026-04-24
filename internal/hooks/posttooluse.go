package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// RunPostToolUse reads a Codex CLI PostToolUse JSON payload from r, extracts
// the tool call summary, persists it to the session DB, and writes an empty
// acknowledgement to w (Codex ignores PostToolUse output).
func RunPostToolUse(st store.Store, r io.Reader, w io.Writer) error {
	input, err := readInput(r)
	if err != nil {
		// Non-fatal: swallow errors so the hook never blocks the agent.
		_, _ = fmt.Fprintln(w, "{}")
		return nil
	}

	projectPath := resolveProjectPath(input.Cwd)
	sessionID := resolveSessionID(input.SessionID)

	toolInputJSON := marshalJSON(input.ToolInput)
	toolOutputStr := extractOutputText(input.ToolOutput)
	summary := buildSummary(input.ToolName, input.ToolInput, toolOutputStr)

	// Annotate native tool usage so adherence tracking can identify it.
	if isNativeShellTool(input.ToolName) {
		summary = "⚠️  NATIVE_SHELL: " + summary + " (consider ctx_execute)"
	} else if isNativeReadTool(input.ToolName) {
		summary = "⚠️  NATIVE_READ: " + summary + " (consider ctx_read_file)"
	}

	event := &store.SessionEvent{
		SessionID:   sessionID,
		ProjectPath: projectPath,
		EventType:   "posttooluse",
		ToolName:    input.ToolName,
		ToolInput:   truncate(toolInputJSON, maxFieldBytes),
		ToolOutput:  truncate(toolOutputStr, 512),
		Summary:     summary,
		CreatedAt:   time.Now().UTC(),
	}

	ctx := context.Background()
	// Log but never block the agent on store errors.
	if st != nil {
		if err := st.SaveSessionEvent(ctx, event); err != nil {
			slog.Warn("hook: failed to save session event", "error", err, "event_type", "posttooluse")
		}
	}

	_, _ = fmt.Fprintln(w, "{}")
	return nil
}

// extractOutputText converts the tool_output field (string or content-block
// array) to a plain string.
func extractOutputText(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var parts []string
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprintf("%v", v)
}

// buildSummary creates a one-line description of the tool call for the session log.
func buildSummary(toolName string, input map[string]any, output string) string {
	cmd := extractCmd(input)
	if cmd == "" {
		cmd = toolName
	}
	cmd = truncate(cmd, 80)

	outputPreview := ""
	if lines := strings.SplitN(strings.TrimSpace(output), "\n", 2); len(lines) > 0 {
		outputPreview = truncate(lines[0], 60)
	}

	if outputPreview != "" {
		return fmt.Sprintf("[%s] %s → %s", toolName, cmd, outputPreview)
	}
	return fmt.Sprintf("[%s] %s", toolName, cmd)
}

// marshalJSON safely marshals a map to a JSON string.
func marshalJSON(v any) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// maxFieldBytes is the maximum byte length stored for ToolInput / ToolOutput fields.
const maxFieldBytes = 1024

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Walk back to a valid UTF-8 rune boundary before slicing.
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max] + "…"
}
