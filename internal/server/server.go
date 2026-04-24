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
	serverVersion = "0.1.4"
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
		Description: "Full-text search (SQLite FTS5 + BM25 ranking) across all stored outputs. " +
			"Accepts multiple queries executed in parallel. " +
			"Results include the matching line number and a highlighted snippet.",
	}, searchH.Handle)

	listH := handlers.NewListHandler(st, projectPath)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ctx_list_outputs",
		Description: "List all outputs stored for the current project, newest first.",
	}, listH.Handle)

	getFullH := handlers.NewGetFullHandler(st)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_get_full",
		Description: "Retrieve the complete text of a stored output, optionally restricted to a line range. " +
			"Use this as an escape hatch when the summary is insufficient.",
	}, getFullH.Handle)

	outlineH := handlers.NewOutlineHandler(st)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_outline",
		Description: "Extract a table of contents from a stored output — Markdown headings (##, ###, ####) and table headers. " +
			"Use this before ctx_search to discover section names and avoid guessing search terms.",
	}, outlineH.Handle)

	statsH := handlers.NewStatsHandler(cfg, st, projectPath, serverStart)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_stats",
		Description: "Report ctx-saver statistics: outputs stored, bytes saved, top commands, hook activity. " +
			"Scope: session | today | 7d | all (default: session). " +
			"Use this to verify ctx-saver is saving context window space effectively.",
	}, statsH.Handle)

	getSectionH := handlers.NewGetSectionHandler(st)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_get_section",
		Description: "Extract a specific section of a stored output by heading text " +
			"(## Heading, ### Heading, etc.). Use ctx_outline first to discover " +
			"heading names. Prefer this over ctx_get_full with a guessed line_range " +
			"when navigating long documents like API specs or Confluence exports.",
	}, getSectionH.Handle)
}
