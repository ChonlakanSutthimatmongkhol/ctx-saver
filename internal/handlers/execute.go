package handlers

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary"
)

// ExecuteInput is the typed input for the ctx_execute MCP tool.
type ExecuteInput struct {
	Language     string `json:"language"                   jsonschema:"programming language to use: shell, python, go, or node"`
	Code         string `json:"code"                       jsonschema:"code or shell command to execute"`
	Intent       string `json:"intent,omitempty"           jsonschema:"optional description of what this command is trying to achieve"`
	SummaryLines int    `json:"summary_lines,omitempty"    jsonschema:"number of head lines to include in summary (default: from config)"`
}

// ExecuteOutput is the typed output for the ctx_execute MCP tool.
type ExecuteOutput struct {
	OutputID     string      `json:"output_id,omitempty"     jsonschema:"ID assigned to this output (only when output was stored)"`
	Summary      string      `json:"summary,omitempty"       jsonschema:"head+tail summary (only when output was stored)"`
	Stats        OutputStats `json:"stats"                   jsonschema:"execution statistics"`
	SearchHint   string      `json:"search_hint,omitempty"   jsonschema:"hint on how to search stored output"`
	DirectOutput string      `json:"direct_output,omitempty" jsonschema:"full output (only when output is small enough to return directly)"`
}

// OutputStats carries execution metadata.
type OutputStats struct {
	Lines      int   `json:"lines"`
	SizeBytes  int   `json:"size_bytes"`
	ExitCode   int   `json:"exit_code"`
	DurationMs int64 `json:"duration_ms"`
}

// ExecuteHandler handles the ctx_execute MCP tool.
type ExecuteHandler struct {
	cfg         *config.Config
	sb          sandbox.Sandbox
	st          store.Store
	projectPath string
	workdir     string
}

// NewExecuteHandler creates an ExecuteHandler.
func NewExecuteHandler(cfg *config.Config, sb sandbox.Sandbox, st store.Store, projectPath, workdir string) *ExecuteHandler {
	return &ExecuteHandler{cfg: cfg, sb: sb, st: st, projectPath: projectPath, workdir: workdir}
}

// Handle implements the ctx_execute tool.
func (h *ExecuteHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input ExecuteInput) (*mcp.CallToolResult, ExecuteOutput, error) {
	if input.Code == "" {
		return nil, ExecuteOutput{}, fmt.Errorf("code must not be empty")
	}

	program, args, stdin := languageToCommand(input.Language, input.Code)
	timeout := time.Duration(h.cfg.Sandbox.TimeoutSeconds) * time.Second

	result, err := h.sb.Execute(ctx, sandbox.ExecuteRequest{
		Program: program,
		Args:    args,
		Stdin:   stdin,
		WorkDir: h.workdir,
		Timeout: timeout,
	})
	if err != nil {
		return nil, ExecuteOutput{}, fmt.Errorf("executing command: %w", err)
	}

	maxBytes := int64(h.cfg.Storage.MaxOutputSizeMB) * 1024 * 1024
	if int64(len(result.Output)) > maxBytes {
		return nil, ExecuteOutput{}, fmt.Errorf("output size (%d bytes) exceeds max_output_size_mb (%d MB)",
			len(result.Output), h.cfg.Storage.MaxOutputSizeMB)
	}

	stats := OutputStats{
		SizeBytes:  len(result.Output),
		ExitCode:   result.ExitCode,
		DurationMs: result.Duration.Milliseconds(),
	}

	threshold := h.cfg.Summary.AutoIndexThresholdBytes
	if len(result.Output) <= threshold {
		// Small output — return directly, no storage needed.
		lines := strings.Split(strings.TrimRight(string(result.Output), "\n"), "\n")
		if len(result.Output) == 0 {
			lines = nil
		}
		stats.Lines = len(lines)
		return nil, ExecuteOutput{
			Stats:        stats,
			DirectOutput: string(result.Output),
		}, nil
	}

	// Large output — store and summarise.
	headLines := h.cfg.Summary.HeadLines
	if input.SummaryLines > 0 {
		headLines = input.SummaryLines
	}
	sum := summary.Summarize(result.Output, headLines, h.cfg.Summary.TailLines)
	stats.Lines = sum.TotalLines

	outputID := generateOutputID()
	// Sanitise the command string before persisting (avoid logging raw secret-bearing args).
	displayCmd := sanitiseCommand(input.Language, input.Code)

	out := &store.Output{
		OutputID:    outputID,
		Command:     displayCmd,
		Intent:      input.Intent,
		FullOutput:  string(result.Output),
		SizeBytes:   int64(len(result.Output)),
		LineCount:   sum.TotalLines,
		ExitCode:    result.ExitCode,
		DurationMs:  result.Duration.Milliseconds(),
		CreatedAt:   time.Now(),
		ProjectPath: h.projectPath,
	}
	if err := h.st.Save(ctx, out); err != nil {
		return nil, ExecuteOutput{}, fmt.Errorf("storing output: %w", err)
	}

	statsLine := summary.FormatStats(sum.TotalLines, sum.TotalBytes, result.ExitCode, result.Duration.Milliseconds())
	return nil, ExecuteOutput{
		OutputID:   outputID,
		Summary:    sum.Text + "\n" + statsLine,
		Stats:      stats,
		SearchHint: fmt.Sprintf("Use ctx_search with output_id=%q to query this output", outputID),
	}, nil
}

// languageToCommand maps a language name and code string to an executable + args + optional stdin.
func languageToCommand(language, code string) (program string, args []string, stdin []byte) {
	switch strings.ToLower(language) {
	case "python":
		return "python3", []string{"-c", code}, nil
	case "go":
		// go run - reads source from stdin.
		return "go", []string{"run", "-"}, []byte(code)
	case "node":
		return "node", []string{"-e", code}, nil
	default: // "shell" or empty
		return "/bin/sh", []string{"-c", code}, nil
	}
}

// sanitiseCommand returns a display-safe representation of the command.
// It truncates long code snippets to avoid logging sensitive data.
func sanitiseCommand(language, code string) string {
	const maxLen = 120
	if len(code) > maxLen {
		code = code[:maxLen] + "…"
	}
	if language == "" {
		language = "shell"
	}
	return fmt.Sprintf("[%s] %s", language, code)
}

// generateOutputID returns a unique output identifier of the form out_YYYYMMDD_XXXXXXXX.
func generateOutputID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("out_%s_%x", time.Now().UTC().Format("20060102"), b)
}
