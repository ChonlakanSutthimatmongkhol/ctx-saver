# ctx-saver

**English** | [ภาษาไทย](README.th.md)

A self-hosted MCP server (Go) that reduces AI context window usage by sandboxing large tool outputs and returning compact summaries instead of raw text.

## Why

Tools like `jira issue list`, `kubectl get pods`, and `git log` produce kilobytes of output that burn through your context window. ctx-saver intercepts those outputs, stores them in a local SQLite database with FTS5 full-text indexing, and returns only a head+tail summary. When you need more, use `ctx_search` or `ctx_get_full`.

**No cloud. No telemetry. No account. 100% auditable code.**

## Quick start (5 minutes)

### Option A — go install (requires Go 1.25+)

```bash
# Install latest release
go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@latest

# Or pin to a specific version
go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@v0.1.0
```

The binary lands in `$(go env GOPATH)/bin/ctx-saver`.

### Option B — clone and build

```bash
git clone https://github.com/ChonlakanSutthimatmongkhol/ctx-saver.git
cd ctx-saver
make install        # build + copy to /usr/local/bin/ctx-saver
```

### Configure your AI client

**Claude Code**
```bash
claude mcp add ctx-saver -- $(go env GOPATH)/bin/ctx-saver
```

**VS Code Copilot** — create `.vscode/mcp.json` in any project root:
```json
{
  "servers": {
    "ctx-saver": {
      "command": "/usr/local/bin/ctx-saver"
    }
  }
}
```

Or globally in VS Code `settings.json`:
```json
{
  "mcp.servers": {
    "ctx-saver": {
      "command": "/Users/<you>/go/bin/ctx-saver"
    }
  }
}
```

Verify: Command Palette → **MCP: List Servers** — should show `ctx-saver` with 7 tools.

### Install hooks (optional but recommended)

Hooks enable automatic routing of large-output commands and session history restoration.

```bash
# Claude Code
./scripts/install-hooks.sh claude

# VS Code Copilot (run from your project root)
./scripts/install-hooks.sh copilot
```

The script detects the binary path automatically, backs up your existing config, and merges the hooks JSON — it will not overwrite unrelated settings. `jq` is required (`brew install jq` / `apt-get install jq`).

To remove hooks:
```bash
./scripts/uninstall-hooks.sh claude   # or copilot
```

See [Hook behaviour](#hooks) below for what each hook does.

## Tools

| Tool | Purpose |
|------|---------|
| `ctx_execute` | Run shell/python/go/node; large output stored + summarised |
| `ctx_read_file` | Read a file, optionally piped through a processing script |
| `ctx_outline` | Extract headings / table-of-contents from a stored output |
| `ctx_search` | FTS5 full-text search across stored outputs (supports `context_lines`) |
| `ctx_list_outputs` | List all stored outputs for this project |
| `ctx_get_full` | Retrieve complete output or a specific line range |
| `ctx_stats` | Report storage and hook statistics (scope: `session\|today\|7d\|all`) |

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

hooks:
  session_history_limit: 10   # max events injected into SessionStart context

deny_commands:
  - "rm -rf /"
  - "sudo *"
  - "dd if=*"
```

## Hooks

Hooks run as lightweight subprocesses alongside the AI agent.  They share the same binary (`ctx-saver hook <event>`) so no extra installation is needed after `make install`.

| Hook | Event | What it does |
|------|-------|-------------|
| PreToolUse | Before any shell/bash tool call | Blocks dangerous commands (`rm -rf`, pipe-to-shell, `eval`, `sudo -s`); redirects large-output commands (`curl`, `wget`, `cat *.log`, `find`, `journalctl`) to equivalent `ctx_execute` calls |
| PostToolUse | After every tool call | Records a summary of the tool call to the per-project SQLite DB for session restoration |
| SessionStart | At the start of every session | Injects routing rules and recent session history (up to `hooks.session_history_limit` deduplicated events) into the model's context |

### Safe curl variants

`curl --version`, `curl -I`, `curl --head`, and `curl -o /dev/null` are **not** redirected — only requests that are likely to return large bodies are sent through `ctx_execute`.

### Dangerous command patterns blocked by PreToolUse

- `rm -rf` / `rm -fr` / any `rm -[rRfF]+` variant
- `find / … -delete`
- Redirect to raw disk (`> /dev/sda`, `> /dev/nvme0`)
- Pipe to shell interpreter (`curl … | bash`, `wget … | sh`, `| zsh`, …)
- Any form of `eval`
- `sudo -s`, `sudo rm`, `sudo dd`
- Reads of credential files (`.env`, `id_rsa`, `.pem`, `.key`)

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
internal/hooks/                PreToolUse / PostToolUse / SessionStart hooks
internal/server/               MCP server wiring
tests/                         integration tests + testdata
scripts/                       install.sh, install-hooks.sh, uninstall-hooks.sh, benchmark.sh
configs/                       setup guides and hook config templates per platform
```

## Roadmap

- **Phase 1:** subprocess sandbox, SQLite FTS5, 5 MCP tools
- **Phase 2:** Anthropic `srt` OS-level sandbox (toggle via `sandbox.use_srt: true`)
- **Phase 3 (current):** Lifecycle hooks — routing enforcement, session capture, context restoration
