package formats

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	reFlutterPassed  = regexp.MustCompile(`\+(\d+)`)
	reFlutterFailed  = regexp.MustCompile(`\s-(\d+)`)
	reFlutterSkipped = regexp.MustCompile(`~(\d+)`)
	reFlutterDur     = regexp.MustCompile(`(?:Exited|elapsed).*?([\d.]+)s`)
	reFlutterTest    = regexp.MustCompile(`(?:FAILED:|^\s*\[E\])\s*(.+dart:\d+)`)
)

func flutterSummarize(output []byte) Summary {
	s := string(output)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	totalLines := len(lines)
	totalBytes := len(output)

	passed, failed, skipped := 0, 0, 0
	var duration float64
	var failedTests []string
	success := strings.Contains(s, "All tests passed!")
	hasFail := strings.Contains(s, "Some tests failed.")

	for _, line := range lines {
		if m := reFlutterPassed.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v > passed {
				passed = v
			}
		}
		if m := reFlutterFailed.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				failed = v
			}
		}
		if m := reFlutterSkipped.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				skipped = v
			}
		}
		if m := reFlutterDur.FindStringSubmatch(line); m != nil {
			duration, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := reFlutterTest.FindStringSubmatch(line); m != nil {
			failedTests = append(failedTests, strings.TrimSpace(m[1]))
		}
	}

	var sb strings.Builder
	passIcon, failIcon, skipIcon := "✅", "❌", "⏭"
	if failed > 0 || hasFail {
		sb.WriteString(fmt.Sprintf("Flutter tests: %s %d passed, %s %d failed, %s %d skipped\n",
			passIcon, passed, failIcon, failed, skipIcon, skipped))
	} else if success {
		sb.WriteString(fmt.Sprintf("Flutter tests: %s %d passed, %s 0 failed\n", passIcon, passed, failIcon))
	} else {
		sb.WriteString(fmt.Sprintf("Flutter tests: %s %d passed, %s %d failed, %s %d skipped\n",
			passIcon, passed, failIcon, failed, skipIcon, skipped))
	}
	if duration > 0 {
		sb.WriteString(fmt.Sprintf("Duration: %.1fs\n", duration))
	}
	if len(failedTests) > 0 {
		sb.WriteString("Failed tests:\n")
		for _, t := range failedTests {
			sb.WriteString(fmt.Sprintf("  - %s\n", t))
		}
	}
	sb.WriteString(`Use ctx_search queries=["FAIL", "exception"] to inspect stack traces.`)

	return Summary{
		Text:       sb.String(),
		TotalLines: totalLines,
		TotalBytes: totalBytes,
		Format:     "flutter_test",
		Metadata: map[string]any{
			"passed":           passed,
			"failed":           failed,
			"skipped":          skipped,
			"duration_seconds": duration,
			"failed_tests":     failedTests,
		},
	}
}
