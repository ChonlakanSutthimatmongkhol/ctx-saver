// Package server wires together the MCP server and all tool handlers.
package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/knowledge"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/search"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

const (
	serverName    = "ctx-saver"
	serverVersion = "0.7.4"
)

// New constructs a fully configured *mcp.Server with all ctx-saver tools registered.
// All dependencies are injected — no global state is used.
func New(cfg *config.Config, sb sandbox.Sandbox, st store.Store, projectPath, workdir string, serverStart time.Time) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	registerTools(srv, cfg, sb, st, projectPath, workdir, serverStart)
	return srv
}

// idleCheckInterval is the polling frequency for RunIdleKnowledgeRefresh.
// Overridden in tests to speed up the tick.
var idleCheckInterval = 5 * time.Minute

// RunIdleKnowledgeRefresh polls every 5 minutes and auto-refreshes
// project-knowledge.md when the project has been idle for cfg.Knowledge.IdleMinutes
// AND at least cfg.Knowledge.MinSessions new sessions have been recorded since
// the last refresh. Runs until ctx is cancelled.
//
// Works on both Claude Code and Copilot because it reads from session_events
// in the database rather than from Claude Code hooks.
func RunIdleKnowledgeRefresh(ctx context.Context, st store.Store, projectPath string, cfg *config.Config) {
	ticker := time.NewTicker(idleCheckInterval)
	defer ticker.Stop()

	idleThreshold := time.Duration(cfg.Knowledge.IdleMinutes) * time.Minute

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lastEvent, err := st.LastEventTime(ctx, projectPath)
			if err != nil || lastEvent.IsZero() {
				continue
			}

			idle := time.Since(lastEvent)
			if idle < idleThreshold {
				continue
			}

			lastRefresh, _ := st.LastKnowledgeRefresh(ctx, projectPath)
			newSessions, _ := st.SessionCountSince(ctx, projectPath, lastRefresh)
			if newSessions < cfg.Knowledge.MinSessions {
				continue
			}

			refreshCtx, refreshCancel := context.WithTimeout(ctx, 30*time.Second)
			refreshErr := knowledge.Refresh(refreshCtx, st, projectPath, cfg)
			refreshCancel()
			if refreshErr != nil {
				slog.Warn("idle knowledge refresh failed", "error", refreshErr)
			} else {
				slog.Info("idle knowledge refresh completed",
					"idle_minutes", int(idle.Minutes()),
					"new_sessions", newSessions)
			}
		}
	}
}

// registerTools binds all MCP tool handlers to the server.
func registerTools(srv *mcp.Server, cfg *config.Config, sb sandbox.Sandbox, st store.Store, projectPath, workdir string, serverStart time.Time) {
	execH := handlers.NewExecuteHandler(cfg, sb, st, projectPath, workdir)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_execute",
		Description: `[PREFERRED for command execution] Run shell, python, go, or node code in a sandboxed subprocess.

CRITICAL: Use this instead of runInTerminal / Shell / Bash / execute_in_terminal for ALL commands.
Why: native tools inject 10–50 KB output into context per call; ctx_execute stores it and returns ~2 KB summary — prevents context overflow and forgotten requirements.

When to use (default choice):
• Build / test / lint: flutter build, flutter test, go test, go build, npm run, cargo test
• VCS: git log, git diff, git status (large repos)
• Container / cloud: kubectl get, docker ps, docker logs, gcloud, aws
• API tooling: acli, gh, jira, curl
• File tooling with many lines: cat large_file, grep -r, find, ls -la

When native terminal is acceptable:
• Single-line informational: pwd, whoami, date, echo "…"
• Interactive TTY commands (rare)

After calling this tool:
• A summary is returned plus an output_id.
• To see more detail: ctx_search (keyword) or ctx_get_full (line range) or ctx_get_section (heading).
• To see what is already cached: ctx_stats(view="outputs").

Configuration:
• language: "shell" (default), "python", "go", "node"
• code: the command or script to run
• intent: optional human-readable goal (helps search ranking)

If unsure whether to use this — USE IT. Over-using ctx_execute is harmless; under-using it silently destroys context.`,
	}, execH.Handle)

	readFileH := handlers.NewReadFileHandler(cfg, sb, st, projectPath, workdir)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_read_file",
		Description: `[PREFERRED for reading files] Read a file through the sandbox, storing full content and returning a compact summary.

Use this INSTEAD of readFile / read_file when the file is likely large or structured.

When to use:
• Files > 50 lines
• Logs, test output, build output
• API specs, Confluence exports, OpenAPI / Swagger JSON
• Structured data: JSON, YAML, CSV with many rows

When native readFile is acceptable:
• Source code files you intend to EDIT (need full content for accurate edits)
• Short config files (< 50 lines)
• Package manifests (go.mod, package.json, pubspec.yaml)

Optional processing:
• process_script: shell or Python snippet applied to the file before summary
  Example: process_script="jq '.endpoints | length'"
• language: "shell" (default) or "python" for the process_script

Optional view filter:
• fields="signatures" — returns only function/type/const declarations with original
  line numbers instead of full content. Reduces output 80–90% for code-heavy files.
  Supported languages: go (full), python (~95%), dart (basic regex).
  Dart limits: complex generics, operator overloads, and multi-line signatures may
  be missed. Use process_script 'grep -nE ...' for complex Dart files.

After calling:
• Same retrieval tools work: ctx_search, ctx_get_full, ctx_get_section, ctx_outline`,
	}, readFileH.Handle)

	syns, err := search.Load(projectPath)
	if err != nil {
		slog.Warn("synonyms: falling back to builtin only", "error", err)
		syns, _ = search.Load("")
	}

	searchH := handlers.NewSearchHandler(st, projectPath, syns)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_search",
		Description: `[PRIMARY retrieval tool after ctx_execute / ctx_read_file] Full-text search across stored outputs using SQLite FTS5 with BM25 ranking.

Accepts multiple queries executed in parallel goroutines; results include line number and a highlighted snippet.
Special characters (#, -, |, :, *, etc.) are auto-escaped — no manual quoting needed.
Query terms are automatically expanded with synonyms (see .ctx-saver-synonyms.yaml for project overrides).
Falls back to LIKE if FTS5 is unavailable; search_mode field in response indicates which backend was used.

Use this when you need keyword matches across stored outputs.
Use ctx_get_section when you already know the heading name.
Use ctx_outline first to discover heading names before writing keyword queries.
Use ctx_stats(view="outputs") to see all available output IDs.

Returns freshness.stale_level field — see ctx_session_init for usage policy.`,
	}, searchH.Handle)

	getFullH := handlers.NewGetFullHandler(st, projectPath).WithFreshness(sb, cfg.Freshness)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_get_full",
		Description: `[ESCAPE HATCH] Retrieve the complete text of a stored output, optionally restricted to a line range.

Use this only when ctx_search and ctx_get_section are insufficient (e.g., you need raw diff output or a region without a heading).
Prefer ctx_get_section for named sections and ctx_search for keyword retrieval — both return less context than ctx_get_full.
Parameters: output_id (required), start_line / end_line (optional, 1-based).

Returns freshness.stale_level field — see ctx_session_init for usage policy.`,
	}, getFullH.Handle)

	outlineH := handlers.NewOutlineHandler(st, projectPath).WithFreshness(sb, cfg.Freshness)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_outline",
		Description: `[USE BEFORE ctx_search on long docs] Extract a table of contents from a stored output.

Returns Markdown headings (##, ###, ####) and setext headings (=== / ---) with their line numbers.
Use this to discover section names before searching, instead of guessing keyword queries.
Pairs with ctx_get_section: outline first → pick heading → extract section.

Typical workflow: ctx_execute → ctx_outline → ctx_get_section → done (no ctx_get_full needed).

Returns freshness.stale_level field — see ctx_session_init for usage policy.`,
	}, outlineH.Handle)

	sessionInitH := handlers.NewSessionInitHandler(cfg, st, projectPath, serverStart, serverVersion)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_session_init",
		Description: `[CALL THIS FIRST in every new session] Initialize ctx-saver context and receive project rules.

Returns:
• Project rules (how to use ctx-saver tools correctly)
• Recent session activity (what was done before)
• Cached output inventory (what is already stored and ready for reuse)
• Active configuration (sandbox, dedup, smart-format settings)

Skipping this tool leads to:
• Re-running commands whose results are already cached
• Flooding context with native Shell / readFile output
• Missing project-specific routing rules

Cost: ~500–1000 tokens. Benefit: saves 10–50× more tokens over the session.
No arguments required.`,
	}, sessionInitH.Handle)

	statsH := handlers.NewStatsHandler(cfg, st, projectPath, serverStart)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_stats",
		Description: `[VERIFICATION + INVENTORY tool] Report ctx-saver state for this project.

View 'stats' (default):
- Adherence score (how consistently ctx-saver tools are being used vs native)
- Outputs stored, bytes saved, top commands, hook activity
- Useful for verifying ctx-saver is working and quantifying savings
- Scope: session | today | 7d | all (default: session)
- Call every ~20 turns; if adherence_score < 80, re-read ctx_session_init rules

View 'outputs':
- Full list of cached outputs newest-first (with output_id, command, size, freshness)
- Use BEFORE re-running commands to check if a recent run is already cached
- Use to find an output_id for ctx_get_full / ctx_search / ctx_outline
- Limit defaults to 50

freshness.stale_level on each entry — see ctx_session_init for usage policy.`,
	}, statsH.Handle)

	getSectionH := handlers.NewGetSectionHandler(st, projectPath).WithFreshness(sb, cfg.Freshness)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_get_section",
		Description: `[STRUCTURED retrieval] Extract a named section from a stored output by heading text.

More precise than ctx_search when you know the section name; returns only the content under that heading.
Handles Markdown (## Heading, ### Heading) and setext (underline with === or ---) styles.

Workflow: ctx_outline → pick heading text → ctx_get_section with that heading → done.
Prefer this over ctx_get_full with a guessed line range when navigating long documents such as API specs or Confluence exports.

Returns freshness.stale_level field — see ctx_session_init for usage policy.`,
	}, getSectionH.Handle)

	noteH := handlers.NewNoteHandler(st, projectPath)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_note",
		Description: `[DECISION LOG] Save or list architectural decisions, design rationale, and important reasoning that should survive /compact and future sessions.

Action 'save' (default if ` + "`text`" + ` is provided):
- Use when you make a non-obvious design choice, discover a constraint, or finalize a tradeoff
- Keep notes short (1-2 sentences ideal, max 2000 chars)
- Tag with topics like 'arch', 'perf', 'security' for filterability
- Set importance='high' only for genuinely critical decisions you'd want flagged at session start
- DO NOT use for routine progress updates or tool output summaries

Action 'list' (default if ` + "`text`" + ` is empty):
- Use when resuming work after /compact, investigating prior decisions, or filtering by tag/importance
- Default scope is "7d" (last 7 days). Use "session", "today", or "all" to widen.

Notes are scoped per-project, persist across sessions, and are surfaced in ctx_session_init.`,
	}, noteH.Handle)

	purgeH := handlers.NewPurgeHandler(st, projectPath)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_purge",
		Description: `[DESTRUCTIVE] Delete cached outputs and session events for this project.

Use when:
- Switching to a new feature/context that won't reuse current cache
- Cache became stale or noisy (irrelevant entries hurting search relevance)
- Pre-demo / pre-handover cleanup

Default behavior: deletes cached outputs + session events. Decision notes
(ctx_note entries) are PRESERVED.

Set all=true to also delete decision notes (irreversible — notes contain
long-term decisions worth keeping).

REQUIRES confirm="yes" to execute (safety check).`,
	}, purgeH.Handle)
}
