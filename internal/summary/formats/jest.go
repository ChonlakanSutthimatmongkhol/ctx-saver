package formats

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var jestCountPattern = regexp.MustCompile(`(\d+)\s+(failed|passed|skipped|total)`)

// JestFormatter summarizes Jest output.
type JestFormatter struct{}

func (f *JestFormatter) Name() string { return "jest" }

func (f *JestFormatter) Detect(output []byte, command string) bool {
	cmd := strings.ToLower(command)
	text := string(output)
	return strings.Contains(cmd, "jest") ||
		(strings.Contains(cmd, "npm test") && strings.Contains(text, "Test Suites:")) ||
		(strings.Contains(text, "Test Suites:") && strings.Contains(text, "Tests:"))
}

func (f *JestFormatter) Summarize(output []byte) Summary {
	lines := splitOutputLines(output)
	suites := map[string]int{}
	tests := map[string]int{}
	duration := ""
	var failures []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Test Suites:"):
			parseJestCounts(trimmed, suites)
		case strings.HasPrefix(trimmed, "Tests:"):
			parseJestCounts(trimmed, tests)
		case strings.HasPrefix(trimmed, "Time:"):
			duration = strings.TrimSpace(strings.TrimPrefix(trimmed, "Time:"))
		case strings.HasPrefix(trimmed, "● "):
			failures = append(failures, strings.TrimSpace(strings.TrimPrefix(trimmed, "● ")))
		}
	}

	var text strings.Builder
	text.WriteString("## Result\n")
	text.WriteString(fmt.Sprintf(
		"Suites: %d passed, %d failed; Tests: %d passed, %d failed, %d skipped",
		suites["passed"], suites["failed"], tests["passed"], tests["failed"], tests["skipped"],
	))
	if duration != "" {
		text.WriteString("; time: " + duration)
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
			"suites_passed": suites["passed"],
			"suites_failed": suites["failed"],
			"tests_passed":  tests["passed"],
			"tests_failed":  tests["failed"],
			"tests_skipped": tests["skipped"],
			"duration":      duration,
		},
	}
}

func parseJestCounts(line string, dst map[string]int) {
	for _, match := range jestCountPattern.FindAllStringSubmatch(line, -1) {
		count, _ := strconv.Atoi(match[1])
		dst[match[2]] = count
	}
}
