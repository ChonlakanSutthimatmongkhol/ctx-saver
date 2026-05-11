---
name: ctx-saver
description: >
  Workflow for running commands and reading files through ctx-saver MCP tools to reduce context window usage.
  Use when: running tests, builds, or any command with large output; reading large files (OpenAPI spec, logs, SQL migrations);
  fetching Confluence/Jira pages; searching previously stored outputs; retrieving full output by ID or line range;
  extracting a specific section from a long document by heading name; viewing only function/type signatures of a source file;
  purging stale cache before switching context; saving or querying architectural decision notes;
  creating task-scoped session handoffs with ctx_note(action="handoff", task="...");
  generating or viewing project-knowledge.md (learned project patterns from stored sessions).
  Tools: ctx_execute, ctx_read_file, ctx_search, ctx_get_full, ctx_outline, ctx_get_section, ctx_stats, ctx_purge, ctx_note.
  CLI: ctx-saver knowledge refresh/show/reset.
argument-hint: 'Describe the command or file you want to run/read'
---

# ctx-saver Workflow

ctx-saver is an MCP server that stores large command outputs in SQLite and returns a compact summary. For ctx_execute, summaries are format-aware (flutter_test, go_test, json, git_log, generic).

## When to Use

| Situation | Tool |
|-----------|------|
| Run shell/python/go/node command | `ctx_execute` |
| Read a large file (spec, log, SQL) | `ctx_read_file` |
| Read only function/type signatures of a source file | `ctx_read_file` with `fields="signatures"` |
| See document structure before searching | `ctx_outline` |
| Extract a specific section by heading name | `ctx_get_section` |
| Search in a previously stored output | `ctx_search` |
| List all stored outputs for this project | `ctx_stats` with `view="outputs"` |
| Get full output or specific line range | `ctx_get_full` |
| Verify ctx-saver is saving context / check hook activity | `ctx_stats` |
| Clear stale cache before switching context | `ctx_purge` |
| Save or resume task-scoped handoff notes | `ctx_note` with `action="handoff"` / `ctx_session_init` with `task` |
| Generate/view learned project patterns | `ctx-saver knowledge refresh` (CLI) |

## Core Decision Rule

- Output **≤32KB** → returned directly, no storage (v0.6.0+ default)
- Output **>32KB** → stored in `.ctx-saver/outputs.db`, AI receives summary only

---

## Procedure

### 1. Running a command

Use `ctx_execute` instead of a raw shell command whenever output may be large.

```json
{
  "language": "shell",
  "code": "go test ./...",
  "intent": "run all tests",
  "summary_lines": 20
}
```

**After receiving the response:**
- If `direct_output` is set → output was small, use it directly
- If `output_id` is set → output was stored; check `format` + read `summary` for overview, then use `ctx_search` or `ctx_get_full` if more detail is needed
- If `duplicate_hint` appears → same command already ran recently; prefer `ctx_get_full` or `ctx_search` on `previous_output_id` to reuse the cached result

### 2. Reading a large file

```json
{
  "path": "generated-doc.yaml",
  "process_script": "grep -A5 '/v1/users'",
  "language": "shell"
}
```

### 3. Exploring document structure

Before searching, use `ctx_outline` to see what sections exist so you don't have to guess keywords:

```json
{ "output_id": "out_20260422_76b3de65" }
```

Returns headings (`#`, `##`, `###`, `####`) and table headers with their line numbers.

### 3.5. Extracting a section by heading

After `ctx_outline` reveals heading names, use `ctx_get_section` to pull the exact section — no need to guess line numbers:

```json
{
  "output_id": "out_20260422_76b3de65",
  "heading": "Sequence Diagram",
  "partial": false
}
```

- `partial: true` allows substring match (e.g. `"Sequence"` matches `"Sequence Diagram"`)
- Returns `found: false` (not an error) when the heading doesn't exist
- Prefer this over `ctx_get_full` with a guessed `line_range`

### 4. Searching stored output

Use when the summary is not enough and you need specific lines.

```json
{
  "queries": ["FAIL", "error", "panic"],
  "output_id": "out_20260422_76b3de65",
  "max_results_per_query": 5,
  "context_lines": 3
}
```

Multiple queries run in parallel. Each returns matched lines with snippet + line number.
Add `context_lines` to include N lines of surrounding context (like `grep -C`) — avoids a follow-up `ctx_get_full` call.

**Special characters** (`#`, `-`, `|`, `:`, `*`) are auto-escaped — no manual escaping needed.

**Synonym expansion** — queries like `api_path` automatically expand to `[api_path, endpoint, route, url, path]`. Check `expanded_queries` in the response to see what was actually searched. Add project terms in `.ctx-saver-synonyms.yaml`.

### 5. Getting full output or line range

```json
{
  "output_id": "out_20260422_76b3de65",
  "line_range": [10, 50]
}
```

Omit `line_range` to retrieve all lines.

### 6. Listing stored outputs

```json
{
  "limit": 10
}
```

Returns: `output_id`, `command`, `created_at`, `size_bytes`, `lines` — newest first.

### 7. Saving decisions and handoffs

Use `ctx_note` for durable decisions. Add `task` when the note belongs to a specific feature or bug:

```json
{
  "text": "Chose migration v8 as additive-only to preserve existing databases.",
  "tags": ["arch", "sqlite"],
  "importance": "normal",
  "task": "task-handoff"
}
```

When ending a session that another host or future session should continue, save a handoff:

```json
{
  "action": "handoff",
  "task": "task-handoff",
  "text": "Schema/store changes are done; next update handler tests and docs."
}
```

Resume by calling `ctx_session_init` with the same task:

```json
{ "task": "task-handoff" }
```

Without `task`, `ctx_session_init` returns only unscoped recent decisions so unrelated sessions stay clean.

---

## Output ID Format

`out_YYYYMMDD_<8hex>` — e.g. `out_20260422_76b3de65`

---

## Storage Location

Data is stored per-project at:
```
<project-root>/.ctx-saver/outputs.db         ← default SQLite location (all stored outputs + FTS5 index)
<project-root>/.ctx-saver/server.log          ← default log location
```

If you configured `storage.data_dir`, files are stored under that directory instead.
Delete a project's default DB with `rm -rf .ctx-saver/` from project root.

---

## Project Knowledge (v0.7.0+)

After 3+ sessions, ctx-saver can generate `.ctx-saver/project-knowledge.md` containing
learned patterns: most-read files, most-run commands, common sequences, and key decisions grouped by task.

```bash
ctx-saver knowledge refresh   # generate/update
ctx-saver knowledge show      # print to stdout (no file write)
ctx-saver knowledge reset     # delete
```

`ctx_session_init` automatically surfaces this file when it exists — no extra action needed per session.

---

## Configuration Override (`.ctx-saver.yaml` in project root)

```yaml
storage:
  retention_days: 7
summary:
  head_lines: 30
  tail_lines: 10
  auto_index_threshold_bytes: 2048
knowledge:
  min_sessions: 3       # sessions required before first generation
  idle_minutes: 30      # 0 = disable idle auto-refresh
  top_files_limit: 10
  top_commands_limit: 10
  decisions_limit: 10
```
