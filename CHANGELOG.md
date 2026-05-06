# Changelog

All notable changes to ctx-saver will be documented in this file.

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
