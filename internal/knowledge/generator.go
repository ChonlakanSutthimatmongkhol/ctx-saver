package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// ErrThresholdNotMet is returned when the project has fewer sessions than required.
var ErrThresholdNotMet = errors.New("not enough data yet")

// Refresh generates (or updates) <projectPath>/.ctx-saver/project-knowledge.md.
// Returns ErrThresholdNotMet if session count < cfg.Knowledge.MinSessions.
func Refresh(ctx context.Context, st store.Store, projectPath string, cfg *config.Config) error {
	md, err := generate(ctx, st, projectPath, cfg)
	if err != nil {
		return err
	}

	dir := filepath.Join(projectPath, ".ctx-saver")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("generating knowledge: creating .ctx-saver dir: %w", err)
	}
	knowledgePath := filepath.Join(dir, "project-knowledge.md")
	if err := os.WriteFile(knowledgePath, []byte(md), 0600); err != nil {
		return fmt.Errorf("generating knowledge: writing file: %w", err)
	}
	return nil
}

// Show generates knowledge markdown and writes it to w without writing any file.
func Show(ctx context.Context, st store.Store, projectPath string, cfg *config.Config, w io.Writer) error {
	md, err := generate(ctx, st, projectPath, cfg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, md)
	return err
}

// generate fetches stats and renders the markdown string.
func generate(ctx context.Context, st store.Store, projectPath string, cfg *config.Config) (string, error) {
	data, err := st.KnowledgeStats(ctx, projectPath)
	if err != nil {
		return "", fmt.Errorf("generating knowledge: %w", err)
	}
	if data.SessionCount < cfg.Knowledge.MinSessions {
		return "", fmt.Errorf("%w (have %d sessions, need %d)",
			ErrThresholdNotMet, data.SessionCount, cfg.Knowledge.MinSessions)
	}
	return Render(data), nil
}
