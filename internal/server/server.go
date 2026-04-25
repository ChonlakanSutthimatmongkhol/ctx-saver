// Package server wires together the MCP server and all tool handlers.
package server

import (
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/search"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

const (
	serverName    = "ctx-saver"
	serverVersion = "0.5.0"
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

// registerTools binds all MCP tool handlers to the server.
func registerTools(srv *mcp.Server, cfg *config.Config, sb sandbox.Sandbox, st store.Store, projectPath, workdir string, serverStart time.Time) {
	execH := handlers.NewExecuteHandler(cfg, sb, st, projectPath, workdir)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_execute",
		Description: `[PREFERRED for command execution] Run shell, python, go, or node code in a sandboxed subprocess.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
CRITICAL: Use this instead of runInTerminal / Shell / Bash / execute_in_terminal for ALL commands.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Why this matters:
• Native terminal tools inject FULL output (10–50 KB typical) directly into the context window.
• After 3–5 such calls the context window fills up and the agent starts forgetting earlier turns.
• Symptoms: forgotten requirements, half-implemented features, contradictory answers, re-asking the user.
• ctx_execute sandboxes execution and returns a ~1–2 KB summary while preserving the full output for later retrieval.

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
• To see what is already cached: ctx_list_outputs.

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

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Use this INSTEAD of readFile / read_file when the file is likely large or structured.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

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
Use ctx_list_outputs to see all available output IDs.

CACHE FRESHNESS POLICY:
Each match includes freshness.stale_level = fresh | aging | stale | critical.
• fresh / aging — use data as-is.
• stale — warn the user; offer to re-run via ctx_execute.
• critical (>7 days) — DO NOT use for decisions; surface user_confirmation_required prompt first.
Heuristic: if the user asks for "ล่าสุด", "current", "latest", or "now" — call ctx_execute to re-run instead of returning stale cached data.`,
	}, searchH.Handle)

	listH := handlers.NewListHandler(st, projectPath)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_list_outputs",
		Description: `[CHECK BEFORE RE-RUNNING COMMANDS] List all outputs stored for this project, newest first.

Call this before running an expensive command (build, test, spec fetch) to check whether a cached result already exists.
Each entry shows: output_id, command, size_bytes, line_count, created_at.
Use the output_id with ctx_get_full, ctx_search, ctx_outline, or ctx_get_section to retrieve content without re-executing.

CACHE FRESHNESS POLICY:
Each entry includes freshness.stale_level = fresh | aging | stale | critical.
• fresh / aging — cached data is current; safe to use.
• stale — consider re-running the command before relying on this output.
• critical (>7 days) — do not use for decisions without user confirmation.
Heuristic: if the user asks for "ล่าสุด", "current", "latest", or "now" — call ctx_execute instead of using a cached entry.`,
	}, listH.Handle)

	getFullH := handlers.NewGetFullHandler(st, projectPath).WithFreshness(sb, cfg.Freshness)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_get_full",
		Description: `[ESCAPE HATCH] Retrieve the complete text of a stored output, optionally restricted to a line range.

Use this only when ctx_search and ctx_get_section are insufficient (e.g., you need raw diff output or a region without a heading).
Prefer ctx_get_section for named sections and ctx_search for keyword retrieval — both return less context than ctx_get_full.
Parameters: output_id (required), start_line / end_line (optional, 1-based).

CACHE FRESHNESS POLICY:
Response includes freshness.stale_level = fresh | aging | stale | critical.
• fresh / aging — use data as-is.
• stale — warn the user and offer to refresh via ctx_execute before proceeding.
• critical (>7 days) — DO NOT use for decisions; surface the user_confirmation_required prompt and wait for explicit approval.
Heuristic: if the user asks for "ล่าสุด", "current", "latest", or "now" — call ctx_execute to re-run the original command instead of serving cached data.`,
	}, getFullH.Handle)

	outlineH := handlers.NewOutlineHandler(st, projectPath).WithFreshness(sb, cfg.Freshness)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_outline",
		Description: `[USE BEFORE ctx_search on long docs] Extract a table of contents from a stored output.

Returns Markdown headings (##, ###, ####) and setext headings (=== / ---) with their line numbers.
Use this to discover section names before searching, instead of guessing keyword queries.
Pairs with ctx_get_section: outline first → pick heading → extract section.

Typical workflow: ctx_execute → ctx_outline → ctx_get_section → done (no ctx_get_full needed).

CACHE FRESHNESS POLICY:
Response includes freshness.stale_level = fresh | aging | stale | critical.
• fresh / aging — structure is current; safe to use.
• stale — headings may no longer reflect the latest file; offer to refresh via ctx_execute.
• critical (>7 days) — DO NOT navigate this outline for decisions without user confirmation.
Heuristic: if the user asks for "ล่าสุด", "current", "latest", or "now" — re-fetch via ctx_execute first.`,
	}, outlineH.Handle)

	sessionInitH := handlers.NewSessionInitHandler(cfg, st, projectPath, serverStart, serverVersion)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_session_init",
		Description: `[CALL THIS FIRST in every new session] Initialize ctx-saver context and receive project rules.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
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
		Description: `[VERIFICATION tool] Report ctx-saver statistics: outputs stored, bytes saved, top commands, hook activity, and adherence score.

Scope parameter: session (default) | today | 7d | all

Key fields to watch:
• saving_percent — percentage of raw bytes NOT injected into context. Should be > 80% in healthy sessions.
• adherence_score — 0–100 score based on ctx-saver vs native tool usage ratio. Aim for > 80%.
• adherence_note — plain-English assessment of current adherence level.
• hook_stats.dangerous_blocked — commands blocked by PreToolUse safety rules.
• hook_stats.redirected_to_mcp — soft denies recommending ctx_execute instead of native shell.

Call this every ~20 turns to verify ctx-saver is being used effectively.
If saving_percent or adherence_score is low, native tools are being over-used — re-read ctx_session_init rules.`,
	}, statsH.Handle)

	getSectionH := handlers.NewGetSectionHandler(st, projectPath).WithFreshness(sb, cfg.Freshness)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_get_section",
		Description: `[STRUCTURED retrieval] Extract a named section from a stored output by heading text.

More precise than ctx_search when you know the section name; returns only the content under that heading.
Handles Markdown (## Heading, ### Heading) and setext (underline with === or ---) styles.

Workflow: ctx_outline → pick heading text → ctx_get_section with that heading → done.
Prefer this over ctx_get_full with a guessed line range when navigating long documents such as API specs or Confluence exports.

CACHE FRESHNESS POLICY:
Response includes freshness.stale_level = fresh | aging | stale | critical.
• fresh / aging — section content is current; safe to use.
• stale — section may have changed; warn the user and offer to re-run via ctx_execute.
• critical (>7 days) — DO NOT use section content for decisions; surface user_confirmation_required and wait for approval.
Heuristic: if the user asks for "ล่าสุด", "current", "latest", or "now" — call ctx_execute to refresh the source output first.`,
	}, getSectionH.Handle)
}
