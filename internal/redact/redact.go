// Package redact removes well-known secret patterns from command output before
// it is summarised, returned to the model, or stored in SQLite.
package redact

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
)

// genericAssignmentRule is the name of the key=value rule, handled specially so
// the key and delimiter survive and only the value is replaced.
const genericAssignmentRule = "generic_assignment"

// Rule is one named redaction pattern.
type Rule struct {
	Name string
	Re   *regexp.Regexp
}

// ExtraPattern is a user-defined rule supplied via config.
type ExtraPattern struct {
	Name  string
	Regex string
}

// defaultRules are compiled once at package init. Order matters: more specific
// patterns run first so the generic assignment rule doesn't shadow them. The
// expensive (?s) private-key block runs first.
var defaultRules = []Rule{
	{"private_key_block", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)},
	{"aws_access_key", regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"github_token", regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b|\bgithub_pat_[A-Za-z0-9_]{22,}\b`)},
	{"gitlab_token", regexp.MustCompile(`\bglpat-[A-Za-z0-9\-_]{20,}\b`)},
	{"slack_token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]{10,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b`)},
	{"bearer_token", regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-_\.=]{20,}`)},
	{genericAssignmentRule, regexp.MustCompile(`(?i)\b(password|passwd|secret|api[_-]?key|access[_-]?token|private[_-]?key)\b(["']?\s*[:=]\s*["']?)[^\s"',;]{6,}`)},
}

// Redactor applies a rule set to byte slices.
type Redactor struct {
	rules []Rule
}

// New builds a Redactor from the default rules plus user-supplied extras.
// Invalid or empty extra patterns are skipped and described in the returned
// warning list; a bad user regex must never fail server startup.
func New(extra []ExtraPattern) (*Redactor, []string) {
	rules := make([]Rule, len(defaultRules))
	copy(rules, defaultRules)

	var warnings []string
	for _, e := range extra {
		if e.Name == "" || e.Regex == "" {
			warnings = append(warnings, fmt.Sprintf("skipping redaction pattern with empty name or regex (name=%q)", e.Name))
			continue
		}
		re, err := regexp.Compile(e.Regex)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipping invalid redaction pattern %q: %v", e.Name, err))
			continue
		}
		rules = append(rules, Rule{Name: e.Name, Re: re})
	}
	return &Redactor{rules: rules}, warnings
}

// Redact replaces every match with "[REDACTED:<rule_name>]" and returns the
// redacted bytes plus the distinct rule names that fired (sorted, for stats).
// When nothing matches the original slice is returned unchanged (zero-cost on
// clean output). For genericAssignmentRule the key and delimiter are preserved
// and only the value part is replaced.
func (r *Redactor) Redact(b []byte) ([]byte, []string) {
	if len(b) == 0 {
		return b, nil
	}

	fired := map[string]struct{}{}
	out := b
	for _, rule := range r.rules {
		token := []byte("[REDACTED:" + rule.Name + "]")

		if rule.Name == genericAssignmentRule {
			out = rule.Re.ReplaceAllFunc(out, func(m []byte) []byte {
				sub := rule.Re.FindSubmatch(m)
				if sub == nil {
					return m
				}
				prefix := append([]byte{}, sub[1]...) // key
				prefix = append(prefix, sub[2]...)    // delimiter (+ optional quote)
				value := m[len(prefix):]
				// Don't re-redact a value a prior rule already replaced.
				if bytes.HasPrefix(value, []byte("[REDACTED:")) {
					return m
				}
				fired[rule.Name] = struct{}{}
				return append(prefix, token...)
			})
			continue
		}

		if rule.Re.Match(out) {
			fired[rule.Name] = struct{}{}
			out = rule.Re.ReplaceAll(out, token)
		}
	}

	if len(fired) == 0 {
		return b, nil
	}
	names := make([]string, 0, len(fired))
	for n := range fired {
		names = append(names, n)
	}
	sort.Strings(names)
	return out, names
}
