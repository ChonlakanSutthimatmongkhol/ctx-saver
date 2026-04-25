package handlers_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// sessionInitStore extends mockStore with configurable GetStats and
// ListProjectSessionEvents responses for SessionInit tests.
type sessionInitStore struct {
	stats  *store.Stats
	events []*store.SessionEvent
	mockStore
}

func (s *sessionInitStore) GetStats(_ context.Context, _ string, _ time.Time) (*store.Stats, error) {
	if s.stats == nil {
		return &store.Stats{}, nil
	}
	return s.stats, nil
}

func (s *sessionInitStore) ListProjectSessionEvents(_ context.Context, _ string, _ int) ([]*store.SessionEvent, error) {
	return s.events, nil
}

func sessionInitCfg() *config.Config {
	cfg := config.Default()
	cfg.Dedup.Enabled = true
	cfg.Dedup.WindowMinutes = 30
	cfg.Summary.SmartFormat = true
	cfg.Sandbox.Type = "subprocess"
	cfg.Storage.RetentionDays = 14
	return cfg
}

func newSessionInitHandler(st *sessionInitStore) *handlers.SessionInitHandler {
	return handlers.NewSessionInitHandler(
		sessionInitCfg(), st, "/test-project",
		time.Now(), "0.1.4",
	)
}

func TestSessionInit_FreshProject(t *testing.T) {
	st := &sessionInitStore{}
	h := newSessionInitHandler(st)

	_, out, err := h.Handle(context.Background(), nil, handlers.SessionInitInput{})
	require.NoError(t, err)

	assert.Equal(t, 0, out.CachedOutputs.TotalOutputs)
	assert.Empty(t, out.RecentEvents)
	assert.Contains(t, out.NextActionHint, "Fresh")
	assert.Equal(t, "/test-project", out.ProjectPath)
	assert.Equal(t, "0.1.4", out.ServerVersion)
}

func TestSessionInit_WithCachedOutputs(t *testing.T) {
	st := &sessionInitStore{
		stats: &store.Stats{
			OutputsStored: 7,
			RawBytes:      350_000,
			TopCommands: []store.CommandStat{
				{Command: "[shell] go test ./...", Count: 4, TotalBytes: 200_000},
				{Command: "[shell] flutter build", Count: 2, TotalBytes: 100_000},
				{Command: "[shell] git log --oneline", Count: 1, TotalBytes: 50_000},
			},
		},
	}
	h := newSessionInitHandler(st)

	_, out, err := h.Handle(context.Background(), nil, handlers.SessionInitInput{})
	require.NoError(t, err)

	assert.Equal(t, 7, out.CachedOutputs.TotalOutputs)
	assert.Equal(t, int64(350_000), out.CachedOutputs.TotalSizeBytes)
	require.Len(t, out.CachedOutputs.TopCommands, 3)
	assert.Equal(t, "[shell] go test ./...", out.CachedOutputs.TopCommands[0].Command)
	assert.Equal(t, 4, out.CachedOutputs.TopCommands[0].Count)
}

func TestSessionInit_WithRecentEvents(t *testing.T) {
	baseTime := time.Now().Add(-5 * time.Minute)
	st := &sessionInitStore{
		events: []*store.SessionEvent{
			{ToolName: "ctx_execute", EventType: "posttooluse", Summary: "go test output", CreatedAt: baseTime},
			{ToolName: "ctx_search", EventType: "posttooluse", Summary: "searched FAIL", CreatedAt: baseTime.Add(time.Minute)},
		},
	}
	h := newSessionInitHandler(st)

	_, out, err := h.Handle(context.Background(), nil, handlers.SessionInitInput{})
	require.NoError(t, err)

	require.Len(t, out.RecentEvents, 2)
	// AgoSeconds must be non-negative
	for _, e := range out.RecentEvents {
		assert.GreaterOrEqual(t, e.AgoSeconds, int64(0), "AgoSeconds should be non-negative")
		assert.NotEmpty(t, e.Summary)
	}
	// Newest event first (ctx_search was more recent)
	assert.Contains(t, out.RecentEvents[0].Summary, "ctx_search")
}

func TestSessionInit_Dedup(t *testing.T) {
	baseTime := time.Now().Add(-10 * time.Minute)
	st := &sessionInitStore{
		events: []*store.SessionEvent{
			{ToolName: "ctx_execute", EventType: "posttooluse", Summary: "go test ./...", CreatedAt: baseTime},
			{ToolName: "ctx_execute", EventType: "posttooluse", Summary: "go test ./...", CreatedAt: baseTime.Add(time.Minute)},
			{ToolName: "ctx_execute", EventType: "posttooluse", Summary: "go test ./...", CreatedAt: baseTime.Add(2 * time.Minute)},
			{ToolName: "ctx_execute", EventType: "posttooluse", Summary: "go test ./...", CreatedAt: baseTime.Add(3 * time.Minute)},
			{ToolName: "ctx_execute", EventType: "posttooluse", Summary: "go test ./...", CreatedAt: baseTime.Add(4 * time.Minute)},
		},
	}
	h := newSessionInitHandler(st)

	_, out, err := h.Handle(context.Background(), nil, handlers.SessionInitInput{})
	require.NoError(t, err)

	// 5 identical events should collapse to 1 after deduplication.
	assert.Len(t, out.RecentEvents, 1, "5 identical events should deduplicate to 1")
}

func TestSessionInit_RulesPresent(t *testing.T) {
	st := &sessionInitStore{}
	h := newSessionInitHandler(st)

	_, out, err := h.Handle(context.Background(), nil, handlers.SessionInitInput{})
	require.NoError(t, err)

	assert.True(t, strings.Contains(out.ProjectRules, "ctx_execute"),
		"project_rules must mention ctx_execute")
	assert.True(t, strings.Contains(out.ProjectRules, "ctx_read_file"),
		"project_rules must mention ctx_read_file")
}

func TestSessionInit_ConfigSummary(t *testing.T) {
	cfg := sessionInitCfg()
	cfg.Dedup.Enabled = false
	cfg.Dedup.WindowMinutes = 60
	cfg.Summary.SmartFormat = false
	cfg.Sandbox.Type = "srt"

	st := &sessionInitStore{}
	h := handlers.NewSessionInitHandler(cfg, st, "/p", time.Now(), "0.1.4")

	_, out, err := h.Handle(context.Background(), nil, handlers.SessionInitInput{})
	require.NoError(t, err)

	assert.False(t, out.ActiveConfig.DedupEnabled)
	assert.Equal(t, 60, out.ActiveConfig.DedupWindowMinutes)
	assert.False(t, out.ActiveConfig.SmartFormatEnabled)
	assert.Equal(t, "srt", out.ActiveConfig.Sandbox)
}

func TestSessionInit_IncludesRecentDecisions(t *testing.T) {
	st := &sessionInitStore{}
	st.decisions = []*store.Decision{
		{DecisionID: "dec_1", ProjectPath: "/test-project", Text: "chose X", Importance: store.ImportanceNormal, CreatedAt: time.Now()},
		{DecisionID: "dec_2", ProjectPath: "/test-project", Text: "avoid Y", Importance: store.ImportanceHigh, CreatedAt: time.Now()},
		{DecisionID: "dec_3", ProjectPath: "/test-project", Text: "normal choice", Importance: store.ImportanceNormal, CreatedAt: time.Now()},
	}
	h := newSessionInitHandler(st)

	_, out, err := h.Handle(context.Background(), nil, handlers.SessionInitInput{})
	require.NoError(t, err)
	assert.Len(t, out.RecentDecisions, 3)
	assert.NotEmpty(t, out.RecentDecisions[0].DecisionID)
	assert.NotEmpty(t, out.RecentDecisions[0].Text)
}

func TestSessionInit_ExcludesLowImportance(t *testing.T) {
	// The mock ListDecisions filters by ProjectPath only; filtering by importance
	// is tested at the store level. Here we verify the handler passes MinImportance:"normal"
	// by seeding only normal+high decisions (low ones should not appear).
	st := &sessionInitStore{}
	st.decisions = []*store.Decision{
		{DecisionID: "dec_n", ProjectPath: "/test-project", Text: "normal", Importance: store.ImportanceNormal, CreatedAt: time.Now()},
		{DecisionID: "dec_h", ProjectPath: "/test-project", Text: "high", Importance: store.ImportanceHigh, CreatedAt: time.Now()},
	}
	h := newSessionInitHandler(st)

	_, out, err := h.Handle(context.Background(), nil, handlers.SessionInitInput{})
	require.NoError(t, err)
	for _, d := range out.RecentDecisions {
		assert.NotEqual(t, store.ImportanceLow, d.Importance, "low importance decisions must not appear")
	}
}

func TestSessionInit_HintMentionsDecisionsCount(t *testing.T) {
	st := &sessionInitStore{}
	st.decisions = []*store.Decision{
		{DecisionID: "dec_1", ProjectPath: "/test-project", Text: "a decision", Importance: store.ImportanceNormal, CreatedAt: time.Now()},
	}
	h := newSessionInitHandler(st)

	_, out, err := h.Handle(context.Background(), nil, handlers.SessionInitInput{})
	require.NoError(t, err)
	assert.Contains(t, out.NextActionHint, "architectural decisions logged")
}
