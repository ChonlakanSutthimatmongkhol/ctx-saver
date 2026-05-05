package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/freshness"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers/signatures"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary"
)

// ReadFileInput is the typed input for the ctx_read_file MCP tool.
type ReadFileInput struct {
	Path          string `json:"path"                      jsonschema:"path to the file to read (relative paths resolved from server working directory)"`
	ProcessScript string `json:"process_script,omitempty"  jsonschema:"optional shell or python script that receives the file content via stdin (e.g. 'jq .endpoints')"`
	Language      string `json:"language,omitempty"        jsonschema:"language for process_script: shell or python (default: shell)"`
	Fields        string `json:"fields,omitempty"          jsonschema:"optional view filter: 'signatures' returns only function/type/const declarations with original line numbers. Omit for full content. Supported: go, python, dart (basic regex; complex generics, operator overloads, and multi-line signatures may be missed for dart — use process_script 'grep -nE ...' for complex Dart files)."`
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

// findCachedOutputForPath returns the most recent cached output for the given
// absolute file path, or nil if none exists. Uses a 2-step lookup:
// FindRecentSameCommand (returns lightweight meta) → Get (returns full Output with SourceHash).
func (h *ReadFileHandler) findCachedOutputForPath(ctx context.Context, absPath string) (*store.Output, error) {
	command := "[read_file] " + absPath
	meta, err := h.st.FindRecentSameCommand(ctx, h.projectPath, command, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}
	return h.st.Get(ctx, meta.OutputID)
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

	// Check for a valid cached output before hitting disk.
	cached, err := h.findCachedOutputForPath(ctx, absPath)
	if err != nil {
		slog.Warn("read_file: cache lookup failed (proceeding without cache)",
			"path", absPath, "error", err)
		cached = nil
	}
	if cached != nil && cached.SourceHash != "" && input.ProcessScript == "" {
		currentHash, hashErr := freshness.FileSHA256(absPath)
		if hashErr != nil {
			slog.Warn("read_file: cannot hash file, falling through to read",
				"path", absPath, "error", hashErr)
		} else if currentHash == cached.SourceHash {
			recordToolCall(ctx, h.st, h.projectPath, "ctx_read_file",
				absPath, "", "read (cache-hit): "+absPath)
			return nil, ReadFileOutput{
				OutputID: cached.OutputID,
				Summary:  truncatePreview(cached.FullOutput, 1024),
				Stats: OutputStats{
					Lines:      cached.LineCount,
					SizeBytes:  int(cached.SizeBytes),
					DurationMs: 0,
				},
				SearchHint: fmt.Sprintf(
					"Returning cached content (file unchanged since %s). Use ctx_get_full %s for the full file.",
					cached.RefreshedAt.Format(time.RFC3339),
					cached.OutputID,
				),
				Path: absPath,
			}, nil
		}
		// Hash mismatch → fall through and re-read from disk.
	}

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
		directContent := string(rawOutput)
		if input.Fields == "signatures" {
			filtered, ferr := applySignaturesFilter(rawOutput, absPath)
			if ferr != nil {
				return nil, ReadFileOutput{}, ferr
			}
			directContent = filtered
		}
		return nil, ReadFileOutput{
			Stats:        stats,
			DirectOutput: directContent,
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

	// Compute SHA-256 for direct reads only; process_script outputs depend on
	// script logic beyond the file content, so hashing the file alone is ambiguous.
	sourceHash := ""
	if input.ProcessScript == "" {
		if fh, herr := freshness.FileSHA256(absPath); herr == nil {
			sourceHash = fh
		}
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
		SourceHash:  sourceHash,
	}
	if err := h.st.Save(ctx, out); err != nil {
		return nil, ReadFileOutput{}, fmt.Errorf("storing output: %w", err)
	}

	statsLine := summary.FormatStats(sum.TotalLines, sum.TotalBytes, exitCode, durationMs)
	outSummary := sum.Text + "\n" + statsLine
	recordToolCall(ctx, h.st, h.projectPath, "ctx_read_file", input.Path, outSummary, "read: "+input.Path)

	// Apply signatures filter AFTER saving full content to DB (view-only).
	if input.Fields == "signatures" {
		filtered, ferr := applySignaturesFilter(rawOutput, absPath)
		if ferr != nil {
			return nil, ReadFileOutput{}, ferr
		}
		return nil, ReadFileOutput{
			OutputID:   outputID,
			Summary:    filtered,
			Stats:      stats,
			SearchHint: fmt.Sprintf("Signatures view. Use ctx_get_full %q for full file content.", outputID),
			Path:       absPath,
		}, nil
	}

	return nil, ReadFileOutput{
		OutputID:   outputID,
		Summary:    outSummary,
		Stats:      stats,
		SearchHint: fmt.Sprintf("Use ctx_search with output_id=%q to query this output", outputID),
		Path:       absPath,
	}, nil
}

// applySignaturesFilter extracts function/type/const signatures from rawOutput
// using the language detected from absPath.
// Returns an error if the file type is unsupported for fields=signatures.
func applySignaturesFilter(rawOutput []byte, absPath string) (string, error) {
	lang := signatures.DetectLanguage(absPath)
	if lang == signatures.LangNone {
		ext := filepath.Ext(absPath)
		if ext == "" {
			ext = "(no extension)"
		}
		return "", fmt.Errorf("fields=signatures unsupported for file type %s; omit --fields or use process_script", ext)
	}
	filtered, err := signatures.Extract(rawOutput, lang)
	if err != nil {
		return "", fmt.Errorf("fields=signatures: extracting from %s: %w", absPath, err)
	}
	return filtered, nil
}
