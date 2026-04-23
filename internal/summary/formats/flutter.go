package formats

import "strings"

// FlutterTestFormatter detects and summarises `flutter test` output.
type FlutterTestFormatter struct{}

// Name returns "flutter_test".
func (f *FlutterTestFormatter) Name() string { return "flutter_test" }

// Detect returns true when the output or command looks like flutter test output.
func (f *FlutterTestFormatter) Detect(output []byte, command string) bool {
	if strings.Contains(command, "flutter test") {
		return true
	}
	s := string(output)
	return strings.Contains(s, "All tests passed!") ||
		strings.Contains(s, "Some tests failed.") ||
		strings.Contains(s, "Running flutter test")
}

// Summarize produces a compact flutter test summary.
func (f *FlutterTestFormatter) Summarize(output []byte) Summary {
	return flutterSummarize(output)
}
