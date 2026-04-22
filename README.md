# ctx-saver

A self-hosted MCP server (Go) that reduces AI context window usage by sandboxing large tool outputs and returning compact summaries instead of raw text.

## Why

Tools like `jira issue list`, `kubectl get pods`, and `git log` produce kilobytes of output that burn through your context window. ctx-saver intercepts those outputs, stores them in a local SQLite database with FTS5 full-text indexing, and returns only a head+tail summary. When you need more, use `ctx_search` or `ctx_get_full`.

**No cloud. No telemetry. No account. 100% auditable code.**

## Quick start (5 minutes)

```bash
# 1. Build
make build

# 2. Install
make install        # copies to /usr/local/bin/ctx-saver

# 3a. Claude Code
claude mcp add ctx-saver -- /usr/local/bin/ctx-saver

# 3b. VS Code Copilot — create .vscode/mcp.json
echo '{"servers":{"ctx-saver":{"command":"/usr/local/bin/ctx-saver"}}}' > .vscode/mcp.json
```

## Tools

| Tool | Purpose |
|------|---------|
| `ctx_execute` | Run shell/python/go/node; large output stored + summarised |
| `ctx_read_file` | Read a file, optionally piped through a processing script |
| `ctx_search` | FTS5 full-text search across stored outputs |
| `ctx_list_outputs` | List all stored outputs for this project |
| `ctx_get_full` | Retrieve complete output or a specific line range |

## How it works

```
Claude Code / VS Code Copilot
        │
        ▼  MCP (stdio)
  ctx-saver server (Go binary)
        │
        ├── ctx_execute: subprocess → capture output
        │       ├── small (≤5KB) → return directly
        │       └── large (>5KB) → store in SQLite + return summary
        │
        └── SQLite (~/.local/share/ctx-saver/<hash>.db)
                ├── outputs table  (full text, metadata)
                └── outputs_fts    (FTS5 + BM25 ranking)
```

## Configuration

Default config lives at `~/.config/ctx-saver/config.yaml`.  Per-project overrides go in `.ctx-saver.yaml` at the project root.

```yaml
sandbox:
  timeout_seconds: 60

storage:
  data_dir: ~/.local/share/ctx-saver
  retention_days: 14
  max_output_size_mb: 50

summary:
  head_lines: 20
  tail_lines: 5
  auto_index_threshold_bytes: 5120   # 5 KB

logging:
  level: info
  file: ~/.local/share/ctx-saver/server.log

deny_commands:
  - "rm -rf /"
  - "sudo *"
  - "dd if=*"
```

## Build

```bash
# Requires Go 1.25+
make build          # → bin/ctx-saver
make test           # unit tests + coverage
make lint           # golangci-lint
make install        # → /usr/local/bin/ctx-saver
```

## Security

- SQLite database permissions: `0600` (owner read/write only)
- Command deny list checked before execution
- Binary output (null bytes) rejected
- Path traversal resolved via `filepath.Abs` + `filepath.Clean`
- Log file truncates command strings to 120 chars to avoid logging secrets
- No network access — purely local

## Repository structure

```
cmd/ctx-saver/main.go          entry point
internal/config/               YAML config loader
internal/sandbox/              execution interface (subprocess + srt stub)
internal/store/                SQLite + FTS5 storage layer
internal/summary/              head+tail+stats summariser
internal/handlers/             one file per MCP tool
internal/server/               MCP server wiring
tests/                         integration tests + testdata
scripts/                       install.sh, benchmark.sh
configs/                       setup guides per platform
```

## Roadmap

- **Phase 1 (current):** subprocess sandbox, SQLite FTS5, 5 MCP tools
- **Phase 2:** Anthropic `srt` OS-level sandbox (toggle via `sandbox.use_srt: true`)
- **Phase 3:** Secret scanner hook, encrypted SQLite option
