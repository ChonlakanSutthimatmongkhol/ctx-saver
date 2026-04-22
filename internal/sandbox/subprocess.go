package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SubprocessSandbox executes commands as plain OS subprocesses.
// This is the Phase 1 implementation — no OS-level isolation.
type SubprocessSandbox struct {
	denyCommands []string
}

// NewSubprocess creates a SubprocessSandbox with the given deny-command patterns.
func NewSubprocess(denyCommands []string) *SubprocessSandbox {
	return &SubprocessSandbox{denyCommands: denyCommands}
}

// Execute runs req and returns the combined stdout+stderr.
// It honours req.Timeout and checks the command against the deny list before running.
func (s *SubprocessSandbox) Execute(ctx context.Context, req ExecuteRequest) (Result, error) {
	// For shell -c invocations the deny list should match the shell code string,
	// not the full "/bin/sh -c <code>" representation.
	if isShellC(req.Program, req.Args) {
		if err := s.checkDenyList(req.Args[1]); err != nil {
			return Result{}, err
		}
	} else {
		cmdStr := strings.Join(append([]string{req.Program}, req.Args...), " ")
		if err := s.checkDenyList(cmdStr); err != nil {
			return Result{}, err
		}
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, req.Program, req.Args...)
	if req.WorkDir != "" {
		cmd.Dir = req.WorkDir
	}
	if req.Stdin != nil {
		cmd.Stdin = bytes.NewReader(req.Stdin)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if runErr != nil {
		// Check timeout first: a killed process also returns *exec.ExitError, so
		// we must inspect the context error before the exit-code branch.
		if ctx.Err() == context.DeadlineExceeded {
			return Result{}, fmt.Errorf("command timed out after %s", timeout)
		}
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			// Non-zero exit is not a sandbox error — return it to the caller.
			exitCode = exitErr.ExitCode()
		} else {
			return Result{}, fmt.Errorf("executing command: %w", runErr)
		}
	}

	output := buf.Bytes()

	// Refuse binary output (null bytes) — it would corrupt the SQLite text column
	// and is almost never useful in an AI context.
	if bytes.IndexByte(output, 0) >= 0 {
		return Result{}, fmt.Errorf("command produced binary output; only text output is supported")
	}

	return Result{
		Output:   output,
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

// isShellC returns true when the invocation is /bin/sh -c <code>.
func isShellC(program string, args []string) bool {
	return (program == "/bin/sh" || program == "sh") &&
		len(args) >= 2 && args[0] == "-c"
}

// checkDenyList returns an error if any line of the command matches a deny pattern.
// Patterns support a trailing '*' wildcard (e.g. "sudo *", "dd if=*").
func (s *SubprocessSandbox) checkDenyList(command string) error {
	for _, line := range strings.Split(command, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, pattern := range s.denyCommands {
			if matchesDenyPattern(line, pattern) {
				return fmt.Errorf("command is blocked by deny list (pattern %q)", pattern)
			}
		}
	}
	return nil
}

// matchesDenyPattern returns true if cmd matches pattern.
// A trailing '*' acts as a wildcard suffix; otherwise an exact-word-prefix match is used.
func matchesDenyPattern(cmd, pattern string) bool {
	cmd = strings.TrimSpace(cmd)
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(cmd, prefix)
	}
	// Exact match or word-prefix match (e.g. "rm -rf /" matches "rm -rf / --no-preserve-root").
	return cmd == pattern || strings.HasPrefix(cmd, pattern+" ")
}
