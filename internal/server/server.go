// Package server wires together the MCP server and all tool handlers.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

const (
	serverName    = "ctx-saver"
	serverVersion = "0.1.0"
)

// New constructs a fully configured *mcp.Server with all five ctx-saver tools registered.
// All dependencies are injected — no global state is used.
func New(cfg *config.Config, sb sandbox.Sandbox, st store.Store, projectPath, workdir string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	registerTools(srv, cfg, sb, st, projectPath, workdir)
	return srv
}

// registerTools binds all five MCP tool handlers to the server.
func registerTools(srv *mcp.Server, cfg *config.Config, sb sandbox.Sandbox, st store.Store, projectPath, workdir string) {
	execH := handlers.NewExecuteHandler(cfg, sb, st, projectPath, workdir)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_execute",
		Description: "Execute a shell or script command. " +
			"Outputs larger than the configured threshold are stored in SQLite and a head+tail summary is returned, " +
			"drastically reducing context window usage. " +
			"Use ctx_search to query stored results, or ctx_get_full to retrieve specific line ranges.",
	}, execH.Handle)

	readFileH := handlers.NewReadFileHandler(cfg, sb, st, projectPath, workdir)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "ctx_read_file",
		Description: "Read a file and optionally process it through a shell or Python script. " +
			"The same summary/storage logic as ctx_execute applies.",
	}, readFileH.Handle)

	searchH := handlers.NewSearchHandler(st, projectPath)
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
}
