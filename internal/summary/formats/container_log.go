package formats

import (
	"fmt"
	"regexp"
	"strings"
)

const maxContainerDiagnostics = 50

var timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\S+`)

// ContainerLogFormatter summarizes kubectl and docker log streams.
type ContainerLogFormatter struct{}

func (f *ContainerLogFormatter) Name() string { return "container_log" }

func (f *ContainerLogFormatter) Detect(_ []byte, command string) bool {
	cmd := strings.ToLower(command)
	return strings.Contains(cmd, "kubectl logs") || strings.Contains(cmd, "docker logs")
}

func (f *ContainerLogFormatter) Summarize(output []byte) Summary {
	lines := splitOutputLines(output)
	var diagnostics []string
	firstTimestamp := ""
	lastTimestamp := ""
	panicBlock := ""

	for i, line := range lines {
		if match := timestampPattern.FindString(line); match != "" {
			if firstTimestamp == "" {
				firstTimestamp = match
			}
			lastTimestamp = match
		}
		upper := strings.ToUpper(line)
		if len(diagnostics) < maxContainerDiagnostics &&
			(strings.Contains(upper, "ERROR") || strings.Contains(upper, "FATAL") || strings.Contains(upper, "WARN")) {
			diagnostics = append(diagnostics, strings.TrimSpace(line))
		}
		if panicBlock == "" && strings.Contains(strings.ToLower(line), "panic") {
			end := minInt(i+8, len(lines))
			panicBlock = strings.Join(lines[i:end], "\n")
		}
	}

	var text strings.Builder
	text.WriteString("## Errors\n")
	if len(diagnostics) == 0 {
		text.WriteString("None\n")
	} else {
		for _, diagnostic := range diagnostics {
			text.WriteString("- " + diagnostic + "\n")
		}
	}
	text.WriteString("\n## Timeline\n")
	text.WriteString(fmt.Sprintf(
		"%s to %s; %d total line(s)",
		valueOrUnknown(firstTimestamp), valueOrUnknown(lastTimestamp), len(lines),
	))
	if panicBlock != "" {
		text.WriteString("\n\n## Panic\n" + panicBlock)
	}

	return Summary{
		Text:       strings.TrimRight(text.String(), "\n"),
		TotalLines: len(lines),
		TotalBytes: len(output),
		Format:     f.Name(),
		Metadata: map[string]any{
			"diagnostics":     len(diagnostics),
			"first_timestamp": firstTimestamp,
			"last_timestamp":  lastTimestamp,
			"line_count":      len(lines),
		},
	}
}
