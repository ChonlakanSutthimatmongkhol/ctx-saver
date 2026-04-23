package sandbox_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSandbox(deny ...string) *sandbox.SubprocessSandbox {
	return sandbox.NewSubprocess(deny)
}

func TestSubprocess_SimpleCommand(t *testing.T) {
	sb := newSandbox()
	result, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "echo hello"},
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hello\n", string(result.Output))
}

func TestSubprocess_NonZeroExitCode(t *testing.T) {
	sb := newSandbox()
	result, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "exit 42"},
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err) // non-zero exit is not a sandbox error
	assert.Equal(t, 42, result.ExitCode)
}

func TestSubprocess_StderrCombined(t *testing.T) {
	sb := newSandbox()
	result, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "echo stdout; echo stderr >&2"},
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	assert.Contains(t, string(result.Output), "stdout")
	assert.Contains(t, string(result.Output), "stderr")
}

func TestSubprocess_Stdin(t *testing.T) {
	sb := newSandbox()
	result, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "cat"},
		Stdin:   []byte("hello from stdin"),
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "hello from stdin", string(result.Output))
}

func TestSubprocess_Timeout(t *testing.T) {
	sb := newSandbox()
	_, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "sleep 60"},
		Timeout: 100 * time.Millisecond,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestSubprocess_DenyList_ExactMatch(t *testing.T) {
	sb := newSandbox("rm -rf /")
	_, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "rm -rf /"},
		Timeout: 5 * time.Second,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deny list")
}

func TestSubprocess_DenyList_WildcardSuffix(t *testing.T) {
	sb := newSandbox("sudo *")
	_, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "sudo apt-get install curl"},
		Timeout: 5 * time.Second,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deny list")
}

func TestSubprocess_DenyList_AllowsNonMatchingCommands(t *testing.T) {
	sb := newSandbox("sudo *", "rm -rf /")
	result, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "echo allowed"},
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, "allowed\n", string(result.Output))
}

func TestSubprocess_LargeOutput(t *testing.T) {
	sb := newSandbox()
	// Generate ~50KB of output.
	result, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "python3 -c \"print('x'*80+'\\n'*1, end='')\" ; for i in $(seq 1 600); do echo \"$i: " + strings.Repeat("log line content ", 5) + "\"; done"},
		Timeout: 10 * time.Second,
	})
	require.NoError(t, err)
	assert.Greater(t, len(result.Output), 10000)
}

func TestSubprocess_WorkingDirectory(t *testing.T) {
	sb := newSandbox()
	result, err := sb.Execute(context.Background(), sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", "pwd"},
		WorkDir: "/tmp",
		Timeout: 5 * time.Second,
	})
	require.NoError(t, err)
	assert.Contains(t, strings.TrimSpace(string(result.Output)), "tmp")
}
