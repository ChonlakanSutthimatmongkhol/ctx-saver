package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// RunPreToolUse reads a Codex CLI PreToolUse JSON payload from r, applies
// routing rules, and writes the decision JSON to w.
//
// Codex CLI PreToolUse only supports "deny" — additionalContext and
// "allow" are ignored by codex-rs output_parser.rs.  We therefore emit
// either a deny decision or an empty passthrough object.
func RunPreToolUse(_ store.Store, r io.Reader, w io.Writer) error {	input, err := readInput(r)
	if err != nil {
		return allowPassthrough(w, "PreToolUse")
	}

	cmd := extractCmd(input.ToolInput)
	decision := routePreToolUse(input.ToolName, cmd)

	if decision.allow {
		return allowPassthrough(w, "PreToolUse")
	}

	return writeJSON(w, CodexHookOutput{
		HookSpecificOutput: CodexSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "deny",
		},
	})
}

// extractCmd pulls the shell command string out of a tool input map.
// Codex CLI uses {"cmd": "..."} for Shell; Claude Code uses {"command": "..."}.
func extractCmd(input map[string]any) string {
	for _, key := range []string{"cmd", "command", "input"} {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	// Fallback: join all string values.
	var parts []string
	for _, v := range input {
		if s, ok := v.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// allowPassthrough emits the minimal Codex-compatible passthrough object.
func allowPassthrough(w io.Writer, eventName string) error {
	return writeJSON(w, CodexHookOutput{
		HookSpecificOutput: CodexSpecificOutput{
			HookEventName: eventName,
		},
	})
}

// readInput decodes a HookInput from r; returns a zero value on parse error.
func readInput(r io.Reader) (HookInput, error) {
	var input HookInput
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return HookInput{}, fmt.Errorf("decoding hook input: %w", err)
	}
	return input, nil
}

// writeJSON encodes v to w as a single JSON line followed by a newline.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
