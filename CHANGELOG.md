# Changelog

All notable changes to ctx-saver will be documented in this file.

## v0.8.3 — Store reliability + FTS5 performance

### Fixed
- **session_events duplicate records** (migration v7): added `UNIQUE (session_id, event_type, tool_name, tool_input, summary, created_at)` constraint to prevent double-counting when a hook insert is retried. Existing duplicates are silently dropped during migration via `INSERT OR IGNORE`.
- **FTS5 overfetch**: when searching within a specific output (`outputID` filter), the previous implementation fetched `maxResults × 20` rows then discarded non-matching rows in Go. The filter is now pushed into SQL (`AND output_id = ?`), reducing rows read by up to 20×.

## v0.8.2 — Git allowlist + TTL tuning

### Fixed
- git add/commit/push/fetch/pull/merge/rebase/checkout/switch/restore/reset/stash no longer
  trigger redirect hints — safe write commands with small output
- git commit messages containing words like "log" or "diff" no longer falsely trigger hints
- git log/diff/blame now show soft redirect hint only (not block) — large output benefits from ctx_execute
- Default cache TTL raised 3600 → 14400 seconds (4h) — reduces false stale warnings in long sessions
- shell:git TTL raised 120 → 600 seconds (10 min)

### Added
- README Troubleshooting section: duplicate tool names (multi-host expected behavior) and
  ctx_session_init not called automatically

## v0.8.1 — Docs: README update for Codex CLI

### Changed
- README: added Codex CLI to Quick start, install section, tools table, and new "For Codex CLI users" section

## v0.8.0 — Codex CLI support

### Added
- `ctx-saver init codex` — installs MCP server into `~/.codex/config.toml`
  and hooks (PreToolUse, PostToolUse, SessionStart) into `~/.codex/hooks.json`
- `ctx-saver init agents-md` — creates AGENTS.md instruction file at project root
  (teaches Codex to use ctx-saver tools instead of native Shell/read)
- `configs/codex/AGENTS.md` template

### Result
ctx-saver now supports 3 AI coding hosts: Claude Code, Copilot Enterprise, and Codex CLI.
Hook formatter already supported Codex — no logic changes needed.

## v0.7.2 — Fix: read-only connection for KnowledgeStats

### Fixed
- `ctx_execute` (and other MCP tools) hanging while idle knowledge refresh was running — `KnowledgeStats` now uses a dedicated read-only connection (`roDB`) instead of the shared write connection, eliminating DB contention entirely
- Idle knowledge refresh goroutine now runs with a 30s context deadline, preventing indefinite blocking of MCP tool calls in worst-case scenarios

### Changed
- `SQLiteStore` now maintains two connections: `db` (writer, `MaxOpenConns=1`) and `roDB` (reader, `MaxOpenConns=4`)
- `Close()` closes both connections
- Migration 6: composite index `idx_outputs_project_created ON outputs(project_path, created_at)` for faster self-join in command-sequence aggregation

## v0.7.1 — Bug Fix

### Fixed
- `knowledge refresh` / `knowledge show` crash: `sql: Scan error on column index 2, name "avg_bytes": converting driver.Value type float64 to int64` — SQLite `COUNT(*)` and `AVG()` aggregate functions return `float64`; scan into `float64` intermediates and convert to `int`/`int64` afterwards ([#sqlite.go](internal/store/sqlite.go))

## v0.7.0 — Materialized Project Knowledge

### Added
- `ctx-saver knowledge refresh/show/reset` CLI subcommand
- `.ctx-saver/project-knowledge.md` — auto-generated project context file
  containing most-read files, most-run commands, command sequences, and
  high-importance decisions
- Idle detection: MCP server auto-refreshes knowledge after 30 min of
  inactivity (configurable via `knowledge.idle_minutes`; set to `0` to disable)
- `ctx-saver init` now auto-injects a knowledge reference line into
  `CLAUDE.md` and `copilot-instructions.md` (idempotent)
- `knowledge` config section in `.ctx-saver.yaml` with `min_sessions`,
  `idle_minutes`, `top_files_limit`, `top_commands_limit`, `decisions_limit`

### Result
AI sessions start with learned project context — no extra tokens per turn,
no manual maintenance. Works on both Claude Code and Copilot Enterprise
because idle detection reads from `session_events` DB, not Claude Code hooks.

## v0.6.2 — Tool consolidation for Copilot Enterprise compatibility

### Why
Copilot Enterprise enforces a 10-tool cap per MCP server. ctx-saver v0.6.1 had
12 tools; the last two registered (`ctx_list_notes`, `ctx_purge`) were silently
deferred and unreachable from Copilot sessions.

### Changed
- `ctx_note` now handles both save and list operations via `action` parameter
  (`action="save"` default when `text` is provided; `action="list"` default when `text` is empty).
  Default behavior preserved for existing callers.
- `ctx_stats` now handles both stats summary and outputs listing via `view` parameter
  (`view="stats"` default; `view="outputs"` lists cached outputs).
  Default behavior preserved for existing callers.

### Removed (breaking)
- `ctx_list_notes` — use `ctx_note` with `action="list"` instead
- `ctx_list_outputs` — use `ctx_stats` with `view="outputs"` instead

### Result
Tool count: 12 → 10. All features now callable on Copilot Enterprise.

## v0.6.1 — Tool description compression

### Changed
- Removed decorative banner separators (━━━) from `ctx_execute`, `ctx_read_file`,
  and `ctx_session_init` descriptions (~120 tokens/turn saved).
- Compressed `ctx_execute` "Why this matters" 4-bullet block to 1 line while
  preserving behavioral reasoning (~55 tokens/turn saved).

Total fixed overhead reduction: ~175 tokens/turn.

## v0.6.0 — Token reduction release

### Added
- `ctx_purge` tool for clearing cached outputs and session events for a project.
  - Preserves decision notes (`ctx_note` entries) by default.
  - `all=true` also deletes notes.
  - Requires `confirm="yes"` to execute (safety check against accidental invocation).
- `--fields=signatures` flag on `ctx_read_file` for code structure-only view.
  - Returns only function/type/const declarations with original line numbers.
  - Supported: Go (full), Python (~95%), Dart (basic regex with documented limits).
  - DB always stores full file content; signatures is a view-only filter applied before return.
  - Cache key unchanged — `fields=signatures` and full reads share the same cached entry.
- `freshness_policy` field in `ctx_session_init` response — contains the full
  stale_level action matrix that was previously repeated in 5 retrieval tool descriptions.

### Changed
- Default `auto_index_threshold_bytes`: **5120 → 32768** (5 KB → 32 KB).
  - Most Go/Python source files (300–500 lines) now return inline (`direct_output`)
    without requiring a `ctx_get_full` round-trip.
  - Large outputs (build logs, test output, spec dumps) still use `output_id` + summary.
  - User configs with an explicit value are **unaffected**.
- Tool descriptions for 5 retrieval tools no longer repeat the verbose freshness policy
  block (`ctx_search`, `ctx_list_outputs`, `ctx_get_full`, `ctx_outline`, `ctx_get_section`).
  Each now contains a single reference line: _"see ctx_session_init for usage policy"_.
  This reduces fixed per-turn token overhead by **~400–600 tokens**.

### Removed
- Verbose `CACHE FRESHNESS POLICY:` blocks from `ctx_search`, `ctx_list_outputs`,
  `ctx_get_full`, `ctx_outline`, and `ctx_get_section` tool descriptions.

### Migration
- See [`docs/migration-v0.6.md`](docs/migration-v0.6.md) for upgrade notes.

---

## v0.5.2 — Source file SHA-256 cache invalidation (hotfix)

- Added SHA-256 hash to `outputs` schema (migration 5) for file-backed cache invalidation.
- `ctx_read_file` now revalidates the hash on each call; stale cache is skipped automatically.
- Added `source_hash` column to SQLite store; pre-existing rows default to `""` (re-read on next call).

---

## v0.5.1 — Decision notes (ctx_note / ctx_list_notes)

- Added `ctx_note` tool for saving architectural decisions that survive `/compact`.
- Added `ctx_list_notes` tool for querying saved decisions by scope, tag, and importance.
- Decisions are injected into `ctx_session_init` (last 10, normal+high, 7-day window).
- Added `decisions` table (migration 4).

---

## v0.5.0 — Cache freshness policy (Phase 7)

- Added freshness metadata to every retrieval response: `stale_level`, `age_human`, `source_kind`, `refresh_hint`.
- Configurable per-source TTL rules with `auto_refresh` support.
- `user_confirm_threshold_seconds` (default 7 days) gates critical-stale outputs.
- Auto-refresh silently re-runs expired commands and updates the DB in-place.
- Added `configs/freshness-examples/` preset configurations.
