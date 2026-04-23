package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	st, err := store.NewSQLiteStore(dir, "/test/project")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	return st
}

func sampleOutput(id string) *store.Output {
	return &store.Output{
		OutputID:    id,
		Command:     "[shell] echo hello",
		Intent:      "test",
		FullOutput:  "line one\nline two with error code 404\nline three\nline four\nline five",
		SizeBytes:   50,
		LineCount:   5,
		ExitCode:    0,
		DurationMs:  42,
		CreatedAt:   time.Now(),
		ProjectPath: "/test/project",
	}
}

func TestSQLiteStore_SaveAndGet(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	out := sampleOutput("out_20260422_aabb")
	require.NoError(t, st.Save(ctx, out))

	got, err := st.Get(ctx, out.OutputID)
	require.NoError(t, err)
	assert.Equal(t, out.OutputID, got.OutputID)
	assert.Equal(t, out.Command, got.Command)
	assert.Equal(t, out.FullOutput, got.FullOutput)
	assert.Equal(t, out.ExitCode, got.ExitCode)
}

func TestSQLiteStore_GetNotFound(t *testing.T) {
	st := newTestStore(t)
	_, err := st.Get(context.Background(), "out_nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSQLiteStore_List(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := "out_20260422_" + string(rune('a'+i)) + "000"
		out := sampleOutput(id)
		out.CreatedAt = time.Now().Add(time.Duration(i) * time.Second)
		require.NoError(t, st.Save(ctx, out))
	}

	metas, err := st.List(ctx, "/test/project", 10)
	require.NoError(t, err)
	assert.Len(t, metas, 5)

	// Should be ordered newest first.
	assert.Greater(t, metas[0].CreatedAt.Unix(), metas[4].CreatedAt.Unix())
}

func TestSQLiteStore_ListLimit(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		id := "out_20260422_lim" + string(rune('a'+i))
		require.NoError(t, st.Save(ctx, sampleOutput(id)))
	}

	metas, err := st.List(ctx, "/test/project", 3)
	require.NoError(t, err)
	assert.Len(t, metas, 3)
}

func TestSQLiteStore_Search_FindsMatch(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, st.Save(ctx, sampleOutput("out_search_001")))

	matches, err := st.Search(ctx, "error", "", 5)
	require.NoError(t, err)
	require.NotEmpty(t, matches, "expected at least one match for 'error'")
	assert.Equal(t, "out_search_001", matches[0].OutputID)
}

func TestSQLiteStore_Search_FilterByOutputID(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Two outputs that both contain "error".
	o1 := sampleOutput("out_filter_001")
	o2 := sampleOutput("out_filter_002")
	require.NoError(t, st.Save(ctx, o1))
	require.NoError(t, st.Save(ctx, o2))

	matches, err := st.Search(ctx, "error", "out_filter_001", 5)
	require.NoError(t, err)
	for _, m := range matches {
		assert.Equal(t, "out_filter_001", m.OutputID)
	}
}

func TestSQLiteStore_Search_NoMatch(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, st.Save(ctx, sampleOutput("out_nomatch_001")))

	matches, err := st.Search(ctx, "xyzzy_unlikely_term_9999", "", 5)
	require.NoError(t, err)
	assert.Empty(t, matches)
}

func TestSQLiteStore_Cleanup(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Save one old and one recent output.
	old := sampleOutput("out_old_001")
	old.CreatedAt = time.Now().AddDate(0, 0, -30) // 30 days ago
	require.NoError(t, st.Save(ctx, old))

	recent := sampleOutput("out_recent_001")
	require.NoError(t, st.Save(ctx, recent))

	require.NoError(t, st.Cleanup(ctx, "/test/project", 14))

	// Old output should be gone.
	_, err := st.Get(ctx, "out_old_001")
	assert.Error(t, err)

	// Recent output should remain.
	got, err := st.Get(ctx, "out_recent_001")
	require.NoError(t, err)
	assert.Equal(t, "out_recent_001", got.OutputID)
}

func TestSQLiteStore_DBFilePermissions(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewSQLiteStore(dir, "/test/project")
	require.NoError(t, err)
	defer st.Close()

	// Find the db file.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	info, err := os.Stat(dir + "/" + entries[0].Name())
	require.NoError(t, err)
	// Should be 0600 (owner rw, no group/other).
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
