package formats

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	reGitCommit  = regexp.MustCompile(`(?m)^commit ([0-9a-f]{7,40})`)
	reGitAuthor  = regexp.MustCompile(`(?m)^Author:\s+(.+?)\s+<`)
	reGitDate    = regexp.MustCompile(`(?m)^Date:\s+(.+)`)
	reGitSubject = regexp.MustCompile(`(?m)^    (.+)`)
)

func gitLogSummarize(output []byte) Summary {
	s := string(output)
	totalBytes := len(output)
	totalLines := strings.Count(s, "\n") + 1

	commits := reGitCommit.FindAllStringSubmatch(s, -1)
	commitCount := len(commits)

	// Authors with counts.
	authorMatches := reGitAuthor.FindAllStringSubmatch(s, -1)
	authorCounts := map[string]int{}
	for _, m := range authorMatches {
		authorCounts[m[1]]++
	}
	type authorStat struct {
		name  string
		count int
	}
	var authors []authorStat
	for name, count := range authorCounts {
		authors = append(authors, authorStat{name, count})
	}
	sort.Slice(authors, func(i, j int) bool { return authors[i].count > authors[j].count })

	// Dates (first = newest, last = oldest in default `git log` order).
	dateMatches := reGitDate.FindAllStringSubmatch(s, -1)
	subjects := reGitSubject.FindAllStringSubmatch(s, -1)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Git log: %d commits (%d authors)\n", commitCount, len(authorCounts)))

	if len(commits) > 0 && len(subjects) > 0 && len(dateMatches) > 0 {
		newestHash := commits[0][1][:7]
		newestSubject := subjects[0][1]
		newestAgo := parseRelative(strings.TrimSpace(dateMatches[0][1]))
		sb.WriteString(fmt.Sprintf("Newest: %s — %q (%s)\n", newestAgo, newestSubject, newestHash))
	}
	if len(commits) > 1 && len(subjects) > 1 && len(dateMatches) > 1 {
		oldestHash := commits[len(commits)-1][1][:7]
		oldestSubject := subjects[len(subjects)-1][1]
		oldestAgo := parseRelative(strings.TrimSpace(dateMatches[len(dateMatches)-1][1]))
		sb.WriteString(fmt.Sprintf("Oldest: %s — %q (%s)\n", oldestAgo, oldestSubject, oldestHash))
	}

	if len(authors) > 0 {
		sb.WriteString("Top authors:\n")
		limit := 5
		if len(authors) < limit {
			limit = len(authors)
		}
		for _, a := range authors[:limit] {
			sb.WriteString(fmt.Sprintf("  - %s (%d commits)\n", a.name, a.count))
		}
	}
	sb.WriteString(`Use ctx_search queries=["bug fix", "refactor"] to find specific commits.`)

	return Summary{
		Text:       sb.String(),
		TotalLines: totalLines,
		TotalBytes: totalBytes,
		Format:     "git_log",
		Metadata: map[string]any{
			"commit_count":  commitCount,
			"author_count":  len(authorCounts),
		},
	}
}

// parseRelative converts a git date string to a human-readable relative string.
func parseRelative(dateStr string) string {
	// Git date format: "Mon Jan 2 15:04:05 2006 -0700"
	layouts := []string{
		"Mon Jan 2 15:04:05 2006 -0700",
		"Mon Jan  2 15:04:05 2006 -0700",
	}
	var t time.Time
	var err error
	for _, layout := range layouts {
		t, err = time.Parse(layout, dateStr)
		if err == nil {
			break
		}
	}
	if err != nil {
		return dateStr
	}
	diff := time.Since(t)
	switch {
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	case diff < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	case diff < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(diff.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dyr ago", int(diff.Hours()/(24*365)))
	}
}
