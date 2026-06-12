package formats

import "strings"

func splitOutputLines(output []byte) []string {
	if len(output) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(output), "\n"), "\n")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
