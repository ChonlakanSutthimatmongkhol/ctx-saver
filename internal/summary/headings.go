package summary

import "strings"

// Heading is a single structural element found in text.
type Heading struct {
	Line  int    // 1-based line number
	Level int    // 1=# 2=## 3=### 4=####; 0=table header or other
	Text  string // trimmed heading text (without prefix)
	Raw   string // original line
}

// ExtractHeadings scans lines and returns all Markdown-style headings.
// Supports ATX-style (#, ##, ###, ####) and setext-style (=== / --- underline).
func ExtractHeadings(lines []string) []Heading {
	var heads []Heading
	for i, line := range lines {
		lineNo := i + 1
		switch {
		case strings.HasPrefix(line, "#### "):
			heads = append(heads, Heading{
				Line: lineNo, Level: 4,
				Text: strings.TrimPrefix(line, "#### "),
				Raw:  line,
			})
		case strings.HasPrefix(line, "### "):
			heads = append(heads, Heading{
				Line: lineNo, Level: 3,
				Text: strings.TrimPrefix(line, "### "),
				Raw:  line,
			})
		case strings.HasPrefix(line, "## "):
			heads = append(heads, Heading{
				Line: lineNo, Level: 2,
				Text: strings.TrimPrefix(line, "## "),
				Raw:  line,
			})
		case strings.HasPrefix(line, "# "):
			heads = append(heads, Heading{
				Line: lineNo, Level: 1,
				Text: strings.TrimPrefix(line, "# "),
				Raw:  line,
			})
		// Setext style: text followed by === or --- underline.
		case i+1 < len(lines) && isSetextUnderline(lines[i+1], '='):
			heads = append(heads, Heading{
				Line: lineNo, Level: 1,
				Text: strings.TrimSpace(line),
				Raw:  line,
			})
		case i+1 < len(lines) && isSetextUnderline(lines[i+1], '-'):
			heads = append(heads, Heading{
				Line: lineNo, Level: 2,
				Text: strings.TrimSpace(line),
				Raw:  line,
			})
		}
	}
	return heads
}

func isSetextUnderline(line string, char byte) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	for i := range len(trimmed) {
		if trimmed[i] != char {
			return false
		}
	}
	return true
}

// FindSection returns the 1-based inclusive line range [start, end] for the
// section with the given heading text. Returns (0, 0, false) if not found.
// The section ends just before the next heading at the same or higher level
// (numerically ≤ hit.Level), or at end of file.
// Match is case-insensitive; partial substring match when allowPartial=true.
func FindSection(lines []string, heading string, allowPartial bool) (start, end int, found bool) {
	heads := ExtractHeadings(lines)
	target := strings.ToLower(strings.TrimSpace(heading))

	var hit *Heading
	var hitIdx int
	for i := range heads {
		text := strings.ToLower(strings.TrimSpace(heads[i].Text))
		if text == target || (allowPartial && strings.Contains(text, target)) {
			hit = &heads[i]
			hitIdx = i
			break
		}
	}
	if hit == nil {
		return 0, 0, false
	}

	endLine := len(lines)
	for j := hitIdx + 1; j < len(heads); j++ {
		if heads[j].Level > 0 && heads[j].Level <= hit.Level {
			endLine = heads[j].Line - 1
			break
		}
	}
	return hit.Line, endLine, true
}
