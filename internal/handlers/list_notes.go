package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// ListNotesInput is the input for ctx_list_notes.
type ListNotesInput struct {
	Scope         string   `json:"scope,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	MinImportance string   `json:"min_importance,omitempty"`
	Limit         int      `json:"limit,omitempty"`
}

// ListNotesOutput is the response from ctx_list_notes.
type ListNotesOutput struct {
	Decisions []DecisionOut `json:"decisions"`
	Count     int           `json:"count"`
	Scope     string        `json:"scope"`
}

// DecisionOut is the wire format for one decision.
type DecisionOut struct {
	DecisionID string   `json:"decision_id"`
	Text       string   `json:"text"`
	Tags       []string `json:"tags,omitempty"`
	LinksTo    []string `json:"links_to,omitempty"`
	Importance string   `json:"importance"`
	AgoSeconds int64    `json:"ago_seconds"`
	AgoHuman   string   `json:"ago_human"`
	SavedAt    string   `json:"saved_at"`
}

// ListNotesHandler handles ctx_list_notes.
type ListNotesHandler struct {
	st          store.Store
	projectPath string
}

// NewListNotesHandler constructs a ListNotesHandler.
func NewListNotesHandler(st store.Store, projectPath string) *ListNotesHandler {
	return &ListNotesHandler{st: st, projectPath: projectPath}
}

// Handle processes a ctx_list_notes request.
func (h *ListNotesHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input ListNotesInput) (*mcp.CallToolResult, ListNotesOutput, error) {
	scope := input.Scope
	if scope == "" {
		scope = "7d"
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	decisions, err := h.st.ListDecisions(ctx, store.ListDecisionsOptions{
		ProjectPath:   h.projectPath,
		SessionID:     mcpSessionID,
		Scope:         scope,
		MinImportance: input.MinImportance,
		Tags:          input.Tags,
		Limit:         limit,
	})
	if err != nil {
		return nil, ListNotesOutput{}, fmt.Errorf("listing decisions: %w", err)
	}

	recordToolCall(
		ctx, h.st, h.projectPath, "ctx_list_notes",
		scope, "", "list_notes: "+scope,
	)

	now := time.Now()
	out := ListNotesOutput{
		Scope: scope,
		Count: len(decisions),
	}
	for _, d := range decisions {
		ago := now.Sub(d.CreatedAt)
		out.Decisions = append(out.Decisions, DecisionOut{
			DecisionID: d.DecisionID,
			Text:       d.Text,
			Tags:       d.Tags,
			LinksTo:    d.LinksTo,
			Importance: d.Importance,
			AgoSeconds: int64(ago.Seconds()),
			AgoHuman:   humanAgeShort(ago),
			SavedAt:    d.CreatedAt.Format(time.RFC3339),
		})
	}
	return nil, out, nil
}

// humanAgeShort returns a compact human-readable age string: "<1m", "5m", "3h", "2d".
func humanAgeShort(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
