package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary"
)

// ReadFileInput is the typed input for the ctx_read_file MCP tool.
type ReadFileInput struct {
	Path          string `json:"path"                      jsonschema:"path to the file to read (relative paths resolved from server working directory)"`
	ProcessScript string `json:"process_script,omitempty"  jsonschema:"optional shell or python script that receives the file content via stdin (e.g. 'jq .endpoints')"`
	Language      string `json:"language,omitempty"        jsonschema:"language for process_script: shell or python (default: shell)"`
}

// ReadFileOutput is the typed output for ctx_read_file.
type ReadFileOutput struct {
	OutputID     string      `json:"output_id,omitempty"`
	Summary      string      `json:"summary,omitempty"`
	Stats        OutputStats `json:"stats"`
	SearchHint   string      `json:"search_hint,omitempty"`
	DirectOutput string      `json:"direct_output,omitempty"`
	Path         string      `json:"path" jsonschema:"resolved absolute path of the file"`
}

// ReadFileHandler handles the ctx_read_file MCP tool.
type ReadFileHandler struct {
	cfg         *config.Config
	sb          sandbox.Sandbox
	st          store.Store
	projectPath string
	workdir     string
}

// NewReadFileHandler creates a ReadFileHandler.
func NewReadFileHandler(cfg *config.Config, sb sandbox.Sandbox, st store.Store, projectPath, workdir string) *ReadFileHandler {
	return &ReadFileHandler{cfg: cfg, sb: sb, st: st, projectPath: projectPath, workdir: workdir}
}

// Handle implements the ctx_read_file tool.
func (h *ReadFileHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input ReadFileInput) (*mcp.CallToolResult, ReadFileOutput, error) {
	if input.Path == "" {
		return nil, ReadFileOutput{}, fmt.Errorf("path must not be empty")
	}

	// Resolve to an absolute path.  Relative paths are anchored to the working directory.
	absPath := input.Path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(h.workdir, absPath)
	}
	absPath = filepath.Clean(absPath)

	var rawOutput []byte
	var durationMs int64
	var exitCode int

	if input.ProcessScript == "" {
		// Direct file read — no sandbox involved.
		t0 := time.Now()
		data, err := os.ReadFile(absPath)
		durationMs = time.Since(t0).Milliseconds()
		if err != nil {
			return nil, ReadFileOutput{}, fmt.Errorf("reading file %s: %w", absPath, err)
		}
		rawOutput = data
	} else {
		// Pipe file content through the process script via sandbox stdin.
		fileData, err := os.ReadFile(absPath)
		if err != nil {
			return nil, ReadFileOutput{}, fmt.Errorf("reading file %s: %w", absPath, err)
		}

		lang := strings.ToLower(input.Language)
		var program string
		var args []string
		switch lang {
		case "python":
			program, args = "python3", []string{"-c", input.ProcessScript}
		default: // shell
			program, args = "/bin/sh", []string{"-c", input.ProcessScript}
		}

		result, err := h.sb.Execute(ctx, sandbox.ExecuteRequest{
			Program: program,
			Args:    args,
			Stdin:   fileData,
			WorkDir: h.workdir,
			Timeout: time.Duration(h.cfg.Sandbox.TimeoutSeconds) * time.Second,
		})
		if err != nil {
			return nil, ReadFileOutput{}, fmt.Errorf("processing file: %w", err)
		}
		rawOutput = result.Output
		durationMs = result.Duration.Milliseconds()
		exitCode = result.ExitCode
	}

	maxBytes := int64(h.cfg.Storage.MaxOutputSizeMB) * 1024 * 1024
	if int64(len(rawOutput)) > maxBytes {
		return nil, ReadFileOutput{}, fmt.Errorf("output size (%d bytes) exceeds max_output_size_mb (%d MB)",
			len(rawOutput), h.cfg.Storage.MaxOutputSizeMB)
	}

	stats := OutputStats{
		SizeBytes:  len(rawOutput),
		ExitCode:   exitCode,
		DurationMs: durationMs,
	}

	threshold := h.cfg.Summary.AutoIndexThresholdBytes
	if len(rawOutput) <= threshold {
		lineCount := len(strings.Split(strings.TrimRight(string(rawOutput), "\n"), "\n"))
		if len(rawOutput) == 0 {
			lineCount = 0
		}
		stats.Lines = lineCount
		recordToolCall(ctx, h.st, h.projectPath, "ctx_read_file", input.Path, string(rawOutput), "read: "+input.Path)
		return nil, ReadFileOutput{
			Stats:        stats,
			DirectOutput: string(rawOutput),
			Path:         absPath,
		}, nil
	}

	sum := summary.GenericSummarize(rawOutput, h.cfg.Summary.HeadLines, h.cfg.Summary.TailLines)
	stats.Lines = sum.TotalLines

	outputID := generateOutputID()
	displayCmd := fmt.Sprintf("[read_file] %s", absPath)
	if input.ProcessScript != "" {
		displayCmd = fmt.Sprintf("[read_file|%s] %s | %s", input.Language, absPath, sanitiseCommand(input.Language, input.ProcessScript))
	}

	out := &store.Output{
		OutputID:    outputID,
		Command:     displayCmd,
		Intent:      "",
		FullOutput:  string(rawOutput),
		SizeBytes:   int64(len(rawOutput)),
		LineCount:   sum.TotalLines,
		ExitCode:    exitCode,
		DurationMs:  durationMs,
		CreatedAt:   time.Now(),
		ProjectPath: h.projectPath,
	}
	if err := h.st.Save(ctx, out); err != nil {
		return nil, ReadFileOutput{}, fmt.Errorf("storing output: %w", err)
	}

	statsLine := summary.FormatStats(sum.TotalLines, sum.TotalBytes, exitCode, durationMs)
	outSummary := sum.Text + "\n" + statsLine
	recordToolCall(ctx, h.st, h.projectPath, "ctx_read_file", input.Path, outSummary, "read: "+input.Path)
	return nil, ReadFileOutput{
		OutputID:   outputID,
		Summary:    outSummary,
		Stats:      stats,
		SearchHint: fmt.Sprintf("Use ctx_search with output_id=%q to query this output", outputID),
		Path:       absPath,
	}, nil
}
