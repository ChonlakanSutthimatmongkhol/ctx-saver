package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/knowledge"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// runKnowledge handles the `ctx-saver knowledge <action> [flags]` subcommand.
func runKnowledge(args []string) error {
	fs := flag.NewFlagSet("knowledge", flag.ContinueOnError)
	project := fs.String("project", "", "project root (default: cwd)")
	minSessions := fs.Int("min-sessions", 0, "override minimum session threshold")
	dryRun := fs.Bool("dry-run", false, "print what would be written without writing")
	quiet := fs.Bool("quiet", false, "suppress all output except errors")

	if err := fs.Parse(args); err != nil {
		return err
	}

	action := "refresh"
	if fs.NArg() > 0 {
		action = fs.Arg(0)
	}

	projectPath := *project
	if projectPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("knowledge: getting working directory: %w", err)
		}
		projectPath = cwd
	}
	projectPath = filepath.Clean(projectPath)

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	config.ResolveDataDir(cfg, projectPath)

	if *minSessions > 0 {
		cfg.Knowledge.MinSessions = *minSessions
	}

	st, err := store.NewSQLiteStore(cfg.Storage.DataDir, projectPath)
	if err != nil {
		return fmt.Errorf("knowledge: opening store: %w", err)
	}
	defer st.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch action {
	case "refresh":
		return runKnowledgeRefresh(ctx, st, projectPath, cfg, *dryRun, *quiet)
	case "show":
		return knowledge.Show(ctx, st, projectPath, cfg, os.Stdout)
	case "reset":
		return runKnowledgeReset(projectPath, *quiet)
	default:
		printKnowledgeUsage()
		return fmt.Errorf("unknown action %q (want: refresh | show | reset)", action)
	}
}

func runKnowledgeRefresh(ctx context.Context, st store.Store, projectPath string, cfg *config.Config, dryRun, quiet bool) error {
	if dryRun {
		var buf fmt.Stringer
		_ = buf
		err := knowledge.Show(ctx, st, projectPath, cfg, os.Stdout)
		if err != nil && errors.Is(err, knowledge.ErrThresholdNotMet) {
			fmt.Fprintf(os.Stderr, "ctx-saver: %v\n", err)
			os.Exit(2)
		}
		return err
	}

	err := knowledge.Refresh(ctx, st, projectPath, cfg)
	if err != nil {
		if errors.Is(err, knowledge.ErrThresholdNotMet) {
			fmt.Fprintf(os.Stderr, "ctx-saver: %v\n", err)
			os.Exit(2)
		}
		return fmt.Errorf("knowledge refresh: %w", err)
	}

	if !quiet {
		knowledgePath := filepath.Join(projectPath, ".ctx-saver", "project-knowledge.md")
		fmt.Printf("Updated %s\n", knowledgePath)
	}
	return nil
}

func runKnowledgeReset(projectPath string, quiet bool) error {
	knowledgePath := filepath.Join(projectPath, ".ctx-saver", "project-knowledge.md")
	if err := os.Remove(knowledgePath); err != nil {
		if os.IsNotExist(err) {
			if !quiet {
				fmt.Println("project-knowledge.md does not exist, nothing to reset.")
			}
			return nil
		}
		return fmt.Errorf("knowledge reset: %w", err)
	}
	if !quiet {
		fmt.Printf("Deleted %s\n", knowledgePath)
	}
	return nil
}

func printKnowledgeUsage() {
	fmt.Fprintf(os.Stderr, `Usage: ctx-saver knowledge <action> [flags]

Actions:
  refresh   Generate or update .ctx-saver/project-knowledge.md
  show      Print current knowledge to stdout (no file write)
  reset     Delete .ctx-saver/project-knowledge.md

Flags:
  --project <path>      project root (default: cwd)
  --min-sessions <n>    override minimum session threshold (default: 3)
  --dry-run             print what would be written without writing
  --quiet               suppress all output except errors
`)
}
