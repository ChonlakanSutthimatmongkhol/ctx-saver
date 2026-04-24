package summary_test

import (
	"strings"
	"testing"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lines(s string) []string {
	return strings.Split(s, "\n")
}

func TestExtractHeadings_Markdown(t *testing.T) {
	input := lines("# H1\n## H2\n### H3\n#### H4\nsome text")
	heads := summary.ExtractHeadings(input)
	require.Len(t, heads, 4)
	assert.Equal(t, 1, heads[0].Level)
	assert.Equal(t, "H1", heads[0].Text)
	assert.Equal(t, 2, heads[1].Level)
	assert.Equal(t, "H2", heads[1].Text)
	assert.Equal(t, 3, heads[2].Level)
	assert.Equal(t, 4, heads[3].Level)
}

func TestExtractHeadings_Setext(t *testing.T) {
	input := lines("Title\n=====\nSubtitle\n--------\nnormal")
	heads := summary.ExtractHeadings(input)
	require.Len(t, heads, 2)
	assert.Equal(t, 1, heads[0].Level)
	assert.Equal(t, "Title", heads[0].Text)
	assert.Equal(t, 2, heads[1].Level)
	assert.Equal(t, "Subtitle", heads[1].Text)
}

func TestExtractHeadings_Mixed(t *testing.T) {
	input := lines("# ATX\nSetext\n======\n## ATX2\nnormal")
	heads := summary.ExtractHeadings(input)
	require.Len(t, heads, 3)
	assert.Equal(t, 1, heads[0].Level) // # ATX
	assert.Equal(t, 1, heads[1].Level) // Setext ===
	assert.Equal(t, 2, heads[2].Level) // ## ATX2
}

func TestExtractHeadings_Empty(t *testing.T) {
	heads := summary.ExtractHeadings([]string{"no headings here", "just text"})
	assert.Empty(t, heads)
}

func TestFindSection_Exact(t *testing.T) {
	input := lines("# Intro\ninto text\n## Usage\nusage text\n## End\nend text")
	start, end, found := summary.FindSection(input, "Usage", false)
	require.True(t, found)
	assert.Equal(t, 3, start)
	assert.Equal(t, 4, end)
}

func TestFindSection_CaseInsensitive(t *testing.T) {
	input := lines("## Sequence Diagram\ncontent\n## Next\nother")
	start, _, found := summary.FindSection(input, "sequence diagram", false)
	require.True(t, found)
	assert.Equal(t, 1, start)
}

func TestFindSection_Partial(t *testing.T) {
	input := lines("## Sequence Diagram\ncontent\n## Next\nother")
	start, _, found := summary.FindSection(input, "sequence", true)
	require.True(t, found)
	assert.Equal(t, 1, start)
}

func TestFindSection_EndAtSameLevel(t *testing.T) {
	// ## A ends at next ## B
	input := lines("## A\nline1\nline2\n## B\nother")
	_, end, found := summary.FindSection(input, "A", false)
	require.True(t, found)
	assert.Equal(t, 3, end)
}

func TestFindSection_EndAtLowerLevel(t *testing.T) {
	// ### X under ## Parent ends when ## Parent ends
	input := lines("## Parent\n### Child\nchild text\n## Sibling\nsibling")
	start, end, found := summary.FindSection(input, "Child", false)
	require.True(t, found)
	assert.Equal(t, 2, start)
	assert.Equal(t, 3, end)
}

func TestFindSection_EndAtEOF(t *testing.T) {
	input := lines("## Last Section\nline1\nline2\nline3")
	start, end, found := summary.FindSection(input, "Last Section", false)
	require.True(t, found)
	assert.Equal(t, 1, start)
	assert.Equal(t, 4, end)
}

func TestFindSection_NotFound(t *testing.T) {
	input := lines("## Existing\ncontent")
	_, _, found := summary.FindSection(input, "NonExistent", false)
	assert.False(t, found)
}
