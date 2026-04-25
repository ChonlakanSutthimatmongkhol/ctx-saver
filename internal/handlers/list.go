package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// ListInput is the typed input for ctx_list_outputs.
type ListInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"maximum number of outputs to return (default: 50)"`
}

// OutputEntry is one row in the ctx_list_outputs response.
type OutputEntry struct {
	OutputID  string `json:"output_id"`
	Command   string `json:"command"`
	CreatedAt string `json:"created_at"`
	SizeBytes int64  `json:"size_bytes"`
	Lines     int    `json:"lines"`
}

// ListOutput is the typed output for ctx_list_outputs.
type ListOutput struct {
	Outputs []OutputEntry `json:"outputs"`
}

// ListHandler handles the ctx_list_outputs MCP tool.
type ListHandler struct {
	st          store.Store
	projectPath string
}

// NewListHandler creates a ListHandler.
func NewListHandler(st store.Store, projectPath string) *ListHandler {
	return &ListHandler{st: st, projectPath: projectPath}
}

// Handle implements the ctx_list_outputs tool.
func (h *ListHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input ListInput) (*mcp.CallToolResult, ListOutput, error) {
	metas, err := h.st.List(ctx, h.projectPath, input.Limit)
	if err != nil {
		return nil, ListOutput{}, fmt.Errorf("listing outputs: %w", err)
	}

	entries := make([]OutputEntry, 0, len(metas))
	for _, m := range metas {
		entries = append(entries, OutputEntry{
			OutputID:  m.OutputID,
			Command:   m.Command,
			CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
			SizeBytes: m.SizeBytes,
			Lines:     m.LineCount,
		})
	}

	recordToolCall(ctx, h.st, h.projectPath, "ctx_list_outputs", "", "", "list outputs")
	return nil, ListOutput{Outputs: entries}, nil
}
