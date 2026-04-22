package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ResolveDataDir turns a relative DataDir into an absolute path anchored at
// projectPath and fills in the log file default when not set by the user.
// Call this in main.go after both cfg and projectPath are known.
func ResolveDataDir(cfg *Config, projectPath string) {
	if !filepath.IsAbs(cfg.Storage.DataDir) {
		cfg.Storage.DataDir = filepath.Join(projectPath, cfg.Storage.DataDir)
	}
	if cfg.Logging.File == "" {
		cfg.Logging.File = filepath.Join(cfg.Storage.DataDir, "server.log")
	}
}

// Config holds all runtime configuration for ctx-saver.
type Config struct {
	Sandbox      SandboxConfig  `yaml:"sandbox"`
	Storage      StorageConfig  `yaml:"storage"`
	Summary      SummaryConfig  `yaml:"summary"`
	Logging      LoggingConfig  `yaml:"logging"`
	DenyCommands []string       `yaml:"deny_commands"`
}

// SandboxConfig controls how commands are executed.
type SandboxConfig struct {
	Type           string `yaml:"type"`            // subprocess | srt
	TimeoutSeconds int    `yaml:"timeout_seconds"` // default 60
	UseSRT         bool   `yaml:"use_srt"`         // Phase 2
}

// StorageConfig controls the SQLite database location and retention.
type StorageConfig struct {
	DataDir         string `yaml:"data_dir"`
	RetentionDays   int    `yaml:"retention_days"`
	MaxOutputSizeMB int    `yaml:"max_output_size_mb"`
}

// SummaryConfig controls how outputs are summarised before returning to the AI.
type SummaryConfig struct {
	HeadLines               int `yaml:"head_lines"`
	TailLines               int `yaml:"tail_lines"`
	AutoIndexThresholdBytes int `yaml:"auto_index_threshold_bytes"` // outputs larger than this are stored
}

// LoggingConfig controls structured logging.
type LoggingConfig struct {
	Level string `yaml:"level"` // debug | info | warn | error
	File  string `yaml:"file"`
}

// Default returns a Config populated with safe defaults.
// DataDir defaults to ".ctx-saver" — a relative path resolved against the project
// working directory at startup (see main.go).  Set an absolute path in
// ~/.config/ctx-saver/config.yaml to revert to a central store.
func Default() *Config {
	return &Config{
		Sandbox: SandboxConfig{
			Type:           "subprocess",
			TimeoutSeconds: 60,
			UseSRT:         false,
		},
		Storage: StorageConfig{
			DataDir:         ".ctx-saver",
			RetentionDays:   14,
			MaxOutputSizeMB: 50,
		},
		Summary: SummaryConfig{
			HeadLines:               20,
			TailLines:               5,
			AutoIndexThresholdBytes: 5120,
		},
		Logging: LoggingConfig{
			Level: "info",
			File:  "", // resolved to <DataDir>/server.log after projectPath is known
		},
		DenyCommands: []string{
			"rm -rf /",
			"sudo *",
			"dd if=*",
		},
	}
}

// Load merges the global config file and any per-project override into a Config.
// Missing files are silently ignored; parse errors are returned.
func Load() (*Config, error) {
	cfg := Default()

	home, err := os.UserHomeDir()
	if err != nil {
		// Cannot find home dir; proceed with defaults.
		return cfg, nil
	}

	globalPath := filepath.Join(home, ".config", "ctx-saver", "config.yaml")
	if err := mergeFile(cfg, globalPath); err != nil {
		return nil, fmt.Errorf("loading global config %s: %w", globalPath, err)
	}

	if err := mergeFile(cfg, ".ctx-saver.yaml"); err != nil {
		return nil, fmt.Errorf("loading project config .ctx-saver.yaml: %w", err)
	}

	// Expand leading ~ in path fields.
	cfg.Storage.DataDir = expandHome(cfg.Storage.DataDir)
	cfg.Logging.File = expandHome(cfg.Logging.File)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

// mergeFile unmarshals a YAML file on top of dst.  Non-existent files are a no-op.
func mergeFile(dst *Config, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return yaml.Unmarshal(data, dst)
}

// expandHome replaces a leading ~/ with the user's home directory.
func expandHome(path string) string {
	if len(path) >= 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// validate checks that config values are within acceptable ranges.
func validate(cfg *Config) error {
	if cfg.Sandbox.TimeoutSeconds <= 0 {
		return fmt.Errorf("sandbox.timeout_seconds must be > 0, got %d", cfg.Sandbox.TimeoutSeconds)
	}
	if cfg.Storage.RetentionDays <= 0 {
		return fmt.Errorf("storage.retention_days must be > 0, got %d", cfg.Storage.RetentionDays)
	}
	if cfg.Storage.MaxOutputSizeMB <= 0 {
		return fmt.Errorf("storage.max_output_size_mb must be > 0, got %d", cfg.Storage.MaxOutputSizeMB)
	}
	if cfg.Summary.HeadLines <= 0 {
		return fmt.Errorf("summary.head_lines must be > 0, got %d", cfg.Summary.HeadLines)
	}
	if cfg.Summary.TailLines < 0 {
		return fmt.Errorf("summary.tail_lines must be >= 0, got %d", cfg.Summary.TailLines)
	}
	if cfg.Summary.AutoIndexThresholdBytes <= 0 {
		return fmt.Errorf("summary.auto_index_threshold_bytes must be > 0, got %d", cfg.Summary.AutoIndexThresholdBytes)
	}
	return nil
}
