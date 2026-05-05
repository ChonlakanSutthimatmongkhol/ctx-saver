package signatures

import (
	"bufio"
	"fmt"
	"strings"
)

// Extract returns a signatures-only view of source code with original line
// numbers preserved. Each matched line is formatted as:
//
//	[<original_line_num>]  <line_content>
//
// This lets callers use ctx_get_full --line=N or ctx_get_section correctly
// against the original file.
//
// Returns ErrUnsupportedLanguage if lang has no registered patterns.
// Returns an empty string (no error) if the file contains no matching lines.
func Extract(content []byte, lang Language) (string, error) {
	patterns, ok := patternsByLang[lang]
	if !ok {
		return "", ErrUnsupportedLanguage
	}

	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	// Support very long lines (e.g. generated files with long type parameters).
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		for _, re := range patterns {
			if re.MatchString(line) {
				fmt.Fprintf(&out, "[%d]  %s\n", lineNum, line)
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("signatures: scanning content: %w", err)
	}
	return out.String(), nil
}
