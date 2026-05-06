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
1. `ctx_stats(view="outputs")` — see what is already stored for this project
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

### Rule 5: Respect cache freshness before acting on retrieved data

Every retrieval response (`ctx_get_full`, `ctx_get_section`, `ctx_search`, `ctx_stats(view=outputs)`, `ctx_outline`) includes a `freshness` field with `stale_level`:

| stale_level | Age | What to do |
|---|---|---|
| `fresh` | < 1 h | Use data as-is |
| `aging` | 1–24 h | Use data as-is; note age if relevant |
| `stale` | 1–7 days | Warn the user; offer to re-run via `ctx_execute` |
| `critical` | > 7 days | **DO NOT use for decisions.** Surface `user_confirmation_required` prompt to user and wait for explicit approval before proceeding. |

**Heuristic — when user asks for current state:**
If the user's message contains "ล่าสุด", "current", "latest", "now", or equivalent intent — call `ctx_execute` to re-run the original command instead of returning stale cached data.

**Auto-refresh:** outputs with `auto_refresh: true` in config are refreshed automatically on retrieval. The freshness field reflects the updated timestamp.

### Rule 6: Log architectural decisions with `ctx_note`

When you make a non-obvious design choice or learn a constraint that future-you needs to know:

```
ctx_note(text="...", tags=["arch", "<area>"], importance="high"|"normal")
```

Examples to log:
- "Chose X over Y because Z" (design choices)
- "Cannot use approach A because of constraint B" (discovered limits)
- "User confirmed: 7-day threshold for stale cache" (decisions made together)

Examples NOT to log:
- "Starting task 3" (routine progress)
- "Read file foo.go" (already in session_events)

These notes survive `/compact` and are surfaced at next `ctx_session_init`. Use `ctx_note(action="list")` to review past decisions.

### Rule 7: Additional tools (v0.6.0+)

- **`ctx_purge`** — use to clear stale/noisy cache when switching feature context or before a demo handover. Always requires `confirm="yes"`. Decision notes are preserved by default; pass `all=true` to delete them too. **Irreversible — confirm with user first.**
- **`ctx_read_file` with `fields="signatures"`** — use to get only function/type/const declarations (with original line numbers) without reading the full file. Supported: Go (full), Python (~95%), Dart (basic regex). Omit for full content.

### Rule 8: Read project-knowledge.md at session start (v0.7.0+)

If `.ctx-saver/project-knowledge.md` exists, it contains pre-computed project patterns:
most-read files, most-run commands, common command sequences, and high-importance decisions.

`ctx_session_init` surfaces this automatically — no extra action needed.
To regenerate it: `ctx-saver knowledge refresh` (run in terminal, not via MCP tool).

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

<!-- ctx-saver -->
See .ctx-saver/project-knowledge.md for learned project patterns (auto-generated, refresh with `ctx-saver knowledge refresh`).
