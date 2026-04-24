package store

import "strings"

// BuildFTS5PhraseQuery escapes user input into a safe FTS5 MATCH phrase query.
// All FTS5 operators (AND, OR, NOT, NEAR, column filters, wildcards) are disabled.
// Double quotes inside the query are escaped by doubling, per FTS5 phrase syntax.
//
// Examples:
//
//	BuildFTS5PhraseQuery("payment-service") → `"payment-service"`
//	BuildFTS5PhraseQuery(`error "critical"`) → `"error ""critical"""`
func BuildFTS5PhraseQuery(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	escaped := strings.ReplaceAll(q, `"`, `""`)
	return `"` + escaped + `"`
}

// IsFTS5SyntaxError reports whether err looks like an FTS5 MATCH parse error
// from the modernc.org/sqlite driver. Used to decide whether to fall back to
// LIKE search.
//
// Patterns verified against modernc.org/sqlite on 2026-04-24:
//   - "fts5: syntax error near"  — #, |, (, AND, NOT, ...
//   - "no such column:"          — dash (NOT), colon (column filter)
//   - "unterminated string"      — unmatched double-quote
//   - "unknown special query:"   — leading * wildcard
func IsFTS5SyntaxError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	patterns := []string{
		"fts5: syntax error",
		"no such column:",
		"unterminated string",
		"unknown special query:",
		"malformed MATCH",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
