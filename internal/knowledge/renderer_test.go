package knowledge

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// fixedNow is the reference time used for all renderer tests.
var fixedNow = time.Date(2026, 5, 6, 23, 0, 0, 0, time.UTC)

func withFixedNow(t *testing.T) {
	t.Helper()
	old := nowFn
	nowFn = func() time.Time { return fixedNow }
	t.Cleanup(func() { nowFn = old })
}

// T6: renderer output matches golden file.
func TestRender_GoldenFile(t *testing.T) {
	withFixedNow(t)

	// Fixed dates relative to fixedNow (2026-05-06):
	//   internal/service.go: stable, last changed 14 days ago → 2026-04-22
	//   go.mod: changing (HashStable=false)
	data := &store.KnowledgeData{
		SessionCount:  5,
		OutputCount:   42,
		DecisionCount: 3,
		TopFiles: []store.FileFreq{
			{
				Path:        "internal/service.go",
				ReadCount:   8,
				HashStable:  true,
				LastChanged: fixedNow.Add(-14 * 24 * time.Hour),
			},
			{
				Path:        "go.mod",
				ReadCount:   3,
				HashStable:  false,
				LastChanged: fixedNow.Add(-1 * time.Hour),
			},
		},
		TopCommands: []store.CommandFreq{
			{Command: "[shell] go test ./...", RunCount: 12, AvgBytes: 4096},
			{Command: "[shell] make build", RunCount: 7, AvgBytes: 512},
		},
		Sequences: []store.CmdSequence{
			{First: "[shell] make build", Second: "[shell] go test ./...", Percent: 85.7},
		},
		KeyDecisions: []store.DecisionOut{
			{
				Text:      "Use CGO_ENABLED=0 for portability",
				Tags:      []string{"build"},
				CreatedAt: fixedNow.Add(-24 * time.Hour),
			},
			{
				Text:      "Resume from parser validation",
				Tags:      []string{"handoff"},
				Task:      "retirement-feature",
				CreatedAt: fixedNow.Add(-2 * time.Hour),
			},
		},
	}

	got := Render(data)

	golden, err := os.ReadFile("../../tests/testdata/knowledge/sample_knowledge.md")
	require.NoError(t, err)

	require.Equal(t, strings.TrimRight(string(golden), "\n"), strings.TrimRight(got, "\n"))
}

func TestRender_EmptySections(t *testing.T) {
	withFixedNow(t)

	data := &store.KnowledgeData{
		SessionCount:  3,
		OutputCount:   5,
		DecisionCount: 0,
	}

	got := Render(data)

	require.Contains(t, got, "_No file reads recorded yet._")
	require.Contains(t, got, "_No commands recorded yet._")
	require.Contains(t, got, "_Not enough data to detect patterns yet._")
	require.Contains(t, got, "_No high-importance decisions recorded yet._")
	require.Contains(t, got, "Total sessions: 3")
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{4096, "4.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{2 * 1024 * 1024, "2.0 MB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
