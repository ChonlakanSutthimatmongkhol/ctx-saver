package handlers

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// PurgeInput is the typed input for the ctx_purge MCP tool.
type PurgeInput struct {
	All     bool   `json:"all,omitempty"  jsonschema:"if true, also delete decision notes (ctx_note entries). Default false preserves notes."`
	Confirm string `json:"confirm"        jsonschema:"must be 'yes' to execute (safety check)"`
}

// PurgeOutput is the typed output for ctx_purge.
type PurgeOutput struct {
	OutputsDeleted int    `json:"outputs_deleted"`
	EventsDeleted  int    `json:"events_deleted"`
	NotesDeleted   int    `json:"notes_deleted"`
	NotesKept      int    `json:"notes_kept,omitempty"`
	Message        string `json:"message"`
}

// PurgeHandler handles the ctx_purge MCP tool.
type PurgeHandler struct {
	st          store.Store
	projectPath string
}

// NewPurgeHandler constructs a PurgeHandler.
func NewPurgeHandler(st store.Store, projectPath string) *PurgeHandler {
	return &PurgeHandler{st: st, projectPath: projectPath}
}

// Handle implements the ctx_purge tool.
func (h *PurgeHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input PurgeInput) (*mcp.CallToolResult, PurgeOutput, error) {
	if input.Confirm != "yes" {
		return nil, PurgeOutput{}, fmt.Errorf("purge: confirm must be \"yes\" to execute (got %q); this operation deletes cached data", input.Confirm)
	}

	outputsDeleted, err := h.st.PurgeOutputs(ctx, h.projectPath)
	if err != nil {
		return nil, PurgeOutput{}, fmt.Errorf("purge: deleting outputs for %s: %w", h.projectPath, err)
	}

	eventsDeleted, err := h.st.PurgeEvents(ctx, h.projectPath)
	if err != nil {
		return nil, PurgeOutput{}, fmt.Errorf("purge: deleting events for %s: %w", h.projectPath, err)
	}

	var notesDeleted, notesKept int
	if input.All {
		notesDeleted, err = h.st.PurgeNotes(ctx, h.projectPath)
		if err != nil {
			return nil, PurgeOutput{}, fmt.Errorf("purge: deleting notes for %s: %w", h.projectPath, err)
		}
	} else {
		// Count notes kept (do not delete them).
		notes, listErr := h.st.ListDecisions(ctx, store.ListDecisionsOptions{
			ProjectPath:   h.projectPath,
			Scope:         "all",
			MinImportance: "normal",
			Limit:         10000,
		})
		if listErr == nil {
			notesKept = len(notes)
		}
	}

	msg := fmt.Sprintf("Purged %d outputs, %d events", outputsDeleted, eventsDeleted)
	if input.All {
		msg += fmt.Sprintf(", %d notes", notesDeleted)
	} else {
		msg += fmt.Sprintf("; %d decision notes preserved (use all=true to also delete notes)", notesKept)
	}

	recordToolCall(ctx, h.st, h.projectPath, "ctx_purge", "", "", msg)
	return nil, PurgeOutput{
		OutputsDeleted: outputsDeleted,
		EventsDeleted:  eventsDeleted,
		NotesDeleted:   notesDeleted,
		NotesKept:      notesKept,
		Message:        msg,
	}, nil
}
