package formats

import (
	"encoding/json"
	"fmt"
	"strings"
)

// jsonValid reports whether b is valid JSON.
func jsonValid(b []byte) bool {
	return json.Valid(b)
}

func jsonSummarize(output []byte) Summary {
	totalBytes := len(output)
	trimmed := strings.TrimSpace(string(output))
	lines := strings.Count(trimmed, "\n") + 1
	sizeFmt := formatKB(totalBytes)

	var v any
	if err := json.Unmarshal(output, &v); err != nil {
		// Fallback: output is valid (Detect passed) but unmarshal failed somehow.
		return Summary{
			Text:       fmt.Sprintf("JSON (%s) — use ctx_search or ctx_get_full for details.", sizeFmt),
			TotalLines: lines,
			TotalBytes: totalBytes,
			Format:     "json",
		}
	}

	var sb strings.Builder
	switch root := v.(type) {
	case map[string]any:
		sb.WriteString(fmt.Sprintf("JSON object (%s)\n", sizeFmt))
		sb.WriteString(fmt.Sprintf("Top-level keys: %d\n", len(root)))
		for k, val := range root {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", k, describeValue(val)))
		}
		// Sample: first array or nested key
		for k, val := range root {
			if arr, ok := val.([]any); ok && len(arr) > 0 {
				sample := marshalCompact(arr[0])
				sb.WriteString(fmt.Sprintf("Sample: $.%s[0] = %s\n", k, sample))
				break
			}
		}
		sb.WriteString("Use ctx_search to query specific sections.")

	case []any:
		sb.WriteString(fmt.Sprintf("JSON array (%s, %d items)\n", sizeFmt, len(root)))
		if len(root) > 0 {
			sb.WriteString(fmt.Sprintf("Item type: %s\n", typeName(root[0])))
			if obj, ok := root[0].(map[string]any); ok {
				keys := make([]string, 0, len(obj))
				for k := range obj {
					keys = append(keys, k)
					if len(keys) >= 5 {
						break
					}
				}
				sb.WriteString(fmt.Sprintf("Common keys: %s\n", strings.Join(keys, ", ")))
			}
			sb.WriteString(fmt.Sprintf("Sample: $[0] = %s\n", marshalCompact(root[0])))
		}
		sb.WriteString("Use ctx_search or ctx_get_full for details.")
	}

	return Summary{
		Text:       sb.String(),
		TotalLines: lines,
		TotalBytes: totalBytes,
		Format:     "json",
	}
}

func describeValue(v any) string {
	switch val := v.(type) {
	case string:
		if len(val) <= 40 {
			return fmt.Sprintf("%q", val)
		}
		return fmt.Sprintf("string (%d chars)", len(val))
	case float64:
		return fmt.Sprintf("%v", val)
	case bool:
		return fmt.Sprintf("%v", val)
	case nil:
		return "null"
	case []any:
		return fmt.Sprintf("array (%d items)", len(val))
	case map[string]any:
		return fmt.Sprintf("object (%d keys)", len(val))
	default:
		return fmt.Sprintf("%T", v)
	}
}

func typeName(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

func marshalCompact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	s := string(b)
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

func formatKB(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
}
