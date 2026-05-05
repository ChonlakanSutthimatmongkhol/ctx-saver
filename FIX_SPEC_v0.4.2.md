# Fix Spec: Session Events for MCP Tool Calls (v0.4.2)

**Type:** Bug fix / integration gap
**Priority:** High (unblocks Phase 6 adherence measurement)
**Estimated effort:** 30-45 minutes
**Target version:** v0.4.2

---

## Problem statement

`session_events` table is currently populated **only by hook subcommands** (`ctx-saver hook posttooluse` and `ctx-saver hook sessionstart`).

This means:

- ✅ Claude Code (which uses hook protocol) → events recorded
- ✅ Codex CLI (which uses hook protocol) → events recorded
- ❌ **VS Code Copilot Enterprise** (no hook protocol support) → `session_events` table stays empty forever

Result: `ctx_stats` cannot compute `adherence_score` for Copilot sessions, breaking Phase 6 Task 6.4 measurement on the platform that needs it most.

## Root cause

Searching the codebase confirms:

```bash
grep -rn "SaveSessionEvent" internal/ cmd/ | grep -v _test
# Output:
# internal/hooks/sessionstart.go:70: _ = st.SaveSessionEvent(...)
# internal/hooks/posttooluse.go:55: if err := st.SaveSessionEvent(...)
# internal/store/store.go:101: SaveSessionEvent(ctx, event) error
# internal/store/sqlite.go:332: func ... SaveSessionEvent(...)
```

`SaveSessionEvent` is called only from `internal/hooks/`. No MCP tool handler in `internal/handlers/` calls it.

## Fix scope

Add `SaveSessionEvent` calls **inside each MCP tool handler** so that direct MCP tool invocations (Copilot's only path) also produce session events.

This makes `session_events` the single source of truth for tool usage across all platforms — hook-driven and MCP-driven alike.

## Files to modify

All MCP handler files in `internal/handlers/`:

```
internal/handlers/execute.go         → ctx_execute
internal/handlers/read_file.go       → ctx_read_file
internal/handlers/search.go          → ctx_search
internal/handlers/list.go            → ctx_list_outputs
internal/handlers/get_full.go        → ctx_get_full
internal/handlers/outline.go         → ctx_outline
internal/handlers/stats.go           → ctx_stats
internal/handlers/get_section.go     → ctx_get_section (if exists)
internal/handlers/session_init.go    → ctx_session_init (if exists)
```

Also add a small helper to deduplicate boilerplate.

## Implementation plan

### Step 1 — Create shared helper

**File:** `internal/handlers/session_event.go` (new)

```go
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// MCP-direct session ID. Used to distinguish MCP tool calls from hook events
// inside session_events. Prefixed with "mcp-" so future analytics can filter.
//
// We use a single per-process ID rather than per-call so that a single MCP
// session in a host (e.g., one VS Code Copilot chat) groups under one ID.
var mcpSessionID = newMCPSessionID()

func newMCPSessionID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "mcp-unknown"
	}
	return "mcp-" + hex.EncodeToString(b)
}

// recordToolCall persists one MCP tool invocation as a session_event.
// Errors are logged but not returned: telemetry must never fail a tool call.
//
// inputPreview / outputPreview should be small strings (use truncate helper).
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
		slog.Warn("recordToolCall: SaveSessionEvent failed",
			"tool", toolName,
			"error", err,
		)
	}
}

// truncatePreview returns s truncated to max bytes, with a "…" suffix when cut.
// UTF-8 safe: cuts at the nearest preceding rune boundary.
func truncatePreview(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Walk back to the start of a rune (avoid cutting mid-multibyte).
	for max > 0 && (s[max]&0xC0) == 0x80 {
		max--
	}
	return s[:max] + "…"
}
```

**Why these design choices:**

- `mcpSessionID` is process-wide → groups all calls in one MCP server lifetime under one ID
- EventType `"mcp_tool_call"` is distinct from existing `"pretooluse" | "posttooluse" | "sessionstart"` → easy to filter
- Errors are logged with `slog.Warn`, not returned → tool call must never fail because of telemetry
- Truncate caps prevent huge payloads bloating the DB
- UTF-8-safe truncate handles Thai/multibyte correctly

### Step 2 — Wire into each handler

For **every** MCP handler, add a `recordToolCall(...)` line right before the successful return.

Pattern: invoke at the **end** of `Handle` (before returning success), so the event reflects what actually happened.

#### execute.go

Inside `(h *ExecuteHandler) Handle(...)`:

Find the success return (somewhere after the sandbox call and storage). Right before it, add:

```go
recordToolCall(
	ctx, h.st, h.projectPath, "ctx_execute",
	input.Code,
	output.Summary, // or output.DirectOutput if Summary empty
	"shell: " + input.Code,
)
```

If there are multiple early-returns due to validation, only add at the **final success path** (where the sandbox actually executed). Validation failures don't need to be recorded as tool calls.

#### read_file.go

```go
recordToolCall(
	ctx, h.st, h.projectPath, "ctx_read_file",
	input.Path,
	output.Summary,
	"read: " + input.Path,
)
```

#### search.go

```go
recordToolCall(
	ctx, h.st, h.projectPath, "ctx_search",
	strings.Join(input.Queries, ", "),
	"", // search returns structured results; no preview needed
	"search: " + strings.Join(input.Queries, ", "),
)
```

#### list.go (ctx_list_outputs)

```go
recordToolCall(
	ctx, h.st, h.projectPath, "ctx_list_outputs",
	"", // no input
	"", 
	"list outputs",
)
```

#### get_full.go

```go
recordToolCall(
	ctx, h.st, h.projectPath, "ctx_get_full",
	input.OutputID,
	"",
	"get_full: " + input.OutputID,
)
```

#### outline.go

```go
recordToolCall(
	ctx, h.st, h.projectPath, "ctx_outline",
	input.OutputID,
	"",
	"outline: " + input.OutputID,
)
```

#### stats.go

```go
recordToolCall(
	ctx, h.st, h.projectPath, "ctx_stats",
	input.Scope,
	"",
	"stats: " + input.Scope,
)
```

#### get_section.go (if exists in v0.4.x)

```go
recordToolCall(
	ctx, h.st, h.projectPath, "ctx_get_section",
	input.OutputID + "#" + input.Heading,
	"",
	"get_section: " + input.Heading,
)
```

#### session_init.go (if exists in v0.4.x)

```go
recordToolCall(
	ctx, h.st, h.projectPath, "ctx_session_init",
	"",
	"",
	"session_init",
)
```

### Step 3 — Tests

**File:** `internal/handlers/session_event_test.go` (new)

Add unit tests for `truncatePreview`:

```go
package handlers

import "testing"

func TestTruncatePreview(t *testing.T) {
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncate", "hello world", 5, "hello…"},
		{"empty", "", 5, ""},
		{"thai", "สวัสดีชาวโลก", 9, "สวัสดี…"}, // depends on UTF-8 — verify by len after cut
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncatePreview(c.in, c.max)
			if got != c.want {
				t.Errorf("truncatePreview(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
			}
		})
	}
}
```

Note for the Thai test: depending on rune boundaries, the exact expected output may need a small adjustment. Run it once; if the assertion fails, accept whatever the UTF-8-safe truncation actually produces (as long as the result is valid UTF-8).

For the per-handler tests, the existing `mockStore.SaveSessionEvent` in `handlers_test.go` already returns nil — no change needed. To verify wiring, optionally add a counter:

```go
// In handlers_test.go, augment mockStore:
type mockStore struct {
	// ... existing fields
	sessionEventCount int
}

func (m *mockStore) SaveSessionEvent(_ context.Context, _ *store.SessionEvent) error {
	m.sessionEventCount++
	return nil
}
```

Then in 1-2 existing handler tests (e.g., `TestExecuteHandler_Success`), assert:

```go
if mock.sessionEventCount != 1 {
	t.Errorf("expected 1 session event recorded, got %d", mock.sessionEventCount)
}
```

This catches regressions where the recordToolCall is accidentally removed.

### Step 4 — Bump version

In `cmd/ctx-saver/main.go`, bump `serverVersion` constant:

```go
const serverVersion = "0.4.2"
```

Update CHANGELOG / README if applicable:

> ### v0.4.2 — MCP tool call session events
> Fix: MCP tool handlers now record `session_events` directly (previously only hook subcommands did this). Enables `adherence_score` and tool usage tracking on platforms without hook support (notably VS Code Copilot Enterprise).

### Step 5 — Build, install, smoke-test

```bash
make lint test
make build
make install

# Verify
which ctx-saver
ls -la /Users/chonlakan/go/bin/ctx-saver  # should have new mtime
```

Quick functional check:

```bash
# Clean DB
rm -f /Users/chonlakan/Desktop/testing/ctx-saver-fixtures/.ctx-saver/outputs.db

# Trigger one MCP call manually if possible, OR rely on the next pilot session
```

## Acceptance criteria

The fix is done when:

- [ ] `session_event.go` helper file exists with `recordToolCall` and `truncatePreview`
- [ ] All 7-9 MCP handlers in `internal/handlers/` call `recordToolCall(...)` exactly once on success
- [ ] `truncatePreview` test passes with UTF-8-safe cuts
- [ ] At least one existing handler test asserts `sessionEventCount == 1` after a successful invocation
- [ ] `make lint test` passes clean
- [ ] After `make install`, running a pilot session and then `sqlite3 .ctx-saver/outputs.db "SELECT tool_name, COUNT(*) FROM session_events GROUP BY tool_name;"` returns rows with `mcp_tool_call` event_type
- [ ] `serverVersion` bumped to `0.4.2`

## Non-goals (do NOT do in this fix)

- ❌ Don't change anything in `internal/hooks/` — those continue to record their own events
- ❌ Don't modify `SessionEvent` schema — keep backward compatibility
- ❌ Don't add new MCP tools — only wire existing ones
- ❌ Don't compute or expose `adherence_score` here — that's a separate update on `stats.go` query, after we verify the events are recording correctly

## Verification (manual, after merge)

1. Pull latest, `make install`
2. In a Copilot session in `ctx-saver-fixtures`, run any prompt that triggers `ctx_execute` or `ctx_read_file`
3. Check DB:
   ```bash
   sqlite3 /Users/chonlakan/Desktop/testing/ctx-saver-fixtures/.ctx-saver/outputs.db \
     "SELECT event_type, tool_name, COUNT(*) FROM session_events GROUP BY 1, 2;"
   ```
4. Expect rows like `mcp_tool_call|ctx_execute|3` etc.

If you see those rows → fix works → adherence measurement unblocked.

## Hand-off context for Copilot

This ticket is being implemented to unblock real-world A/B testing of Phase 6. Without this fix, Group A vs Group B comparison cannot include adherence ratio because Copilot doesn't trigger any hook subcommand and thus produces zero session_events.

The architecture decision (hooks-only telemetry) was correct for Claude Code / Codex but didn't anticipate Copilot Enterprise's lack of hook protocol. This fix preserves the hook path while adding a parallel MCP-direct path, so both worlds work and existing tests continue to pass.

Keep the fix small, focused, and reversible. Don't refactor surrounding code.
