package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
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
	OutputID   string         `json:"output_id"`
	TotalLines int            `json:"total_lines"`
	Entries    []OutlineEntry `json:"entries"`
}

// OutlineHandler handles the ctx_outline MCP tool.
type OutlineHandler struct {
	st store.Store
}

// NewOutlineHandler creates an OutlineHandler.
func NewOutlineHandler(st store.Store) *OutlineHandler {
	return &OutlineHandler{st: st}
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

	var entries []OutlineEntry
	for i, line := range lines {
		lineNo := i + 1
		switch {
		case strings.HasPrefix(line, "#### "):
			entries = append(entries, OutlineEntry{Line: lineNo, Level: 3, Text: line})
		case strings.HasPrefix(line, "### "):
			entries = append(entries, OutlineEntry{Line: lineNo, Level: 2, Text: line})
		case strings.HasPrefix(line, "## "):
			entries = append(entries, OutlineEntry{Line: lineNo, Level: 1, Text: line})
		default:
			// Table header: current line starts with | and next line is a separator row
			if strings.HasPrefix(line, "|") && i+1 < len(lines) && isTableSeparator(lines[i+1]) {
				entries = append(entries, OutlineEntry{Line: lineNo, Level: 0, Text: line})
			}
		}
	}

	return nil, OutlineOutput{
		OutputID:   input.OutputID,
		TotalLines: totalLines,
		Entries:    entries,
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
