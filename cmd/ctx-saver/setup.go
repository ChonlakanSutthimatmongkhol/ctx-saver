package main

import (
	"fmt"
	"os"
)

func runSetup(args []string) error {
	if len(args) == 0 || args[0] != "copilot" {
		fmt.Fprintln(os.Stderr, "Usage: ctx-saver setup copilot [--repo-hooks]")
		return fmt.Errorf("unknown setup target")
	}

	repoHooks := false
	for _, arg := range args[1:] {
		switch arg {
		case "--repo-hooks":
			if repoHooks {
				return fmt.Errorf("--repo-hooks specified more than once")
			}
			repoHooks = true
		default:
			return fmt.Errorf("unknown setup option %q", arg)
		}
	}

	fmt.Println("[1/3] Installing VS Code Copilot MCP configuration")
	if err := initCopilot(); err != nil {
		return fmt.Errorf("installing Copilot MCP configuration: %w", err)
	}

	fmt.Println("[2/3] Installing Copilot instructions")
	if err := installCopilotInstructions(false); err != nil {
		return fmt.Errorf("installing Copilot instructions: %w", err)
	}

	fmt.Println("[3/3] Installing Copilot hooks")
	hookArgs := []string(nil)
	if repoHooks {
		hookArgs = []string{"--repo"}
	}
	if err := initCopilotHooks(hookArgs); err != nil {
		return fmt.Errorf("installing Copilot hooks: %w", err)
	}

	fmt.Println("Copilot setup complete. Run `ctx-saver doctor`, then reload VS Code.")
	return nil
}
