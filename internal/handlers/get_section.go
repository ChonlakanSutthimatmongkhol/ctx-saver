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

// GetSectionInput is the typed input for ctx_get_section.
type GetSectionInput struct {
	OutputID string `json:"output_id"         jsonschema:"ID of the stored output"`
	Heading  string `json:"heading"           jsonschema:"heading text to match (case-insensitive)"`
	Partial  bool   `json:"partial,omitempty" jsonschema:"allow substring match on heading (default: false)"`
}

// GetSectionOutput is the typed output for ctx_get_section.
type GetSectionOutput struct {
	OutputID  string                  `json:"output_id"`
	Heading   string                  `json:"heading"`
	StartLine int                     `json:"start_line,omitempty"`
	EndLine   int                     `json:"end_line,omitempty"`
	Lines     []string                `json:"lines,omitempty"`
	LineCount int                     `json:"line_count,omitempty"`
	Found     bool                    `json:"found"`
	Freshness freshness.FreshnessInfo `json:"freshness"`
}

// GetSectionHandler handles the ctx_get_section MCP tool.
type GetSectionHandler struct {
	st          store.Store
	projectPath string
}

// NewGetSectionHandler creates a GetSectionHandler.
func NewGetSectionHandler(st store.Store, projectPath string) *GetSectionHandler {
	return &GetSectionHandler{st: st, projectPath: projectPath}
}

// Handle implements the ctx_get_section tool.
func (h *GetSectionHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input GetSectionInput) (*mcp.CallToolResult, GetSectionOutput, error) {
	if input.OutputID == "" {
		return nil, GetSectionOutput{}, fmt.Errorf("output_id must not be empty")
	}
	if strings.TrimSpace(input.Heading) == "" {
		return nil, GetSectionOutput{}, fmt.Errorf("heading must not be empty")
	}

	out, err := h.st.Get(ctx, input.OutputID)
	if err != nil {
		return nil, GetSectionOutput{}, fmt.Errorf("getting output: %w", err)
	}

	fi := freshness.NewFreshnessInfo(out.SourceKind, out.RefreshedAt, out.TTLSeconds, time.Now())
	lines := strings.Split(strings.TrimRight(out.FullOutput, "\n"), "\n")
	start, end, found := summary.FindSection(lines, input.Heading, input.Partial)
	if !found {
		recordToolCall(ctx, h.st, h.projectPath, "ctx_get_section", input.OutputID+"#"+input.Heading, "", "get_section: "+input.Heading)
		return nil, GetSectionOutput{
			OutputID:  input.OutputID,
			Heading:   input.Heading,
			Found:     false,
			Freshness: fi,
		}, nil
	}

	selected := lines[start-1 : end]
	recordToolCall(ctx, h.st, h.projectPath, "ctx_get_section", input.OutputID+"#"+input.Heading, "", "get_section: "+input.Heading)
	return nil, GetSectionOutput{
		OutputID:  input.OutputID,
		Heading:   input.Heading,
		StartLine: start,
		EndLine:   end,
		Lines:     selected,
		LineCount: len(selected),
		Found:     true,
		Freshness: fi,
	}, nil
}
