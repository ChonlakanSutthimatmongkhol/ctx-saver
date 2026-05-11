package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

func TestNoteHandler_HappyPath(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")

	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Text:       "Use WithFreshness pattern because 15 test sites would break",
		Tags:       []string{"arch", "phase7"},
		Importance: "high",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.DecisionID)
	assert.NotEmpty(t, out.SavedAt)
	assert.Contains(t, out.Echo, "Use WithFreshness")
	assert.Equal(t, 1, st.savedDecisions)
	assert.Equal(t, "high", st.decisions[0].Importance)
	assert.Equal(t, []string{"arch", "phase7"}, st.decisions[0].Tags)
}

func TestNoteHandler_EmptyTextRejected(t *testing.T) {
	h := handlers.NewNoteHandler(&mockStore{}, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.NoteInput{Action: "save", Text: "   "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text must not be empty")
}

func TestNoteHandler_TooLongRejected(t *testing.T) {
	h := handlers.NewNoteHandler(&mockStore{}, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Text: strings.Repeat("x", 2001),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text too long")
}

func TestNoteHandler_InvalidImportance(t *testing.T) {
	h := handlers.NewNoteHandler(&mockStore{}, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Text:       "some decision",
		Importance: "critical",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid importance")
}

func TestNoteHandler_DefaultImportance(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Text: "some decision with default importance",
	})
	require.NoError(t, err)
	assert.Equal(t, store.ImportanceNormal, st.decisions[0].Importance)
}

func TestNoteHandler_SaveWithTask(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Text: "continue retirement forms",
		Task: "retirement-feature",
	})
	require.NoError(t, err)
	assert.Equal(t, "save", out.Action)
	require.Len(t, st.decisions, 1)
	assert.Equal(t, "retirement-feature", st.decisions[0].Task)
}

func TestNoteHandler_TagsSanitized(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Text: "decision",
		Tags: []string{"good", "bad,tag", "  ", "also\nbad", "fine"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"good", "fine"}, st.decisions[0].Tags)
}

func TestNoteHandler_RecordsSessionEvent(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Text: "decision that triggers telemetry",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, st.sessionEventCount)
}

func TestNoteHandler_EchoTruncatedAt100(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	longText := strings.Repeat("a", 150)
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{Text: longText})
	require.NoError(t, err)
	assert.True(t, len(out.Echo) <= 104, "echo should be ~100 chars + ellipsis") // 100 + "…" (3 bytes)
	assert.Contains(t, out.Echo, "…")
}

// M1 dispatch tests

func TestNoteHandler_ActionOmittedWithText_Saves(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{Text: "some decision"})
	require.NoError(t, err)
	assert.Equal(t, "save", out.Action)
	assert.NotEmpty(t, out.DecisionID)
}

func TestNoteHandler_ActionOmittedNoText_Lists(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{})
	require.NoError(t, err)
	assert.Equal(t, "list", out.Action)
	assert.Equal(t, "7d", out.Scope)
}

func TestNoteHandler_ActionSave_Saves(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{Action: "save", Text: "explicit save"})
	require.NoError(t, err)
	assert.Equal(t, "save", out.Action)
	assert.Equal(t, 1, st.savedDecisions)
}

func TestNoteHandler_ActionList_Lists(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{Action: "list"})
	require.NoError(t, err)
	assert.Equal(t, "list", out.Action)
}

func TestNoteHandler_ActionList_WithTaskFilters(t *testing.T) {
	st := &mockStore{}
	st.decisions = []*store.Decision{
		{DecisionID: "dec_task", ProjectPath: "/proj", Text: "scoped", Task: "retirement-feature", Importance: store.ImportanceNormal},
		{DecisionID: "dec_other", ProjectPath: "/proj", Text: "other", Task: "tax-feature", Importance: store.ImportanceNormal},
		{DecisionID: "dec_general", ProjectPath: "/proj", Text: "general", Importance: store.ImportanceNormal},
	}
	h := handlers.NewNoteHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Action: "list",
		Task:   "retirement-feature",
	})
	require.NoError(t, err)
	require.Len(t, out.Decisions, 1)
	assert.Equal(t, "dec_task", out.Decisions[0].DecisionID)
}

func TestNoteHandler_ActionHandoff_RequiresTextAndTask(t *testing.T) {
	h := handlers.NewNoteHandler(&mockStore{}, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Action: "handoff",
		Task:   "retirement-feature",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text")

	_, _, err = h.Handle(context.Background(), nil, handlers.NoteInput{
		Action: "handoff",
		Text:   "state",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task")
}

func TestNoteHandler_ActionHandoff_AutoSetsImportanceAndTags(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{
		Action:     "handoff",
		Text:       "parser is green; next wire UI",
		Task:       "retirement-feature",
		Tags:       []string{"arch", "handoff"},
		Importance: store.ImportanceLow,
	})
	require.NoError(t, err)
	assert.Equal(t, "save", out.Action)
	require.Len(t, st.decisions, 1)
	assert.Equal(t, store.ImportanceHigh, st.decisions[0].Importance)
	assert.Equal(t, "retirement-feature", st.decisions[0].Task)
	assert.Equal(t, []string{"arch", "handoff", "session-end"}, st.decisions[0].Tags)
}

func TestNoteHandler_ActionBogus_Error(t *testing.T) {
	h := handlers.NewNoteHandler(&mockStore{}, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.NoteInput{Action: "bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
	assert.Contains(t, err.Error(), "bogus")
}
