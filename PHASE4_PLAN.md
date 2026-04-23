# Phase 4 Plan — ctx-saver

Status: Phase 1, 2, 3 เสร็จแล้ว (ดู `README.md` / `README.th.md`)
Phase 4 เพิ่ม 2 features: **ctx_stats tool** + **Smart summarizer**

เป้าหมาย: ให้ AI (Copilot/Claude Code) ทำตามแผนนี้ต่อได้เองโดย commit เป็นชุด ๆ ผ่าน `make lint test` ระหว่างแต่ละ commit

---

## Ground rules สำหรับ AI ที่ทำ Phase 4

1. **อ่านให้ครบก่อนเริ่ม:** `README.md`, `README.th.md`, `CLAUDE.md`, `PROJECT_PLAN.md` (ถ้ามี)
2. **Commit แยกตามแผน** — ห้ามรวม sub-task ใน commit เดียว
3. **ระหว่างทุก commit รัน `make lint test`** — ถ้า fail ห้าม commit ให้แก้ก่อน
4. **ทำ Task 1 ให้เสร็จก่อน Task 2** — Task 2 จะใช้ข้อมูลจาก Task 1 ได้ดีขึ้น
5. **ก่อนเขียน code ในแต่ละ sub-task อธิบาย approach แล้วรอ user confirm** — อย่าพุ่งเขียนเลย
6. **ห้าม modify Phase 1-3 behavior** ที่ไม่จำเป็น — ถ้าต้องแตะ ให้แจ้ง user ก่อน
7. **รักษา coding conventions เดิม:**
   - `gofmt` clean
   - Error wrap ด้วย `fmt.Errorf("...: %w", err)`
   - Interface-first (Store, Sandbox, Summarizer)
   - Dependency injection via struct fields
   - `log/slog` structured logging
   - Table-driven tests
   - Doc comment บน exported types
   - ห้าม `panic` ใน handler
   - ห้ามใช้ global state

---

# Task 1: ctx_stats MCP tool

**Scope:** 1-2 วัน
**Goal:** ให้ AI และ user เช็คได้ว่า ctx-saver ประหยัด token ไปเท่าไหร่, hook ทำงานมากแค่ไหน

## รายละเอียด

### 1.1 เพิ่ม Store method `GetStats`

ไฟล์: `internal/store/store.go`

เพิ่ม method ใน interface:
```go
// GetStats returns aggregate statistics for outputs and session events
// belonging to projectPath, filtered by scope.
GetStats(ctx context.Context, projectPath, scope string) (*Stats, error)
```

เพิ่ม struct types ใน package `store`:
```go
// StatsScope represents the time window for stats aggregation.
type StatsScope string

const (
    ScopeSession StatsScope = "session" // since server process started
    ScopeToday   StatsScope = "today"   // today local midnight
    Scope7d      StatsScope = "7d"      // last 7 days
    ScopeAll     StatsScope = "all"     // everything retained
)

// Stats is the aggregate result returned by Store.GetStats.
type Stats struct {
    Scope           string
    OutputsStored   int
    RawBytes        int64
    LargestBytes    int64
    AvgDurationMs   int64
    TopCommands     []CommandStat
    LargestOutputs  []OutputMeta
    DangerousBlocked int
    RedirectedToMCP  int
    EventsCaptured   int
}

// CommandStat is the aggregate for one display-command bucket.
type CommandStat struct {
    Command    string // sanitised display string (same as stored in outputs.command)
    Count      int
    TotalBytes int64
}
```

**Implementation notes:**
- Session scope: ใช้ filter `created_at >= <server_start_time>` — server ต้อง pass start time เข้า query (เก็บใน struct handler)
- Today scope: `DATE(created_at, 'unixepoch', 'localtime') = DATE('now', 'localtime')`
- 7d scope: `created_at >= strftime('%s', 'now', '-7 days')`
- All scope: no time filter
- `TopCommands` query: `GROUP BY command ORDER BY COUNT(*) DESC LIMIT 5`
- `LargestOutputs` query: `ORDER BY size_bytes DESC LIMIT 3`
- Hook stats จาก `session_events` — นับ `event_type` + ค้นใน `summary` ด้วย `LIKE '%deny%'` หรือ `'%redirect%'`

### 1.2 Implement ใน SQLiteStore

ไฟล์: `internal/store/sqlite.go`

เพิ่ม `(s *SQLiteStore) GetStats(...)` — ใช้ SQL aggregate, ไม่ load full rows
ระวัง: ต้อง handle NULL จาก `AVG` / `SUM` เมื่อ DB ว่าง (ใช้ `COALESCE`)

### 1.3 Test ใน SQLiteStore

ไฟล์: `internal/store/sqlite_test.go`

เพิ่ม:
- `TestGetStats_EmptyDB` — zeros + nil slices
- `TestGetStats_SingleOutput` — counts ถูก
- `TestGetStats_MultipleCommands` — top commands ถูก เรียงตาม count
- `TestGetStats_ScopeFilter` — seed events ที่เวลาต่างกัน, verify 7d ไม่นับอันเก่าเกิน 7 วัน
- `TestGetStats_HookCounts` — seed session_events ที่ summary มีคำว่า "deny" / "redirect" แล้ว verify count

### 1.4 Stats handler

ไฟล์ใหม่: `internal/handlers/stats.go`

```go
package handlers

import (
    "context"
    "fmt"
    "time"

    "github.com/modelcontextprotocol/go-sdk/mcp"

    "github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
    "github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// StatsInput is the input for ctx_stats MCP tool.
type StatsInput struct {
    Scope string `json:"scope,omitempty" jsonschema:"session | today | 7d | all (default: session)"`
}

// StatsOutput mirrors store.Stats with estimated savings added.
type StatsOutput struct {
    Scope                 string              `json:"scope"`
    OutputsStored         int                 `json:"outputs_stored"`
    RawBytes              int64               `json:"raw_bytes"`
    EstimatedSummaryBytes int64               `json:"estimated_summary_bytes"`
    SavingPercent         float64             `json:"saving_percent"`
    AvgDurationMs         int64               `json:"avg_duration_ms"`
    TopCommands           []CommandStatOut    `json:"top_commands,omitempty"`
    LargestOutputs        []OutputMetaOut     `json:"largest_outputs,omitempty"`
    HookStats             HookStatsOut        `json:"hook_stats"`
}

type CommandStatOut struct {
    Command    string `json:"command"`
    Count      int    `json:"count"`
    TotalBytes int64  `json:"total_bytes"`
}

type OutputMetaOut struct {
    OutputID  string `json:"output_id"`
    Command   string `json:"command"`
    SizeBytes int64  `json:"size_bytes"`
    LineCount int    `json:"line_count"`
}

type HookStatsOut struct {
    DangerousBlocked int `json:"dangerous_blocked"`
    RedirectedToMCP  int `json:"redirected_to_mcp"`
    EventsCaptured   int `json:"events_captured"`
}

type StatsHandler struct {
    cfg          *config.Config
    st           store.Store
    projectPath  string
    serverStart  time.Time
}

func NewStatsHandler(cfg *config.Config, st store.Store, projectPath string, serverStart time.Time) *StatsHandler {
    return &StatsHandler{cfg: cfg, st: st, projectPath: projectPath, serverStart: serverStart}
}

func (h *StatsHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input StatsInput) (*mcp.CallToolResult, StatsOutput, error) {
    scope := input.Scope
    if scope == "" {
        scope = "session"
    }
    // Validate scope
    switch scope {
    case "session", "today", "7d", "all":
    default:
        return nil, StatsOutput{}, fmt.Errorf("invalid scope %q — must be: session | today | 7d | all", scope)
    }

    stats, err := h.st.GetStats(ctx, h.projectPath, scope)
    if err != nil {
        return nil, StatsOutput{}, fmt.Errorf("fetching stats: %w", err)
    }

    // Estimate summary bytes: headLines * 80 + tailLines * 80 + 200 (stats line) per output
    estPerOutput := int64(h.cfg.Summary.HeadLines*80 + h.cfg.Summary.TailLines*80 + 200)
    estimatedSummaryBytes := estPerOutput * int64(stats.OutputsStored)

    savingPercent := 0.0
    if stats.RawBytes > 0 {
        saved := stats.RawBytes - estimatedSummaryBytes
        if saved < 0 {
            saved = 0
        }
        savingPercent = float64(saved) / float64(stats.RawBytes) * 100
    }

    out := StatsOutput{
        Scope:                 scope,
        OutputsStored:         stats.OutputsStored,
        RawBytes:              stats.RawBytes,
        EstimatedSummaryBytes: estimatedSummaryBytes,
        SavingPercent:         savingPercent,
        AvgDurationMs:         stats.AvgDurationMs,
        HookStats: HookStatsOut{
            DangerousBlocked: stats.DangerousBlocked,
            RedirectedToMCP:  stats.RedirectedToMCP,
            EventsCaptured:   stats.EventsCaptured,
        },
    }
    for _, c := range stats.TopCommands {
        out.TopCommands = append(out.TopCommands, CommandStatOut{
            Command: c.Command, Count: c.Count, TotalBytes: c.TotalBytes,
        })
    }
    for _, o := range stats.LargestOutputs {
        out.LargestOutputs = append(out.LargestOutputs, OutputMetaOut{
            OutputID: o.OutputID, Command: o.Command,
            SizeBytes: o.SizeBytes, LineCount: o.LineCount,
        })
    }
    return nil, out, nil
}
```

### 1.5 Register tool ใน server

ไฟล์: `internal/server/server.go`

- ต้องรับ `serverStart time.Time` เข้า `New(...)` (pass จาก main.go `time.Now()`)
- เพิ่ม tool registration:
  ```go
  statsH := handlers.NewStatsHandler(cfg, st, projectPath, serverStart)
  mcp.AddTool(srv, &mcp.Tool{
      Name: "ctx_stats",
      Description: "Report ctx-saver statistics: outputs stored, bytes saved, top commands, hook activity. Scope: session | today | 7d | all (default: session). Use this to verify ctx-saver is saving context window space effectively.",
  }, statsH.Handle)
  ```

### 1.6 Update main.go

ไฟล์: `cmd/ctx-saver/main.go`

- เพิ่ม `serverStart := time.Now()` ต้น `runServer()`
- Pass เข้า `server.New(..., serverStart)`

### 1.7 Test ใน handler

ไฟล์ใหม่: `internal/handlers/stats_test.go`

- Mock store ที่ return fixed stats
- Test: empty → all zeros
- Test: populated → fields ตรง, saving% > 0
- Test: invalid scope → error
- Test: scope passthrough ไป store

### 1.8 Documentation

- อัพเดต `README.md` + `README.th.md` — เพิ่ม `ctx_stats` ใน tools table
- อัพเดต `docs/skills/ctx-saver/references/tools.md` — เพิ่มรายละเอียด
- อัพเดต `CLAUDE.md` — "When in doubt, call ctx_stats to verify tool is working"

## Task 1 — Commit sequence

```
1. feat(store): add GetStats interface + Stats types
2. feat(store): implement GetStats in SQLiteStore
3. test(store): GetStats coverage (5 test cases)
4. feat(handlers): add ctx_stats handler
5. feat(server): register ctx_stats tool with server start time
6. test(handlers): ctx_stats handler coverage
7. docs: document ctx_stats in README + CLAUDE.md
```

## Task 1 — Acceptance criteria

- [ ] `ctx_stats(scope="session")` return ตัวเลขจริง ไม่ error
- [ ] `ctx_stats(scope="7d")` filter ตามวันถูก
- [ ] `saving_percent` คำนวณถูก (raw_bytes สูงกว่า summary_bytes → % บวก)
- [ ] DB ว่าง → return zeros ไม่ crash
- [ ] Invalid scope → return error ที่อ่านเข้าใจ
- [ ] `make lint test` ผ่านทุก commit
- [ ] Test coverage ของ `internal/store` + `internal/handlers` ไม่ลดลง

---

# Task 2: Smart summarizer

**Scope:** 3-5 วัน
**Goal:** Summary ที่ฉลาดตาม format (Flutter test, Go test, JSON, git log) + fallback ไป generic

## Current state

`internal/summary/summary.go` มี `Summarize(output, headLines, tailLines) Summary` — generic head+tail
ใช้ใน `internal/handlers/execute.go` + `read_file.go` + `list.go`

## Target architecture

```
internal/summary/
├── summary.go              # เดิม — ให้ rename func เป็น GenericSummarize
├── summary_test.go         # เดิม
├── detector.go             # NEW — เลือก formatter
├── detector_test.go        # NEW
├── testdata/               # NEW — real fixtures
│   ├── flutter_test_pass.txt
│   ├── flutter_test_fail.txt
│   ├── go_test_pass.txt
│   ├── go_test_fail.txt
│   ├── git_log_linear.txt
│   ├── git_log_merges.txt
│   ├── acli_page.json
│   └── kubectl_pods.json
└── formats/
    ├── format.go           # NEW — interface
    ├── flutter_test.go     # NEW
    ├── flutter_test_test.go
    ├── go_test.go          # NEW
    ├── go_test_test.go
    ├── json.go             # NEW
    ├── json_test.go
    ├── git_log.go          # NEW
    ├── git_log_test.go
    └── generic.go          # NEW — wrapper รอบ GenericSummarize เดิม
```

## 2.1 Formatter interface

ไฟล์: `internal/summary/formats/format.go`

```go
// Package formats provides format-specific summarizers for ctx_execute outputs.
package formats

// Formatter summarizes command output in a format-specific way.
// Detect should be cheap (byte prefix / command substring checks).
type Formatter interface {
    // Name returns the formatter identifier (used in Summary.Format).
    Name() string

    // Detect returns true if this formatter can handle the output.
    // command is the sanitised command string (for hint-based detection).
    Detect(output []byte, command string) bool

    // Summarize produces a format-specific Summary.
    // Output should be compact (< 1 KB ideally) — callers rely on ctx_search
    // or ctx_get_full for details.
    Summarize(output []byte) Summary
}

// Summary is a format-aware summary result.
type Summary struct {
    Text       string         // human-readable summary, injected into context
    TotalLines int
    TotalBytes int
    Format     string         // formatter Name()
    Metadata   map[string]any // format-specific data (pass/fail counts etc.)
}
```

## 2.2 Detector

ไฟล์: `internal/summary/detector.go`

```go
package summary

import (
    "github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary/formats"
)

// registeredFormatters is the ordered list tried by Detect.
// Order matters: more specific formatters come first.
var registeredFormatters = []formats.Formatter{
    &formats.FlutterTestFormatter{},
    &formats.GoTestFormatter{},
    &formats.JSONFormatter{},
    &formats.GitLogFormatter{},
}

// Detect returns the first formatter that matches, or GenericFormatter as fallback.
func Detect(output []byte, command string) formats.Formatter {
    for _, f := range registeredFormatters {
        if f.Detect(output, command) {
            return f
        }
    }
    return &formats.GenericFormatter{
        HeadLines: 20,
        TailLines: 5,
    }
}

// DetectWithConfig lets callers override generic fallback settings.
func DetectWithConfig(output []byte, command string, headLines, tailLines int) formats.Formatter {
    for _, f := range registeredFormatters {
        if f.Detect(output, command) {
            return f
        }
    }
    return &formats.GenericFormatter{
        HeadLines: headLines,
        TailLines: tailLines,
    }
}
```

## 2.3 Formatter: FlutterTest

ไฟล์: `internal/summary/formats/flutter_test.go`

Detection:
- `command` มี `flutter test`
- หรือ output มี `"All tests passed!"` หรือ `"Some tests failed."`
- หรือ output มี `"Running flutter test"`

Parse (regex-based, ไม่ต้องเป๊ะ):
- `All tests passed!` + `NNNN: Some tests failed.` → success / failure
- Pattern `\+(\d+)\s*-(\d+)` หรือ `\+(\d+)\s*~(\d+)\s*-(\d+)` → passed/skipped/failed counts
- Lines ที่มี `[E]` หรือ `FAILED:` + filename `test/.../foo_test.dart` → failed test list
- Duration จาก `"Exited"` หรือ stopwatch line

Summary format:
```
Flutter tests: ✅ 47 passed, ❌ 3 failed, ⏭ 2 skipped
Duration: 12.4s
Failed tests:
  - test/payment_test.dart:45 — TestProcessPayment
  - test/auth_test.dart:12 — TestLogin
  - test/api_test.dart:78 — TestRetry
Use ctx_search queries=["FAIL", "exception"] to inspect stack traces.
```

Metadata:
```go
map[string]any{
    "passed":   47,
    "failed":   3,
    "skipped":  2,
    "duration_seconds": 12.4,
    "failed_tests":     []string{"test/payment_test.dart:45", ...},
}
```

## 2.4 Formatter: GoTest

ไฟล์: `internal/summary/formats/go_test.go`

Detection:
- command มี `go test`
- output มี `"=== RUN"` + (`"--- PASS:"` หรือ `"--- FAIL:"`)
- output มี `"ok  \tpkg/path"` หรือ `"FAIL\tpkg/path"`

Parse:
- Count lines ที่เริ่มด้วย `"--- PASS:"`, `"--- FAIL:"`, `"--- SKIP:"`
- Package results: lines `"ok  \t"` vs `"FAIL\t"`
- Failed test name + file:line จาก lines หลัง `--- FAIL:`
- Coverage: regex `coverage: (\d+\.\d+)% of statements`

Summary format:
```
Go tests: 23 packages PASS, 1 FAIL
Failed:
  - internal/payment — TestProcessPayment (0.05s)
    payment_test.go:78: expected 100, got 50
  - internal/payment — TestRetry (0.01s)
Coverage: 82.3%
Use ctx_get_full output_id=<id> line_range=[50,90] for full stack traces.
```

## 2.5 Formatter: JSON

ไฟล์: `internal/summary/formats/json.go`

Detection:
- `bytes.TrimSpace(output)` เริ่มด้วย `{` หรือ `[`
- Attempt `json.Valid()` — ถ้าไม่ผ่าน return false

Parse (ใช้ `encoding/json` decode เป็น `any` แล้ว traverse):
- Top-level structure: object keys / array length
- Key counts + value types + sizes
- Sample first value of each key (แสดง path `$.key`)
- Array: แสดง length + first element type

Summary format (for object):
```
JSON object (15.4 KB)
Top-level keys: 5
  id: string (24 chars)
  title: string (45 chars)
  version: "2.3.1"
  endpoints: array (12 items)
  schemas: array (8 items)
Sample: $.endpoints[0] = {method: "POST", path: "/api/v1/payments"}
Use ctx_search to query specific sections.
```

Summary format (for array):
```
JSON array (12.1 KB, 247 items)
Item type: object (common keys: id, name, status)
Sample: $[0] = {id: "abc-123", name: "payment_method", status: "active"}
Use ctx_search or ctx_get_full for details.
```

## 2.6 Formatter: GitLog

ไฟล์: `internal/summary/formats/git_log.go`

Detection:
- command มี `git log`
- output เริ่มด้วย `commit ` (40 hex) หรือมีบรรทัด `commit [0-9a-f]{7,40}`

Parse:
- Count commits: regex `^commit [0-9a-f]{7,40}` (multiline)
- Author lines: `^Author: (.+) <.+>`
- Date lines: `^Date:   (.+)` → parse first and last
- Subject: line หลัง empty line หลัง Author/Date

Summary format:
```
Git log: 153 commits (4 authors)
Newest: 2d ago — "fix: payment validation" (abc1234)
Oldest: 3mo ago — "initial commit" (def5678)
Top authors:
  - chonlakan (120 commits)
  - teammate (33 commits)
Use ctx_search queries=["bug fix", "refactor"] to find specific commits.
```

## 2.7 Formatter: Generic (wrapper)

ไฟล์: `internal/summary/formats/generic.go`

```go
package formats

import "github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary"

// GenericFormatter wraps summary.GenericSummarize to conform to Formatter.
type GenericFormatter struct {
    HeadLines int
    TailLines int
}

func (g *GenericFormatter) Name() string { return "generic" }

// Detect always returns true (catch-all fallback).
func (g *GenericFormatter) Detect(_ []byte, _ string) bool { return true }

func (g *GenericFormatter) Summarize(output []byte) Summary {
    s := summary.GenericSummarize(output, g.HeadLines, g.TailLines)
    return Summary{
        Text:       s.Text,
        TotalLines: s.TotalLines,
        TotalBytes: s.TotalBytes,
        Format:     "generic",
    }
}
```

**Note:** นี่จะสร้าง import cycle — ต้องย้าย `Summary` struct เดิมใน `internal/summary` หรือ restructure ให้ generic ไม่ import parent package
แนะนำ: เปลี่ยนให้ `formats/` เป็น top-level package ไม่ขึ้นกับ parent

## 2.8 Refactor existing summary.go

ไฟล์: `internal/summary/summary.go`

- Rename `Summarize` → `GenericSummarize` (keep behavior เหมือนเดิม)
- Keep `FormatStats` function ที่ handlers ใช้
- เพิ่ม re-export สำหรับ backward compatibility (optional):
  ```go
  // Deprecated: use formats.GenericFormatter or Detect().
  func Summarize(output []byte, headLines, tailLines int) Summary {
      return GenericSummarize(output, headLines, tailLines)
  }
  ```

## 2.9 Integrate ใน execute handler

ไฟล์: `internal/handlers/execute.go`

```go
// Old (remove):
// sum := summary.Summarize(result.Output, headLines, h.cfg.Summary.TailLines)

// New:
formatter := summary.DetectWithConfig(
    result.Output,
    input.Code,
    headLines,
    h.cfg.Summary.TailLines,
)
sum := formatter.Summarize(result.Output)

// สำหรับ metadata-aware logging:
slog.Debug("summary produced",
    "format", sum.Format,
    "lines", sum.TotalLines,
    "bytes", sum.TotalBytes,
)

// ใช้ sum.Text ใน return เหมือนเดิม
```

เพิ่ม field `Format` ใน `ExecuteOutput`:
```go
type ExecuteOutput struct {
    // ... เดิม
    Format string `json:"format,omitempty" jsonschema:"summary format used: flutter_test | go_test | json | git_log | generic"`
}
```

## 2.10 Config toggle

ไฟล์: `internal/config/config.go`

เพิ่มใน `SummaryConfig`:
```go
type SummaryConfig struct {
    HeadLines               int      `yaml:"head_lines"`
    TailLines               int      `yaml:"tail_lines"`
    AutoIndexThresholdBytes int      `yaml:"auto_index_threshold_bytes"`
    SmartFormat             bool     `yaml:"smart_format"`                // NEW — default true
    EnabledFormatters       []string `yaml:"enabled_formatters,omitempty"` // NEW — nil = all
}
```

`Default()`:
```go
SmartFormat: true,
EnabledFormatters: nil, // nil = all enabled
```

Detector respect config — ถ้า `SmartFormat: false` ข้าม detection, ใช้ generic ตรง ๆ

## 2.11 Tests — format-specific fixtures

### Testdata files

สร้างใน `internal/summary/testdata/`:

**flutter_test_pass.txt** — ก็อปจริงจาก `flutter test` ที่ผ่านหมด (20-30 บรรทัด)

**flutter_test_fail.txt** — output ที่มี fail 2-3 test พร้อม stack trace

**go_test_pass.txt** — `go test ./... -v` ที่ผ่านหมด

**go_test_fail.txt** — มี `--- FAIL:` ชัด ๆ

**acli_page.json** — JSON object ขนาด ~3KB

**kubectl_pods.json** — JSON array หลาย items

**git_log_linear.txt** — output `git log -10 --oneline` หรือ default format

**Note สำหรับ Copilot:** ถ้าไม่มี output จริง สามารถ generate synthetic ได้ แต่ต้องสมจริง (ใช้ format ของเครื่องมือจริง)

### Test files

แต่ละ formatter ต้องมี test file ที่ test:
1. Detect: positive case (output จริง) + negative case (random text)
2. Summarize: basic fixture → verify Text มี key info
3. Summarize: edge case (empty output, partial output)
4. Summarize: metadata ถูก
5. Generic fallback wins เมื่อไม่ match

## 2.12 Documentation

- อัพเดต `README.md` + `README.th.md`:
  - เพิ่ม section "Smart Summarizer" อธิบาย format ที่ support
  - เพิ่ม sample output สำหรับแต่ละ format
- อัพเดต `CLAUDE.md` — "Summaries are format-aware: flutter_test, go_test, json, git_log, generic"
- เพิ่ม `docs/summary-formats.md` (optional) — detail สำหรับผู้ใช้ที่อยากเพิ่ม formatter เอง

## Task 2 — Commit sequence

```
1.  refactor(summary): rename Summarize → GenericSummarize (no behaviour change)
2.  feat(summary): add formats package with Formatter interface
3.  feat(summary/formats): generic formatter (wrapper)
4.  feat(summary): detector with fallback to generic
5.  test(summary): detector with generic-only fixtures
6.  feat(summary/formats): flutter_test formatter
7.  test(summary/formats): flutter_test fixtures + coverage
8.  feat(summary/formats): go_test formatter
9.  test(summary/formats): go_test fixtures + coverage
10. feat(summary/formats): json formatter
11. test(summary/formats): json fixtures + coverage
12. feat(summary/formats): git_log formatter
13. test(summary/formats): git_log fixtures + coverage
14. feat(config): add smart_format + enabled_formatters options
15. refactor(handlers): use Detect in execute handler
16. test(handlers): verify format field returned
17. docs: document smart summarizer in README + CLAUDE.md
```

## Task 2 — Acceptance criteria

- [ ] `flutter test` output → summary มี pass/fail counts + failed test names
- [ ] `go test ./...` output → summary มี package results + coverage
- [ ] JSON object → summary แสดง top-level keys + sample value
- [ ] JSON array → summary แสดง length + item type + sample
- [ ] `git log` output → summary มี commit count + newest/oldest + top authors
- [ ] Generic fallback ทำงานเมื่อ detect ไม่เจอ
- [ ] `smart_format: false` ใน config → ใช้ generic อย่างเดียว
- [ ] ExecuteOutput มี field `format` ตามที่ใช้
- [ ] `ctx_stats` (Task 1) report format breakdown ได้ (optional enhancement)
- [ ] Test coverage `internal/summary/formats/` ≥ 80%
- [ ] `make lint test` ผ่านทุก commit
- [ ] ไม่มี breaking change สำหรับ existing tools (read_file, list, get_full)

---

# Overall Phase 4 completion checklist

- [ ] Task 1 — `ctx_stats` ส่งผลจริง ใช้งานได้
- [ ] Task 2 — smart summarizer support 4 format + fallback
- [ ] `make lint test` clean
- [ ] README.md + README.th.md อัพเดต
- [ ] CLAUDE.md อัพเดต
- [ ] Test coverage รวมไม่ลดลง
- [ ] ใช้จริงใน Claude Code session ของ user — verify ว่า flow ดีขึ้น
- [ ] Benchmark updated (optional) — เพิ่ม smart summary scenarios

---

# สำหรับ AI ที่ทำตามแผนนี้

**เริ่มจาก:**

1. อ่าน `README.md`, `README.th.md`, `CLAUDE.md`
2. ดูโครงสร้างปัจจุบัน: `ls internal/` + `ls cmd/`
3. รัน `make test` ดูว่า baseline pass
4. อธิบาย approach สำหรับ Task 1 sub-task แรก (`feat(store): add GetStats interface`)
5. รอ user confirm
6. ถึงเริ่มเขียน code

**ถ้าเจอปัญหา:**
- Design ไม่ชัด → ถาม user ก่อนเขียน
- Test fail → แก้ก่อน commit ห้าม commit fail
- Scope creep → หยุด ถาม user ก่อนขยายงาน

**ห้ามทำ:**
- Commit fail tests
- รวมหลาย sub-task ใน commit เดียว
- เปลี่ยนพฤติกรรม Phase 1-3 ที่ทำงานดีอยู่แล้ว
- เพิ่ม dependency ใหม่โดยไม่บอก user
- Skip test ที่ตัวเอง "ไม่แน่ใจ" — บอก user แทน

Good luck 👍
