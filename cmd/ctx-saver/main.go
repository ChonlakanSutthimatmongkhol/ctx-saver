// Command ctx-saver is a self-hosted MCP server that reduces AI context window usage
// by sandboxing large tool outputs and returning compact summaries instead of raw text.
//
// Usage:
//
//	ctx-saver [--debug]                       — start the MCP server (stdio transport)
//	ctx-saver hook pretooluse                 — PreToolUse routing enforcement hook
//	ctx-saver hook posttooluse                — PostToolUse session capture hook
//	ctx-saver hook sessionstart               — SessionStart state-restoration hook
//	ctx-saver init claude                     — install hooks into ~/.claude/settings.json
//	ctx-saver init copilot                    — install MCP server into .vscode/mcp.json
//	ctx-saver init copilot-instructions       — install .github/copilot-instructions.md
//	ctx-saver knowledge refresh               — generate/update project-knowledge.md
//	ctx-saver knowledge show                  — print current knowledge to stdout
//	ctx-saver knowledge reset                 — delete project-knowledge.md
//
// The server communicates over stdin/stdout using the MCP protocol (stdio transport).
// All log output goes to the configured log file (default: ~/.local/share/ctx-saver/server.log)
// so it does not interfere with the protocol stream.
//
// Quick-start (works with go install — no repo clone required):
//
//	go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@latest
//	ctx-saver init claude                     # Claude Code hooks
//	ctx-saver init copilot                    # VS Code Copilot MCP entry
//	ctx-saver init copilot-instructions       # Copilot Enterprise instruction rules
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/hooks"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/server"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

func main() {
	if err := run(); err != nil {
		// Write startup errors to stderr; stdout belongs to the MCP protocol.
		fmt.Fprintf(os.Stderr, "ctx-saver: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]

	// ── Hook subcommand: ctx-saver hook <event> ────────────────────────────────
	// Hooks are lightweight — they open the store, run, and exit.
	// They do NOT start the full MCP server.
	if len(args) >= 1 && args[0] == "hook" {
		if len(args) < 2 {
			return fmt.Errorf("usage: ctx-saver hook <pretooluse|posttooluse|sessionstart>")
		}
		return runHook(args[1])
	}

	// ── Init subcommand: ctx-saver init <platform> ────────────────────────────
	// One-shot setup that writes config files; does NOT start the MCP server.
	if len(args) >= 1 && args[0] == "init" {
		return runInit(args[1:])
	}

	// ── Knowledge subcommand: ctx-saver knowledge <action> ────────────────────
	if len(args) >= 1 && args[0] == "knowledge" {
		return runKnowledge(args[1:])
	}

	// ── MCP server mode ────────────────────────────────────────────────────────
	return runServer()
}

// runHook handles the `ctx-saver hook <event>` subcommand.
func runHook(event string) error {
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determining working directory: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		// Config failure must not block hooks — use defaults.
		cfg = config.Default()
	}
	config.ResolveDataDir(cfg, projectPath)

	// Set up logging so hook subprocesses can emit slog warnings.
	if logger, logCloser, lerr := setupLogger(cfg); lerr == nil {
		slog.SetDefault(logger)
		if logCloser != nil {
			defer logCloser()
		}
	}

	st, err := store.NewSQLiteStore(cfg.Storage.DataDir, projectPath)
	if err != nil {
		// If the store is unavailable, still try to emit a valid hook response.
		st = nil
	}
	if st != nil {
		defer st.Close()
	}

	switch event {
	case "pretooluse":
		return hooks.RunPreToolUse(st, os.Stdin, os.Stdout)
	case "posttooluse":
		return hooks.RunPostToolUse(st, os.Stdin, os.Stdout)
	case "sessionstart":
		return hooks.RunSessionStart(st, os.Stdin, os.Stdout, cfg.Hooks.SessionHistoryLimit)
	default:
		return fmt.Errorf("unknown hook event %q (want: pretooluse | posttooluse | sessionstart)", event)
	}
}

// runServer starts the full MCP server.
func runServer() error {
	serverStart := time.Now()

	// ── Configuration ──────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// ── Logging ────────────────────────────────────────────────────────────────
	logger, logCloser, err := setupLogger(cfg)
	if err != nil {
		// Non-fatal: fall back to stderr.
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	if logCloser != nil {
		defer logCloser()
	}
	slog.SetDefault(logger)

	// ── Project path (working directory) ──────────────────────────────────────
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determining working directory: %w", err)
	}

	// Resolve relative DataDir (default: .ctx-saver) against the project root.
	config.ResolveDataDir(cfg, projectPath)

	// ── Storage ────────────────────────────────────────────────────────────────
	st, err := store.NewSQLiteStore(cfg.Storage.DataDir, projectPath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer st.Close()

	// ── Signal-aware context ───────────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Clean up expired outputs at startup (non-fatal if it fails).
	if err := st.Cleanup(ctx, projectPath, cfg.Storage.RetentionDays); err != nil {
		slog.Warn("startup cleanup failed", "error", err)
	}

	// ── Auto-cleanup background goroutine ──────────────────────────────────────
	go runPeriodicCleanup(ctx, st, projectPath, cfg.Storage.RetentionDays)

	// ── Idle knowledge refresh goroutine (disabled when idle_minutes = 0) ─────
	if cfg.Knowledge.IdleMinutes > 0 {
		go server.RunIdleKnowledgeRefresh(ctx, st, projectPath, cfg)
	}

	// ── Sandbox ────────────────────────────────────────────────────────────────
	sb := sandbox.NewSubprocess(cfg.DenyCommands)

	// ── MCP Server ─────────────────────────────────────────────────────────────
	srv := server.New(cfg, sb, st, projectPath, projectPath, serverStart)

	slog.Info("ctx-saver starting", "project", projectPath, "data_dir", cfg.Storage.DataDir)

	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("MCP server error: %w", err)
	}
	return nil
}

// runPeriodicCleanup deletes expired outputs every hour until ctx is cancelled.
func runPeriodicCleanup(ctx context.Context, st store.Store, projectPath string, retentionDays int) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := st.Cleanup(ctx, projectPath, retentionDays); err != nil {
				slog.Warn("periodic cleanup failed", "error", err)
			} else {
				slog.Debug("periodic cleanup completed")
			}
		}
	}
}

// setupLogger configures structured logging to the file specified in cfg.
// Returns a closer function that must be called to flush/close the log file.
func setupLogger(cfg *config.Config) (*slog.Logger, func(), error) {
	level := parseLogLevel(cfg.Logging.Level)
	opts := &slog.HandlerOptions{Level: level}

	if cfg.Logging.File == "" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts)), nil, nil
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Logging.File), 0700); err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}

	f, err := os.OpenFile(cfg.Logging.File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file %s: %w", cfg.Logging.File, err)
	}

	logger := slog.New(slog.NewTextHandler(f, opts))
	closer := func() { f.Close() }
	return logger, closer, nil
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
