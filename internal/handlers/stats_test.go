package handlers_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// statsStore is a minimal store.Store that returns a fixed *store.Stats.
type statsStore struct {
	stats        *store.Stats
	statsErr     error
	adherence    *store.AdherenceStats
	adherenceErr error
	// capturedSince records the since value passed to GetStats.
	capturedSince     time.Time
	capturedThreshold int
	mockStore         // embed to satisfy remaining interface methods
}

func (s *statsStore) GetStats(_ context.Context, _ string, since time.Time) (*store.Stats, error) {
	s.capturedSince = since
	return s.stats, s.statsErr
}

func (s *statsStore) GetAdherenceStats(_ context.Context, _ string, since time.Time, threshold int) (*store.AdherenceStats, error) {
	s.capturedSince = since
	s.capturedThreshold = threshold
	if s.adherence == nil {
		s.adherence = &store.AdherenceStats{}
	}
	return s.adherence, s.adherenceErr
}

func statsCfg() *config.Config {
	cfg := config.Default()
	cfg.Summary.HeadLines = 20
	cfg.Summary.TailLines = 5
	return cfg
}

func TestStatsHandler_Empty(t *testing.T) {
	st := &statsStore{stats: &store.Stats{}}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{})
	require.NoError(t, err)
	assert.Equal(t, "session", out.Scope)
	assert.Equal(t, 0, out.OutputsStored)
	assert.Equal(t, float64(0), out.SavingPercent)
	assert.Contains(t, out.SavingsNote, "normal")
}

func TestStatsHandler_AllScalarMetricFieldsPresent(t *testing.T) {
	for _, scope := range []string{"session", "today", "7d", "all"} {
		t.Run(scope, func(t *testing.T) {
			st := &statsStore{stats: &store.Stats{}}
			h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
			_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{Scope: scope})
			require.NoError(t, err)

			raw, err := json.Marshal(out)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &fields))

			for _, key := range []string{
				"scope", "outputs_stored", "raw_bytes", "estimated_summary_bytes",
				"estimated_tokens_saved", "saving_percent", "raw_tokens",
				"response_tokens", "tokens_saved", "token_saving_percent",
				"tokenizer", "tokenized_outputs", "untokenized_outputs",
				"avg_duration_ms", "hook_stats", "adherence_score",
				"native_shell_count", "native_read_count", "ctx_execute_count",
				"ctx_read_file_count", "missed_large_outputs",
				"missed_large_bytes", "sanctioned_reads",
			} {
				assert.Contains(t, fields, key)
			}
			assert.Contains(t, fields, "savings_note")
			assert.NotContains(t, fields, "adherence_note")
			assert.NotContains(t, fields, "top_commands")
			assert.NotContains(t, fields, "largest_outputs")
			assert.NotContains(t, fields, "outputs")
		})
	}
}

func TestStatsHandler_Populated(t *testing.T) {
	st := &statsStore{stats: &store.Stats{
		OutputsStored:      5,
		RawBytes:           500_000,
		RawTokens:          100_000,
		ResponseTokens:     15_000,
		ResponseBytes:      60_000,
		Tokenizer:          "o200k_base",
		TokenizedOutputs:   4,
		UntokenizedOutputs: 1,
		AvgDurationMs:      120,
		TopCommands:        []store.CommandStat{{Command: "[shell] go test", Count: 3, TotalBytes: 300_000}},
		LargestOutputs: []*store.OutputMeta{
			{OutputID: "out_abc", Command: "[shell] go test", SizeBytes: 100_000, LineCount: 2000},
		},
		DangerousBlocked: 2,
		RedirectedToMCP:  5,
		EventsCaptured:   10,
	}}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{Scope: "all"})
	require.NoError(t, err)
	assert.Equal(t, "all", out.Scope)
	assert.Equal(t, 5, out.OutputsStored)
	assert.Greater(t, out.SavingPercent, float64(0))
	assert.Equal(t, int64(85_000), out.TokensSaved)
	assert.InDelta(t, 85.0, out.TokenSavingPercent, 0.01)
	assert.Equal(t, "o200k_base", out.Tokenizer)
	assert.Equal(t, 4, out.TokenizedOutputs)
	assert.Equal(t, 1, out.UntokenizedOutputs)
	require.Len(t, out.TopCommands, 1)
	assert.Equal(t, "[shell] go test", out.TopCommands[0].Command)
	require.Len(t, out.LargestOutputs, 1)
	assert.Equal(t, "out_abc", out.LargestOutputs[0].OutputID)
	assert.Equal(t, 2, out.HookStats.DangerousBlocked)
	assert.Equal(t, 5, out.HookStats.RedirectedToMCP)
	assert.Equal(t, 10, out.HookStats.EventsCaptured)
}

func TestAdherenceScore_BackwardCompatible(t *testing.T) {
	st := &statsStore{
		stats: &store.Stats{},
		adherence: &store.AdherenceStats{
			CtxExecuteCount:  9,
			NativeShellCount: 1,
		},
	}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{})
	require.NoError(t, err)
	assert.InDelta(t, 90.0, out.AdherenceScore, 0.01)
	assert.Contains(t, out.AdherenceNote, "Excellent")
}

func TestAdherenceNote_EditHeavySessionHealthy(t *testing.T) {
	st := &statsStore{
		stats:     &store.Stats{},
		adherence: &store.AdherenceStats{CtxExecuteCount: 2, NativeShellCount: 8},
	}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{})
	require.NoError(t, err)
	assert.InDelta(t, 20.0, out.AdherenceScore, 0.01)
	assert.Contains(t, out.AdherenceNote, "Healthy")
	assert.NotContains(t, out.AdherenceNote, "session may fail early")
}

func TestAdherenceNote_MissedLargeSeverity(t *testing.T) {
	cases := []struct {
		missed int
		bytes  int64
		sub    string
	}{
		{1, 6 * 1024, "⚠️"},
		{2, 12 * 1024, "⚠️"},
		{3, 18 * 1024, "🚨"},
	}
	for _, c := range cases {
		st := &statsStore{
			stats: &store.Stats{},
			adherence: &store.AdherenceStats{
				NativeShellCount:   c.missed,
				MissedLargeOutputs: c.missed,
				MissedLargeBytes:   c.bytes,
			},
		}
		h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
		_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{})
		require.NoError(t, err)
		assert.Contains(t, out.AdherenceNote, c.sub)
		assert.Equal(t, c.missed, out.MissedLargeOutputs)
		assert.Equal(t, c.bytes, out.MissedLargeBytes)
	}
}

func TestAdherenceScore_NoData(t *testing.T) {
	st := &statsStore{stats: &store.Stats{}}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{})
	require.NoError(t, err)
	assert.Equal(t, float64(0), out.AdherenceScore)
	assert.Empty(t, out.AdherenceNote, "no note when no tool events")
}

func TestStatsHandler_ReportsSanctionedReadsAndThreshold(t *testing.T) {
	cfg := statsCfg()
	st := &statsStore{
		stats:     &store.Stats{OutputsStored: 1},
		adherence: &store.AdherenceStats{SanctionedReads: 2},
	}
	h := handlers.NewStatsHandler(cfg, st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{})
	require.NoError(t, err)
	assert.Equal(t, 2, out.SanctionedReads)
	assert.Equal(t, cfg.Summary.AutoIndexThresholdBytes, st.capturedThreshold)
	assert.Contains(t, out.SavingsNote, "predate exact token accounting")
}

func TestStatsHandler_InvalidScope(t *testing.T) {
	st := &statsStore{stats: &store.Stats{}}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, _, err := h.Handle(context.Background(), nil, handlers.StatsInput{Scope: "week"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid scope")
}

func TestStatsHandler_ScopePassthrough(t *testing.T) {
	serverStart := time.Now().Add(-5 * time.Minute)
	st := &statsStore{stats: &store.Stats{}}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", serverStart)

	// "session" scope → since == serverStart
	_, _, err := h.Handle(context.Background(), nil, handlers.StatsInput{Scope: "session"})
	require.NoError(t, err)
	assert.WithinDuration(t, serverStart, st.capturedSince, time.Second)

	// "all" scope → since is zero
	_, _, err = h.Handle(context.Background(), nil, handlers.StatsInput{Scope: "all"})
	require.NoError(t, err)
	assert.True(t, st.capturedSince.IsZero())
}

// M2 dispatch tests

func TestStatsHandler_ViewOmitted_Stats(t *testing.T) {
	st := &statsStore{stats: &store.Stats{}}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{})
	require.NoError(t, err)
	assert.Equal(t, "stats", out.View)
}

func TestStatsHandler_ViewStats_Stats(t *testing.T) {
	st := &statsStore{stats: &store.Stats{}}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{View: "stats"})
	require.NoError(t, err)
	assert.Equal(t, "stats", out.View)
}

func TestStatsHandler_ViewOutputs_ListsOutputs(t *testing.T) {
	now := time.Now().UTC()
	st := &mockStore{
		listMeta: []*store.OutputMeta{
			{OutputID: "out_x", Command: "go build", CreatedAt: now, SizeBytes: 2048, LineCount: 50},
		},
	}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{View: "outputs"})
	require.NoError(t, err)
	assert.Equal(t, "outputs", out.View)
	require.Len(t, out.Outputs, 1)
	assert.Equal(t, "out_x", out.Outputs[0].OutputID)
}

func TestStatsHandler_ViewOutputs_RespectsLimit(t *testing.T) {
	st := &mockStore{}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, out, err := h.Handle(context.Background(), nil, handlers.StatsInput{View: "outputs", Limit: 5})
	require.NoError(t, err)
	assert.Equal(t, "outputs", out.View)
	assert.Empty(t, out.Outputs)
}

func TestStatsHandler_ViewBogus_Error(t *testing.T) {
	st := &statsStore{stats: &store.Stats{}}
	h := handlers.NewStatsHandler(statsCfg(), st, "/proj", time.Now())
	_, _, err := h.Handle(context.Background(), nil, handlers.StatsInput{View: "bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown view")
	assert.Contains(t, err.Error(), "bogus")
}
