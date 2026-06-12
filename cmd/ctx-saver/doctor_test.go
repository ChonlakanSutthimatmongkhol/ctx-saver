package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctor_MissingConfigFails(t *testing.T) {
	var output bytes.Buffer
	err := doctorProject(t.TempDir(), &output)
	require.Error(t, err)
	assert.Contains(t, output.String(), "FAIL")
	assert.Contains(t, output.String(), ".vscode")
}

func TestDoctor_MalformedConfigFails(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".vscode"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".vscode", "mcp.json"), []byte("{broken"), 0644))
	var output bytes.Buffer
	require.Error(t, doctorProject(dir, &output))
	assert.Contains(t, output.String(), "invalid MCP config")
}

func TestDoctor_WrongBinaryFails(t *testing.T) {
	dir := t.TempDir()
	writeMCPConfig(t, dir, filepath.Join(dir, "missing"))
	var output bytes.Buffer
	require.Error(t, doctorProject(dir, &output))
	assert.Contains(t, output.String(), "unavailable")
}

func TestDoctor_VersionMismatchFails(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "old-ctx-saver")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\necho 'ctx-saver 0.12.0'\n"), 0755))
	writeMCPConfig(t, dir, binary)

	var output bytes.Buffer
	require.Error(t, doctorProject(dir, &output))
	assert.Contains(t, output.String(), "expected ctx-saver 0.14.0")
}

func TestDoctor_MissingInstructionsAndHooksAreWarnings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COPILOT_HOME", filepath.Join(dir, "copilot-home"))
	var output bytes.Buffer
	reporter := &doctorReporter{w: &output}

	checkInstructions(dir, reporter)
	checkHooks(dir, reporter)

	assert.Zero(t, reporter.failures)
	assert.Contains(t, output.String(), "instructions missing")
	assert.Contains(t, output.String(), "hooks not found")
}

func TestDoctorSmokeConfiguredBinary(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ctx-saver")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = "."
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))

	project := filepath.Join(dir, "project")
	require.NoError(t, os.MkdirAll(filepath.Join(project, ".github"), 0755))
	writeMCPConfig(t, project, binary)
	require.NoError(t, os.WriteFile(
		filepath.Join(project, ".github", "copilot-instructions.md"),
		[]byte("call ctx_session_init; fallback tool_search"),
		0644,
	))
	copilotHome := filepath.Join(dir, "copilot-home")
	t.Setenv("COPILOT_HOME", copilotHome)
	require.NoError(t, os.MkdirAll(filepath.Join(copilotHome, "hooks"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(copilotHome, "hooks", "ctx-saver.json"),
		[]byte(`{"version":1,"hooks":{}}`),
		0644,
	))

	var doctorOutput bytes.Buffer
	require.NoError(t, doctorProject(project, &doctorOutput), doctorOutput.String())
	assert.Contains(t, doctorOutput.String(), "all 10 tools")
	assert.Contains(t, doctorOutput.String(), "ctx_stats")
}

func writeMCPConfig(t *testing.T, project, command string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(project, ".vscode"), 0755))
	data, err := json.Marshal(map[string]any{
		"servers": map[string]any{
			"ctx-saver": map[string]any{"command": command},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(project, ".vscode", "mcp.json"), data, 0644))
}
