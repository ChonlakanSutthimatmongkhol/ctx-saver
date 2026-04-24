package store_test

import (
	"errors"
	"testing"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/stretchr/testify/assert"
)

func TestBuildFTS5PhraseQuery_Plain(t *testing.T) {
	assert.Equal(t, `"payment"`, store.BuildFTS5PhraseQuery("payment"))
}

func TestBuildFTS5PhraseQuery_WithSpecialChars(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`payment-service`, `"payment-service"`},
		{`#API-123`, `"#API-123"`},
		{`error | warning`, `"error | warning"`},
		{`field:value`, `"field:value"`},
		{`(unmatched`, `"(unmatched"`},
		{`*wildcard`, `"*wildcard"`},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, store.BuildFTS5PhraseQuery(tc.input))
		})
	}
}

func TestBuildFTS5PhraseQuery_WithInternalQuotes(t *testing.T) {
	// Double quotes inside must be escaped by doubling per FTS5 phrase syntax.
	assert.Equal(t, `"error ""critical"""`, store.BuildFTS5PhraseQuery(`error "critical"`))
	assert.Equal(t, `"a ""b"" c"`, store.BuildFTS5PhraseQuery(`a "b" c`))
}

func TestBuildFTS5PhraseQuery_Empty(t *testing.T) {
	assert.Equal(t, "", store.BuildFTS5PhraseQuery(""))
	assert.Equal(t, "", store.BuildFTS5PhraseQuery("   "))
}

func TestBuildFTS5PhraseQuery_TrimsWhitespace(t *testing.T) {
	assert.Equal(t, `"hello"`, store.BuildFTS5PhraseQuery("  hello  "))
}

func TestIsFTS5SyntaxError_Nil(t *testing.T) {
	assert.False(t, store.IsFTS5SyntaxError(nil))
}

func TestIsFTS5SyntaxError_Positive(t *testing.T) {
	// Error strings verified from modernc.org/sqlite probe (2026-04-24).
	cases := []string{
		`SQL logic error: fts5: syntax error near "#" (1)`,
		`SQL logic error: fts5: syntax error near "|" (1)`,
		`SQL logic error: fts5: syntax error near "AND" (1)`,
		`SQL logic error: no such column: service (1)`,
		`SQL logic error: no such column: field (1)`,
		`SQL logic error: unterminated string (1)`,
		`SQL logic error: unknown special query: wildcard (1)`,
		`FTS search for "x": SQL logic error: malformed MATCH expression`,
	}
	for _, msg := range cases {
		t.Run(msg[:min(40, len(msg))], func(t *testing.T) {
			assert.True(t, store.IsFTS5SyntaxError(errors.New(msg)))
		})
	}
}

func TestIsFTS5SyntaxError_Negative(t *testing.T) {
	cases := []string{
		`connection refused`,
		`context deadline exceeded`,
		`disk I/O error`,
		`database is locked`,
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			assert.False(t, store.IsFTS5SyntaxError(errors.New(msg)))
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
