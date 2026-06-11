package redact_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/redact"
)

// A structurally valid dummy JWT (header.payload.signature), not a real token.
const dummyJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
	"eyJzdWIiOiIxMjM0NTY3ODkwIn0." +
	"dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"

const dummyPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAabcdef1234567890
ghijklmnopqrstuvwxyz0987654321
-----END RSA PRIVATE KEY-----`

func TestRedact_DefaultRules_Positive(t *testing.T) {
	r, warnings := redact.New(nil)
	require.Empty(t, warnings)

	cases := []struct {
		name     string
		input    string
		wantRule string
	}{
		{"aws_access_key", "key=AKIAIOSFODNN7EXAMPLE done", "aws_access_key"},
		{"github_pat_classic", "token ghp_" + strings.Repeat("a", 36) + " end", "github_token"},
		{"github_pat_fine", "github_pat_" + strings.Repeat("b", 30) + " end", "github_token"},
		{"gitlab_token", "glpat-" + strings.Repeat("c", 20) + " x", "gitlab_token"},
		{"slack_token", "xoxb-1234567890-abcdEFGH x", "slack_token"},
		{"jwt", "auth " + dummyJWT + " ok", "jwt"},
		{"bearer", "Authorization: Bearer " + strings.Repeat("Z", 24), "bearer_token"},
		{"private_key", dummyPrivateKey, "private_key_block"},
		{"generic_password", "password=hunter22", "generic_assignment"},
		{"generic_apikey_json", `"api_key": "abc123def456"`, "generic_assignment"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, rules := r.Redact([]byte(tc.input))
			assert.Contains(t, rules, tc.wantRule, "expected rule to fire")
			assert.Contains(t, string(out), "[REDACTED:"+tc.wantRule+"]")
			// The original secret body must not survive verbatim.
			assert.NotContains(t, string(out), "AKIAIOSFODNN7EXAMPLE")
			assert.NotContains(t, string(out), "hunter22")
		})
	}
}

func TestRedact_GenericAssignment_KeepsKey(t *testing.T) {
	r, _ := redact.New(nil)
	out, rules := r.Redact([]byte("password=hunter22"))
	assert.Equal(t, []string{"generic_assignment"}, rules)
	assert.Equal(t, "password=[REDACTED:generic_assignment]", string(out))
}

func TestRedact_NoFalsePositives(t *testing.T) {
	r, _ := redact.New(nil)
	cases := []struct {
		name  string
		input string
	}{
		{"sha256_hex", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
		{"git_commit", "commit a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0"},
		{"uuid", "id=550e8400-e29b-41d4-a716-446655440000"},
		{"word_secretary", "she is the secretary of state"},
		{"password_too_short", "password=short"},
		{"password_empty", "password= "},
		{"plain_prose", "the build succeeded with no errors at all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, rules := r.Redact([]byte(tc.input))
			assert.Empty(t, rules, "no rule should fire for %q", tc.input)
			assert.Equal(t, tc.input, string(out))
		})
	}
}

func TestRedact_NoMatchReturnsIdenticalSlice(t *testing.T) {
	r, _ := redact.New(nil)
	in := []byte("nothing secret here, just logs")
	out, rules := r.Redact(in)
	assert.Nil(t, rules)
	require.Equal(t, len(in), len(out))
	assert.Equal(t, string(in), string(out))
}

func TestRedact_MultipleRulesSorted(t *testing.T) {
	r, _ := redact.New(nil)
	in := "aws AKIAIOSFODNN7EXAMPLE and jwt " + dummyJWT
	_, rules := r.Redact([]byte(in))
	assert.Equal(t, []string{"aws_access_key", "jwt"}, rules, "rule names must be sorted + distinct")
}

func TestRedact_ExtraPattern(t *testing.T) {
	r, warnings := redact.New([]redact.ExtraPattern{
		{Name: "internal_id", Regex: `INT-[0-9]{6}`},
	})
	require.Empty(t, warnings)
	out, rules := r.Redact([]byte("ticket INT-123456 closed"))
	assert.Contains(t, rules, "internal_id")
	assert.Contains(t, string(out), "[REDACTED:internal_id]")
}

func TestRedact_InvalidExtraPatternSkipped(t *testing.T) {
	r, warnings := redact.New([]redact.ExtraPattern{
		{Name: "broken", Regex: `([unclosed`},
		{Name: "", Regex: `x`},
		{Name: "good", Regex: `SECRETCODE`},
	})
	require.Len(t, warnings, 2, "both the bad regex and the empty-name pattern warn")
	// The valid extra pattern still works; startup did not fail.
	out, rules := r.Redact([]byte("here is SECRETCODE in the log"))
	assert.Contains(t, rules, "good")
	assert.Contains(t, string(out), "[REDACTED:good]")
}

func TestRedact_EmptyInput(t *testing.T) {
	r, _ := redact.New(nil)
	out, rules := r.Redact([]byte{})
	assert.Empty(t, rules)
	assert.Empty(t, out)
}
