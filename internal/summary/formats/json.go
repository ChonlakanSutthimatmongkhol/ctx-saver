package formats

import "strings"

// JSONFormatter detects and summarises JSON output.
type JSONFormatter struct{}

// Name returns "json".
func (f *JSONFormatter) Name() string { return "json" }

// Detect returns true when output is valid JSON (object or array).
func (f *JSONFormatter) Detect(output []byte, _ string) bool {
	trimmed := []byte(strings.TrimSpace(string(output)))
	if len(trimmed) == 0 {
		return false
	}
	return (trimmed[0] == '{' || trimmed[0] == '[') && jsonValid(trimmed)
}

// Summarize produces a compact JSON structure summary.
func (f *JSONFormatter) Summarize(output []byte) Summary {
	return jsonSummarize(output)
}
