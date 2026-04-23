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

You have access to ctx-saver MCP tools that keep large outputs out of the
context window.  Always prefer them over raw shell commands when the output
could exceed ~2 KB.

| Instead of                            | Use                              |
|---------------------------------------|----------------------------------|
| Shell: curl <URL>                     | ctx_execute (lang=shell)         |
| Shell: cat <file>                     | ctx_read_file                    |
| Shell: find / grep / jq on large data | ctx_execute (lang=shell/python)  |
| Shell: git log / diff (large)         | ctx_execute (lang=shell)         |

Dangerous commands (rm -rf /, curl|bash, eval, sudo -s) are automatically
blocked by the PreToolUse hook and must not be attempted.`

// RunSessionStart reads a Codex CLI SessionStart JSON payload from r,
// retrieves recent session events from the store, builds a context-restoration
// directive, and writes it to w as additionalContext.
func RunSessionStart(st store.Store, r io.Reader, w io.Writer) error {
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
		events, err := st.ListProjectSessionEvents(ctx, projectPath, 30)
		if err == nil && len(events) > 0 {
			additionalContext.WriteString("\n\n## Session history (last ")
			additionalContext.WriteString(fmt.Sprintf("%d events)\n\n", len(events)))
			for _, e := range events {
				ts := e.CreatedAt.Format(time.TimeOnly)
				additionalContext.WriteString(fmt.Sprintf("- [%s] %s\n", ts, e.Summary))
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

func writeSessionStartOutput(w io.Writer, additionalContext string) error {
	return writeJSON(w, CodexHookOutput{
		HookSpecificOutput: CodexSpecificOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: additionalContext,
		},
	})
}
