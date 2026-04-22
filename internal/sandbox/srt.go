package sandbox

import (
	"context"
	"fmt"
)

// SRTSandbox wraps commands with the Anthropic sandbox-runtime (srt) tool.
// This is a Phase 2 placeholder — not implemented yet.
type SRTSandbox struct{}

// NewSRT creates an SRTSandbox.  Returns an error if the srt binary is not found.
func NewSRT() (*SRTSandbox, error) {
	// TODO(Phase 2): detect srt binary with exec.LookPath("srt"),
	// build srt-settings.json from config, and wrap commands.
	return nil, fmt.Errorf("srt sandbox is not implemented yet (Phase 2)")
}

// Execute implements Sandbox.
func (s *SRTSandbox) Execute(_ context.Context, _ ExecuteRequest) (Result, error) {
	return Result{}, fmt.Errorf("srt sandbox is not implemented yet (Phase 2)")
}
