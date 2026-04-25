package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/freshness"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// GetFullInput is the typed input for ctx_get_full.
type GetFullInput struct {
	OutputID  string `json:"output_id"            jsonschema:"ID of the output to retrieve"`
	LineRange []int  `json:"line_range,omitempty" jsonschema:"optional [start, end] line range (1-based, inclusive); omit to retrieve all lines"`
}

// GetFullOutput is the typed output for ctx_get_full.
type GetFullOutput struct {
	OutputID   string                `json:"output_id"`
	Lines      []string              `json:"lines"`
	TotalLines int                   `json:"total_lines"`
	Returned   int                   `json:"returned"`
	Freshness  freshness.FreshnessInfo `json:"freshness"`
}

// GetFullHandler handles the ctx_get_full MCP tool.
type GetFullHandler struct {
	st          store.Store
	projectPath string
	sb          sandbox.Sandbox
	fc          config.FreshnessConfig
}

// NewGetFullHandler creates a GetFullHandler.
func NewGetFullHandler(st store.Store, projectPath string) *GetFullHandler {
	return &GetFullHandler{st: st, projectPath: projectPath}
}

// WithFreshness attaches a sandbox and freshness config for auto-refresh support.
func (h *GetFullHandler) WithFreshness(sb sandbox.Sandbox, fc config.FreshnessConfig) *GetFullHandler {
	h.sb = sb
	h.fc = fc
	return h
}

// Handle implements the ctx_get_full tool.
func (h *GetFullHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input GetFullInput) (*mcp.CallToolResult, GetFullOutput, error) {
	if input.OutputID == "" {
		return nil, GetFullOutput{}, fmt.Errorf("output_id must not be empty")
	}

	out, err := h.st.Get(ctx, input.OutputID)
	if err != nil {
		return nil, GetFullOutput{}, fmt.Errorf("retrieving output: %w", err)
	}

	// Auto-refresh before splitting so lines reflect the freshest content.
	if res := freshness.Resolve(out.SourceKind, out.RefreshedAt, h.fc); res.Action == "auto_refresh" {
		out = refreshOutput(ctx, h.st, h.sb, "", out)
	}

	// Split into lines (strip trailing newline first to avoid a ghost empty line).
	allLines := strings.Split(strings.TrimRight(out.FullOutput, "\n"), "\n")
	totalLines := len(allLines)

	start := 1
	end := totalLines

	if len(input.LineRange) == 2 {
		start = input.LineRange[0]
		end = input.LineRange[1]
		if start < 1 {
			start = 1
		}
		if end > totalLines {
			end = totalLines
		}
		if start > end {
			return nil, GetFullOutput{}, fmt.Errorf("line_range start (%d) must be <= end (%d)", start, end)
		}
	} else if len(input.LineRange) != 0 {
		return nil, GetFullOutput{}, fmt.Errorf("line_range must have exactly 2 elements [start, end], got %d", len(input.LineRange))
	}

	selected := allLines[start-1 : end]
	fi := freshness.NewFreshnessInfo(out.SourceKind, out.RefreshedAt, out.TTLSeconds, time.Now())
	recordToolCall(ctx, h.st, h.projectPath, "ctx_get_full", input.OutputID, "", "get_full: "+input.OutputID)
	return nil, GetFullOutput{
		OutputID:   input.OutputID,
		Lines:      selected,
		TotalLines: totalLines,
		Returned:   len(selected),
		Freshness:  fi,
	}, nil
}
