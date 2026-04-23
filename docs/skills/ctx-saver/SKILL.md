---
name: ctx-saver
description: >
  Workflow for running commands and reading files through ctx-saver MCP tools to reduce context window usage.
  Use when: running tests, builds, or any command with large output; reading large files (OpenAPI spec, logs, SQL migrations);
  fetching Confluence/Jira pages; searching previously stored outputs; retrieving full output by ID or line range.
  Tools: ctx_execute, ctx_read_file, ctx_search, ctx_list_outputs, ctx_get_full, ctx_outline.
argument-hint: 'Describe the command or file you want to run/read'
---

# ctx-saver Workflow

ctx-saver is an MCP server that stores large command outputs in SQLite and returns a compact head+tail summary — reducing context window usage by 60–98%.

## When to Use

| Situation | Tool |
|-----------|------|
| Run shell/python/go/node command | `ctx_execute` |
| Read a large file (spec, log, SQL) | `ctx_read_file` |
| See document structure before searching | `ctx_outline` |
| Search in a previously stored output | `ctx_search` |
| List all stored outputs for this project | `ctx_list_outputs` |
| Get full output or specific line range | `ctx_get_full` |

## Core Decision Rule

- Output **≤5KB** → returned directly, no storage
- Output **>5KB** → stored in `.ctx-saver/outputs.db`, AI receives summary only

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
- If `output_id` is set → output was stored; read `summary` for overview, then use `ctx_search` or `ctx_get_full` if more detail is needed

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

Returns headings (`##`, `###`, `####`) and table headers with their line numbers.

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

---

## Fetching Confluence / Jira Pages

Fetch via `ctx_execute` with the `atlassian` CLI, then search the stored result:

```json
{
  "language": "shell",
  "code": "./ai-workflow/atlassian confluence <URL> --ai",
  "intent": "fetch API spec"
}
```

**Rule of thumb:**
- Page **<50 lines** → use regular bash (no ctx needed)
- Page **≥50 lines** → use `ctx_execute` → then `ctx_search` for specific fields

---

## Output ID Format

`out_YYYYMMDD_<8hex>` — e.g. `out_20260422_76b3de65`

---

## Storage Location

Data is stored per-project at:
```
~/.local/share/ctx-saver/<project-hash>.db   ← SQLite (all stored outputs + FTS5 index)
~/.local/share/ctx-saver/server.log
```

Delete a project's DB with `rm ~/.local/share/ctx-saver/<hash>.db` or wipe all with `rm -rf ~/.local/share/ctx-saver/`.

---

## Configuration Override (`.ctx-saver.yaml` in project root)

```yaml
storage:
  retention_days: 7
summary:
  head_lines: 30
  tail_lines: 10
  auto_index_threshold_bytes: 2048
```
