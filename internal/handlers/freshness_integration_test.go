package handlers_test

// Integration tests for Phase 7 cache freshness using a real SQLite store.
// These complement the mock-based unit tests by exercising the full stack:
// SQLite migration v3 columns → ClassifySource → Resolver → handler response.

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

// newTestStore creates a real SQLite store in a temp dir and registers cleanup.
func newTestStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.NewSQLiteStore(dir, "/integration-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// saveOutput is a helper that saves an output to the real store and returns its ID.
func saveOutput(t *testing.T, st store.Store, o *store.Output) {
	t.Helper()
	require.NoError(t, st.Save(context.Background(), o))
}

// ── Pre-Phase 7 rows (source_kind = 'unknown') ────────────────────────────

func TestIntegration_UnknownSourceKind_UsesDefaultTTL(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	// Simulate a pre-Phase-7 row: source_kind will be classified from command.
	// Command "[shell] legacy-tool" → ClassifySource → "shell:legacy-tool"
	// No rule for this source → DefaultMaxAgeSeconds used.
	saveOutput(t, st, &store.Output{
		OutputID:    "legacy_001",
		Command:     "[shell] legacy-tool status",
		FullOutput:  "status: ok\n",
		SizeBytes:   11,
		LineCount:   1,
		CreatedAt:   now.Add(-30 * time.Minute),
		RefreshedAt: now.Add(-30 * time.Minute),
	})

	// Re-fetch to get actual stored values (ClassifySource applied by Save).
	out, err := st.Get(context.Background(), "legacy_001")
	require.NoError(t, err)
	assert.Equal(t, "shell:legacy-tool", out.SourceKind, "ClassifySource should extract binary name")

	// With 30-min age and default TTL=3600s → still fresh → use_cache.
	fc := config.FreshnessConfig{
		Enabled:              true,
		DefaultMaxAgeSeconds: 3600,
	}
	h := handlers.NewGetFullHandler(st, "/integration-test").WithFreshness(nil, fc)
	_, resp, err := h.Handle(context.Background(), nil, handlers.GetFullInput{OutputID: "legacy_001"})
	require.NoError(t, err)
	assert.Equal(t, "fresh", resp.Freshness.StaleLevel)
	assert.False(t, resp.UserConfirmationRequired)
}

// ── Clock skew: refreshed_at slightly in the future ───────────────────────

func TestIntegration_ClockSkew_ShowsJustNow(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	saveOutput(t, st, &store.Output{
		OutputID:    "skew_001",
		Command:     "[shell] date",
		FullOutput:  "Sun Apr 26 12:00:00 UTC 2026\n",
		SizeBytes:   30,
		LineCount:   1,
		CreatedAt:   now,
		RefreshedAt: now.Add(2 * time.Minute), // slightly in the future
	})

	out, err := st.Get(context.Background(), "skew_001")
	require.NoError(t, err)

	// Manually verify freshness info for a future refreshed_at.
	// age should clamp to 0, yielding "just now" and "fresh".
	h := handlers.NewGetFullHandler(st, "/integration-test")
	_, resp, err := h.Handle(context.Background(), nil, handlers.GetFullInput{OutputID: "skew_001"})
	require.NoError(t, err)
	_ = out
	assert.Equal(t, "fresh", resp.Freshness.StaleLevel)
	assert.Equal(t, "just now", resp.Freshness.AgeHuman)
}

// ── ConfirmThreshold wins over AutoRefresh ────────────────────────────────

func TestIntegration_ConfirmThreshold_WinsOverAutoRefresh(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	saveOutput(t, st, &store.Output{
		OutputID:    "threshold_001",
		Command:     "[shell] kubectl get pods",
		FullOutput:  "pod/app-1 Running\n",
		SizeBytes:   18,
		LineCount:   1,
		CreatedAt:   now.Add(-8 * 24 * time.Hour),
		RefreshedAt: now.Add(-8 * 24 * time.Hour), // 8 days old
	})

	// kubectl has AutoRefresh=true and TTL=60s → would be auto_refresh,
	// but 8 days > confirm_threshold (7 days) → ask_user wins first.
	fc := config.FreshnessConfig{
		Enabled:                     true,
		DefaultMaxAgeSeconds:        3600,
		UserConfirmThresholdSeconds: 604800, // 7 days
		Sources: map[string]config.FreshnessRule{
			"shell:kubectl": {MaxAgeSeconds: 60, AutoRefresh: true},
		},
	}
	h := handlers.NewGetFullHandler(st, "/integration-test").WithFreshness(nil, fc)
	_, resp, err := h.Handle(context.Background(), nil, handlers.GetFullInput{OutputID: "threshold_001"})
	require.NoError(t, err)

	assert.True(t, resp.UserConfirmationRequired, "confirm threshold must win over auto_refresh")
	assert.NotEmpty(t, resp.UserConfirmationPrompt)
	assert.Equal(t, "critical", resp.Freshness.StaleLevel)
}

// ── TTL=0 → fall back to DefaultMaxAgeSeconds ─────────────────────────────

func TestIntegration_TTLZero_UsesDefault(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	saveOutput(t, st, &store.Output{
		OutputID:    "ttl0_001",
		Command:     "[shell] go test ./...",
		FullOutput:  "ok  foo/bar\n",
		SizeBytes:   11,
		LineCount:   1,
		CreatedAt:   now.Add(-90 * time.Minute), // 90 min old
		RefreshedAt: now.Add(-90 * time.Minute),
		TTLSeconds:  0, // explicit zero → use default
	})

	// Default TTL=3600 → age 90min < 3600s → fresh.
	fc := config.FreshnessConfig{
		Enabled:              true,
		DefaultMaxAgeSeconds: 3600,
	}
	h := handlers.NewGetFullHandler(st, "/integration-test").WithFreshness(nil, fc)
	_, resp, err := h.Handle(context.Background(), nil, handlers.GetFullInput{OutputID: "ttl0_001"})
	require.NoError(t, err)
	// 90min = 5400s > 3600s default TTL → no rule for shell:go → use_cache (stale but no auto-refresh)
	assert.Equal(t, "aging", resp.Freshness.StaleLevel)
	assert.False(t, resp.UserConfirmationRequired)
}

// ── Freshness disabled → always use_cache, no confirmation ────────────────

func TestIntegration_FreshnessDisabled_AlwaysUseCache(t *testing.T) {
	st := newTestStore(t)
	now := time.Now()

	saveOutput(t, st, &store.Output{
		OutputID:    "disabled_001",
		Command:     "[shell] acli page view 1",
		FullOutput:  "very old content\n",
		SizeBytes:   17,
		LineCount:   1,
		CreatedAt:   now.Add(-30 * 24 * time.Hour),
		RefreshedAt: now.Add(-30 * 24 * time.Hour), // 30 days old
	})

	fc := config.FreshnessConfig{Enabled: false}
	h := handlers.NewGetFullHandler(st, "/integration-test").WithFreshness(nil, fc)
	_, resp, err := h.Handle(context.Background(), nil, handlers.GetFullInput{OutputID: "disabled_001"})
	require.NoError(t, err)

	// Freshness info still reflects real age, but no confirmation gate triggers.
	assert.False(t, resp.UserConfirmationRequired, "disabled freshness must never require confirmation")
	assert.Equal(t, "critical", resp.Freshness.StaleLevel) // age info still accurate
}
