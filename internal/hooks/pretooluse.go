package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

var sourceExtensions = map[string]struct{}{
	".go": {}, ".dart": {}, ".ts": {}, ".tsx": {}, ".js": {}, ".jsx": {},
	".py": {}, ".java": {}, ".kt": {}, ".swift": {}, ".rs": {}, ".c": {},
	".cc": {}, ".cpp": {}, ".h": {}, ".hpp": {}, ".cs": {}, ".rb": {},
	".php": {}, ".sh": {}, ".sql": {}, ".proto": {}, ".vue": {}, ".svelte": {},
}

// RunPreToolUse reads a Codex CLI PreToolUse JSON payload from r, applies
// routing rules, and writes the decision JSON to w.
//
// Codex CLI PreToolUse only supports "deny" — additionalContext and
// "allow" are ignored by codex-rs output_parser.rs.  We therefore emit
// either a deny decision or an empty passthrough object.
func RunPreToolUse(_ store.Store, r io.Reader, w io.Writer, largeThresholdBytes int) error {
	input, host, err := readInput(r)
	if err != nil {
		return allowPassthrough(w, "PreToolUse")
	}

	cmd := extractCmd(input.ToolInput)
	decision := routePreToolUse(input.ToolName, cmd)
	if decision.allow && host == HostCopilot {
		decision = routeCopilotNativeTool(input, cmd, largeThresholdBytes)
	}

	if !decision.allow {
		if host == HostCopilot {
			return writeJSON(w, CopilotHookOutput{
				PermissionDecision:       "deny",
				PermissionDecisionReason: decision.reason,
			})
		}
		return writeJSON(w, CodexHookOutput{
			HookSpecificOutput: CodexSpecificOutput{
				HookEventName:      "PreToolUse",
				PermissionDecision: "deny",
			},
		})
	}

	if host == HostCopilot {
		return writeJSON(w, CopilotHookOutput{PermissionDecision: "allow"})
	}

	// Soft nudge: if a native tool is being used for a large-output command,
	// emit additionalContext. Claude Code surfaces this to the model; Codex
	// ignores it (safe — no functional change for Codex).
	if hint := routeNativeToolUsage(input.ToolName, cmd); hint != "" {
		return writeJSON(w, CodexHookOutput{
			HookSpecificOutput: CodexSpecificOutput{
				HookEventName:     "PreToolUse",
				AdditionalContext: hint,
			},
		})
	}

	return allowPassthrough(w, "PreToolUse")
}

func routeCopilotNativeTool(input HookInput, cmd string, largeThresholdBytes int) routingDecision {
	if strings.EqualFold(input.ToolName, "bash") && !isGitSafeCommand(cmd) && looksLargeOutput(cmd) {
		return routingDecision{
			allow:  false,
			reason: "Use ctx_execute for commands likely to produce large output, then inspect the stored result with ctx_search or ctx_get_section.",
		}
	}
	if !strings.EqualFold(input.ToolName, "view") || largeThresholdBytes <= 0 {
		return routingDecision{allow: true}
	}
	path := extractPath(input.ToolInput)
	if path == "" {
		return routingDecision{allow: true}
	}
	if _, ok := sourceExtensions[strings.ToLower(filepath.Ext(path))]; ok {
		return routingDecision{allow: true}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(resolveProjectPath(input.Cwd), path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= int64(largeThresholdBytes) {
		return routingDecision{allow: true}
	}
	return routingDecision{
		allow: false,
		reason: fmt.Sprintf(
			"This looks like a reference read of %s (%d bytes, above the %d-byte threshold). "+
				"Use ctx_read_file to cache it, then ctx_search or ctx_get_section to consult it.",
			path, info.Size(), largeThresholdBytes,
		),
	}
}

func extractPath(input map[string]any) string {
	for _, key := range []string{"path", "file_path", "filePath"} {
		if value, ok := input[key].(string); ok {
			return value
		}
	}
	return ""
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
	// No known shell field → return empty so routing defaults to allow.
	return ""
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
func readInput(r io.Reader) (HookInput, HostFormat, error) {
	var input HookInput
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return HookInput{}, HostClaudeCodex, fmt.Errorf("decoding hook input: %w", err)
	}
	return input, input.normalize(), nil
}

// writeJSON encodes v to w as a single JSON line followed by a newline.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
