package handlers_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// ── T2.1: confirm="" → error, no deletion ────────────────────────────────

func TestPurge_ConfirmEmpty_ReturnsError(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewPurgeHandler(st, "/proj")

	_, _, err := h.Handle(context.Background(), nil, handlers.PurgeInput{
		Confirm: "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirm")
	// Nothing should have been deleted.
	assert.Nil(t, st.saved)
}

func TestPurge_ConfirmWrongValue_ReturnsError(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewPurgeHandler(st, "/proj")

	_, _, err := h.Handle(context.Background(), nil, handlers.PurgeInput{
		Confirm: "YES", // wrong case
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirm")
}

// ── T2.2: confirm="yes", all=false → outputs+events deleted, notes kept ──

func TestPurge_ConfirmYes_DefaultPreservesNotes(t *testing.T) {
	st := &mockStore{}

	// Seed some data.
	st.saved = []*store.Output{
		{OutputID: "o1", ProjectPath: "/proj"},
		{OutputID: "o2", ProjectPath: "/proj"},
	}
	st.sessionEventCount = 3
	_ = st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj",
		Text:        "decision 1",
		Importance:  "normal",
	})

	h := handlers.NewPurgeHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.PurgeInput{
		Confirm: "yes",
		All:     false,
	})
	require.NoError(t, err)

	assert.Equal(t, 2, out.OutputsDeleted)
	assert.Equal(t, 3, out.EventsDeleted)
	assert.Equal(t, 0, out.NotesDeleted)
	assert.Equal(t, 1, out.NotesKept)
	assert.Nil(t, st.saved, "outputs should be cleared")
}

// ── T2.3: confirm="yes", all=true → all three deleted ─────────────────────

func TestPurge_ConfirmYes_AllTrue_DeletesNotes(t *testing.T) {
	st := &mockStore{}

	st.saved = []*store.Output{
		{OutputID: "o1", ProjectPath: "/proj"},
	}
	st.sessionEventCount = 2
	_ = st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj",
		Text:        "decision x",
		Importance:  "normal",
	})

	h := handlers.NewPurgeHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.PurgeInput{
		Confirm: "yes",
		All:     true,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, out.OutputsDeleted)
	assert.Equal(t, 2, out.EventsDeleted)
	assert.Equal(t, 1, out.NotesDeleted)
	assert.Equal(t, 0, out.NotesKept)
	assert.Nil(t, st.decisions, "notes should be cleared")
}

// ── T2.4: Project isolation ────────────────────────────────────────────────

func TestPurge_ProjectIsolation(t *testing.T) {
	// Use a real SQLite store to validate project isolation at the DB level.
	dir := t.TempDir()
	st, err := store.NewSQLiteStore(dir, "/proj-a")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()

	// Save outputs to two different projects.
	require.NoError(t, st.Save(ctx, &store.Output{
		OutputID:    "a1",
		Command:     "cmd-a",
		FullOutput:  "output a",
		ProjectPath: "/proj-a",
	}))
	require.NoError(t, st.Save(ctx, &store.Output{
		OutputID:    "b1",
		Command:     "cmd-b",
		FullOutput:  "output b",
		ProjectPath: "/proj-b",
	}))

	// Purge only project A.
	n, err := st.PurgeOutputs(ctx, "/proj-a")
	require.NoError(t, err)
	assert.Equal(t, 1, n, "should delete exactly 1 output from proj-a")

	// Project B should still have its output.
	list, err := st.List(ctx, "/proj-b", 10)
	require.NoError(t, err)
	assert.Len(t, list, 1, "project B outputs must be unaffected by purge of project A")

	// Project A should have no outputs left.
	listA, err := st.List(ctx, "/proj-a", 10)
	require.NoError(t, err)
	assert.Empty(t, listA)
}

// ── T2.5: Counts in response match what was actually deleted ──────────────

func TestPurge_CountsMatchDeletions(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewSQLiteStore(dir, "/proj")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()

	// Seed outputs and notes.
	for i := 0; i < 3; i++ {
		require.NoError(t, st.Save(ctx, &store.Output{
			OutputID:    fmt.Sprintf("out_%d", i),
			Command:     "cmd",
			FullOutput:  "output",
			ProjectPath: "/proj",
		}))
	}
	require.NoError(t, st.SaveDecision(ctx, &store.Decision{
		ProjectPath: "/proj",
		Text:        "note 1",
		Importance:  "normal",
	}))
	require.NoError(t, st.SaveDecision(ctx, &store.Decision{
		ProjectPath: "/proj",
		Text:        "note 2",
		Importance:  "high",
	}))

	h := handlers.NewPurgeHandler(st, "/proj")
	_, out, err := h.Handle(ctx, nil, handlers.PurgeInput{
		Confirm: "yes",
		All:     true,
	})
	require.NoError(t, err)

	assert.Equal(t, 3, out.OutputsDeleted)
	assert.Equal(t, 2, out.NotesDeleted)

	// Verify DB is actually empty.
	remaining, err := st.List(ctx, "/proj", 10)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}
