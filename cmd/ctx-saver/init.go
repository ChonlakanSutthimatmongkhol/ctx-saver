package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/configs"
)

const knowledgeRefLine = "\n<!-- ctx-saver -->\n" +
	"See .ctx-saver/project-knowledge.md for learned project patterns " +
	"(auto-generated, refresh with `ctx-saver knowledge refresh`).\n"

// injectKnowledgeReference appends a one-line reference to project-knowledge.md
// into the file at path. It is idempotent: if the file already contains
// "project-knowledge.md" the function returns nil without modifying the file.
// If the file does not exist, the function returns nil silently.
func injectKnowledgeReference(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if strings.Contains(string(data), "project-knowledge.md") {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprint(f, knowledgeRefLine)
	return err
}

func runInit(args []string) error {
	if len(args) == 0 {
		printInitUsage()
		return fmt.Errorf("platform required")
	}
	switch args[0] {
	case "claude":
		return initClaude()
	case "copilot":
		return initCopilot()
	case "copilot-instructions":
		return initCopilotInstructions()
	case "codex":
		return initCodex()
	case "agents-md":
		return initAgentsMd()
	default:
		printInitUsage()
		return fmt.Errorf("unknown platform %q", args[0])
	}
}

func printInitUsage() {
	fmt.Fprintf(os.Stderr, `Usage: ctx-saver init <platform>

Platforms:
  claude                — Install hooks into ~/.claude/settings.json
  copilot               — Install MCP server into .vscode/mcp.json (current directory)
  copilot-instructions  — Install .github/copilot-instructions.md (current directory)
  codex                 — Install MCP server + hooks into ~/.codex/
  agents-md             — Install AGENTS.md (current directory)
`)
}

func initClaude() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding binary path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}
	target := filepath.Join(home, ".claude", "settings.json")

	patch := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash|Shell",
					"hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook pretooluse"},
					},
				},
			},
			"PostToolUse": []any{
				map[string]any{
					"matcher": ".*",
					"hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook posttooluse"},
					},
				},
			},
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": bin + " hook sessionstart"},
					},
				},
			},
		},
	}

	fmt.Printf("Installing ctx-saver hooks for Claude Code → %s\n", target)
	if err := mergeJSONFile(target, patch); err != nil {
		return err
	}
	fmt.Println("Done. Restart Claude Code to activate the hooks.")

	// Inject knowledge reference into CLAUDE.md (non-fatal).
	cwd, err := os.Getwd()
	if err == nil {
		if injErr := injectKnowledgeReference(filepath.Join(cwd, "CLAUDE.md")); injErr != nil {
			slog.Warn("could not update CLAUDE.md with knowledge reference", "error", injErr)
		}
	}
	return nil
}

func initCopilot() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding binary path: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	target := filepath.Join(cwd, ".vscode", "mcp.json")

	patch := map[string]any{
		"servers": map[string]any{
			"ctx-saver": map[string]any{"command": bin},
		},
	}

	fmt.Printf("Installing ctx-saver MCP server for VS Code Copilot → %s\n", target)
	if err := mergeJSONFile(target, patch); err != nil {
		return err
	}
	fmt.Println("Done. Reload VS Code to activate the MCP server.")
	return nil
}

func initCopilotInstructions() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	targetDir := filepath.Join(cwd, ".github")
	targetFile := filepath.Join(targetDir, "copilot-instructions.md")

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("creating .github directory: %w", err)
	}

	if _, err := os.Stat(targetFile); err == nil {
		fmt.Printf("  %s already exists.\n", targetFile)
		fmt.Print("  Append ctx-saver rules? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		confirm, _ := reader.ReadString('\n')
		confirm = strings.TrimSpace(confirm)
		if confirm != "y" && confirm != "Y" {
			fmt.Println("  Skipped.")
			return nil
		}
		f, err := os.OpenFile(targetFile, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("opening %s: %w", targetFile, err)
		}
		defer f.Close()
		if _, err := fmt.Fprintf(f, "\n---\n\n%s", configs.CopilotInstructionsTemplate); err != nil {
			return fmt.Errorf("appending to %s: %w", targetFile, err)
		}
		fmt.Printf("  Appended ctx-saver rules to %s\n", targetFile)
	} else {
		if err := os.WriteFile(targetFile, []byte(configs.CopilotInstructionsTemplate), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", targetFile, err)
		}
		fmt.Printf("  Created %s\n", targetFile)
	}

	// Inject knowledge reference into copilot-instructions.md (non-fatal).
	if injErr := injectKnowledgeReference(targetFile); injErr != nil {
		slog.Warn("could not update copilot-instructions.md with knowledge reference", "error", injErr)
	}

	fmt.Println("Done. Commit .github/copilot-instructions.md to share rules with your team.")
	return nil
}

func initCodex() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding binary path: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0700); err != nil {
		return fmt.Errorf("creating ~/.codex directory: %w", err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	if err := appendCodexMCPConfig(configPath, bin); err != nil {
		return err
	}

	hooksPath := filepath.Join(codexDir, "hooks.json")
	hookPatch := map[string]any{
		"PreToolUse": []any{
			map[string]any{"script": bin + " hook pretooluse"},
		},
		"PostToolUse": []any{
			map[string]any{"script": bin + " hook posttooluse"},
		},
		"SessionStart": []any{
			map[string]any{"script": bin + " hook sessionstart"},
		},
	}
	fmt.Printf("Installing ctx-saver hooks for Codex CLI → %s\n", hooksPath)
	if err := mergeJSONFile(hooksPath, hookPatch); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err == nil {
		if injErr := injectKnowledgeReference(filepath.Join(cwd, "AGENTS.md")); injErr != nil {
			slog.Warn("could not update AGENTS.md with knowledge reference", "error", injErr)
		}
	}

	fmt.Println("Done. Restart Codex CLI to activate.")
	return nil
}

func appendCodexMCPConfig(configPath, bin string) error {
	if data, err := os.ReadFile(configPath); err == nil {
		if strings.Contains(string(data), "[mcp_servers.ctx-saver]") {
			fmt.Printf("  ctx-saver MCP already configured in %s\n", configPath)
			return nil
		}
	}

	block := fmt.Sprintf("\n[mcp_servers.ctx-saver]\ncommand = %q\n", bin)

	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", configPath, err)
	}
	defer f.Close()

	if _, err := fmt.Fprint(f, block); err != nil {
		return fmt.Errorf("writing MCP config to %s: %w", configPath, err)
	}

	fmt.Printf("  written: %s\n", configPath)
	return nil
}

func initAgentsMd() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	targetFile := filepath.Join(cwd, "AGENTS.md")

	if _, err := os.Stat(targetFile); err == nil {
		fmt.Printf("  %s already exists.\n", targetFile)
		fmt.Print("  Append ctx-saver rules? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		confirm, _ := reader.ReadString('\n')
		confirm = strings.TrimSpace(confirm)
		if confirm != "y" && confirm != "Y" {
			fmt.Println("  Skipped.")
			return nil
		}
		f, err := os.OpenFile(targetFile, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("opening %s: %w", targetFile, err)
		}
		defer f.Close()
		if _, err := fmt.Fprintf(f, "\n---\n\n%s", configs.AgentsMdTemplate); err != nil {
			return fmt.Errorf("appending to %s: %w", targetFile, err)
		}
		fmt.Printf("  Appended ctx-saver rules to %s\n", targetFile)
	} else {
		if err := os.WriteFile(targetFile, []byte(configs.AgentsMdTemplate), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", targetFile, err)
		}
		fmt.Printf("  Created %s\n", targetFile)
	}

	if injErr := injectKnowledgeReference(targetFile); injErr != nil {
		slog.Warn("could not update AGENTS.md with knowledge reference", "error", injErr)
	}

	fmt.Println("Done. Commit AGENTS.md to share rules with your team.")
	return nil
}

// mergeJSONFile deep-merges patch into the JSON object at target.
// Creates target if it does not exist; backs up existing file to target.bak.
func mergeJSONFile(target string, patch map[string]any) error {
	base := map[string]any{}

	if data, err := os.ReadFile(target); err == nil {
		if err := json.Unmarshal(data, &base); err != nil {
			return fmt.Errorf("parsing existing %s: %w", target, err)
		}
		backup := target + ".bak"
		if err := os.WriteFile(backup, data, 0600); err != nil {
			return fmt.Errorf("creating backup %s: %w", backup, err)
		}
		fmt.Printf("  Backed up existing file to %s\n", backup)
	}

	merged := deepMerge(base, patch)
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	if err := os.WriteFile(target, append(out, '\n'), 0600); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}
	fmt.Printf("  written: %s\n", target)
	return nil
}

// deepMerge recursively merges patch into base. patch values win over base.
// Arrays are replaced entirely (not appended) — same behaviour as jq `*`.
func deepMerge(base, patch map[string]any) map[string]any {
	result := make(map[string]any, len(base))
	for k, v := range base {
		result[k] = v
	}
	for k, pv := range patch {
		if pm, ok := pv.(map[string]any); ok {
			if bm, ok := result[k].(map[string]any); ok {
				result[k] = deepMerge(bm, pm)
				continue
			}
		}
		result[k] = pv
	}
	return result
}
