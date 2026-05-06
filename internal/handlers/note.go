package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// NoteInput is the input for ctx_note.
type NoteInput struct {
	// Discriminator: "save" (default if text is provided) or "list" (default if text is empty).
	Action string `json:"action,omitempty"`

	// Save fields (used when action="save" or omitted with non-empty text).
	Text       string   `json:"text,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	LinksTo    []string `json:"links_to,omitempty"`
	Importance string   `json:"importance,omitempty"`

	// List fields (used when action="list" or omitted with empty text).
	Scope         string `json:"scope,omitempty"`
	MinImportance string `json:"min_importance,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

// NoteOutput is the response from ctx_note (discriminated by Action field).
type NoteOutput struct {
	Action string `json:"action"`

	// Save result (when Action == "save").
	DecisionID string `json:"decision_id,omitempty"`
	SavedAt    string `json:"saved_at,omitempty"`
	Echo       string `json:"echo,omitempty"`

	// List result (when Action == "list").
	Decisions []DecisionOut `json:"decisions,omitempty"`
	Count     int           `json:"count,omitempty"`
	Scope     string        `json:"scope,omitempty"`
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

// NoteHandler handles ctx_note (save and list actions).
type NoteHandler struct {
	st          store.Store
	projectPath string
}

// NewNoteHandler constructs a NoteHandler.
func NewNoteHandler(st store.Store, projectPath string) *NoteHandler {
	return &NoteHandler{st: st, projectPath: projectPath}
}

// Handle dispatches to handleSave or handleList based on the action field.
// Backward compat: omitted action defaults to "save" if text is present, else "list".
func (h *NoteHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input NoteInput) (*mcp.CallToolResult, NoteOutput, error) {
	action := input.Action
	if action == "" {
		if strings.TrimSpace(input.Text) != "" {
			action = "save"
		} else {
			action = "list"
		}
	}

	switch action {
	case "save":
		return h.handleSave(ctx, input)
	case "list":
		return h.handleList(ctx, input)
	default:
		return nil, NoteOutput{}, fmt.Errorf("unknown action %q (expected 'save' or 'list')", action)
	}
}

func (h *NoteHandler) handleSave(ctx context.Context, input NoteInput) (*mcp.CallToolResult, NoteOutput, error) {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return nil, NoteOutput{}, fmt.Errorf("text must not be empty")
	}
	if len(text) > 2000 {
		return nil, NoteOutput{}, fmt.Errorf("text too long: %d chars (max 2000)", len(text))
	}

	importance := input.Importance
	if importance == "" {
		importance = store.ImportanceNormal
	}
	switch importance {
	case store.ImportanceLow, store.ImportanceNormal, store.ImportanceHigh:
		// valid
	default:
		return nil, NoteOutput{}, fmt.Errorf("invalid importance %q (must be low|normal|high)", importance)
	}

	cleanTags := make([]string, 0, len(input.Tags))
	for _, t := range input.Tags {
		t = strings.TrimSpace(t)
		if t == "" || strings.ContainsAny(t, ",\n") {
			continue
		}
		cleanTags = append(cleanTags, t)
	}

	d := &store.Decision{
		SessionID:   mcpSessionID,
		ProjectPath: h.projectPath,
		Text:        text,
		Tags:        cleanTags,
		LinksTo:     input.LinksTo,
		Importance:  importance,
	}

	if err := h.st.SaveDecision(ctx, d); err != nil {
		return nil, NoteOutput{}, fmt.Errorf("saving decision: %w", err)
	}

	recordToolCall(
		ctx, h.st, h.projectPath, "ctx_note",
		truncatePreview(text, 200),
		d.DecisionID,
		"note: "+truncatePreview(text, 80),
	)

	echo := text
	if len(echo) > 100 {
		echo = echo[:100] + "…"
	}

	return nil, NoteOutput{
		Action:     "save",
		DecisionID: d.DecisionID,
		SavedAt:    d.CreatedAt.Format(time.RFC3339),
		Echo:       echo,
	}, nil
}

func (h *NoteHandler) handleList(ctx context.Context, input NoteInput) (*mcp.CallToolResult, NoteOutput, error) {
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
		return nil, NoteOutput{}, fmt.Errorf("listing decisions: %w", err)
	}

	recordToolCall(
		ctx, h.st, h.projectPath, "ctx_note",
		scope, "", "note list: "+scope,
	)

	now := time.Now()
	out := NoteOutput{
		Action: "list",
		Scope:  scope,
		Count:  len(decisions),
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
