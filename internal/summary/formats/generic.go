package formats

import (
	"fmt"
	"strings"
)

// GenericFormatter implements a head+tail summariser as the catch-all fallback.
type GenericFormatter struct {
	// HeadLines is the number of leading lines to include.
	HeadLines int
	// TailLines is the number of trailing lines to include.
	TailLines int
}

// Name returns "generic".
func (g *GenericFormatter) Name() string { return "generic" }

// Detect always returns true — GenericFormatter is the catch-all fallback.
func (g *GenericFormatter) Detect(_ []byte, _ string) bool { return true }

// Summarize returns the first HeadLines and last TailLines of output.
// The head/tail logic is self-contained to avoid an import cycle with the
// parent summary package.
func (g *GenericFormatter) Summarize(output []byte) Summary {
	totalBytes := len(output)
	if totalBytes == 0 {
		return Summary{
			Text:       "(empty output)",
			TotalLines: 0,
			TotalBytes: 0,
			Format:     "generic",
		}
	}

	headLines := g.HeadLines
	tailLines := g.TailLines
	if headLines <= 0 {
		headLines = 20
	}
	if tailLines < 0 {
		tailLines = 0
	}

	text := strings.TrimRight(string(output), "\n")
	lines := strings.Split(text, "\n")
	totalLines := len(lines)

	window := headLines + tailLines
	if totalLines <= window {
		return Summary{
			Text:       string(output),
			TotalLines: totalLines,
			TotalBytes: totalBytes,
			Format:     "generic",
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

	return Summary{
		Text:       sb.String(),
		TotalLines: totalLines,
		TotalBytes: totalBytes,
		Format:     "generic",
	}
}
