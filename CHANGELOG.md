# Changelog

All notable changes to ctx-saver will be documented in this file.

## v0.14.0 — Structured CI output, diffs, and managed storage

### Added
- ANSI CSI/OSC stripping before command output is summarized, returned, indexed,
  or stored.
- Format-aware summaries for xcodebuild/Gradle, pytest, Jest, kubectl/docker
  logs, golangci-lint, and ESLint.
- `ctx_get_full(diff_against=...)` unified diff mode with three context lines,
  add/remove counts, identical-output detection, and bounded large diffs.
- Schema migration v11 with zstd body compression, output access tracking, and
  optional `storage.max_db_size_mb` LRU enforcement.

### Changed
- Stored bodies larger than 4 KiB use zstd while FTS content remains plaintext,
  preserving search behavior.
- Reads update LRU access time asynchronously. Size enforcement protects outputs
  accessed or refreshed within the last hour and never touches decisions.

### Notes
- Existing plain rows remain readable. The schema-11 upgrade may run a one-time
  `VACUUM` to enable incremental auto-vacuum on existing databases.
- Compression applies to `full_output`, not the separate FTS index, so database
  savings depend on the relative size of those two components.
- Schema version is 11 and the MCP tool count remains 10.

## v0.13.1 — Copilot view routing and latency fixes

### Added
- `hooks.view_deny_threshold_bytes`, defaulting to 128 KiB and supporting `0`
  to disable Copilot native-view enforcement independently of output storage.
- Background token-metric backfill for stored outputs larger than 2 MiB.

### Fixed
- Copilot native `view` calls now always allow source-code files, preserving
  exact native read-to-edit workflows while redirecting only oversized
  reference files to `ctx_read_file`.
- `ctx_stats` scalar metric fields are present for every scope even when zero,
  so callers no longer need to retry with broader scopes to discover fields.
- Large stored outputs no longer block request completion on tokenization;
  purged rows and worker failures are handled without affecting requests.

### Notes
- Schema version remains 10 and the MCP tool count remains 10.

## v0.13.0 — Copilot reliability and exact token accounting

### Added
- `ctx-saver setup copilot [--repo-hooks]` installs MCP configuration,
  instructions, and hooks in one command.
- `ctx-saver doctor` validates the configured binary, version, instructions,
  hooks, and all 10 MCP tools through a real stdio `tools/list` smoke check.
- Exact `o200k_base` token accounting for stored outputs and returned summaries,
  with schema migration v10 and legacy-row visibility in `ctx_stats`.
- Copilot PreToolUse enforcement redirects large-risk native `bash` commands
  and oversized `view` reads to ctx-saver tools with actionable reasons.

### Changed
- The project now requires Go 1.26 and uses
  `github.com/tiktoken-go/tokenizer` v0.8.0 locally without API calls.
- Copilot instructions include a `tool_search` fallback when deferred MCP tools
  such as `ctx_stats` are not visible in a new chat.

### Notes
- MCP tool count remains 10. `setup` and `doctor` are CLI commands, not tools.
- Copilot still ignores SessionStart hook output; restoration remains driven by
  instructions calling `ctx_session_init`.

## v0.12.0 — GitHub Copilot hooks

### Added
- GitHub Copilot hook payload normalization for Copilot CLI, coding agent, and
  VS Code Preview, including JSON-string `toolArgs` and camelCase fields.
- Copilot-native PreToolUse allow/deny responses with redirect reasons,
  PostToolUse event capture, and `view`/`create` tool recognition.
- `ctx-saver init copilot-hooks`, installing personal hooks by default with an
  explicit `--repo` option for repository-level hooks.

### Notes
- Copilot ignores SessionStart hook output, so session restoration remains
  instruction-driven through `ctx_session_init`.
- No schema migration and no new MCP tools; schema version remains 9 and the
  tool count remains 10.

## v0.11.1 — Instruction consistency fix

### Fixed
- Aligned the `ctx_execute` tool description with `ctx_session_init`: commands
  that may produce large output should use ctx-saver, while git write/admin
  commands remain sanctioned native operations.
- Documented `auto_index_threshold_bytes` as the routing boundary and added
  regression tests preventing the conflicting "ALL commands" guidance from
  returning.

## v0.11.0 — Adherence measurement fix

### Added
- Schema migration v9 adds `session_events.output_bytes`, preserving `0` as
  unknown for pre-v9 rows.
- `ctx_stats` now reports `missed_large_outputs`, `missed_large_bytes`,
  `sanctioned_reads`, and a neutral `savings_note` when nothing needed
  summarising.

### Fixed
- Adherence counting now uses the same PostToolUse annotations as the hook, so
  git-safe native commands excluded since v0.8.4 are no longer counted.
- Native reads of files edited in the same session are sanctioned instead of
  reported as violations or missed opportunities.

### Changed
- `adherence_note` severity is based on large outputs that bypassed ctx-saver,
  not the raw ratio of native tool calls. `adherence_score` remains available
  for backward compatibility.

## v0.10.0 — Cached files in session_init + secret redaction

### Added
- **`cached_files` in `ctx_session_init`**: surfaces previously read files (reference
  reads) at session start with a short SHA, freshness `stale_level`, age, and a
  `changed_on_disk` flag. The flag is set when the file was edited or removed since it
  was cached (the file is re-hashed at init), so the agent can reuse cached content via
  `ctx_search` / `ctx_get_full` from turn 1 instead of re-reading — or knows to re-read.
- **Secret redaction before storage**: command and file output is scrubbed of well-known
  secret patterns (private key blocks, AWS keys, GitHub/GitLab/Slack tokens, JWTs, Bearer
  tokens, and generic `key=value` secrets) before it is summarised, returned, or stored.
  Matches are replaced with `[REDACTED:<rule>]` and reported in `stats.redacted_rules`.
  Enabled by default; configurable via the new `redaction` config block (`enabled`,
  `extra_patterns`). Invalid user-supplied patterns are logged and skipped, never failing
  startup.

### Notes
- No schema migration: `currentSchemaVersion` stays 8 and the tool count stays 10.
- Redaction applies to **new** output only. Rows already stored before upgrading are not
  retroactively scrubbed — use `ctx_purge` to clear them if needed.
- File cache invalidation is unaffected: `SourceHash` is still computed from the on-disk
  file, not from the redacted content.

## v0.9.1 — Copilot tool discovery docs

### Fixed
- Clarified VS Code Copilot deferred tool discovery via `tool_search` before `ctx_session_init`.
- Updated Claude Code and VS Code Copilot setup docs to reflect the current 10-tool surface.
- Documented the manual init fallback when `ctx_session_init` is not exposed by the current MCP client/toolset.

## v0.9.0 — Task scope + session handoff

### Added
- `ctx_note` supports optional `task` scoping and `action="handoff"` for
  cross-session, multi-host continuation workflows.
- `ctx_session_init(task="...")` loads recent decisions for a specific task;
  default session init now returns only unscoped notes to avoid unrelated noise.

### Changed
- High-importance decisions in `project-knowledge.md` are grouped by task.
- Codex and Copilot instruction templates document task-scoped handoff usage.

### Migration
- Schema v8 adds `decisions.task` and an index on `(project_path, task, created_at DESC)`.

## v0.8.5 — Knowledge quality improvements

### Changed
- **Top Commands filter**: only commands run ≥ 2 times appear in `project-knowledge.md` —
  one-off grep/search exploration commands no longer dominate the list
- **Recent commits section**: `project-knowledge.md` now includes last 7 git commits
  (`--oneline`) so AI sessions start with context of recent codebase changes

## v0.8.4 — PostToolUse git safe annotation fix

### Fixed
- `git add/commit/push/fetch/pull` and other write commands no longer annotated
  as `⚠️ NATIVE_SHELL` in session history — they are intentional native operations
  that ctx_execute cannot replace. This fixes false negatives in `adherence_score`.

## v0.8.3 — Store reliability + FTS5 performance + Token savings counter

### Fixed
- **session_events duplicate records** (migration v7): added `UNIQUE (session_id, event_type, tool_name, tool_input, summary, created_at)` constraint to prevent double-counting when a hook insert is retried. Existing duplicates are silently dropped during migration via `INSERT OR IGNORE`.
- **FTS5 overfetch**: when searching within a specific output (`outputID` filter), the previous implementation fetched `maxResults × 20` rows then discarded non-matching rows in Go. The filter is now pushed into SQL (`AND output_id = ?`), reducing rows read by up to 20×.

### Added
- `estimated_tokens_saved` field in `ctx_stats` (stats view) — shows roughly how many tokens ctx-saver avoided sending back to the AI, calculated from bytes saved divided by 4 (~4 chars/token).

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
