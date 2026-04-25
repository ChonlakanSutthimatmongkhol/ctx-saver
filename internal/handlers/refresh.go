package handlers

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// refreshOutput re-executes the command stored in out, updates the DB row in-place,
// and returns the refreshed Output. On any failure it logs and returns the original.
// Only shell commands are supported; other source kinds are returned unchanged.
func refreshOutput(ctx context.Context, st store.Store, sb sandbox.Sandbox, workdir string, out *store.Output) *store.Output {
	if sb == nil {
		return out
	}
	if !strings.HasPrefix(out.SourceKind, "shell:") {
		return out
	}

	// Strip [shell] prefix from stored command.
	code := out.Command
	if strings.HasPrefix(code, "[shell] ") {
		code = strings.TrimPrefix(code, "[shell] ")
	} else if strings.HasPrefix(code, "[") {
		end := strings.Index(code, "]")
		if end > 0 {
			code = strings.TrimSpace(code[end+1:])
		}
	}

	timeout := 60 * time.Second
	result, err := sb.Execute(ctx, sandbox.ExecuteRequest{
		Program: "/bin/sh",
		Args:    []string{"-c", code},
		WorkDir: workdir,
		Timeout: timeout,
	})
	if err != nil {
		slog.Warn("auto-refresh execute failed", "output_id", out.OutputID, "err", err)
		return out
	}

	updated := *out
	updated.FullOutput = string(result.Output)
	updated.SizeBytes = int64(len(result.Output))
	updated.LineCount = countLines(updated.FullOutput)
	updated.ExitCode = result.ExitCode
	updated.DurationMs = result.Duration.Milliseconds()
	updated.RefreshedAt = time.Now()

	if err := st.UpdateRefreshed(ctx, &updated); err != nil {
		slog.Warn("auto-refresh store update failed", "output_id", out.OutputID, "err", err)
		return out
	}
	return &updated
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
}
