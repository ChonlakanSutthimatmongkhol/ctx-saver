package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
)

func TestDefault_HasSaneValues(t *testing.T) {
	cfg := config.Default()

	assert.Equal(t, "subprocess", cfg.Sandbox.Type)
	assert.Equal(t, 60, cfg.Sandbox.TimeoutSeconds)
	assert.Equal(t, 14, cfg.Storage.RetentionDays)
	assert.Equal(t, 50, cfg.Storage.MaxOutputSizeMB)
	assert.Equal(t, 20, cfg.Summary.HeadLines)
	assert.Equal(t, 5, cfg.Summary.TailLines)
	assert.Equal(t, 5120, cfg.Summary.AutoIndexThresholdBytes)
	assert.NotEmpty(t, cfg.DenyCommands)
}

func TestLoad_NoConfigFile_UsesDefaults(t *testing.T) {
	// Temporarily change home to a directory without a config file.
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 60, cfg.Sandbox.TimeoutSeconds)
}

func TestLoad_GlobalConfig_MergesOverDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := filepath.Join(home, ".config", "ctx-saver")
	require.NoError(t, os.MkdirAll(cfgDir, 0755))

	yaml := `sandbox:
  timeout_seconds: 120
summary:
  head_lines: 30
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(yaml), 0600))

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 120, cfg.Sandbox.TimeoutSeconds)
	assert.Equal(t, 30, cfg.Summary.HeadLines)
	// Unset fields should retain defaults.
	assert.Equal(t, 14, cfg.Storage.RetentionDays)
}

func TestValidate_RejectsZeroTimeout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := filepath.Join(home, ".config", "ctx-saver")
	require.NoError(t, os.MkdirAll(cfgDir, 0755))

	yaml := `sandbox:
  timeout_seconds: 0
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(yaml), 0600))

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout_seconds")
}
