package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/configs"
)

// T7: injectKnowledgeReference is idempotent — running twice must not duplicate.
func TestInjectKnowledgeReference_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# My project\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := injectKnowledgeReference(path); err != nil {
		t.Fatalf("first inject: %v", err)
	}
	if err := injectKnowledgeReference(path); err != nil {
		t.Fatalf("second inject: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	count := strings.Count(content, "project-knowledge.md")
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of 'project-knowledge.md', got %d\ncontent:\n%s", count, content)
	}
}

// T8: injectKnowledgeReference is a no-op when file doesn't exist.
func TestInjectKnowledgeReference_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := injectKnowledgeReference(path); err != nil {
		t.Fatalf("expected nil for missing file, got: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should not have been created")
	}
}

// Extra: injectKnowledgeReference appends the reference line.
func TestInjectKnowledgeReference_AppendsLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# My project\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := injectKnowledgeReference(path); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "project-knowledge.md") {
		t.Error("reference line not appended")
	}
}

// T1: appendCodexMCPConfig creates config.toml with [mcp_servers.ctx-saver] block.
func TestAppendCodexMCPConfig_Creates(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := appendCodexMCPConfig(configPath, "/usr/local/bin/ctx-saver"); err != nil {
		t.Fatalf("appendCodexMCPConfig: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[mcp_servers.ctx-saver]") {
		t.Errorf("config.toml missing [mcp_servers.ctx-saver]:\n%s", data)
	}
	if !strings.Contains(string(data), "/usr/local/bin/ctx-saver") {
		t.Errorf("config.toml missing binary path:\n%s", data)
	}
}

// T2: appendCodexMCPConfig is idempotent — running twice must not duplicate the block.
func TestAppendCodexMCPConfig_Idempotent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := appendCodexMCPConfig(configPath, "/usr/local/bin/ctx-saver"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := appendCodexMCPConfig(configPath, "/usr/local/bin/ctx-saver"); err != nil {
		t.Fatalf("second call: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	count := strings.Count(string(data), "[mcp_servers.ctx-saver]")
	if count != 1 {
		t.Errorf("expected exactly 1 [mcp_servers.ctx-saver] block, got %d\n%s", count, data)
	}
}

// T3: initAgentsMd creates AGENTS.md from AgentsMdTemplate.
func TestInitAgentsMd_Creates(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	if err := initAgentsMd(); err != nil {
		t.Fatalf("initAgentsMd: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), configs.AgentsMdTemplate[:50]) {
		t.Error("AGENTS.md does not contain template content")
	}
}

// T4: injectKnowledgeReference is idempotent on AGENTS.md — a second call must not
// increase the occurrence count (the template already mentions project-knowledge.md
// in Rule 8, so injectKnowledgeReference skips injection on its first call too).
func TestInitAgentsMd_KnowledgeRefIdempotent(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	if err := initAgentsMd(); err != nil {
		t.Fatalf("initAgentsMd: %v", err)
	}

	// Use canonical path (resolves macOS /var → /private/var symlink).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	agentsMdPath := filepath.Join(cwd, "AGENTS.md")

	data1, err := os.ReadFile(agentsMdPath)
	if err != nil {
		t.Fatal(err)
	}
	countBefore := strings.Count(string(data1), "project-knowledge.md")
	if countBefore == 0 {
		t.Fatal("expected project-knowledge.md to appear at least once after initAgentsMd")
	}

	// Second inject must be a no-op — count must not increase.
	if err := injectKnowledgeReference(agentsMdPath); err != nil {
		t.Fatalf("second injectKnowledgeReference: %v", err)
	}

	data2, err := os.ReadFile(agentsMdPath)
	if err != nil {
		t.Fatal(err)
	}
	countAfter := strings.Count(string(data2), "project-knowledge.md")
	if countAfter != countBefore {
		t.Errorf("injectKnowledgeReference added duplicates: count went from %d to %d", countBefore, countAfter)
	}
}

// T5: mergeJSONFile with Codex hook patch produces valid JSON with all 3 hook keys.
func TestMergeJSONFile_CodexHooks(t *testing.T) {
	dir := t.TempDir()
	hooksPath := filepath.Join(dir, "hooks.json")

	patch := map[string]any{
		"PreToolUse": []any{
			map[string]any{"script": "/usr/local/bin/ctx-saver hook pretooluse"},
		},
		"PostToolUse": []any{
			map[string]any{"script": "/usr/local/bin/ctx-saver hook posttooluse"},
		},
		"SessionStart": []any{
			map[string]any{"script": "/usr/local/bin/ctx-saver hook sessionstart"},
		},
	}

	if err := mergeJSONFile(hooksPath, patch); err != nil {
		t.Fatalf("mergeJSONFile: %v", err)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	for _, key := range []string{"PreToolUse", "PostToolUse", "SessionStart"} {
		if _, ok := result[key]; !ok {
			t.Errorf("hooks.json missing key %q", key)
		}
	}
}
