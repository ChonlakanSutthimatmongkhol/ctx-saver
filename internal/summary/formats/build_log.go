package formats

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	buildDurationPattern = regexp.MustCompile(`(?i)BUILD (?:SUCCESSFUL|FAILED) in (.+)$`)
	failedTaskPattern    = regexp.MustCompile(`(?i)(?:Execution failed for task ['"]([^'"]+)['"]|> Task (:\S+) FAILED)`)
)

// BuildLogFormatter summarizes xcodebuild and Gradle build output.
type BuildLogFormatter struct{}

func (f *BuildLogFormatter) Name() string { return "build_log" }

func (f *BuildLogFormatter) Detect(output []byte, command string) bool {
	cmd := strings.ToLower(command)
	if strings.Contains(cmd, "xcodebuild") || strings.Contains(cmd, "gradle") || strings.Contains(cmd, "./gradlew") {
		return true
	}
	text := string(output)
	return strings.Contains(text, "** BUILD SUCCEEDED **") ||
		strings.Contains(text, "** BUILD FAILED **") ||
		strings.Contains(text, "BUILD SUCCESSFUL in ") ||
		strings.Contains(text, "BUILD FAILED in ") ||
		strings.Contains(text, "> Task :")
}

func (f *BuildLogFormatter) Summarize(output []byte) Summary {
	lines := splitOutputLines(output)
	result := "unknown"
	duration := ""
	failedTask := ""
	warnings := 0
	var diagnostics []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.Contains(line, "** BUILD SUCCEEDED **"), strings.Contains(line, "BUILD SUCCESSFUL"):
			result = "succeeded"
		case strings.Contains(line, "** BUILD FAILED **"), strings.Contains(line, "BUILD FAILED"):
			result = "failed"
		}
		if match := buildDurationPattern.FindStringSubmatch(trimmed); match != nil {
			duration = match[1]
		}
		if match := failedTaskPattern.FindStringSubmatch(trimmed); match != nil {
			if match[1] != "" {
				failedTask = match[1]
			} else {
				failedTask = match[2]
			}
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "warning:") || strings.HasPrefix(lower, "w: ") {
			warnings++
		}
		if strings.Contains(lower, "error:") || strings.HasPrefix(lower, "e: ") {
			diagnostics = append(diagnostics, trimmed)
		}
	}

	var text strings.Builder
	text.WriteString("## Result\n")
	text.WriteString(fmt.Sprintf("Build %s", result))
	if duration != "" {
		text.WriteString(" in " + duration)
	}
	if failedTask != "" {
		text.WriteString("; failed task: " + failedTask)
	}
	text.WriteString("\n\n## Errors\n")
	if len(diagnostics) == 0 {
		text.WriteString("None\n")
	} else {
		for _, diagnostic := range diagnostics {
			text.WriteString("- " + diagnostic + "\n")
		}
	}
	text.WriteString("\n## Warnings\n")
	text.WriteString(fmt.Sprintf("%d warning(s)", warnings))

	return Summary{
		Text:       text.String(),
		TotalLines: len(lines),
		TotalBytes: len(output),
		Format:     f.Name(),
		Metadata: map[string]any{
			"result":      result,
			"errors":      len(diagnostics),
			"warnings":    warnings,
			"duration":    duration,
			"failed_task": failedTask,
		},
	}
}
