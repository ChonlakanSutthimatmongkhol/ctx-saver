# ctx-saver MCP Tools Reference

ctx-saver is an MCP server that reduces AI context window usage.
When a command output exceeds **32KB** (v0.6.0+ default), it is stored in SQLite and only a summary is returned.
Full content is always retrievable on demand.

---

## ctx_session_init ⭐

**Call first in every new session.** Returns project rules, recent session activity, cached output inventory, and active configuration in a single call.

**Input:** none (no parameters required).

**Output**
| Field | Description |
|-------|-------------|
| `project_path` | Resolved project path |
| `project_rules` | Condensed routing rules (~500 tokens) |
| `recent_events` | Up to 10 recent tool calls (deduplicated, newest first) |
| `recent_events[].ago_seconds` | Seconds since the event |
| `recent_events[].summary` | One-line description of the tool call |
| `cached_outputs.total_outputs` | Number of outputs stored in last 7 days |
| `cached_outputs.total_size_bytes` | Total raw bytes stored |
| `cached_outputs.top_commands` | Up to 5 most-run commands |
| `cached_outputs.retention_days_left` | Configured retention window |
| `active_config.sandbox` | Sandbox type (`subprocess` or `srt`) |
| `active_config.dedup_enabled` | Whether dedup is active |
| `active_config.dedup_window_minutes` | Dedup window |
| `active_config.smart_format_enabled` | Whether format-aware summarizer is on |
| `freshness_policy.stale_levels` | Ordered list: `fresh`, `aging`, `stale`, `critical` |
| `freshness_policy.actions` | Action per stale_level (use-as-is / warn / require confirmation) |
| `freshness_policy.refresh_keywords_th` | Thai words that signal user wants fresh data (`ล่าสุด`, `ปัจจุบัน`) |
| `freshness_policy.refresh_keywords_en` | English words: `current`, `latest`, `now` |
| `next_action_hint` | Recommended next step |
| `server_version` | ctx-saver binary version |
| `session_start_time` | Server start time (UTC) |

**Session startup behaviour:**
- **Claude Code** — `SessionStart` hook calls `ctx_session_init` automatically via `~/.claude/settings.json`.
- **Copilot Enterprise** — agent must call `ctx_session_init` explicitly as its first tool call.

---

## ctx_execute

**Preferred for command execution** — use instead of `runInTerminal` / `Shell` / `Bash` / `execute_in_terminal`.

Run a shell or script command in a sandboxed subprocess. Outputs exceeding the configured threshold (~5 KB) are stored in SQLite and a compact head+tail summary is returned, preserving the full output for later retrieval via `ctx_search`, `ctx_get_section`, or `ctx_get_full`.

**Why this matters:** native terminal tools inject 10–50 KB of raw output directly into the context window. After 3–5 such calls the model starts forgetting earlier instructions silently. Use `ctx_execute` for all build/test/git/kubectl/API commands. Only use native terminal for trivial one-liners (`pwd`, `whoami`, `date`).

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `language` | string | Y | `shell`, `python`, `go`, or `node` |
| `code` | string | Y | Command or script to run |
| `intent` | string | N | Human-readable description of what this command achieves |
| `summary_lines` | int | N | Number of head lines in summary (default: from config, usually 20) |

**Output**
| Field | Present when | Description |
|-------|-------------|-------------|
| `direct_output` | output ≤ threshold | Full raw output returned directly |
| `output_id` | output > threshold | ID for later retrieval |
| `summary` | output > threshold | head + tail excerpt |
| `format` | output > threshold | Summary format used: `flutter_test`, `go_test`, `json`, `git_log`, or `generic` |
| `search_hint` | output > threshold | Hint to use `ctx_search` with this `output_id` |
| `duplicate_hint` | same cmd ran recently | Advisory: same command already ran within the dedup window |
| `previous_output_id` | same cmd ran recently | `output_id` of the previous identical run |
| `stats.lines` | always | Total line count |
| `stats.size_bytes` | always | Output size in bytes |
| `stats.exit_code` | always | Process exit code |
| `stats.duration_ms` | always | Execution time in milliseconds |

`ctx_execute` uses format-aware summarization when enabled by config (`summary.smart_format: true`).

**Dedup hint:** When `duplicate_hint` appears, the same command was already run within the last 30 minutes (configurable via `dedup.window_minutes`). Prefer `ctx_get_full` or `ctx_search` on `previous_output_id` to reuse the cached result instead of re-executing. The command still runs — the hint is advisory only.

**Example**
```json
{
  "language": "shell",
  "code": "go test ./...",
  "intent": "run all unit tests"
}
```

---

## ctx_read_file

**Preferred for reading files** — use instead of `readFile` / `read_file` when the file is large or structured.

Read a file through the sandbox, storing the full content and returning a compact summary. Same storage logic as `ctx_execute`.

**When to use:** files > 50 lines, logs, API specs (OpenAPI/Swagger), Confluence exports, JSON/YAML/CSV with many rows.
**When native `readFile` is fine:** source code you will edit (need full content), short config files (< 50 lines).

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | Y | File path (absolute or relative to server working directory) |
| `process_script` | string | N | Shell or Python script that receives file content via stdin |
| `language` | string | N | Language for `process_script`: `shell` or `python` (default: `shell`) |
| `fields` | string | N | View filter: `"signatures"` returns only function/type/const declarations with original line numbers. Supported: `go` (full), `python` (~95%), `dart` (basic regex). Omit for full content. |

**Output** — same fields as `ctx_execute` plus:
| Field | Description |
|-------|-------------|
| `path` | Resolved absolute path of the file |

**Example**
```json
{
  "path": "openapi.yaml",
  "process_script": "grep -A10 '/v1/users'",
  "language": "shell"
}
```

---

## ctx_search

**Primary retrieval tool after `ctx_execute` / `ctx_read_file`.** Full-text search (SQLite FTS5 + BM25 ranking) across all stored outputs. All queries run in parallel.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `queries` | string[] | Y | One or more search terms |
| `output_id` | string | N | Limit search to a specific stored output |
| `max_results_per_query` | int | N | Max matches per query (default: 5) |
| `context_lines` | int | N | Lines of surrounding context before/after each match, like `grep -C` (default: 0) |

**Output**
```json
{
  "results": [
    {
      "query": "FAIL",
      "matches": [
        {
          "output_id": "out_20260422_76b3de65",
          "line": 42,
          "snippet": "--- FAIL: TestLogin (0.03s)",
          "score": 1.0,
          "context": [
            "=== RUN   TestLogin",
            "    login_test.go:38: unexpected status 401",
            "--- FAIL: TestLogin (0.03s)"
          ]
        }
      ]
    }
  ],
  "search_mode": "fts5"
}
```

`search_mode` values:
- `"fts5"` — query was served by the FTS5 full-text index (default, fastest)
- `"like_fallback"` — FTS5 failed with a syntax error; query was retried with LIKE scan

**Special characters are auto-escaped** — characters such as `#`, `-`, `|`, `:`, `*`, `(`, `)` are automatically wrapped into an FTS5 phrase literal before the query is executed. You never need to escape them manually. If FTS5 still fails (e.g. extremely malformed input), the query automatically falls back to a LIKE scan and `search_mode` will be `"like_fallback"`.

**Synonym expansion** — queries are automatically expanded using a built-in synonym dictionary. For example, `api_path` expands to `["api_path", "endpoint", "route", "url", "path"]`. All expanded queries run in parallel. The `expanded_queries` field in the response shows exactly which queries were used.

To add project-specific synonyms, create `.ctx-saver-synonyms.yaml` in your project root:
```yaml
payment_flow: [checkout, billing, invoice, transaction]
user_model: [account, profile, member]
```
Project overrides replace (not merge) any builtin entry with the same key.

**Example**
```json
{
  "queries": ["api_path", "#API-123", "payment-service"],
  "output_id": "out_20260422_76b3de65",
  "max_results_per_query": 5,
  "context_lines": 3
}
```

---

## ctx_stats (view="outputs")

**Check before re-running commands.** List all outputs stored for the current project, newest first. Call this before running an expensive command (build, test, spec fetch) to verify whether a cached result already exists. Use the `output_id` with `ctx_search`, `ctx_get_full`, `ctx_outline`, or `ctx_get_section` to retrieve content without re-executing.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `view` | string | Y | Must be `"outputs"` to list cached outputs |
| `limit` | int | N | Max number of outputs to return (default: 50) |

**Output**
```json
{
  "view": "outputs",
  "outputs": [
    {
      "output_id": "out_20260422_76b3de65",
      "command": "go test ./...",
      "created_at": "2026-04-22T18:30:00Z",
      "size_bytes": 175706,
      "lines": 3200
    }
  ]
}
```

---

## ctx_get_full

**Escape hatch** — prefer `ctx_get_section` or `ctx_search` first. Retrieve the complete text of a stored output, optionally restricted to a line range.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `output_id` | string | Y | ID of the output to retrieve |
| `line_range` | int[2] | N | `[start, end]` — 1-based, inclusive. Omit to get all lines. |

**Output**
| Field | Description |
|-------|-------------|
| `output_id` | Echo of the requested ID |
| `lines` | Array of line strings |
| `total_lines` | Total lines in the stored output |
| `returned` | Number of lines actually returned |

**Example**
```json
{
  "output_id": "out_20260422_76b3de65",
  "line_range": [100, 150]
}
```

---

## ctx_outline

**Use before `ctx_search` on long documents.** Extract a table of contents from a stored output — Markdown headings (##, ###, ####) and setext headings (=== / ---), plus table headers. Returns section names with line numbers. Typical workflow: `ctx_outline` → pick heading → `ctx_get_section`.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `output_id` | string | Y | ID of the stored output to outline |

**Output**
| Field | Description |
|-------|-------------|
| `output_id` | Echo of the requested ID |
| `total_lines` | Total lines in the stored output |
| `entries` | Array of structural entries found |

Each entry:
| Field | Description |
|-------|-------------|
| `line` | 1-based line number |
| `level` | Standard Markdown depth: `1`=`#`, `2`=`##`, `3`=`###`, `4`=`####`; `0`=table header |
| `text` | Full text of the heading or table header line |

**Example**
```json
{ "output_id": "out_20260422_76b3de65" }
```

**Response example**
```json
{
  "output_id": "out_20260422_76b3de65",
  "total_lines": 420,
  "entries": [
    { "line": 5,  "level": 2, "text": "## Request" },
    { "line": 12, "level": 0, "text": "| Field | Type | Required | Description |" },
    { "line": 28, "level": 2, "text": "## Response" },
    { "line": 35, "level": 3, "text": "### retirementAges" }
  ]
}
```

---

## ctx_get_section

**Structured retrieval** — extract a named section by heading text. More precise than `ctx_search` when the section name is known. Use `ctx_outline` first to discover available headings, then `ctx_get_section` to pull the exact content. Prefer this over `ctx_get_full` with a guessed line range.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `output_id` | string | Y | ID of the stored output |
| `heading` | string | Y | Heading text to match (case-insensitive) |
| `partial` | bool | N | Allow substring match on heading (default: false) |

**Output**
| Field | Description |
|-------|-------------|
| `found` | `true` if a matching heading was found |
| `output_id` | Echo of the requested ID |
| `heading` | Echo of the requested heading |
| `start_line` | 1-based start line of the section (only when `found=true`) |
| `end_line` | 1-based inclusive end line (only when `found=true`) |
| `lines` | Array of lines in the section (only when `found=true`) |
| `line_count` | Number of lines returned (only when `found=true`) |

The section ends just before the next heading at the same or higher level (e.g. a `##` section ends at the next `##` or `#`). The last section extends to end of file.

When `found=false` the tool returns successfully — it is **not** an error.

**Example**
```json
{
  "output_id": "out_20260422_76b3de65",
  "heading": "Sequence Diagram",
  "partial": false
}
```

**Navigate long specs:** use `ctx_outline` to discover heading names, then `ctx_get_section` to extract the exact section you need. Prefer this over `ctx_get_full` with a guessed `line_range`.

---

## ctx_stats

**Verification tool** — call every ~20 turns to confirm ctx-saver is working effectively. Report aggregate statistics for the current project.

Key metrics:
- `saving_percent` — should be > 80% in healthy sessions
- `adherence_score` — 0–100 score; ctx-saver calls / (ctx-saver + native) * 100. Aim for > 80%
- `adherence_note` — plain-English assessment: Excellent (≥90), Good (≥70), Mixed (≥50), Low (<50)
- `native_shell_count` / `native_read_count` — how many times native Shell/Read was used instead of ctx-saver
- `ctx_execute_count` / `ctx_read_file_count` — ctx-saver tool usage counts

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `scope` | string | N | `session` (default), `today`, `7d`, or `all` |

**Output**
| Field | Description |
|-------|-------------|
| `scope` | Echo of the requested scope |
| `outputs_stored` | Number of outputs stored in the scope |
| `raw_bytes` | Total raw bytes of all stored outputs |
| `estimated_summary_bytes` | Estimated bytes ctx-saver returned to the AI (summaries only) |
| `saving_percent` | Percentage of raw bytes saved by summarisation |
| `avg_duration_ms` | Average command execution time |
| `top_commands` | Up to 5 most-run commands with count + total bytes |
| `largest_outputs` | Up to 3 largest stored outputs |
| `hook_stats.dangerous_blocked` | Commands blocked by PreToolUse deny list |
| `hook_stats.redirected_to_mcp` | Commands redirected to ctx_execute by PreToolUse |
| `hook_stats.events_captured` | Total hook events recorded |
| `adherence_score` | 0–100 — ctx-saver calls / total tracked calls * 100 |
| `adherence_note` | Plain-English assessment: Excellent / Good / Mixed / Low |
| `native_shell_count` | runInTerminal / Shell / Bash calls detected (posttooluse) |
| `native_read_count` | readFile / read_file / Read calls detected (posttooluse) |
| `ctx_execute_count` | ctx_execute calls in scope |
| `ctx_read_file_count` | ctx_read_file calls in scope |

**Example**
```json
{ "scope": "session" }
```

**Response example**
```json
{
  "scope": "session",
  "outputs_stored": 12,
  "raw_bytes": 1843200,
  "estimated_summary_bytes": 26400,
  "saving_percent": 98.6,
  "avg_duration_ms": 340,
  "top_commands": [
    { "command": "[shell] go test ./...", "count": 4, "total_bytes": 720000 }
  ],
  "largest_outputs": [
    { "output_id": "out_20260423_a1b2c3d4", "command": "[shell] go test ./...", "size_bytes": 240000, "line_count": 4800 }
  ],
  "hook_stats": {
    "dangerous_blocked": 1,
    "redirected_to_mcp": 3,
    "events_captured": 45
  }
}
```

---

## Output ID Format

`out_YYYYMMDD_<8hex>` — e.g. `out_20260422_76b3de65`

---

## ctx_purge

**[DESTRUCTIVE]** Delete cached outputs and session events for this project.

Use when switching feature context, cache is noisy/stale, or before a demo handover.

**REQUIRES `confirm="yes"`** — safety check against accidental invocation.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `confirm` | string | **Y** | Must be `"yes"` to execute |
| `all` | bool | N | If `true`, also deletes decision notes (`ctx_note` entries). Default `false` — notes are preserved. |

**Output**
| Field | Description |
|-------|-------------|
| `outputs_deleted` | Number of cached outputs deleted |
| `events_deleted` | Number of session events deleted |
| `notes_deleted` | Number of decision notes deleted (0 unless `all=true`) |
| `notes_kept` | Number of notes preserved (when `all=false`) |
| `message` | Human-readable summary |

**Default vs `all=true`**

| Target | Default | `all=true` |
|--------|---------|------------|
| Cached outputs | ✅ deleted | ✅ deleted |
| Session events | ✅ deleted | ✅ deleted |
| Decision notes | ❌ kept | ✅ deleted |

**Example**
```json
{ "confirm": "yes" }
```

---

## ctx_note

**[DECISION LOG]** Save an architectural decision or constraint that should survive `/compact` and future sessions.

Use for: non-obvious design choices, discovered constraints, confirmed tradeoffs.
Do NOT use for: routine progress, tool output summaries, obvious code facts.

Notes are scoped per-project, persist across sessions, and are surfaced in `ctx_session_init`.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `text` | string | **Y** | Note content (1–2 sentences ideal, max 2000 chars) |
| `tags` | string[] | N | Topic tags e.g. `["arch", "perf", "security"]` |
| `importance` | string | N | `"normal"` (default) or `"high"` |
| `links_to` | string[] | N | Output IDs this decision relates to |

**Output**
| Field | Description |
|-------|-------------|
| `decision_id` | Unique ID for this note |
| `message` | Confirmation message |

---

## ctx_note (action="list")

**[DECISION LOG]** List saved decisions/notes for this project.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | Y | Must be `"list"` to query saved notes |
| `scope` | string | N | `session`, `today`, `7d` (default), or `all` |
| `tags` | string[] | N | Filter by tags (OR match) |
| `min_importance` | string | N | `"normal"` (default) or `"high"` |

**Output:** list of decision entries, each with `decision_id`, `text`, `tags`, `importance`, `ago`.

---

## Storage Location

```
<project-root>/.ctx-saver/outputs.db   ← default SQLite DB (all outputs + FTS5 index)
<project-root>/.ctx-saver/server.log   ← default log file
```

If `storage.data_dir` is set, files are stored there instead.

Delete all stored outputs for the current project (default path): `rm -rf .ctx-saver/`
