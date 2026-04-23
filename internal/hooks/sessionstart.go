package hooks

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// routingInstructions is injected into every session's additionalContext so
// the model routes large-output commands through ctx-saver MCP tools.
const routingInstructions = `## ctx-saver routing rules

Use ctx-saver MCP tools instead of raw shell when output may exceed ~2 KB:
- For HTTP fetches → ctx_execute (lang=shell) with curl/wget
- For file reads → ctx_read_file
- For find/grep/jq on large data → ctx_execute (lang=shell or python)
- For git log/diff (large) → ctx_execute (lang=shell)

Dangerous commands are blocked by the PreToolUse hook:
"rm -rf", pipe-to-shell (curl/wget piped to sh/bash), eval, "sudo -s".
`

// RunSessionStart reads a Codex CLI SessionStart JSON payload from r,
// retrieves recent session events from the store, builds a context-restoration
// directive, and writes it to w as additionalContext.
// limit is the maximum number of recent events to include (from config).
func RunSessionStart(st store.Store, r io.Reader, w io.Writer, limit int) error {
	input, err := readInput(r)
	if err != nil {
		// Still emit routing instructions even if we cannot read session history.
		return writeSessionStartOutput(w, routingInstructions)
	}

	projectPath := resolveProjectPath(input.Cwd)
	sessionID := resolveSessionID(input.SessionID)

	ctx := context.Background()

	var additionalContext strings.Builder
	additionalContext.WriteString(routingInstructions)

	// Try to restore the most recent session state.
	if st != nil {
		events, err := st.ListProjectSessionEvents(ctx, projectPath, limit)
		if err == nil && len(events) > 0 {
			// Deduplicate by tool name + first word of summary to avoid noise.
			seen := make(map[string]bool)
			var deduped []*store.SessionEvent
			for _, e := range events {
				key := e.ToolName + "|" + firstWord(e.Summary)
				if !seen[key] {
					seen[key] = true
					deduped = append(deduped, e)
				}
			}
			if len(deduped) > 0 {
				additionalContext.WriteString(fmt.Sprintf("\n\n## Session history (last %d events)\n\n", len(deduped)))
				for _, e := range deduped {
					ts := e.CreatedAt.Format(time.TimeOnly)
					additionalContext.WriteString(fmt.Sprintf("- [%s] %s\n", ts, e.Summary))
				}
			}
		}

		// Record the SessionStart event itself.
		_ = st.SaveSessionEvent(ctx, &store.SessionEvent{
			SessionID:   sessionID,
			ProjectPath: projectPath,
			EventType:   "sessionstart",
			Summary:     "session started",
			CreatedAt:   time.Now().UTC(),
		})
	}

	return writeSessionStartOutput(w, additionalContext.String())
}

// firstWord returns the first whitespace-delimited word of s, or s if there
// is no whitespace.  Used for deduplication keying.
func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

func writeSessionStartOutput(w io.Writer, additionalContext string) error {
	return writeJSON(w, CodexHookOutput{
		HookSpecificOutput: CodexSpecificOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: additionalContext,
		},
	})
}
