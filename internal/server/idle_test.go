package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// idleStore is a test-only store stub for idle goroutine tests.
// It reports a fixed last-event time and session count.
type idleStore struct {
	store.Store // embed to satisfy interface; panics on unimplemented calls
	lastEvent    time.Time
	sessionCount int
	refreshCalls int
}

func (s *idleStore) LastEventTime(_ context.Context, _ string) (time.Time, error) {
	return s.lastEvent, nil
}
func (s *idleStore) LastKnowledgeRefresh(_ context.Context, _ string) (time.Time, error) {
	return time.Time{}, nil // never refreshed
}
func (s *idleStore) SessionCountSince(_ context.Context, _ string, _ time.Time) (int, error) {
	return s.sessionCount, nil
}
func (s *idleStore) KnowledgeStats(_ context.Context, _ string) (*store.KnowledgeData, error) {
	s.refreshCalls++
	return &store.KnowledgeData{
		SessionCount:  s.sessionCount,
		OutputCount:   10,
		DecisionCount: 1,
		TopCommands: []store.CommandFreq{
			{Command: "[shell] go test", RunCount: 3, AvgBytes: 1024},
		},
	}, nil
}

func withFastTick(t *testing.T) {
	t.Helper()
	old := idleCheckInterval
	idleCheckInterval = 10 * time.Millisecond
	t.Cleanup(func() { idleCheckInterval = old })
}

func testKnowledgeCfg(minSessions, idleMinutes int) *config.Config {
	cfg := config.Default()
	cfg.Knowledge.MinSessions = minSessions
	cfg.Knowledge.IdleMinutes = idleMinutes
	return cfg
}

// T9: idle goroutine does NOT refresh when idle < threshold.
func TestRunIdleKnowledgeRefresh_NoRefreshWhenNotIdle(t *testing.T) {
	withFastTick(t)

	dir := t.TempDir()
	st := &idleStore{
		lastEvent:    time.Now(), // just now — not idle
		sessionCount: 5,
	}
	cfg := testKnowledgeCfg(3, 30)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go RunIdleKnowledgeRefresh(ctx, st, dir, cfg)
	<-ctx.Done()

	knowledgePath := filepath.Join(dir, ".ctx-saver", "project-knowledge.md")
	if _, err := os.Stat(knowledgePath); !os.IsNotExist(err) {
		t.Error("knowledge.md should NOT have been created when system is not idle")
	}
}

// T10: idle goroutine refreshes when idle >= threshold AND new_sessions >= min.
func TestRunIdleKnowledgeRefresh_RefreshesWhenIdleAndEnoughSessions(t *testing.T) {
	withFastTick(t)

	dir := t.TempDir()
	st := &idleStore{
		lastEvent:    time.Now().Add(-60 * time.Minute), // idle for 60 min > 30 min threshold
		sessionCount: 5,                                  // 5 >= min_sessions(3)
	}
	cfg := testKnowledgeCfg(3, 30)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go RunIdleKnowledgeRefresh(ctx, st, dir, cfg)
	<-ctx.Done()

	knowledgePath := filepath.Join(dir, ".ctx-saver", "project-knowledge.md")
	if _, err := os.Stat(knowledgePath); os.IsNotExist(err) {
		t.Error("knowledge.md should have been created when idle >= threshold and sessions >= min")
	}
}

// T10b: idle goroutine does NOT refresh when sessions < min.
func TestRunIdleKnowledgeRefresh_NoRefreshWhenTooFewSessions(t *testing.T) {
	withFastTick(t)

	dir := t.TempDir()
	st := &idleStore{
		lastEvent:    time.Now().Add(-60 * time.Minute), // idle for 60 min
		sessionCount: 1,                                  // 1 < min_sessions(3)
	}
	cfg := testKnowledgeCfg(3, 30)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go RunIdleKnowledgeRefresh(ctx, st, dir, cfg)
	<-ctx.Done()

	knowledgePath := filepath.Join(dir, ".ctx-saver", "project-knowledge.md")
	if _, err := os.Stat(knowledgePath); !os.IsNotExist(err) {
		t.Error("knowledge.md should NOT have been created when sessions < min")
	}
}
