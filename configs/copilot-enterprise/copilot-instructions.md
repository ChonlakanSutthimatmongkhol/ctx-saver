# Project Development Rules

This project uses the **ctx-saver** MCP server to prevent context window exhaustion.
ctx-saver is available as an MCP tool — you must use it instead of native tools for the operations listed below.

## Tool Usage Rules (MANDATORY, EVERY TURN)

### Rule 1: Commands → `ctx_execute`

**MUST use `ctx_execute`** (not `runInTerminal`, `Shell`, `Bash`, `execute_in_terminal`) for:
- Build commands (`flutter build`, `go build`, `npm run build`, `cargo build`)
- Test commands (`flutter test`, `go test`, `jest`, `pytest`)
- Git with many results (`git log`, `git diff`, `git status` on large repos)
- Kubernetes / Docker (`kubectl get`, `docker ps`, `docker logs`)
- API tooling (`acli`, `gh`, `jira`, `curl`)
- File tooling (`cat` on large files, `grep -r`, `find`)
- **ANY command expected to output > 20 lines**

**Allowed to use native terminal** only for:
- `pwd`, `whoami`, `date`, `echo "..."`
- Interactive TTY commands (rare)

### Rule 2: Long files → `ctx_read_file`

**MUST use `ctx_read_file`** (not `readFile` / `read_file`) for:
- Files > 50 lines
- JSON / YAML / CSV with structured data
- Log files, test output, build output
- API specs (OpenAPI, Swagger, Confluence exports)

**Allowed to use native `readFile`** for:
- Source code you will EDIT (need full content for accurate edits)
- Short config files (< 50 lines)

### Rule 3: Use cached outputs before re-running

Before running an expensive command (build / test / spec fetch), check the cache:
1. `ctx_list_outputs` — see what is already stored for this project
2. `ctx_search` — keyword search across stored outputs
3. `ctx_get_section` — extract a named section (use `ctx_outline` first to discover heading names)
4. `ctx_get_full` — retrieve a line range as a last resort

### Rule 4: Start every session with `ctx_session_init`

Your **first tool call** in a new session must be `ctx_session_init`.
This returns:
- Project rules (this file, condensed)
- Recent session events (what was done last time)
- Cached output inventory (what is already stored, ready for reuse)
- Active configuration

Skipping this leads to re-running commands that already have cached results.

## Why these rules exist

Sessions without these tools hit **80% context window usage within 10–15 turns** in this repo
(large specs + verbose build logs + multiple test runs). At > 80% usage, the model starts
forgetting earlier instructions silently, producing wrong or contradictory answers.

Following these rules extends productive session length **5–10×** with minimal overhead.

## Verification

Call `ctx_stats` every ~20 turns.
- `saving_percent` should be > 80%
- `adherence_score` should be > 80%

If either metric is low, native tools are being over-used — re-read these rules.
