package formats

import (
	"fmt"
	"regexp"
	"strings"
)

const maxLintIssues = 100

var (
	golangciIssuePattern = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s+(.+?)\s+\(([^)]+)\)$`)
	eslintIssuePattern   = regexp.MustCompile(`^\s*(\d+):(\d+)\s+(error|warning)\s+(.+?)\s+(\S+)$`)
)

// LintFormatter summarizes golangci-lint and ESLint output.
type LintFormatter struct{}

func (f *LintFormatter) Name() string { return "lint" }

func (f *LintFormatter) Detect(_ []byte, command string) bool {
	cmd := strings.ToLower(command)
	return strings.Contains(cmd, "golangci-lint") || strings.Contains(cmd, "eslint")
}

func (f *LintFormatter) Summarize(output []byte) Summary {
	lines := splitOutputLines(output)
	counts := map[string]int{}
	var issues []string
	currentFile := ""
	summaryLine := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := golangciIssuePattern.FindStringSubmatch(trimmed); match != nil {
			counts[match[5]]++
			if len(issues) < maxLintIssues {
				issues = append(issues, fmt.Sprintf("%s:%s:%s: %s (%s)", match[1], match[2], match[3], match[4], match[5]))
			}
			continue
		}
		if strings.HasSuffix(trimmed, ".js") || strings.HasSuffix(trimmed, ".jsx") ||
			strings.HasSuffix(trimmed, ".ts") || strings.HasSuffix(trimmed, ".tsx") {
			currentFile = trimmed
			continue
		}
		if match := eslintIssuePattern.FindStringSubmatch(line); match != nil && currentFile != "" {
			counts[match[5]]++
			if len(issues) < maxLintIssues {
				issues = append(issues, fmt.Sprintf(
					"%s:%s:%s: %s (%s)", currentFile, match[1], match[2], match[4], match[5],
				))
			}
			continue
		}
		summaryLine = trimmed
	}

	var text strings.Builder
	text.WriteString("## Result\n")
	text.WriteString(fmt.Sprintf("%d issue(s)", len(issues)))
	if summaryLine != "" {
		text.WriteString("; " + summaryLine)
	}
	text.WriteString("\n\n## Issues\n")
	if len(issues) == 0 {
		text.WriteString("None")
	} else {
		for _, issue := range issues {
			text.WriteString("- " + issue + "\n")
		}
	}
	if len(issues) == maxLintIssues {
		text.WriteString(fmt.Sprintf("Showing first %d issues.\n", maxLintIssues))
	}

	return Summary{
		Text:       strings.TrimRight(text.String(), "\n"),
		TotalLines: len(lines),
		TotalBytes: len(output),
		Format:     f.Name(),
		Metadata: map[string]any{
			"issue_count": len(issues),
			"by_rule":     counts,
		},
	}
}
