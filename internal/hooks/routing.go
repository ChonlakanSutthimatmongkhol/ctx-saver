package hooks

import (
	"regexp"
	"strings"
)

// routingDecision is the result of PreToolUse routing analysis.
type routingDecision struct {
	// allow is true when the tool call should proceed.
	allow bool
	// reason is a human-readable explanation for a deny decision.
	reason string
}

// ── Dangerous command patterns ────────────────────────────────────────────
// These commands are always denied regardless of context; they can cause
// irreversible damage or exfiltrate data.

var dangerousPatterns = []*regexp.Regexp{
	// Destructive disk operations
	regexp.MustCompile(`(?i)\brm\s+-[^\s]*r[^\s]*\s+/`),
	regexp.MustCompile(`(?i)\brm\s+-rf\b`),
	regexp.MustCompile(`(?i)\bdd\b.*\bof=/dev/(s|h|v|xv)d`),
	regexp.MustCompile(`(?i):\(\)\s*\{.*:\|:.*\}`), // fork bomb

	// Shell injection via pipes to interpreters
	regexp.MustCompile(`(?i)\|\s*(sh|bash|zsh|fish|dash)\b`),
	regexp.MustCompile(`(?i)\bcurl\b[^|]*\|\s*(sh|bash|zsh)\b`),
	regexp.MustCompile(`(?i)\bwget\b[^|]*\|\s*(sh|bash|zsh)\b`),
	regexp.MustCompile(`(?i)\beval\s+["'\x60]`),

	// Privilege escalation
	regexp.MustCompile(`(?i)\bsudo\s+(rm|dd|chmod 777|chown)\b`),
	regexp.MustCompile(`(?i)\bsudo\s+-s\b`),

	// Credential exfiltration
	regexp.MustCompile(`(?i)\bcat\s+.*\.env\b`),
	regexp.MustCompile(`(?i)\bcat\s+[/~].*?(id_rsa|id_ecdsa|id_ed25519|\.pem|\.key)\b`),
}

// ── Large-output redirect patterns ────────────────────────────────────────
// These commands produce large outputs that are better handled by ctx_execute
// to avoid flooding the context window.  We note the redirect suggestion in
// the deny reason so the model can retry with the appropriate MCP tool.

type redirectRule struct {
	pattern    *regexp.Regexp
	suggestion string
}

var redirectPatterns = []redirectRule{
	{
		pattern:    regexp.MustCompile(`(?i)\bcurl\b`),
		suggestion: "Use ctx_execute with lang=shell and the same curl command to sandbox the response.",
	},
	{
		pattern:    regexp.MustCompile(`(?i)\bwget\b`),
		suggestion: "Use ctx_execute with lang=shell and the same wget command to sandbox the response.",
	},
	{
		pattern:    regexp.MustCompile(`(?i)\bcat\s+\S+\.(log|json|csv|xml|html)\b`),
		suggestion: "Use ctx_read_file to read the file — large files are summarised automatically.",
	},
	{
		pattern:    regexp.MustCompile(`(?i)\bfind\s+\S+\s+-type\s+f\b`),
		suggestion: "Use ctx_execute with lang=shell and the same find command to capture output safely.",
	},
	{
		pattern:    regexp.MustCompile(`(?i)\bjournalctl\b|\bdmesg\b|\btail\s+-f\b`),
		suggestion: "Use ctx_execute with lang=shell for log commands — raw output is stored and summarised.",
	},
}

// routePreToolUse evaluates a tool call and returns a routing decision.
//
// tool is the tool name (e.g. "Shell", "Bash").
// cmd is the command string extracted from tool_input.
func routePreToolUse(tool, cmd string) routingDecision {
	// Only inspect shell / bash tool calls; other tools are always allowed.
	toolLower := strings.ToLower(tool)
	if toolLower != "shell" && toolLower != "bash" {
		return routingDecision{allow: true}
	}

	// Check dangerous patterns first (hard deny).
	for _, re := range dangerousPatterns {
		if re.MatchString(cmd) {
			return routingDecision{
				allow:  false,
				reason: "Blocked: the command matches a dangerous pattern (" + re.String()[:min(40, len(re.String()))] + "…). Use a safer alternative.",
			}
		}
	}

	// Check redirect patterns (soft deny with MCP tool suggestion).
	for _, rule := range redirectPatterns {
		if rule.pattern.MatchString(cmd) {
			return routingDecision{
				allow:  false,
				reason: "Redirected to MCP sandbox: " + rule.suggestion,
			}
		}
	}

	return routingDecision{allow: true}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
