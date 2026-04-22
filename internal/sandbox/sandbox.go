// Package sandbox defines the interface for executing commands in an isolated environment.
package sandbox

import (
	"context"
	"time"
)

// ExecuteRequest carries the parameters for a single execution.
type ExecuteRequest struct {
	// Program is the executable to run (e.g. "/bin/sh").
	Program string
	// Args are the arguments passed to Program.
	Args []string
	// Stdin is optional data written to the process's standard input.
	Stdin []byte
	// WorkDir is the working directory.  Empty means the process inherits the
	// server's working directory.
	WorkDir string
	// Timeout is the maximum time the command may run before being killed.
	Timeout time.Duration
}

// Result holds the combined output and metadata of a completed execution.
type Result struct {
	// Output is combined stdout + stderr.
	Output []byte
	// ExitCode is the process exit code (0 = success).
	ExitCode int
	// Duration is the wall-clock time elapsed.
	Duration time.Duration
}

// Sandbox executes commands in an isolated environment.
// Phase 1 implementation is a plain OS subprocess; Phase 2 wraps with srt.
type Sandbox interface {
	Execute(ctx context.Context, req ExecuteRequest) (Result, error)
}
