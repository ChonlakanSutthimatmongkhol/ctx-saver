package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// SRTSandbox wraps commands with the Anthropic sandbox-runtime (srt) tool.
type SRTSandbox struct {
	srtPath      string
	denyCommands []string
}

// srtSettings is serialised to a temp JSON file consumed by the srt binary.
type srtSettings struct {
	DenyCommands []string `json:"deny_commands,omitempty"`
}

// NewSRT creates an SRTSandbox. Returns an error if the srt binary is not found in PATH.
func NewSRT(denyCommands []string) (*SRTSandbox, error) {
	path, err := exec.LookPath("srt")
	if err != nil {
		return nil, fmt.Errorf("srt binary not found in PATH: %w", err)
	}
	return &SRTSandbox{srtPath: path, denyCommands: denyCommands}, nil
}

// Execute implements Sandbox by wrapping the command with the srt binary.
// It writes a temporary srt-settings.json, invokes
// `srt run --settings <file> -- <program> [args...]`, then removes the temp file.
func (s *SRTSandbox) Execute(ctx context.Context, req ExecuteRequest) (Result, error) {
	settingsFile, err := s.writeTempSettings()
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(settingsFile)

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	srtArgs := append([]string{"run", "--settings", settingsFile, "--", req.Program}, req.Args...)
	cmd := exec.CommandContext(ctx, s.srtPath, srtArgs...)
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
		if ctx.Err() == context.DeadlineExceeded {
			return Result{}, fmt.Errorf("command timed out after %s", timeout)
		}
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return Result{}, fmt.Errorf("executing command via srt: %w", runErr)
		}
	}

	output := buf.Bytes()
	if bytes.IndexByte(output, 0) >= 0 {
		return Result{}, fmt.Errorf("command produced binary output; only text output is supported")
	}

	return Result{
		Output:   output,
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

func (s *SRTSandbox) writeTempSettings() (string, error) {
	data, err := json.Marshal(srtSettings{DenyCommands: s.denyCommands})
	if err != nil {
		return "", fmt.Errorf("marshalling srt settings: %w", err)
	}
	f, err := os.CreateTemp("", "srt-settings-*.json")
	if err != nil {
		return "", fmt.Errorf("creating srt settings file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("writing srt settings: %w", err)
	}
	return f.Name(), nil
}
