# Feature Spec: ctx_note — Decision Log (v0.5.1)

**Type:** New feature
**Priority:** High (highest-ROI feedback from real-world Phase 7 work)
**Estimated effort:** 1-1.5 days
**Target version:** v0.5.1

---

## Problem statement

When working on long sessions (multi-hour codebase work), `/compact` is used multiple times to reset context. Each compact discards the conversation history — including **architectural decisions and rationale** that don't survive in any persistent form.

Examples of decisions lost during Phase 7 implementation:
- "Chose `WithFreshness` builder pattern because 15 test call sites would break with a positional arg"
- "`refreshOutput` must run before line splitting — otherwise truncated output looks valid"
- "Use `mcp_tool_call` event_type instead of reusing `posttooluse` to keep filtering simple"

These decisions are:
- Small (1-2 sentences each, < 200 tokens)
- High value per byte (context is essential to avoid re-deciding)
- Not stored anywhere by ctx-saver currently
- Lost on every `/compact`

After compact, AI loses access to its own reasoning. User has to re-explain or AI re-derives — both wasteful.

## Solution

Add a lightweight **Decision Log** that AI can write to and read from. Decisions persist in SQLite (per-project, like outputs) and are surfaced through `ctx_session_init` so they survive compact and even cross-session.

**Three new tools:**

1. `ctx_note` — save a decision/note
2. `ctx_list_notes` — query saved notes
3. (existing) `ctx_session_init` — extended to inject recent notes

**Storage:** new `decisions` table in the existing per-project SQLite DB.

**Scope:** notes are scoped per-project (same as outputs and session_events).

---

## Files to add / modify

```
NEW:
  internal/store/decisions.go              — schema + CRUD
  internal/handlers/note.go                — ctx_note handler
  internal/handlers/list_notes.go          — ctx_list_notes handler

MODIFY:
  internal/store/store.go                  — add Decision interface methods
  internal/store/sqlite.go                 — implement new methods
  internal/store/migrations.go             — add decisions table migration
  internal/handlers/session_init.go        — inject recent decisions
  internal/server/server.go                — register new tools
  cmd/ctx-saver/main.go                    — bump serverVersion to "0.5.1"
  README.md, README.th.md, CLAUDE.md       — document new tools
```

---

## Schema

### Decisions table

**File:** `internal/store/migrations.go` — add new migration

```go
{
    Version: 5, // หรือเลขถัดไปจาก migration ล่าสุด
    Description: "Add decisions table for ctx_note feature",
    Statements: []string{
        `CREATE TABLE decisions (
            id           INTEGER PRIMARY KEY AUTOINCREMENT,
            decision_id  TEXT    NOT NULL UNIQUE,
            session_id   TEXT    NOT NULL DEFAULT '',
            project_path TEXT    NOT NULL,
            text         TEXT    NOT NULL,
            tags         TEXT    NOT NULL DEFAULT '',  -- comma-separated
            links_to     TEXT    NOT NULL DEFAULT '',  -- comma-separated output_ids
            importance   TEXT    NOT NULL DEFAULT 'normal', -- low|normal|high
            created_at   INTEGER NOT NULL
        )`,
        `CREATE INDEX idx_decisions_project_created 
            ON decisions(project_path, created_at DESC)`,
        `CREATE INDEX idx_decisions_session 
            ON decisions(session_id, created_at DESC)`,
        `CREATE INDEX idx_decisions_importance 
            ON decisions(project_path, importance, created_at DESC)`,
    },
},
```

**Why these design choices:**

- `decision_id` (TEXT, unique) — let callers reference decisions later. Format: `dec_<unix_micro>_<random4hex>` (similar to output_id pattern).
- `session_id` — match the per-process MCP session ID used in `recordToolCall` (from v0.4.2). Lets us scope "this session's decisions" cleanly.
- `tags` and `links_to` as comma-separated strings — keeps schema simple. We don't need full join queries for v1 — just LIKE filtering. (If usage grows, can normalize later.)
- `importance` enum — surface high-priority items first in `ctx_session_init`. Default `normal`.
- 3 indexes match the 3 main query patterns: by project+time (list/session_init), by session (current session view), by importance (priority filter).

### Decision struct

**File:** `internal/store/decisions.go` (new)

```go
package store

import "time"

// Decision is a small, durable note recording an architectural decision
// or rationale that should survive context compaction.
type Decision struct {
    ID          int64
    DecisionID  string    // "dec_<unix_micro>_<random4hex>"
    SessionID   string    // empty if recorded outside a tool call
    ProjectPath string
    Text        string
    Tags        []string
    LinksTo     []string  // related output_ids
    Importance  string    // "low" | "normal" | "high"
    CreatedAt   time.Time
}

// Importance levels.
const (
    ImportanceLow    = "low"
    ImportanceNormal = "normal"
    ImportanceHigh   = "high"
)
```

---

## Store interface additions

**File:** `internal/store/store.go` — add to `Store` interface

```go
// SaveDecision stores a new decision/note. Generates decision_id if empty.
SaveDecision(ctx context.Context, d *Decision) error

// ListDecisions returns decisions for a project, newest first.
// scope: "session" filters by current SessionID; "today" by today's date;
//        "7d" by last 7 days; "all" returns everything.
// minImportance: "low" returns all; "normal" returns normal+high; "high" returns only high.
// tags: if non-empty, decision must have at least one matching tag.
// limit: 0 means default (50).
ListDecisions(ctx context.Context, opts ListDecisionsOptions) ([]*Decision, error)

// GetDecision returns one decision by ID, or nil if not found.
GetDecision(ctx context.Context, decisionID string) (*Decision, error)
```

```go
// ListDecisionsOptions are parameters for ListDecisions.
type ListDecisionsOptions struct {
    ProjectPath   string
    SessionID     string    // for scope="session"
    Scope         string    // "session" | "today" | "7d" | "all"
    MinImportance string    // "low" | "normal" | "high"
    Tags          []string  // OR-match
    Limit         int       // 0 = default (50), max 200
}
```

---

## SQLite implementation

**File:** `internal/store/sqlite.go` — add methods

```go
// SaveDecision implements Store.
func (s *SQLiteStore) SaveDecision(ctx context.Context, d *Decision) error {
    if d.DecisionID == "" {
        d.DecisionID = newDecisionID()
    }
    if d.Importance == "" {
        d.Importance = ImportanceNormal
    }
    if d.CreatedAt.IsZero() {
        d.CreatedAt = time.Now()
    }
    
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO decisions
            (decision_id, session_id, project_path, text, tags, links_to, importance, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `,
        d.DecisionID,
        d.SessionID,
        d.ProjectPath,
        d.Text,
        strings.Join(d.Tags, ","),
        strings.Join(d.LinksTo, ","),
        d.Importance,
        d.CreatedAt.Unix(),
    )
    if err != nil {
        return fmt.Errorf("inserting decision: %w", err)
    }
    return nil
}

// ListDecisions implements Store.
func (s *SQLiteStore) ListDecisions(ctx context.Context, opts ListDecisionsOptions) ([]*Decision, error) {
    if opts.Limit <= 0 {
        opts.Limit = 50
    }
    if opts.Limit > 200 {
        opts.Limit = 200
    }
    
    var (
        clauses = []string{"project_path = ?"}
        args    = []any{opts.ProjectPath}
    )
    
    // Scope filter
    switch opts.Scope {
    case "session":
        if opts.SessionID != "" {
            clauses = append(clauses, "session_id = ?")
            args = append(args, opts.SessionID)
        }
    case "today":
        midnight := time.Now().Truncate(24 * time.Hour).Unix()
        clauses = append(clauses, "created_at >= ?")
        args = append(args, midnight)
    case "7d":
        weekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()
        clauses = append(clauses, "created_at >= ?")
        args = append(args, weekAgo)
    case "", "all":
        // no filter
    default:
        return nil, fmt.Errorf("invalid scope %q", opts.Scope)
    }
    
    // Importance filter
    switch opts.MinImportance {
    case "high":
        clauses = append(clauses, "importance = 'high'")
    case "normal":
        clauses = append(clauses, "importance IN ('normal', 'high')")
    case "", "low":
        // no filter
    default:
        return nil, fmt.Errorf("invalid min_importance %q", opts.MinImportance)
    }
    
    // Tag filter (OR-match: any tag matches)
    if len(opts.Tags) > 0 {
        tagClauses := make([]string, 0, len(opts.Tags))
        for _, tag := range opts.Tags {
            // Match comma-bounded tag in CSV column
            tagClauses = append(tagClauses,
                "(',' || tags || ',') LIKE ?")
            args = append(args, "%,"+tag+",%")
        }
        clauses = append(clauses, "("+strings.Join(tagClauses, " OR ")+")")
    }
    
    query := fmt.Sprintf(`
        SELECT id, decision_id, session_id, project_path, text, tags, links_to, importance, created_at
        FROM decisions
        WHERE %s
        ORDER BY created_at DESC
        LIMIT ?
    `, strings.Join(clauses, " AND "))
    args = append(args, opts.Limit)
    
    rows, err := s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, fmt.Errorf("listing decisions: %w", err)
    }
    defer rows.Close()
    
    var out []*Decision
    for rows.Next() {
        var (
            d        Decision
            tagsCSV  string
            linksCSV string
            createdAt int64
        )
        if err := rows.Scan(
            &d.ID, &d.DecisionID, &d.SessionID, &d.ProjectPath,
            &d.Text, &tagsCSV, &linksCSV, &d.Importance, &createdAt,
        ); err != nil {
            return nil, fmt.Errorf("scanning decision row: %w", err)
        }
        if tagsCSV != "" {
            d.Tags = strings.Split(tagsCSV, ",")
        }
        if linksCSV != "" {
            d.LinksTo = strings.Split(linksCSV, ",")
        }
        d.CreatedAt = time.Unix(createdAt, 0)
        out = append(out, &d)
    }
    return out, rows.Err()
}

// GetDecision implements Store.
func (s *SQLiteStore) GetDecision(ctx context.Context, decisionID string) (*Decision, error) {
    row := s.db.QueryRowContext(ctx, `
        SELECT id, decision_id, session_id, project_path, text, tags, links_to, importance, created_at
        FROM decisions
        WHERE decision_id = ?
    `, decisionID)
    
    var (
        d        Decision
        tagsCSV  string
        linksCSV string
        createdAt int64
    )
    err := row.Scan(
        &d.ID, &d.DecisionID, &d.SessionID, &d.ProjectPath,
        &d.Text, &tagsCSV, &linksCSV, &d.Importance, &createdAt,
    )
    if err == sql.ErrNoRows {
        return nil, nil
    }
    if err != nil {
        return nil, fmt.Errorf("getting decision: %w", err)
    }
    if tagsCSV != "" {
        d.Tags = strings.Split(tagsCSV, ",")
    }
    if linksCSV != "" {
        d.LinksTo = strings.Split(linksCSV, ",")
    }
    d.CreatedAt = time.Unix(createdAt, 0)
    return &d, nil
}

// newDecisionID generates a unique ID for a decision.
// Format: "dec_<unix_micro>_<4hex>"
func newDecisionID() string {
    b := make([]byte, 2)
    if _, err := rand.Read(b); err != nil {
        return fmt.Sprintf("dec_%d_0000", time.Now().UnixMicro())
    }
    return fmt.Sprintf("dec_%d_%s", time.Now().UnixMicro(), hex.EncodeToString(b))
}
```

---

## Handler 1: ctx_note

**File:** `internal/handlers/note.go` (new)

```go
package handlers

import (
    "context"
    "fmt"
    "strings"

    "github.com/modelcontextprotocol/go-sdk/mcp"

    "github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// NoteInput is the input for ctx_note.
type NoteInput struct {
    Text       string   `json:"text"                 jsonschema:"the decision or note text (1-2 sentences recommended)"`
    Tags       []string `json:"tags,omitempty"       jsonschema:"freeform tags for filtering, e.g. ['arch','phase7']"`
    LinksTo    []string `json:"links_to,omitempty"   jsonschema:"output_ids this decision relates to"`
    Importance string   `json:"importance,omitempty" jsonschema:"low | normal | high (default: normal)"`
}

// NoteOutput is the response from ctx_note.
type NoteOutput struct {
    DecisionID string `json:"decision_id"`
    SavedAt    string `json:"saved_at"`
    Echo       string `json:"echo"` // first 100 chars of text, for confirmation
}

// NoteHandler handles ctx_note.
type NoteHandler struct {
    st          store.Store
    projectPath string
}

func NewNoteHandler(st store.Store, projectPath string) *NoteHandler {
    return &NoteHandler{st: st, projectPath: projectPath}
}

func (h *NoteHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input NoteInput) (*mcp.CallToolResult, NoteOutput, error) {
    // Validate
    text := strings.TrimSpace(input.Text)
    if text == "" {
        return nil, NoteOutput{}, fmt.Errorf("text must not be empty")
    }
    if len(text) > 2000 {
        return nil, NoteOutput{}, fmt.Errorf("text too long: %d chars (max 2000)", len(text))
    }
    
    importance := input.Importance
    if importance == "" {
        importance = store.ImportanceNormal
    }
    switch importance {
    case store.ImportanceLow, store.ImportanceNormal, store.ImportanceHigh:
        // ok
    default:
        return nil, NoteOutput{}, fmt.Errorf("invalid importance %q (must be low|normal|high)", importance)
    }
    
    // Sanitize tags (no commas, no whitespace-only)
    cleanTags := make([]string, 0, len(input.Tags))
    for _, t := range input.Tags {
        t = strings.TrimSpace(t)
        if t == "" || strings.ContainsAny(t, ",\n") {
            continue
        }
        cleanTags = append(cleanTags, t)
    }
    
    d := &store.Decision{
        SessionID:   mcpSessionID, // from session_event.go (v0.4.2)
        ProjectPath: h.projectPath,
        Text:        text,
        Tags:        cleanTags,
        LinksTo:     input.LinksTo,
        Importance:  importance,
    }
    
    if err := h.st.SaveDecision(ctx, d); err != nil {
        return nil, NoteOutput{}, fmt.Errorf("saving decision: %w", err)
    }
    
    // Record this tool call as a session_event (consistency with v0.4.2)
    recordToolCall(
        ctx, h.st, h.projectPath, "ctx_note",
        truncatePreview(text, 200),
        d.DecisionID,
        "note: "+truncatePreview(text, 80),
    )
    
    echo := text
    if len(echo) > 100 {
        echo = echo[:100] + "…"
    }
    
    return nil, NoteOutput{
        DecisionID: d.DecisionID,
        SavedAt:    d.CreatedAt.Format(time.RFC3339),
        Echo:       echo,
    }, nil
}
```

---

## Handler 2: ctx_list_notes

**File:** `internal/handlers/list_notes.go` (new)

```go
package handlers

import (
    "context"
    "fmt"
    "time"

    "github.com/modelcontextprotocol/go-sdk/mcp"

    "github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// ListNotesInput is the input for ctx_list_notes.
type ListNotesInput struct {
    Scope         string   `json:"scope,omitempty"          jsonschema:"session | today | 7d | all (default: 7d)"`
    Tags          []string `json:"tags,omitempty"           jsonschema:"OR-match filter; empty = no filter"`
    MinImportance string   `json:"min_importance,omitempty" jsonschema:"low | normal | high (default: low)"`
    Limit         int      `json:"limit,omitempty"          jsonschema:"max results (default: 20, max: 100)"`
}

// ListNotesOutput is the response.
type ListNotesOutput struct {
    Decisions []DecisionOut `json:"decisions"`
    Count     int           `json:"count"`
    Scope     string        `json:"scope"`
}

// DecisionOut is the wire format for one decision.
type DecisionOut struct {
    DecisionID string   `json:"decision_id"`
    Text       string   `json:"text"`
    Tags       []string `json:"tags,omitempty"`
    LinksTo    []string `json:"links_to,omitempty"`
    Importance string   `json:"importance"`
    AgoSeconds int64    `json:"ago_seconds"`
    AgoHuman   string   `json:"ago_human"`
    SavedAt    string   `json:"saved_at"`
}

// ListNotesHandler handles ctx_list_notes.
type ListNotesHandler struct {
    st          store.Store
    projectPath string
}

func NewListNotesHandler(st store.Store, projectPath string) *ListNotesHandler {
    return &ListNotesHandler{st: st, projectPath: projectPath}
}

func (h *ListNotesHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input ListNotesInput) (*mcp.CallToolResult, ListNotesOutput, error) {
    scope := input.Scope
    if scope == "" {
        scope = "7d"
    }
    
    limit := input.Limit
    if limit <= 0 {
        limit = 20
    }
    if limit > 100 {
        limit = 100
    }
    
    decisions, err := h.st.ListDecisions(ctx, store.ListDecisionsOptions{
        ProjectPath:   h.projectPath,
        SessionID:     mcpSessionID,
        Scope:         scope,
        MinImportance: input.MinImportance,
        Tags:          input.Tags,
        Limit:         limit,
    })
    if err != nil {
        return nil, ListNotesOutput{}, fmt.Errorf("listing decisions: %w", err)
    }
    
    // Record this tool call
    recordToolCall(
        ctx, h.st, h.projectPath, "ctx_list_notes",
        scope, "", "list_notes: "+scope,
    )
    
    now := time.Now()
    out := ListNotesOutput{
        Scope: scope,
        Count: len(decisions),
    }
    for _, d := range decisions {
        ago := now.Sub(d.CreatedAt)
        out.Decisions = append(out.Decisions, DecisionOut{
            DecisionID: d.DecisionID,
            Text:       d.Text,
            Tags:       d.Tags,
            LinksTo:    d.LinksTo,
            Importance: d.Importance,
            AgoSeconds: int64(ago.Seconds()),
            AgoHuman:   humanAgeShort(ago),
            SavedAt:    d.CreatedAt.Format(time.RFC3339),
        })
    }
    return nil, out, nil
}

// humanAgeShort returns "5m", "3h", "2d" — short form for compact display.
func humanAgeShort(d time.Duration) string {
    switch {
    case d < time.Minute:
        return "<1m"
    case d < time.Hour:
        return fmt.Sprintf("%dm", int(d.Minutes()))
    case d < 24*time.Hour:
        return fmt.Sprintf("%dh", int(d.Hours()))
    default:
        return fmt.Sprintf("%dd", int(d.Hours()/24))
    }
}
```

---

## Update session_init to inject decisions

**File:** `internal/handlers/session_init.go` — add decisions to output

Find the existing `SessionInitOutput` struct and add a new field:

```go
type SessionInitOutput struct {
    // ... existing fields ...
    
    RecentDecisions []DecisionDigest `json:"recent_decisions,omitempty" jsonschema:"architectural decisions and rationale from recent sessions; survives /compact"`
}

type DecisionDigest struct {
    DecisionID string   `json:"decision_id"`
    Text       string   `json:"text"`
    Tags       []string `json:"tags,omitempty"`
    AgoHuman   string   `json:"ago"`
    Importance string   `json:"importance"`
}
```

In `Handle`, after populating `RecentEvents` and before returning, add:

```go
// Inject recent decisions (last 10, normal+high importance from past 7 days)
decisions, derr := h.st.ListDecisions(ctx, store.ListDecisionsOptions{
    ProjectPath:   h.projectPath,
    Scope:         "7d",
    MinImportance: "normal",
    Limit:         10,
})
if derr == nil {
    now := time.Now()
    for _, d := range decisions {
        out.RecentDecisions = append(out.RecentDecisions, DecisionDigest{
            DecisionID: d.DecisionID,
            Text:       d.Text,
            Tags:       d.Tags,
            AgoHuman:   humanAgeShort(now.Sub(d.CreatedAt)),
            Importance: d.Importance,
        })
    }
}
```

Also update the `next_action_hint` logic — if there are recent decisions, add a line:

```go
if len(out.RecentDecisions) > 0 {
    out.NextActionHint += " You have " +
        fmt.Sprintf("%d recent architectural decisions logged — ", len(out.RecentDecisions)) +
        "review them to understand prior reasoning."
}
```

---

## Register tools in server

**File:** `internal/server/server.go`

```go
noteH := handlers.NewNoteHandler(st, projectPath)
mcp.AddTool(srv, &mcp.Tool{
    Name: "ctx_note",
    Description: `[DECISION LOG] Save an architectural decision, design rationale, or important reasoning that should survive /compact and future sessions.

Use this when:
- You make a non-obvious design choice ("chose X over Y because Z")
- You discover a constraint that future-you needs to know ("can't use approach A because of dep B")
- You make a tradeoff that's not encoded in code ("simplified for now, will revisit if N exceeds 1000")
- User confirms an important decision ("agreed: use 7-day staleness threshold")

DO NOT use for:
- Routine progress updates ("starting task 3")
- Tool output summaries (use ctx_execute / ctx_read_file instead)
- Anything that's already obvious from the code

Notes are scoped per-project, persist across sessions, and are surfaced in ctx_session_init.

Keep notes short (1-2 sentences ideal, max 2000 chars). Tag with topics like 'arch', 'perf', 'security' for filterability. Set importance='high' only for genuinely critical decisions you'd want flagged at session start.`,
}, noteH.Handle)

listNotesH := handlers.NewListNotesHandler(st, projectPath)
mcp.AddTool(srv, &mcp.Tool{
    Name: "ctx_list_notes",
    Description: `[DECISION LOG] List recent decisions/notes saved via ctx_note.

Use this when:
- Resuming work after /compact and want to see what was decided
- Investigating why a piece of code looks the way it does
- Looking for a specific decision by tag

Default scope is "7d" (last 7 days). Use "session" for current session only, "today" for today, "all" for everything.`,
}, listNotesH.Handle)
```

---

## Update version

**File:** `cmd/ctx-saver/main.go`

```go
const serverVersion = "0.5.1"
```

**Why minor version bump:** new feature (not breaking, but adds new capability and new schema). Consistent with semver pre-1.0 conventions in this project.

---

## Tests

### Test 1: store

**File:** `internal/store/decisions_test.go` (new)

Test cases:

1. `TestSaveDecision_Basic` — save, verify GetDecision returns same data
2. `TestSaveDecision_AutoFillsDefaults` — empty importance defaults to "normal", empty CreatedAt = now, empty DecisionID auto-generated
3. `TestListDecisions_ScopeAll` — save 3 decisions, list all
4. `TestListDecisions_ScopeSession` — save 2 with session A, 1 with session B; list session=A returns 2
5. `TestListDecisions_ScopeToday` — save 1 today, 1 yesterday (manual created_at); today scope returns 1
6. `TestListDecisions_ScopeSevenDays` — save in window vs out
7. `TestListDecisions_TagsFilter` — OR match, multiple tags
8. `TestListDecisions_ImportanceFilter` — high, normal+high, all
9. `TestListDecisions_LimitAndOrder` — newest first, respect limit
10. `TestListDecisions_DifferentProject` — isolation by project_path
11. `TestListDecisions_InvalidScope` — error
12. `TestNewDecisionID_Format` — matches pattern `dec_\d+_[0-9a-f]{4}`

### Test 2: handler

**File:** `internal/handlers/note_test.go` (new)

```go
func TestNoteHandler_HappyPath(t *testing.T) {
    mock := newMockStore()
    h := NewNoteHandler(mock, "/proj")
    
    _, out, err := h.Handle(ctx, nil, NoteInput{
        Text: "Use WithFreshness pattern because 15 test sites would break",
        Tags: []string{"arch", "phase7"},
        Importance: "high",
    })
    require.NoError(t, err)
    require.NotEmpty(t, out.DecisionID)
    require.Contains(t, out.Echo, "Use WithFreshness")
    require.Equal(t, 1, mock.savedDecisions)
}
```

Other tests:
- `TestNoteHandler_EmptyTextRejected`
- `TestNoteHandler_TooLongRejected` (> 2000 chars)
- `TestNoteHandler_InvalidImportance` 
- `TestNoteHandler_DefaultImportance` (empty → normal)
- `TestNoteHandler_TagsSanitized` (commas/newlines stripped)
- `TestNoteHandler_RecordsSessionEvent` (verifies recordToolCall)

### Test 3: list_notes handler

**File:** `internal/handlers/list_notes_test.go` (new)

- `TestListNotesHandler_DefaultScope` (7d)
- `TestListNotesHandler_LimitClamping` (>100 → 100)
- `TestListNotesHandler_AgeHuman` (returns "5m", "3h", etc.)
- `TestListNotesHandler_RecordsSessionEvent`

### Test 4: session_init integration

**File:** `internal/handlers/session_init_test.go` — add cases

- `TestSessionInit_IncludesRecentDecisions` — seed 3 decisions, verify `RecentDecisions` has 3
- `TestSessionInit_ExcludesLowImportance` — seed mix; only normal+high in output
- `TestSessionInit_HintMentionsDecisionsCount` — when decisions present

### Mock store update

In `internal/handlers/handlers_test.go`, extend mock:

```go
type mockStore struct {
    // ... existing fields
    savedDecisions int
    decisions      []*store.Decision
}

func (m *mockStore) SaveDecision(_ context.Context, d *store.Decision) error {
    if d.DecisionID == "" {
        d.DecisionID = fmt.Sprintf("dec_%d_test", len(m.decisions))
    }
    m.savedDecisions++
    m.decisions = append(m.decisions, d)
    return nil
}

func (m *mockStore) ListDecisions(_ context.Context, opts store.ListDecisionsOptions) ([]*store.Decision, error) {
    var out []*store.Decision
    for _, d := range m.decisions {
        if d.ProjectPath != opts.ProjectPath {
            continue
        }
        // ... apply other filters as needed for the specific test
        out = append(out, d)
    }
    return out, nil
}

func (m *mockStore) GetDecision(_ context.Context, id string) (*store.Decision, error) {
    for _, d := range m.decisions {
        if d.DecisionID == id {
            return d, nil
        }
    }
    return nil, nil
}
```

---

## Documentation

### CLAUDE.md / README — add tool descriptions

Append to tools section:

```markdown
## Decision Log (v0.5.1)

### ctx_note

Save an architectural decision or rationale that should survive /compact.

```
ctx_note(
  text="Chose WithFreshness builder pattern; positional arg would break 15 test sites",
  tags=["arch", "phase7"],
  importance="high"
)
```

### ctx_list_notes

List recent decisions, optionally filtered by tag/importance/scope.

```
ctx_list_notes(scope="session")
ctx_list_notes(tags=["arch"], min_importance="high")
```

Decisions are also injected into `ctx_session_init` so AI sees them automatically at session start.
```

### copilot-instructions.md — add Rule 6

```markdown
### Rule 6: Log architectural decisions

When you make a non-obvious design choice or learn a constraint that future-you needs to know:

```
ctx_note(text="...", tags=["arch", "<area>"], importance="high"|"normal")
```

Examples to log:
- "Chose X over Y because Z" (design choices)
- "Cannot use approach A because of constraint B" (discovered limits)
- "User confirmed: 7-day threshold for stale cache" (decisions made together)

Examples NOT to log:
- "Starting task 3" (routine progress)
- "Read file foo.go" (already in session_events)

These notes survive /compact and are surfaced at next ctx_session_init.
```

---

## Acceptance criteria

- [ ] Migration v5 runs cleanly on existing v0.4.2 DB without data loss
- [ ] `ctx_note` saves with auto-generated decision_id, returns confirmation
- [ ] `ctx_note` rejects empty text and text > 2000 chars
- [ ] `ctx_note` accepts importance=low|normal|high; defaults to normal
- [ ] `ctx_note` records a `mcp_tool_call` session_event
- [ ] `ctx_list_notes` returns decisions newest-first
- [ ] Scope filters (session/today/7d/all) work correctly
- [ ] Tag filter is OR-match
- [ ] Importance filter respects min_importance semantics (low=all, normal=normal+high, high=high only)
- [ ] `ctx_session_init` includes `recent_decisions` field with up to 10 normal+high items from last 7d
- [ ] `next_action_hint` mentions decisions count when non-zero
- [ ] Decisions are scoped per-project (different project_path → isolated)
- [ ] `make lint test` passes clean
- [ ] `serverVersion` bumped to `0.5.1`
- [ ] Documentation updated (README, README.th, CLAUDE.md, copilot-instructions.md)

---

## Non-goals (do NOT do in this fix)

- ❌ Don't add full-text search on decisions (use ctx_search if needed in future)
- ❌ Don't add edit/delete operations (immutability simplifies semantics; add if requested later)
- ❌ Don't add cross-project queries (privacy; respect existing per-project boundary)
- ❌ Don't normalize tags into separate table (CSV is fine for v1)
- ❌ Don't auto-extract decisions from session_events (explicit is better — AI should choose what's worth logging)
- ❌ Don't add markdown rendering (text is plain text)
- ❌ Don't expose internal `id` integer (use decision_id only)

---

## Hand-off context for Copilot

This feature was requested after Phase 7 implementation. The pain point: during a multi-hour /compact-heavy session, architectural decisions and rationale get lost when context is compacted. ctx-saver currently captures tool outputs but not reasoning.

The design intentionally:
- Mirrors the SaveSessionEvent / mcpSessionID architecture from v0.4.2 (consistent patterns)
- Reuses recordToolCall for telemetry (so ctx_stats counts these calls correctly)
- Keeps schema simple (CSV strings instead of join tables) — can normalize later if needed
- Surfaces decisions through ctx_session_init (the existing "context bootstrap" path) instead of inventing a new injection mechanism

Keep the implementation small, focused, and consistent with existing patterns. Don't refactor neighboring code.

## Verification

After merge + `make install`, run a quick functional test:

```bash
# 1. Clean DB
rm -f /Users/chonlakan/Desktop/testing/ctx-saver-fixtures/.ctx-saver/outputs.db

# 2. In Copilot Chat (Agent mode), paste:
"
ใช้ ctx_note บันทึก: 'test decision: ใช้ ctx_note pattern เพราะ /compact ทำให้ลืม'
แล้วเรียก ctx_list_notes ดูว่าบันทึกได้ไหม
"

# 3. Verify DB
DB="/Users/chonlakan/Desktop/testing/ctx-saver-fixtures/.ctx-saver/outputs.db"
sqlite3 "$DB" "SELECT decision_id, text, importance FROM decisions;"
sqlite3 "$DB" "SELECT tool_name, COUNT(*) FROM session_events GROUP BY 1;"
```

Expected:
- `decisions` row inserted with the test text
- `session_events` shows `ctx_note` and `ctx_list_notes` calls
- `ctx_list_notes` response includes the saved decision

If both work → feature complete.
