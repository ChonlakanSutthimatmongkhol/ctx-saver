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
	regexp.MustCompile(`(?i)\brm\s+-[rRfF]+\s`),         // rm -rf, rm -fr, rm -RF, etc.
	regexp.MustCompile(`(?i)\brm\s+-[^\s]*r[^\s]*\s+/`), // rm -r*/path
	regexp.MustCompile(`(?i)\brm\s+-rf\b`),
	regexp.MustCompile(`(?i)\bdd\b.*\bof=/dev/(s|h|v|xv)d`),
	regexp.MustCompile(`(?i):\(\)\s*\{.*:\|:.*\}`),  // fork bomb
	regexp.MustCompile(`(?i)>\s*/dev/(sd|hd|nvme)`), // zero disk via redirect
	regexp.MustCompile(`(?i)\bfind\s+/.*-delete\b`), // find-based deletion

	// Shell injection via pipes to interpreters
	regexp.MustCompile(`(?i)\|\s*(sh|bash|zsh|fish|dash)\b`),
	regexp.MustCompile(`(?i)\bcurl\b[^|]*\|\s*(sh|bash|zsh)\b`),
	regexp.MustCompile(`(?i)\bwget\b[^|]*\|\s*(sh|bash|zsh)\b`),
	regexp.MustCompile(`(?i)\beval\b`), // all eval forms

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

// redirectRule pairs a detection pattern with a human-readable MCP suggestion.
type redirectRule struct {
	name       string
	pattern    *regexp.Regexp
	suggestion string
}

// curlSafePatterns matches curl invocations that produce small/no output and
// should NOT be redirected to ctx_execute.
var curlSafePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bcurl\s+--version\b`),
	regexp.MustCompile(`(?i)\bcurl\s+-I\b`),
	regexp.MustCompile(`(?i)\bcurl\s+--head\b`),
	regexp.MustCompile(`(?i)\bcurl\s+-o\s+/dev/null\b`),
}

// isSafeCurl reports whether cmd is a curl invocation that is expected to
// produce negligible output (version check, HEAD request, discarded output).
func isSafeCurl(cmd string) bool {
	for _, re := range curlSafePatterns {
		if re.MatchString(cmd) {
			return true
		}
	}
	return false
}

var redirectPatterns = []redirectRule{
	{
		name:       "curl",
		pattern:    regexp.MustCompile(`(?i)\bcurl\b`),
		suggestion: "Use ctx_execute with lang=shell and the same curl command to sandbox the response.",
	},
	{
		name:       "wget",
		pattern:    regexp.MustCompile(`(?i)\bwget\b`),
		suggestion: "Use ctx_execute with lang=shell and the same wget command to sandbox the response.",
	},
	{
		name:       "cat-large",
		pattern:    regexp.MustCompile(`(?i)\bcat\s+\S+\.(log|json|csv|xml|html)\b`),
		suggestion: "Use ctx_read_file to read the file — large files are summarised automatically.",
	},
	{
		name:       "find",
		pattern:    regexp.MustCompile(`(?i)\bfind\s+\S+\s+-type\s+f\b`),
		suggestion: "Use ctx_execute with lang=shell and the same find command to capture output safely.",
	},
	{
		name:       "logs",
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
		// Safe curl variants (--version, -I, --head, -o /dev/null) are never redirected.
		if rule.name == "curl" && isSafeCurl(cmd) {
			continue
		}
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
