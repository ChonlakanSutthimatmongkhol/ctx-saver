package store_test

import (
	"context"
	"fmt"
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

func TestGetStats_EmptyDB(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	stats, err := st.GetStats(ctx, "/test/project", time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 0, stats.OutputsStored)
	assert.Equal(t, int64(0), stats.RawBytes)
	assert.Equal(t, int64(0), stats.LargestBytes)
	assert.Equal(t, int64(0), stats.AvgDurationMs)
	assert.Empty(t, stats.TopCommands)
	assert.Empty(t, stats.LargestOutputs)
	assert.Equal(t, 0, stats.DangerousBlocked)
	assert.Equal(t, 0, stats.RedirectedToMCP)
	assert.Equal(t, 0, stats.EventsCaptured)
}

func TestGetStats_SingleOutput(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	out := sampleOutput("out_stats_001")
	out.SizeBytes = 1000
	out.DurationMs = 200
	require.NoError(t, st.Save(ctx, out))

	stats, err := st.GetStats(ctx, "/test/project", time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.OutputsStored)
	assert.Equal(t, int64(1000), stats.RawBytes)
	assert.Equal(t, int64(1000), stats.LargestBytes)
	assert.Equal(t, int64(200), stats.AvgDurationMs)
	require.Len(t, stats.TopCommands, 1)
	assert.Equal(t, out.Command, stats.TopCommands[0].Command)
	require.Len(t, stats.LargestOutputs, 1)
	assert.Equal(t, "out_stats_001", stats.LargestOutputs[0].OutputID)
}

func TestGetStats_MultipleCommands(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// 3 outputs with cmd_a, 2 with cmd_b, 1 with cmd_c.
	for i := range 3 {
		o := sampleOutput(fmt.Sprintf("out_cmd_a%d", i))
		o.Command = "[shell] cmd_a"
		require.NoError(t, st.Save(ctx, o))
	}
	for i := range 2 {
		o := sampleOutput(fmt.Sprintf("out_cmd_b%d", i))
		o.Command = "[shell] cmd_b"
		require.NoError(t, st.Save(ctx, o))
	}
	o := sampleOutput("out_cmd_c0")
	o.Command = "[shell] cmd_c"
	require.NoError(t, st.Save(ctx, o))

	stats, err := st.GetStats(ctx, "/test/project", time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 6, stats.OutputsStored)
	require.GreaterOrEqual(t, len(stats.TopCommands), 3)
	assert.Equal(t, "[shell] cmd_a", stats.TopCommands[0].Command)
	assert.Equal(t, 3, stats.TopCommands[0].Count)
	assert.Equal(t, "[shell] cmd_b", stats.TopCommands[1].Command)
	assert.Equal(t, 2, stats.TopCommands[1].Count)
}

func TestGetStats_SinceFilter(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	old := sampleOutput("out_old_stats")
	old.CreatedAt = time.Now().AddDate(0, 0, -10)
	old.SizeBytes = 9000
	require.NoError(t, st.Save(ctx, old))

	recent := sampleOutput("out_recent_stats")
	recent.SizeBytes = 1000
	require.NoError(t, st.Save(ctx, recent))

	stats, err := st.GetStats(ctx, "/test/project", time.Now().AddDate(0, 0, -1))
	require.NoError(t, err)
	assert.Equal(t, 1, stats.OutputsStored)
	assert.Equal(t, int64(1000), stats.RawBytes)
}

func TestGetStats_HookCounts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	events := []*store.SessionEvent{
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "pretooluse", Summary: "deny: rm -rf /"},
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "pretooluse", Summary: "redirect: curl to ctx_execute"},
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "posttooluse", Summary: "recorded tool call"},
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "pretooluse", Summary: "deny: sudo rm"},
	}
	for _, e := range events {
		e.CreatedAt = time.Now()
		require.NoError(t, st.SaveSessionEvent(ctx, e))
	}

	stats, err := st.GetStats(ctx, "/test/project", time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 4, stats.EventsCaptured)
	assert.Equal(t, 2, stats.DangerousBlocked)
	assert.Equal(t, 1, stats.RedirectedToMCP)
}
