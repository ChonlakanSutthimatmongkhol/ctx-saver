package handlers_test

import (
	"context"
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
	stats    *store.Stats
	statsErr error
	// capturedSince records the since value passed to GetStats.
	capturedSince time.Time
	mockStore     // embed to satisfy remaining interface methods
}

func (s *statsStore) GetStats(_ context.Context, _ string, since time.Time) (*store.Stats, error) {
	s.capturedSince = since
	return s.stats, s.statsErr
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
}

func TestStatsHandler_Populated(t *testing.T) {
	st := &statsStore{stats: &store.Stats{
		OutputsStored: 5,
		RawBytes:      500_000,
		AvgDurationMs: 120,
		TopCommands:   []store.CommandStat{{Command: "[shell] go test", Count: 3, TotalBytes: 300_000}},
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
	require.Len(t, out.TopCommands, 1)
	assert.Equal(t, "[shell] go test", out.TopCommands[0].Command)
	require.Len(t, out.LargestOutputs, 1)
	assert.Equal(t, "out_abc", out.LargestOutputs[0].OutputID)
	assert.Equal(t, 2, out.HookStats.DangerousBlocked)
	assert.Equal(t, 5, out.HookStats.RedirectedToMCP)
	assert.Equal(t, 10, out.HookStats.EventsCaptured)
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
