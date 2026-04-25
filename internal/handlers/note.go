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
	Text       string   `json:"text"`
	Tags       []string `json:"tags,omitempty"`
	LinksTo    []string `json:"links_to,omitempty"`
	Importance string   `json:"importance,omitempty"`
}

// NoteOutput is the response from ctx_note.
type NoteOutput struct {
	DecisionID string `json:"decision_id"`
	SavedAt    string `json:"saved_at"`
	Echo       string `json:"echo"` // first 100 chars of text
}

// NoteHandler handles ctx_note.
type NoteHandler struct {
	st          store.Store
	projectPath string
}

// NewNoteHandler constructs a NoteHandler.
func NewNoteHandler(st store.Store, projectPath string) *NoteHandler {
	return &NoteHandler{st: st, projectPath: projectPath}
}

// Handle processes a ctx_note request.
func (h *NoteHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input NoteInput) (*mcp.CallToolResult, NoteOutput, error) {
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
		DecisionID: d.DecisionID,
		SavedAt:    d.CreatedAt.Format(time.RFC3339),
		Echo:       echo,
	}, nil
}
