package formats

import "strings"

// GoTestFormatter detects and summarises `go test` output.
type GoTestFormatter struct{}

// Name returns "go_test".
func (f *GoTestFormatter) Name() string { return "go_test" }

// Detect returns true when output or command looks like go test output.
func (f *GoTestFormatter) Detect(output []byte, command string) bool {
	if strings.Contains(command, "go test") {
		return true
	}
	s := string(output)
	hasRun := strings.Contains(s, "=== RUN")
	hasResult := strings.Contains(s, "--- PASS:") || strings.Contains(s, "--- FAIL:")
	hasPackage := strings.Contains(s, "ok  \t") || strings.Contains(s, "FAIL\t")
	return (hasRun && hasResult) || hasPackage
}

// Summarize produces a compact go test summary.
func (f *GoTestFormatter) Summarize(output []byte) Summary {
	return goTestSummarize(output)
}
