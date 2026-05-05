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
go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@latest
```

The binary lands in `$(go env GOPATH)/bin/`. Make sure that directory is in your `PATH`:

```bash
# Add to ~/.zshrc or ~/.bashrc (one-time setup)
export PATH="$PATH:$(go env GOPATH)/bin"
```

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
> Or run `ctx-saver init copilot` — it detects the binary path automatically (no `jq` required).

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

Verify: Command Palette → **MCP: List Servers** — should show `ctx-saver` with 11 tools.

### Install Claude hooks and Copilot server entry

Claude hooks enable automatic routing of large-output commands and session history restoration.
For VS Code Copilot, this step only registers the `ctx-saver` MCP server entry.

```bash
# Claude Code
ctx-saver init claude

# VS Code Copilot (run from your project root; registers MCP server only)
ctx-saver init copilot
```

Both commands detect the binary path automatically, back up your existing config, and merge JSON safely without overwriting unrelated settings. No `jq` required.

> **Repo clone users:** `./scripts/install-hooks.sh claude` and `./scripts/install-hooks.sh copilot` also work (require `jq`).

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
| `ctx_session_init` ⭐ | **Call first in every new session.** Returns project rules, recent events, cached output inventory, and config. Copilot Enterprise must call this explicitly; Claude Code uses the SessionStart hook automatically. |
| `ctx_execute` | Run shell/python/go/node; large output stored + summarised (format-aware). Shows `duplicate_hint` if the same command ran within the last 30 min. |
| `ctx_read_file` | Read a file, optionally piped through a processing script. Use `fields="signatures"` to return only function/type/const declarations with original line numbers (Go, Python, Dart). |
| `ctx_outline` | Extract headings / table-of-contents from a stored output. Includes `freshness` field. |
| `ctx_get_section` | Extract a specific section by heading text (use after `ctx_outline` to navigate long docs). Includes `freshness` + `user_confirmation_required` fields. |
| `ctx_search` | FTS5 full-text search across stored outputs (supports `context_lines`). Per-match `freshness` field. Special characters auto-escaped; LIKE fallback on parse errors. |
| `ctx_list_outputs` | List all stored outputs with per-item `freshness` field. |
| `ctx_get_full` | Retrieve complete output or a specific line range. Includes `freshness` + `user_confirmation_required` fields. Set `accept_stale: true` to bypass confirmation gate. |
| `ctx_stats` | Report storage, hook statistics, and adherence score (scope: `session\|today\|7d\|all`) |
| `ctx_note` | Save an architectural decision or rationale that survives `/compact` and future sessions. |
| `ctx_list_notes` | List recent decisions saved via `ctx_note`, filterable by scope/tag/importance. |
| `ctx_purge` | **[DESTRUCTIVE]** Delete all cached outputs and session events for this project. Requires `confirm="yes"`. Decision notes are preserved by default; pass `all=true` to delete them too. |

### Cache Purge (v0.6.0)

Use `ctx_purge` to clear cached data when switching feature context, the cache is noisy/stale, or before a demo handover.

```
ctx_purge(confirm="yes")          # deletes outputs + events; keeps notes
ctx_purge(confirm="yes", all=true) # also deletes ctx_note entries
```

| Target | Default | `all=true` |
|--------|---------|------------|
| Cached outputs | ✅ deleted | ✅ deleted |
| Session events | ✅ deleted | ✅ deleted |
| Decision notes | ❌ kept | ✅ deleted |

> ⚠️ Irreversible. Deleted outputs cannot be recovered.

### Decision Log (v0.5.1)

Use `ctx_note` to record any non-obvious design choice, discovered constraint, or confirmed tradeoff so future sessions (and post-`/compact` context) can recover the reasoning.

```
ctx_note(
  text="Chose WithFreshness builder pattern; positional arg would break 15 test sites",
  tags=["arch", "phase7"],
  importance="high"
)

ctx_list_notes(scope="session")
ctx_list_notes(tags=["arch"], min_importance="high")
```

Decisions are scoped per-project, persist across sessions, and are automatically injected into `ctx_session_init` (up to 10 most recent normal+high importance items from the last 7 days).

### Cache Freshness Policy (v0.5.0)

Every retrieval response includes a `freshness` object:

```json
{
  "freshness": {
    "source_kind": "shell:kubectl",
    "cached_at": "2026-04-26T03:00:00Z",
    "age_seconds": 7200,
    "age_human": "2h ago",
    "stale_level": "aging",
    "refresh_hint": ""
  }
}
```

| `stale_level` | Age | AI behaviour |
|---|---|---|
| `fresh` | < 1 h | Use data as-is |
| `aging` | 1–24 h | Use data as-is; note age if relevant |
| `stale` | 1–7 days | Warn user; offer to refresh via `ctx_execute` |
| `critical` | > 7 days | **Do not use for decisions.** `user_confirmation_required: true` is set — the AI must surface the prompt to the user and wait for approval |

**Auto-refresh**: sources with `auto_refresh: true` (e.g. `shell:kubectl`, `shell:acli`) are silently re-run on retrieval when stale. The original `output_id` is preserved.

**Bypass**: pass `accept_stale: true` in any retrieval input to skip the confirmation gate.

**Disable entirely**: set `freshness.enabled: false` in config.

See [docs/migration-v0.5.md](docs/migration-v0.5.md) for the full upgrade guide and [configs/freshness-examples/](configs/freshness-examples/) for sample configurations.

### Smart Summarizer

`ctx_execute` automatically detects the output format and produces a compact, structured summary:

| Format | Detected when | Summary includes |
|--------|--------------|------------------|
| `flutter_test` | command contains `flutter test`, or output has `All tests passed!` / `Some tests failed.` | pass/fail/skip counts, failed test names, duration |
| `go_test` | command contains `go test`, or output has `=== RUN` + `--- PASS/FAIL` | package pass/fail counts, failed test details, coverage % |
| `json` | output is valid JSON starting with `{` or `[` | top-level keys + types, array length, sample value |
| `git_log` | command contains `git log`, or output starts with `commit <hash>` | commit count, newest/oldest commits, top authors |
| `generic` | fallback for everything else | head + tail lines with omitted-line count |

Set `summary.smart_format: false` in config to always use the generic summariser.

### Search features

**Auto-escape** — special characters (`#`, `-`, `|`, `:`, `*`, `(`, `)`) in `ctx_search` queries are automatically wrapped as FTS5 phrase literals. You never need to escape them manually.

**LIKE fallback** — if FTS5 still fails (malformed input), the search retries with a LIKE scan automatically. The response includes `search_mode: "fts5"` or `search_mode: "like_fallback"`.

**Synonym expansion** — queries are automatically expanded using a built-in dictionary. `api_path` expands to `[api_path, endpoint, route, url, path]`; `authentication` expands to `[auth, login, jwt, oauth, bearer, token]`; etc. The response includes `expanded_queries` so you can see exactly what was searched.

To add project-specific synonyms, create `.ctx-saver-synonyms.yaml` in your project root:

```yaml
payment_flow: [checkout, billing, invoice, transaction]
user_model: [account, profile, member]
```

Project overrides replace (not merge) any built-in entry with the same key.

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
  auto_index_threshold_bytes: 32768  # 32 KB (v0.6.0+)
  smart_format: true                  # format-aware summariser (flutter_test | go_test | json | git_log | generic)
  enabled_formatters: []              # empty = all enabled; list names to restrict

dedup:
  enabled: true
  window_minutes: 30   # show duplicate_hint if same command ran within this window

logging:
  level: info
  file: ~/.local/share/ctx-saver/server.log

hooks:
  session_history_limit: 10   # max events injected into SessionStart context

deny_commands:
  - "rm -rf /"
  - "sudo *"
  - "dd if=*"

freshness:
  enabled: true
  default_max_age_seconds: 3600        # 1 hour for unknown sources
  user_confirm_threshold_seconds: 604800  # 7 days → ask user before use
  sources:
    shell:kubectl: { max_age_seconds: 60,  auto_refresh: true }
    shell:acli:    { max_age_seconds: 300, auto_refresh: true }
    shell:git:     { max_age_seconds: 120, auto_refresh: false }
    # see configs/freshness-examples/ for more presets
```

## Token efficiency tuning

The `auto_index_threshold_bytes` setting controls when outputs are returned inline
vs stored with an `output_id`:

| Value  | Effect |
|--------|--------|
| `32768` (default) | Typical Go/Python source files (300–500 lines) return inline — no round-trip needed |
| `65536` | Keeps medium build outputs inline; uses more per-turn tokens but fewer tool calls |
| `5120` | Legacy v0.5.x behavior — forces `output_id` flow for most files |

Configure in `~/.config/ctx-saver/config.yaml`:

```yaml
summary:
  auto_index_threshold_bytes: 32768  # adjust to taste
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

## For Copilot Enterprise users

If you are using GitHub Copilot in an enterprise context (e.g., at a bank or fintech), see the [Copilot Enterprise Setup Guide](docs/copilot-enterprise-setup.md) for:
- Required admin policies (MCP server allowlist)
- Installation steps specific to VS Code Copilot Agent mode
- Verification procedure (`ctx_session_init` call)
- Troubleshooting tool-adherence issues
- How to engage IT/Security for MCP approval

**Quick start:**
```bash
# 1. Install ctx-saver binary
go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@latest

# 2. Add MCP server to VS Code (run from your project directory)
ctx-saver init copilot

# 3. Add Copilot instruction rules to your repo
ctx-saver init copilot-instructions
```

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
internal/search/               synonym expansion (builtin YAML + project override)
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
