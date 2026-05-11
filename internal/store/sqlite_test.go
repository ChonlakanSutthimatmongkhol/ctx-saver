package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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

// ── FTS5 escape + fallback tests (Task 5.1) ───────────────────────────────

func TestSearch_SpecialChars_Hash(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	out := sampleOutput("out_hash_001")
	out.FullOutput = "found #API-123 in response\nother line"
	require.NoError(t, st.Save(ctx, out))

	matches, err := st.Search(ctx, "#API-123", "", 5)
	require.NoError(t, err, "special char query must not return error")
	require.NotEmpty(t, matches)
	assert.Equal(t, "out_hash_001", matches[0].OutputID)
}

func TestSearch_SpecialChars_Pipe(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	out := sampleOutput("out_pipe_001")
	out.FullOutput = "log: error | warning detected\nother line"
	require.NoError(t, st.Save(ctx, out))

	matches, err := st.Search(ctx, "error | warning", "", 5)
	require.NoError(t, err, "pipe in query must not return error")
	require.NotEmpty(t, matches)
}

func TestSearch_SpecialChars_Dash(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	out := sampleOutput("out_dash_001")
	out.FullOutput = "connecting to payment-service endpoint\nother line"
	require.NoError(t, st.Save(ctx, out))

	matches, err := st.Search(ctx, "payment-service", "", 5)
	require.NoError(t, err, "dash in query must not return error")
	require.NotEmpty(t, matches)
}

func TestSearch_SpecialChars_Colon(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	out := sampleOutput("out_colon_001")
	out.FullOutput = "config field:value must be set\nother line"
	require.NoError(t, st.Save(ctx, out))

	matches, err := st.Search(ctx, "field:value", "", 5)
	require.NoError(t, err, "colon in query must not return error")
	require.NotEmpty(t, matches)
}

func TestSearch_ModeIsFTS5_ForNormalQuery(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, st.Save(ctx, sampleOutput("out_mode_001")))

	matches, err := st.Search(ctx, "error", "", 5)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	for _, m := range matches {
		assert.Equal(t, "fts5", m.Mode)
	}
}

func TestSearchLike_Basic(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	out := sampleOutput("out_like_001")
	out.FullOutput = "payment-service timeout error\nother line"
	require.NoError(t, st.Save(ctx, out))

	matches, err := st.SearchLike(ctx, "payment-service", "", 5)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	assert.Equal(t, "out_like_001", matches[0].OutputID)
	assert.Equal(t, "like_fallback", matches[0].Mode)
}

func TestSearchLike_EscapePercentUnderscore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	out := sampleOutput("out_like_esc_001")
	out.FullOutput = "100% complete\nother line\nstatus_code: 200"
	require.NoError(t, st.Save(ctx, out))

	// % must be treated as literal, not SQL wildcard.
	matches, err := st.SearchLike(ctx, "100%", "", 5)
	require.NoError(t, err)
	require.NotEmpty(t, matches, "literal %% must match")

	// _ must be treated as literal, not SQL single-char wildcard.
	matches2, err := st.SearchLike(ctx, "status_code", "", 5)
	require.NoError(t, err)
	require.NotEmpty(t, matches2, "literal _ must match")
}

func TestSearchLike_FilterByOutputID(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	o1 := sampleOutput("out_like_f_001")
	o1.FullOutput = "payment-service found\nother"
	o2 := sampleOutput("out_like_f_002")
	o2.FullOutput = "payment-service also here\nother"
	require.NoError(t, st.Save(ctx, o1))
	require.NoError(t, st.Save(ctx, o2))

	matches, err := st.SearchLike(ctx, "payment-service", "out_like_f_001", 5)
	require.NoError(t, err)
	for _, m := range matches {
		assert.Equal(t, "out_like_f_001", m.OutputID)
	}
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

// ── FindRecentSameCommand tests (Task 5.2) ────────────────────────────────

func TestFindRecentSameCommand_Found(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	out := sampleOutput("out_dedup_001")
	out.Command = "[shell] go test ./..."
	require.NoError(t, st.Save(ctx, out))

	meta, err := st.FindRecentSameCommand(ctx, "/test/project", "[shell] go test ./...", time.Hour)
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "out_dedup_001", meta.OutputID)
}

func TestFindRecentSameCommand_OutOfWindow(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	out := sampleOutput("out_dedup_old")
	out.Command = "[shell] go test ./..."
	out.CreatedAt = time.Now().Add(-2 * time.Hour)
	require.NoError(t, st.Save(ctx, out))

	meta, err := st.FindRecentSameCommand(ctx, "/test/project", "[shell] go test ./...", time.Hour)
	require.NoError(t, err)
	assert.Nil(t, meta, "out-of-window command should not match")
}

func TestFindRecentSameCommand_DifferentProject(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	out := sampleOutput("out_dedup_proj")
	out.Command = "[shell] go test ./..."
	out.ProjectPath = "/other/project"
	require.NoError(t, st.Save(ctx, out))

	meta, err := st.FindRecentSameCommand(ctx, "/test/project", "[shell] go test ./...", time.Hour)
	require.NoError(t, err)
	assert.Nil(t, meta, "different project should not match")
}

func TestFindRecentSameCommand_Normalization(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	out := sampleOutput("out_dedup_norm")
	out.Command = store.NormalizeCommand("[shell] go   test ./...")
	require.NoError(t, st.Save(ctx, out))

	// Query with extra spaces — NormalizeCommand collapses them.
	meta, err := st.FindRecentSameCommand(ctx, "/test/project", "[shell] go   test ./...", time.Hour)
	require.NoError(t, err)
	require.NotNil(t, meta, "whitespace differences should still match after normalisation")
}

func TestNormalizeCommand(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"go test", "go test"},
		{"go   test", "go test"},
		{"  go test ./...  ", "go test ./..."},
		{"flutter   test\t--no-pub", "flutter test --no-pub"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, store.NormalizeCommand(tc.input))
		})
	}
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

func TestGetStats_ToolUsageCounts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	toolEvents := []*store.SessionEvent{
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "posttooluse", ToolName: "ctx_execute", Summary: "ran go test"},
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "posttooluse", ToolName: "ctx_execute", Summary: "ran flutter build"},
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "posttooluse", ToolName: "ctx_read_file", Summary: "read spec.yaml"},
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "posttooluse", ToolName: "runInTerminal", Summary: "ran pwd"},
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "posttooluse", ToolName: "Bash", Summary: "ran ls"},
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "posttooluse", ToolName: "Read", Summary: "read package.json"},
		// pretooluse events should NOT be counted in tool usage adherence.
		{SessionID: "s1", ProjectPath: "/test/project", EventType: "pretooluse", ToolName: "runInTerminal", Summary: "allowed pwd"},
	}
	for _, e := range toolEvents {
		e.CreatedAt = time.Now()
		require.NoError(t, st.SaveSessionEvent(ctx, e))
	}

	stats, err := st.GetStats(ctx, "/test/project", time.Time{})
	require.NoError(t, err)

	assert.Equal(t, 2, stats.CtxExecuteCount, "ctx_execute count")
	assert.Equal(t, 1, stats.CtxReadFileCount, "ctx_read_file count")
	assert.Equal(t, 2, stats.NativeShellCount, "native shell count (runInTerminal + Bash)")
	assert.Equal(t, 1, stats.NativeReadCount, "native read count (Read)")
}

// ── Phase 7: migration v3 + freshness metadata tests ─────────────────────

func TestMigration_v3_FreshnessColumns(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	out := sampleOutput("out_v3_001")
	require.NoError(t, st.Save(ctx, out))

	got, err := st.Get(ctx, "out_v3_001")
	require.NoError(t, err)

	// source_kind should be classified from "[shell] echo hello"
	assert.Equal(t, "shell:echo", got.SourceKind)
	// refreshed_at should be set (≈ created_at at save time)
	assert.False(t, got.RefreshedAt.IsZero())
	assert.WithinDuration(t, got.CreatedAt, got.RefreshedAt, time.Second)
	// ttl_seconds default is 0
	assert.Equal(t, 0, got.TTLSeconds)
}

func TestMigration_v3_BackfillRefreshedAt(t *testing.T) {
	// A fresh store always applies all migrations, so refreshed_at is set from
	// created_at for any row saved with the default (zero) RefreshedAt.
	st := newTestStore(t)
	ctx := context.Background()

	out := sampleOutput("out_backfill_001")
	out.CreatedAt = time.Now().Add(-24 * time.Hour)
	// Don't set RefreshedAt — should default to CreatedAt.
	require.NoError(t, st.Save(ctx, out))

	got, err := st.Get(ctx, "out_backfill_001")
	require.NoError(t, err)
	assert.WithinDuration(t, out.CreatedAt, got.RefreshedAt, time.Second)
}

func TestMigration_v8_TaskColumnAndIndexOnFreshDB(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), ".ctx-saver")
	st, err := store.NewSQLiteStore(dataDir, "/test/project")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	d := &store.Decision{
		ProjectPath: "/test/project",
		Text:        "handoff",
		Task:        "retirement-feature",
		Importance:  store.ImportanceHigh,
	}
	require.NoError(t, st.SaveDecision(ctx, d))
	got, err := st.GetDecision(ctx, d.DecisionID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "retirement-feature", got.Task)

	assertDecisionTaskIndexExists(t, filepath.Join(dataDir, "outputs.db"))
}

func TestMigration_v8_AppliesToExistingV7DB(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), ".ctx-saver")
	require.NoError(t, os.MkdirAll(dataDir, 0700))
	dbFile := filepath.Join(dataDir, "outputs.db")
	db, err := sql.Open("sqlite", dbFile)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE schema_version (version INTEGER NOT NULL PRIMARY KEY)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO schema_version(version) VALUES (7)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE decisions (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		decision_id  TEXT    NOT NULL UNIQUE,
		session_id   TEXT    NOT NULL DEFAULT '',
		project_path TEXT    NOT NULL,
		text         TEXT    NOT NULL,
		tags         TEXT    NOT NULL DEFAULT '',
		links_to     TEXT    NOT NULL DEFAULT '',
		importance   TEXT    NOT NULL DEFAULT 'normal',
		created_at   INTEGER NOT NULL
	)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	st, err := store.NewSQLiteStore(dataDir, "/test/project")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	d := &store.Decision{ProjectPath: "/test/project", Text: "migrated", Task: "retirement-feature"}
	require.NoError(t, st.SaveDecision(ctx, d))
	got, err := st.GetDecision(ctx, d.DecisionID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "retirement-feature", got.Task)

	assertDecisionTaskIndexExists(t, dbFile)
}

func assertDecisionTaskIndexExists(t *testing.T, dbFile string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbFile)
	require.NoError(t, err)
	defer db.Close()

	rows, err := db.Query(`PRAGMA index_list(decisions)`)
	require.NoError(t, err)
	defer rows.Close()

	found := false
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		require.NoError(t, rows.Scan(&seq, &name, &unique, &origin, &partial))
		if name == "idx_decisions_task" {
			found = true
		}
	}
	require.NoError(t, rows.Err())
	assert.True(t, found, "idx_decisions_task should exist")
}

func TestClassifySource(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"[shell] acli page view 123", "shell:acli"},
		{"[shell] kubectl get pods -n prod", "shell:kubectl"},
		{"[shell] git log --oneline", "shell:git"},
		{"[shell] flutter test", "shell:flutter"},
		{"[shell] go build ./...", "shell:go"},
		{"[shell] npm run build", "shell:npm"},
		{"[python] import os", "python"},
		{"[go] package main", "go"},
		{"[shell] ", "shell:other"},
		{"no prefix command", "shell:no"},
		{"", "shell:other"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			assert.Equal(t, tc.want, store.ClassifySource(tc.command))
		})
	}
}

func TestUpdateRefreshed(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	original := sampleOutput("out_refresh_001")
	require.NoError(t, st.Save(ctx, original))

	refreshedAt := time.Now().Add(5 * time.Minute)
	updated := &store.Output{
		OutputID:    "out_refresh_001",
		FullOutput:  "new content line 1\nnew content line 2\n",
		SizeBytes:   40,
		LineCount:   2,
		DurationMs:  99,
		RefreshedAt: refreshedAt,
	}
	require.NoError(t, st.UpdateRefreshed(ctx, updated))

	got, err := st.Get(ctx, "out_refresh_001")
	require.NoError(t, err)
	assert.Equal(t, "new content line 1\nnew content line 2\n", got.FullOutput)
	assert.Equal(t, int64(40), got.SizeBytes)
	assert.Equal(t, int64(99), got.DurationMs)
	assert.WithinDuration(t, refreshedAt, got.RefreshedAt, time.Second)
	// output_id preserved
	assert.Equal(t, "out_refresh_001", got.OutputID)
}
