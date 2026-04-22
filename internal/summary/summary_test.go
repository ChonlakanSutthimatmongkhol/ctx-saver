package summary_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/chonlakan/ctx-saver/internal/summary"
)

func TestSummarize_EmptyInput(t *testing.T) {
	r := summary.Summarize([]byte{}, 20, 5)
	assert.Equal(t, "(empty output)", r.Text)
	assert.Equal(t, 0, r.TotalLines)
	assert.Equal(t, 0, r.TotalBytes)
	assert.False(t, r.Truncated)
}

func TestSummarize_SmallInput(t *testing.T) {
	input := "line1\nline2\nline3\n"
	r := summary.Summarize([]byte(input), 20, 5)
	assert.False(t, r.Truncated)
	assert.Equal(t, 3, r.TotalLines)
	assert.Equal(t, len(input), r.TotalBytes)
}

func TestSummarize_LargeInput_TruncatesCorrectly(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, strings.Repeat("x", 80))
	}
	input := strings.Join(lines, "\n") + "\n"

	r := summary.Summarize([]byte(input), 10, 3)

	assert.True(t, r.Truncated)
	assert.Equal(t, 100, r.TotalLines)
	assert.Contains(t, r.Text, "87 lines omitted")

	// Verify head lines are present.
	assert.Equal(t, 10, strings.Count(strings.Split(r.Text, "\n... (")[0], "\n"))

	// Verify tail lines are present.
	assert.Contains(t, r.Text, lines[97])
	assert.Contains(t, r.Text, lines[99])
}

func TestSummarize_ExactBoundary(t *testing.T) {
	// Exactly headLines + tailLines — should NOT truncate.
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = "line"
	}
	r := summary.Summarize([]byte(strings.Join(lines, "\n")), 20, 5)
	assert.False(t, r.Truncated)
}

func TestFormatStats(t *testing.T) {
	s := summary.FormatStats(1247, 52341, 0, 234)
	assert.Contains(t, s, "lines=1247")
	assert.Contains(t, s, "size=52341B")
	assert.Contains(t, s, "exit=0")
	assert.Contains(t, s, "duration=234ms")
}
