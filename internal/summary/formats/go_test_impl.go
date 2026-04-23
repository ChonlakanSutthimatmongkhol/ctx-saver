package formats

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reGoPass     = regexp.MustCompile(`^--- PASS:\s+(\S+)\s+\(([\d.]+)s\)`)
	reGoFail     = regexp.MustCompile(`^--- FAIL:\s+(\S+)\s+\(([\d.]+)s\)`)
	reGoSkip     = regexp.MustCompile(`^--- SKIP:\s+`)
	reGoPkgOK    = regexp.MustCompile(`^ok  \t(\S+)`)
	reGoPkgFail  = regexp.MustCompile(`^FAIL\t(\S+)`)
	reGoCoverage = regexp.MustCompile(`coverage:\s+([\d.]+)%`)
	reGoErrLine  = regexp.MustCompile(`^\s+(\S+_test\.go:\d+:.+)`)
)

type goFailedTest struct {
	pkg  string
	name string
	dur  string
	msgs []string
}

func goTestSummarize(output []byte) Summary {
	s := string(output)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	totalLines := len(lines)
	totalBytes := len(output)

	pkgPass, pkgFail := 0, 0
	var coverage string
	var failedTests []goFailedTest
	var current *goFailedTest
	var currentPkg string

	for _, line := range lines {
		if m := reGoPkgOK.FindStringSubmatch(line); m != nil {
			pkgPass++
			currentPkg = m[1]
			current = nil
		} else if m := reGoPkgFail.FindStringSubmatch(line); m != nil {
			pkgFail++
			currentPkg = m[1]
		} else if m := reGoFail.FindStringSubmatch(line); m != nil {
			ft := goFailedTest{pkg: currentPkg, name: m[1], dur: m[2]}
			failedTests = append(failedTests, ft)
			current = &failedTests[len(failedTests)-1]
		} else if reGoPass.MatchString(line) || reGoSkip.MatchString(line) {
			current = nil
		} else if current != nil {
			if m := reGoErrLine.FindStringSubmatch(line); m != nil {
				current.msgs = append(current.msgs, strings.TrimSpace(m[1]))
			}
		}
		if m := reGoCoverage.FindStringSubmatch(line); m != nil {
			coverage = m[1] + "%"
		}
	}
	_ = currentPkg

	var sb strings.Builder
	if pkgFail > 0 {
		sb.WriteString(fmt.Sprintf("Go tests: %d packages PASS, %d FAIL\n", pkgPass, pkgFail))
	} else {
		sb.WriteString(fmt.Sprintf("Go tests: %d packages PASS\n", pkgPass))
	}
	if len(failedTests) > 0 {
		sb.WriteString("Failed:\n")
		for _, ft := range failedTests {
			sb.WriteString(fmt.Sprintf("  - %s — %s (%ss)\n", ft.pkg, ft.name, ft.dur))
			for _, msg := range ft.msgs {
				sb.WriteString(fmt.Sprintf("    %s\n", msg))
			}
		}
	}
	if coverage != "" {
		sb.WriteString(fmt.Sprintf("Coverage: %s\n", coverage))
	}
	sb.WriteString("Use ctx_get_full for full stack traces.")

	return Summary{
		Text:       sb.String(),
		TotalLines: totalLines,
		TotalBytes: totalBytes,
		Format:     "go_test",
		Metadata: map[string]any{
			"pkg_pass": pkgPass,
			"pkg_fail": pkgFail,
			"coverage": coverage,
		},
	}
}
