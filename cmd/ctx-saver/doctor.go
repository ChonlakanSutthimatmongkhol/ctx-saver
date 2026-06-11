package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/version"
)

var expectedMCPTools = []string{
	"ctx_execute",
	"ctx_read_file",
	"ctx_search",
	"ctx_get_full",
	"ctx_outline",
	"ctx_stats",
	"ctx_get_section",
	"ctx_session_init",
	"ctx_note",
	"ctx_purge",
}

type mcpFileConfig struct {
	Servers map[string]mcpServerConfig `json:"servers"`
}

type mcpServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type doctorReporter struct {
	w        io.Writer
	failures int
}

func (r *doctorReporter) pass(format string, args ...any) {
	fmt.Fprintf(r.w, "PASS  "+format+"\n", args...)
}

func (r *doctorReporter) warn(format string, args ...any) {
	fmt.Fprintf(r.w, "WARN  "+format+"\n", args...)
}

func (r *doctorReporter) fail(format string, args ...any) {
	r.failures++
	fmt.Fprintf(r.w, "FAIL  "+format+"\n", args...)
}

func runDoctor() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	return doctorProject(cwd, os.Stdout)
}

func doctorProject(projectPath string, w io.Writer) error {
	reporter := &doctorReporter{w: w}
	fmt.Fprintf(w, "ctx-saver doctor %s\n", version.Version)

	if path, err := exec.LookPath("ctx-saver"); err != nil {
		reporter.warn("ctx-saver is not on PATH; hooks using the bare command may fail")
	} else {
		reporter.pass("PATH resolves ctx-saver to %s", path)
	}

	serverCfg, configOK := checkMCPConfig(projectPath, reporter)
	if configOK {
		checkConfiguredVersion(projectPath, serverCfg, reporter)
		checkMCPTools(projectPath, serverCfg, reporter)
	}
	checkInstructions(projectPath, reporter)
	checkHooks(projectPath, reporter)

	if reporter.failures > 0 {
		return fmt.Errorf("doctor found %d blocking problem(s)", reporter.failures)
	}
	fmt.Fprintln(w, "PASS  Local configuration is healthy.")
	fmt.Fprintln(w, "      If Copilot still cannot see ctx_stats: Developer: Reload Window,")
	fmt.Fprintln(w, "      restart ctx-saver in MCP: List Servers, open a new chat, then call")
	fmt.Fprintln(w, `      tool_search("ctx_stats ctx_session_init ctx_execute ctx_read_file")`)
	return nil
}

func checkMCPConfig(projectPath string, reporter *doctorReporter) (mcpServerConfig, bool) {
	path := filepath.Join(projectPath, ".vscode", "mcp.json")
	data, err := os.ReadFile(path)
	if err != nil {
		reporter.fail("cannot read %s: %v", path, err)
		return mcpServerConfig{}, false
	}
	var cfg mcpFileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		reporter.fail("invalid MCP config %s: %v", path, err)
		return mcpServerConfig{}, false
	}
	serverCfg, ok := cfg.Servers["ctx-saver"]
	if !ok || strings.TrimSpace(serverCfg.Command) == "" {
		reporter.fail("%s has no servers.ctx-saver.command", path)
		return mcpServerConfig{}, false
	}
	resolved, err := resolveConfiguredCommand(projectPath, serverCfg.Command)
	if err != nil {
		reporter.fail("configured ctx-saver binary %q is unavailable: %v", serverCfg.Command, err)
		return mcpServerConfig{}, false
	}
	serverCfg.Command = resolved
	reporter.pass("MCP config points to %s", resolved)
	return serverCfg, true
}

func resolveConfiguredCommand(projectPath, command string) (string, error) {
	if filepath.IsAbs(command) {
		if _, err := os.Stat(command); err != nil {
			return "", err
		}
		return command, nil
	}
	if strings.ContainsRune(command, filepath.Separator) {
		command = filepath.Join(projectPath, command)
		if _, err := os.Stat(command); err != nil {
			return "", err
		}
		return command, nil
	}
	return exec.LookPath(command)
}

func checkConfiguredVersion(projectPath string, cfg mcpServerConfig, reporter *doctorReporter) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.Command, "--version")
	cmd.Dir = projectPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		reporter.fail("configured binary cannot report its version: %v", err)
		return
	}
	got := strings.TrimSpace(string(output))
	if got != "ctx-saver "+version.Version {
		reporter.fail("configured binary version is %q; expected ctx-saver %s", got, version.Version)
		return
	}
	reporter.pass("configured binary version is %s", version.Version)
}

func checkMCPTools(projectPath string, cfg mcpServerConfig, reporter *doctorReporter) {
	tools, err := listConfiguredTools(projectPath, cfg)
	if err != nil {
		reporter.fail("MCP tools/list failed: %v", err)
		return
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	missing := missingToolNames(names, expectedMCPTools)
	if len(tools) != len(expectedMCPTools) || len(missing) > 0 {
		reporter.fail("MCP exposed %d tools; expected 10 (missing: %s)", len(tools), strings.Join(missing, ", "))
		return
	}
	reporter.pass("MCP tools/list exposed all 10 tools, including ctx_stats and ctx_session_init")
}

func listConfiguredTools(projectPath string, cfg mcpServerConfig) ([]*mcp.Tool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "ctx-saver-doctor-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = tempDir
	client := mcp.NewClient(&mcp.Implementation{Name: "ctx-saver-doctor", Version: version.Version}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}, nil)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func missingToolNames(got, expected []string) []string {
	have := make(map[string]bool, len(got))
	for _, name := range got {
		have[name] = true
	}
	var missing []string
	for _, name := range expected {
		if !have[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func checkInstructions(projectPath string, reporter *doctorReporter) {
	path := filepath.Join(projectPath, ".github", "copilot-instructions.md")
	data, err := os.ReadFile(path)
	if err != nil {
		reporter.warn("Copilot instructions missing at %s", path)
		return
	}
	content := string(data)
	var missing []string
	for _, required := range []string{"ctx_session_init", "tool_search"} {
		if !strings.Contains(content, required) {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		reporter.warn("Copilot instructions are missing: %s", strings.Join(missing, ", "))
		return
	}
	reporter.pass("Copilot instructions contain ctx_session_init and tool_search fallback")
}

func checkHooks(projectPath string, reporter *doctorReporter) {
	paths := []string{filepath.Join(projectPath, ".github", "hooks", "ctx-saver.json")}
	if copilotHome := os.Getenv("COPILOT_HOME"); copilotHome != "" {
		paths = append(paths, filepath.Join(copilotHome, "hooks", "ctx-saver.json"))
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".copilot", "hooks", "ctx-saver.json"))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			reporter.warn("Copilot hooks config is invalid at %s: %v", path, err)
			return
		}
		reporter.pass("Copilot hooks found at %s", path)
		return
	}
	reporter.warn("Copilot hooks not found; run `ctx-saver setup copilot`")
}
