# ctx-saver MCP Tools Reference

ctx-saver is an MCP server that reduces AI context window usage.
When a command output exceeds **5KB**, it is stored in SQLite and only a summary is returned.
Full content is always retrievable on demand.

---

## ctx_execute

Run a shell or script command. Large outputs are stored and summarised.

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
| `search_hint` | output > threshold | Hint to use `ctx_search` with this `output_id` |
| `stats.lines` | always | Total line count |
| `stats.size_bytes` | always | Output size in bytes |
| `stats.exit_code` | always | Process exit code |
| `stats.duration_ms` | always | Execution time in milliseconds |

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

Read a file and optionally pipe it through a processing script. Same storage logic as `ctx_execute`.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | Y | File path (absolute or relative to server working directory) |
| `process_script` | string | N | Shell or Python script that receives file content via stdin |
| `language` | string | N | Language for `process_script`: `shell` or `python` (default: `shell`) |

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

Full-text search (SQLite FTS5 + BM25 ranking) across all stored outputs. All queries run in parallel.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `queries` | string[] | Y | One or more search terms |
| `output_id` | string | N | Limit search to a specific stored output |
| `max_results_per_query` | int | N | Max matches per query (default: 5) |

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
          "score": 1.0
        }
      ]
    }
  ]
}
```

**Example**
```json
{
  "queries": ["FAIL", "panic", "error"],
  "output_id": "out_20260422_76b3de65",
  "max_results_per_query": 5
}
```

---

## ctx_list_outputs

List all outputs stored for the current project, newest first.

**Input**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `limit` | int | N | Max number of outputs to return (default: 50) |

**Output**
```json
{
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

Retrieve the complete text of a stored output, optionally restricted to a line range.

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

## Output ID Format

`out_YYYYMMDD_<8hex>` — e.g. `out_20260422_76b3de65`

## Storage Location

```
<project-root>/.ctx-saver/
├── outputs.db    ← SQLite DB (all outputs + FTS5 index)
└── server.log
```

Delete all stored outputs: `rm -rf .ctx-saver/`
