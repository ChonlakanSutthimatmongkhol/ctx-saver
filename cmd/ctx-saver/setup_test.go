package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupCopilot_PersonalDefaultAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	copilotHome := filepath.Join(dir, "copilot-home")
	t.Setenv("COPILOT_HOME", copilotHome)
	withWorkingDirectory(t, dir)

	require.NoError(t, runSetup([]string{"copilot"}))
	require.NoError(t, runSetup([]string{"copilot"}))

	assert.FileExists(t, filepath.Join(dir, ".vscode", "mcp.json"))
	assert.FileExists(t, filepath.Join(dir, ".github", "copilot-instructions.md"))
	assert.FileExists(t, filepath.Join(copilotHome, "hooks", "ctx-saver.json"))
}

func TestSetupCopilot_RepoHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COPILOT_HOME", filepath.Join(dir, "personal"))
	withWorkingDirectory(t, dir)

	require.NoError(t, runSetup([]string{"copilot", "--repo-hooks"}))
	assert.FileExists(t, filepath.Join(dir, ".github", "hooks", "ctx-saver.json"))
	assert.NoFileExists(t, filepath.Join(dir, "personal", "hooks", "ctx-saver.json"))
}

func TestSetupCopilot_PreservesExistingInstructions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COPILOT_HOME", filepath.Join(dir, "copilot-home"))
	withWorkingDirectory(t, dir)
	path := filepath.Join(dir, ".github", "copilot-instructions.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte("custom rules\n"), 0644))

	require.NoError(t, runSetup([]string{"copilot"}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "custom rules\n", string(data))
}

func TestSetupCopilot_InvalidExistingMCPStopsBeforeLaterSteps(t *testing.T) {
	dir := t.TempDir()
	copilotHome := filepath.Join(dir, "copilot-home")
	t.Setenv("COPILOT_HOME", copilotHome)
	withWorkingDirectory(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".vscode"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".vscode", "mcp.json"), []byte("{broken"), 0644))

	err := runSetup([]string{"copilot"})
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(dir, ".github", "copilot-instructions.md"))
	assert.NoFileExists(t, filepath.Join(copilotHome, "hooks", "ctx-saver.json"))
}

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(original) })
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var value map[string]any
	require.NoError(t, json.Unmarshal(data, &value))
	return value
}
