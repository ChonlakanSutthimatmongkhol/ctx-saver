// Package formats provides format-specific summarizers for ctx_execute outputs.
package formats

// Formatter summarizes command output in a format-specific way.
// Detect should be cheap (byte prefix / command substring checks).
type Formatter interface {
	// Name returns the formatter identifier (used in Summary.Format).
	Name() string

	// Detect returns true if this formatter can handle the output.
	// command is the sanitised command string (for hint-based detection).
	Detect(output []byte, command string) bool

	// Summarize produces a format-specific Summary.
	// Output should be compact (< 1 KB ideally) — callers rely on ctx_search
	// or ctx_get_full for details.
	Summarize(output []byte) Summary
}

// Summary is a format-aware summary result.
type Summary struct {
	// Text is the human-readable summary injected into context.
	Text string
	// TotalLines is the total number of lines in the original output.
	TotalLines int
	// TotalBytes is the byte length of the original output.
	TotalBytes int
	// Format is the formatter Name() that produced this summary.
	Format string
	// Metadata holds format-specific structured data (pass/fail counts etc.).
	Metadata map[string]any
}
