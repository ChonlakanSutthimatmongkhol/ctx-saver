# ctx-saver

**English** | [ภาษาไทย](README.th.md)

A self-hosted MCP server (Go) that reduces AI context window usage by sandboxing large tool outputs and returning compact summaries instead of raw text.

## Why

Large-output commands burn through your context window fast:

- **Infrastructure**: `kubectl get pods -A`, `docker ps -a --no-trunc`, `aws s3 ls --recursive`
- **Logs & monitoring**: `journalctl`, `docker logs`, `npm install` (build logs), `git log --all --oneline`
- **Search**: `find / -name "*.ts"`, `grep -r pattern`, `curl https://api.example.com/users`
- **Package mgmt**: `pip list`, `go mod graph`, `npm ls --all`
- **Data**: `cat large_file.json`, `jira issue list`, `ls -la /var/log/`

ctx-saver intercepts these, stores outputs in local SQLite with FTS5 indexing, and returns only a head+tail summary. When you need more, use `ctx_search` or `ctx_get_full`.

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

> **Option A users** (`go install`): replace the path above with `$(go env GOPATH)/bin/ctx-saver`.
> Alternatively, run `./scripts/install-hooks.sh copilot` — it detects the correct path automatically.

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

### Install Claude hooks and Copilot server entry

Claude hooks enable automatic routing of large-output commands and session history restoration.
For VS Code Copilot, this step only registers the `ctx-saver` MCP server entry.

```bash
# Claude Code
./scripts/install-hooks.sh claude

# VS Code Copilot (run from your project root; registers MCP server only)
./scripts/install-hooks.sh copilot
```

If you installed via `go install` and did not clone this repository, use a temporary shallow clone to run the installer:

```bash
tmp="$(mktemp -d)"
git clone --depth 1 https://github.com/ChonlakanSutthimatmongkhol/ctx-saver.git "$tmp"

# Claude Code hooks
"$tmp/scripts/install-hooks.sh" claude

# VS Code Copilot server entry (run in your project root)
cd /path/to/your/project
"$tmp/scripts/install-hooks.sh" copilot

rm -rf "$tmp"
```

The script detects the binary path automatically, backs up your existing config, and merges JSON safely — it will not overwrite unrelated settings. `jq` is required (`brew install jq` / `apt-get install jq`).

For VS Code Copilot, `.vscode/mcp.json` currently accepts `servers` but rejects a top-level `hooks` key.

In short:
- Copilot can use `ctx-saver` MCP tools normally
- Copilot does not run lifecycle hooks automatically (`pretooluse`, `posttooluse`, `sessionstart`)
- Automatic hooks are currently supported via Claude Code only (`~/.claude/settings.json`)

Note: You can still run `ctx-saver hook <event>` manually for one-off testing.

To remove Claude hooks:
```bash
./scripts/uninstall-hooks.sh claude
```

To remove VS Code Copilot server entry, delete `servers.ctx-saver` from `.vscode/mcp.json`.

See [Hook behaviour](#hooks) below for what each hook does.

## Tools

| Tool | Purpose |
|------|---------|
| `ctx_execute` | Run shell/python/go/node; large output stored + summarised (format-aware). Shows `duplicate_hint` if the same command ran within the last 30 min. |
| `ctx_read_file` | Read a file, optionally piped through a processing script |
| `ctx_outline` | Extract headings / table-of-contents from a stored output |
| `ctx_search` | FTS5 full-text search across stored outputs (supports `context_lines`). Special characters auto-escaped; LIKE fallback on parse errors. Queries auto-expanded with synonyms (e.g. `api_path` → endpoint, route…). Project synonyms via `.ctx-saver-synonyms.yaml`. |
| `ctx_list_outputs` | List all stored outputs for this project |
| `ctx_get_full` | Retrieve complete output or a specific line range |
| `ctx_stats` | Report storage and hook statistics (scope: `session\|today\|7d\|all`) |

### Smart Summarizer (Phase 4)

`ctx_execute` automatically detects the output format and produces a compact, structured summary:

| Format | Detected when | Summary includes |
|--------|--------------|------------------|
| `flutter_test` | command contains `flutter test`, or output has `All tests passed!` / `Some tests failed.` | pass/fail/skip counts, failed test names, duration |
| `go_test` | command contains `go test`, or output has `=== RUN` + `--- PASS/FAIL` | package pass/fail counts, failed test details, coverage % |
| `json` | output is valid JSON starting with `{` or `[` | top-level keys + types, array length, sample value |
| `git_log` | command contains `git log`, or output starts with `commit <hash>` | commit count, newest/oldest commits, top authors |
| `generic` | fallback for everything else | head + tail lines with omitted-line count |

Set `summary.smart_format: false` in config to always use the generic summariser.

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

## Real usage: token reduction

Measured from real commands executed through `ctx_execute` in this repository.

Benchmark snapshot (`2026-04-23`): `go test -race -v ./internal/summary/...` and `cat README.md`.

Assumption for token estimate: `1 token ≈ 4 bytes`.

| Command | Raw output (bytes) | Returned summary (bytes) | Bytes saved | Estimated tokens saved |
|---------|---------------------|--------------------------|-------------|------------------------|
| `go test -race -v ./internal/summary/...` | 5,640 | 110 | 5,530 | ~1,383 |
| `cat README.md` | 10,135 | 173 | 9,962 | ~2,490 |
| **Total** | **15,775** | **283** | **15,492** | **~3,873** |

Overall reduction in this run: **98.21%** (from 15,775 bytes to 283 bytes).

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
  smart_format: true                  # format-aware summariser (flutter_test | go_test | json | git_log | generic)
  enabled_formatters: []              # empty = all enabled; list names to restrict

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
make test           # unit tests
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
internal/summary/              smart summariser: format-aware (flutter_test, go_test, json, git_log) + generic fallback
  internal/summary/formats/      one file per formatter + tests
internal/handlers/             one file per MCP tool
internal/hooks/                PreToolUse / PostToolUse / SessionStart hooks
internal/server/               MCP server wiring
tests/                         integration tests + testdata
scripts/                       install.sh, install-hooks.sh, uninstall-hooks.sh, benchmark.sh
configs/                       setup guides and hook config templates per platform
```

## Design decisions

### Subprocess sandbox (not containers or VMs)

**Why**: Simplicity and instant startup. Containers add 50–200ms overhead per execution; subprocess spawns in ~1ms. For tools that run in your session anyway (like AI chat), sandboxing via `exec.Command` with resource limits is sufficient. We focus on isolation of **outputs**, not **processes** — the threat model is context pollution, not malware.

**Future**: Anthropic's `srt` (Secure Runtime) can be added for enforced OS-level isolation when needed (toggle via `sandbox.use_srt: true`).

### FTS5 over traditional indexing

**Why**: BM25 ranking in FTS5 is built-in and tuned for natural language search. Matching "pod status" across 50MB of `kubectl` logs returns relevant lines first, not just substring matches. No extra query complexity — just `SELECT … FROM outputs_fts WHERE outputs_fts MATCH 'pod status'`.

### SQLite instead of Redis/Postgres

**Why**: Self-hosted. No external services to manage. `sqlite` lives in a single `~/.local/share/ctx-saver/<hash>.db` file — permissions are `0600`, backups are trivial, and you own your data. For a tool that runs locally on your machine, zero-config beats "set up a database server."

### Per-project database hash

**Why**: Isolation. Your `~/projects/backend` database is separate from `~/projects/frontend`. Tools and configs are independent per project, but stats can still be queried across all projects (scope: `all`).

### Head+tail summaries instead of full-text extraction

**Why**: Bounded context. Taking the first 20 lines + last 5 lines of a 1000-line JSON response guarantees ~100 tokens instead of ~3000. You see structure and key info instantly, can search for details if needed, and spend context on what matters.
