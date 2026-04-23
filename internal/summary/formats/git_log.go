package formats

import "strings"

// GitLogFormatter detects and summarises `git log` output.
type GitLogFormatter struct{}

// Name returns "git_log".
func (f *GitLogFormatter) Name() string { return "git_log" }

// Detect returns true when output or command looks like git log output.
func (f *GitLogFormatter) Detect(output []byte, command string) bool {
	if strings.Contains(command, "git log") {
		return true
	}
	s := string(output)
	return strings.HasPrefix(s, "commit ") || strings.Contains(s, "\ncommit ")
}

// Summarize produces a compact git log summary.
func (f *GitLogFormatter) Summarize(output []byte) Summary {
	return gitLogSummarize(output)
}
