package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/freshness"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary"
)

// OutlineInput is the typed input for ctx_outline.
type OutlineInput struct {
	OutputID string `json:"output_id" jsonschema:"ID of the stored output to outline"`
}

// OutlineEntry is a single structural element found in the output.
type OutlineEntry struct {
	Line  int    `json:"line"`
	Level int    `json:"level"` // 1=##, 2=###, 3=####; 0=table header or numbered section
	Text  string `json:"text"`
}

// OutlineOutput is the typed output for ctx_outline.
type OutlineOutput struct {
	OutputID   string                  `json:"output_id"`
	TotalLines int                     `json:"total_lines"`
	Entries    []OutlineEntry          `json:"entries"`
	Freshness  freshness.FreshnessInfo `json:"freshness"`
}

// OutlineHandler handles the ctx_outline MCP tool.
type OutlineHandler struct {
	st          store.Store
	projectPath string
}

// NewOutlineHandler creates an OutlineHandler.
func NewOutlineHandler(st store.Store, projectPath string) *OutlineHandler {
	return &OutlineHandler{st: st, projectPath: projectPath}
}

// Handle implements the ctx_outline tool.
func (h *OutlineHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input OutlineInput) (*mcp.CallToolResult, OutlineOutput, error) {
	if input.OutputID == "" {
		return nil, OutlineOutput{}, fmt.Errorf("output_id must not be empty")
	}

	out, err := h.st.Get(ctx, input.OutputID)
	if err != nil {
		return nil, OutlineOutput{}, fmt.Errorf("getting output: %w", err)
	}

	lines := strings.Split(strings.TrimRight(out.FullOutput, "\n"), "\n")
	totalLines := len(lines)

	heads := summary.ExtractHeadings(lines)
	entries := make([]OutlineEntry, 0, len(heads))
	for _, h := range heads {
		entries = append(entries, OutlineEntry{Line: h.Line, Level: h.Level, Text: h.Raw})
	}

	// Table headers: current line starts with | and next line is a separator row.
	for i, line := range lines {
		if strings.HasPrefix(line, "|") && i+1 < len(lines) && isTableSeparator(lines[i+1]) {
			entries = append(entries, OutlineEntry{Line: i + 1, Level: 0, Text: line})
		}
	}

	fi := freshness.NewFreshnessInfo(out.SourceKind, out.RefreshedAt, out.TTLSeconds, time.Now())
	recordToolCall(ctx, h.st, h.projectPath, "ctx_outline", input.OutputID, "", "outline: "+input.OutputID)
	return nil, OutlineOutput{
		OutputID:   input.OutputID,
		TotalLines: totalLines,
		Entries:    entries,
		Freshness:  fi,
	}, nil
}

// isTableSeparator returns true for Markdown table separator rows like |---|---|.
func isTableSeparator(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	for _, ch := range strings.ReplaceAll(trimmed, " ", "") {
		if ch != '|' && ch != '-' && ch != ':' {
			return false
		}
	}
	return true
}
