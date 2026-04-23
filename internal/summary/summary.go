// Package summary provides head+tail+stats summarisation for large command outputs.
package summary

import (
	"fmt"
	"strings"
)

// Result holds the summarised text and metadata.
type Result struct {
	// Text is the formatted summary ready to return to the AI.
	Text string
	// TotalLines is the total number of lines in the original output.
	TotalLines int
	// TotalBytes is the byte length of the original output.
	TotalBytes int
	// Truncated is true when lines were omitted.
	Truncated bool
}

// Summarize returns the first headLines and last tailLines of output, with stats.
// If the total number of lines is within headLines+tailLines, the full text is returned.
func Summarize(output []byte, headLines, tailLines int) Result {
	if len(output) == 0 {
		return Result{
			Text:       "(empty output)",
			TotalLines: 0,
			TotalBytes: 0,
			Truncated:  false,
		}
	}

	// Normalise trailing newline before splitting.
	text := strings.TrimRight(string(output), "\n")
	lines := strings.Split(text, "\n")
	totalLines := len(lines)
	totalBytes := len(output)

	if headLines <= 0 {
		headLines = 20
	}
	if tailLines < 0 {
		tailLines = 0
	}

	window := headLines + tailLines
	if totalLines <= window {
		return Result{
			Text:       string(output),
			TotalLines: totalLines,
			TotalBytes: totalBytes,
			Truncated:  false,
		}
	}

	omitted := totalLines - window
	var sb strings.Builder

	for _, l := range lines[:headLines] {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	sb.WriteString(fmt.Sprintf("\n... (%d lines omitted — use ctx_search or ctx_get_full to access them) ...\n\n", omitted))
	for _, l := range lines[totalLines-tailLines:] {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}

	if headings := extractHeadings(lines); len(headings) > 0 {
		sb.WriteString("\n--- document outline ---\n")
		for _, h := range headings {
			sb.WriteString(h)
			sb.WriteByte('\n')
		}
	}

	return Result{
		Text:       sb.String(),
		TotalLines: totalLines,
		TotalBytes: totalBytes,
		Truncated:  true,
	}
}

// extractHeadings returns "L{n}: {text}" for Markdown heading lines (##, ###, ####).
func extractHeadings(lines []string) []string {
	var out []string
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "#### ") {
			out = append(out, fmt.Sprintf("L%d: %s", i+1, line))
		}
	}
	return out
}

// FormatStats returns a one-line stats string suitable for appending to a summary.
func FormatStats(lines, sizeBytes, exitCode int, durationMs int64) string {
	return fmt.Sprintf("[lines=%d size=%dB exit=%d duration=%dms]",
		lines, sizeBytes, exitCode, durationMs)
}
