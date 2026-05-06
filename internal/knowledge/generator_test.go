package knowledge_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/knowledge"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// stubStore returns a fixed KnowledgeData with the given session count.
type stubStore struct {
	store.Store          // embed to satisfy interface (panics on unimplemented calls)
	sessionCount int
}

func (s *stubStore) KnowledgeStats(_ context.Context, _ string) (*store.KnowledgeData, error) {
	return &store.KnowledgeData{
		SessionCount:  s.sessionCount,
		OutputCount:   10,
		DecisionCount: 1,
		TopCommands: []store.CommandFreq{
			{Command: "[shell] go test ./...", RunCount: 5, AvgBytes: 2048},
		},
	}, nil
}

func testCfg(minSessions int) *config.Config {
	cfg := config.Default()
	cfg.Knowledge.MinSessions = minSessions
	return cfg
}

// T1: refresh below threshold → ErrThresholdNotMet.
func TestRefresh_BelowThreshold(t *testing.T) {
	dir := t.TempDir()
	st := &stubStore{sessionCount: 2}
	err := knowledge.Refresh(context.Background(), st, dir, testCfg(3))
	require.Error(t, err)
	assert.True(t, errors.Is(err, knowledge.ErrThresholdNotMet), "want ErrThresholdNotMet, got %v", err)
	// File must NOT be written.
	_, statErr := os.Stat(filepath.Join(dir, ".ctx-saver", "project-knowledge.md"))
	assert.True(t, os.IsNotExist(statErr), "file should not have been created")
}

// T2: refresh above threshold → file written with correct sections.
func TestRefresh_AboveThreshold(t *testing.T) {
	dir := t.TempDir()
	st := &stubStore{sessionCount: 5}
	err := knowledge.Refresh(context.Background(), st, dir, testCfg(3))
	require.NoError(t, err)

	knowledgePath := filepath.Join(dir, ".ctx-saver", "project-knowledge.md")
	data, readErr := os.ReadFile(knowledgePath)
	require.NoError(t, readErr)
	content := string(data)

	assert.Contains(t, content, "# Project Knowledge")
	assert.Contains(t, content, "## Most-read files")
	assert.Contains(t, content, "## Most-run commands")
	assert.Contains(t, content, "## Common sequences")
	assert.Contains(t, content, "## High-importance decisions")
	assert.Contains(t, content, "## Session patterns")
	assert.Contains(t, content, "[shell] go test ./...")
}

// T3: Show → stdout only, no file written.
func TestShow_NoFileWritten(t *testing.T) {
	dir := t.TempDir()
	st := &stubStore{sessionCount: 5}
	var buf strings.Builder
	err := knowledge.Show(context.Background(), st, dir, testCfg(3), &buf)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "# Project Knowledge")

	// No file must exist.
	_, statErr := os.Stat(filepath.Join(dir, ".ctx-saver", "project-knowledge.md"))
	assert.True(t, os.IsNotExist(statErr), "Show must not create any file")
}

// T3b: Show below threshold → ErrThresholdNotMet.
func TestShow_BelowThreshold(t *testing.T) {
	dir := t.TempDir()
	st := &stubStore{sessionCount: 1}
	var buf strings.Builder
	err := knowledge.Show(context.Background(), st, dir, testCfg(3), &buf)
	require.Error(t, err)
	assert.True(t, errors.Is(err, knowledge.ErrThresholdNotMet))
}
