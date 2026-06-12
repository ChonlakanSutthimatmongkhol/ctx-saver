package formats

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var pytestCountPattern = regexp.MustCompile(`(\d+)\s+(passed|failed|skipped|xfailed|xpassed|error|errors)`)

// PytestFormatter summarizes pytest output.
type PytestFormatter struct{}

func (f *PytestFormatter) Name() string { return "pytest" }

func (f *PytestFormatter) Detect(output []byte, command string) bool {
	return strings.Contains(strings.ToLower(command), "pytest") ||
		strings.Contains(string(output), "== test session starts ==")
}

func (f *PytestFormatter) Summarize(output []byte) Summary {
	lines := splitOutputLines(output)
	counts := map[string]int{}
	duration := ""
	var failures []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "FAILED ") {
			failures = append(failures, strings.TrimPrefix(trimmed, "FAILED "))
		}
		if strings.HasPrefix(trimmed, "=") && strings.Contains(trimmed, " in ") {
			for _, match := range pytestCountPattern.FindAllStringSubmatch(trimmed, -1) {
				count, _ := strconv.Atoi(match[1])
				key := strings.TrimSuffix(match[2], "s")
				if key == "error" {
					key = "failed"
				}
				counts[key] += count
			}
			parts := strings.Split(trimmed, " in ")
			if len(parts) > 1 {
				duration = strings.Trim(strings.TrimSpace(parts[len(parts)-1]), "= ")
			}
		}
	}

	var text strings.Builder
	text.WriteString("## Result\n")
	text.WriteString(fmt.Sprintf(
		"%d passed, %d failed, %d skipped",
		counts["passed"], counts["failed"], counts["skipped"],
	))
	if duration != "" {
		text.WriteString(" in " + duration)
	}
	text.WriteString("\n\n## Failures\n")
	if len(failures) == 0 {
		text.WriteString("None")
	} else {
		for _, failure := range failures {
			text.WriteString("- " + failure + "\n")
		}
	}

	return Summary{
		Text:       strings.TrimRight(text.String(), "\n"),
		TotalLines: len(lines),
		TotalBytes: len(output),
		Format:     f.Name(),
		Metadata: map[string]any{
			"passed":   counts["passed"],
			"failed":   counts["failed"],
			"skipped":  counts["skipped"],
			"duration": duration,
		},
	}
}
