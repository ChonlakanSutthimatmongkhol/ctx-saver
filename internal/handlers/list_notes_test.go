package handlers_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

func TestListNotesHandler_DefaultScope(t *testing.T) {
	st := &mockStore{
		decisions: []*store.Decision{
			{DecisionID: "dec_1", ProjectPath: "/proj", Text: "first", Importance: "normal", CreatedAt: time.Now()},
		},
	}
	h := handlers.NewNoteHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{Action: "list"})
	require.NoError(t, err)
	assert.Equal(t, "7d", out.Scope)
	assert.Equal(t, 1, out.Count)
	assert.Len(t, out.Decisions, 1)
}

func TestListNotesHandler_LimitClamping(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	// limit > 100 should be clamped — no error, just returns empty
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{Action: "list", Limit: 999})
	require.NoError(t, err)
	assert.Equal(t, 0, out.Count)
}

func TestListNotesHandler_AgoHuman(t *testing.T) {
	st := &mockStore{
		decisions: []*store.Decision{
			{
				DecisionID:  "dec_2",
				ProjectPath: "/proj",
				Text:        "recent decision",
				Importance:  "normal",
				CreatedAt:   time.Now().Add(-5 * time.Minute),
			},
		},
	}
	h := handlers.NewNoteHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.NoteInput{Action: "list"})
	require.NoError(t, err)
	require.Len(t, out.Decisions, 1)
	assert.Equal(t, "5m", out.Decisions[0].AgoHuman)
	assert.Greater(t, out.Decisions[0].AgoSeconds, int64(0))
}

func TestListNotesHandler_RecordsSessionEvent(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewNoteHandler(st, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.NoteInput{Action: "list"})
	require.NoError(t, err)
	assert.Equal(t, 1, st.sessionEventCount)
}
