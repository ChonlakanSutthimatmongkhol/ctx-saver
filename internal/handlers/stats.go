package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// StatsInput is the input for the ctx_stats MCP tool.
type StatsInput struct {
	Scope string `json:"scope,omitempty" jsonschema:"session | today | 7d | all (default: session)"`
}

// StatsOutput is the response from ctx_stats.
type StatsOutput struct {
	Scope                 string           `json:"scope"`
	OutputsStored         int              `json:"outputs_stored"`
	RawBytes              int64            `json:"raw_bytes"`
	EstimatedSummaryBytes int64            `json:"estimated_summary_bytes"`
	SavingPercent         float64          `json:"saving_percent"`
	AvgDurationMs         int64            `json:"avg_duration_ms"`
	TopCommands           []CommandStatOut `json:"top_commands,omitempty"`
	LargestOutputs        []OutputMetaOut  `json:"largest_outputs,omitempty"`
	HookStats             HookStatsOut     `json:"hook_stats"`

	// Adherence fields — how consistently ctx-saver tools are being used.
	AdherenceScore   float64 `json:"adherence_score,omitempty"`   // 0–100
	NativeShellCount int     `json:"native_shell_count,omitempty"` // runInTerminal/Shell/Bash calls
	NativeReadCount  int     `json:"native_read_count,omitempty"`  // readFile/read_file/Read calls
	CtxExecuteCount  int     `json:"ctx_execute_count,omitempty"`
	CtxReadFileCount int     `json:"ctx_read_file_count,omitempty"`
	AdherenceNote    string  `json:"adherence_note,omitempty"`
}

// CommandStatOut is the per-command aggregate in StatsOutput.
type CommandStatOut struct {
	Command    string `json:"command"`
	Count      int    `json:"count"`
	TotalBytes int64  `json:"total_bytes"`
}

// OutputMetaOut is a lightweight output record in StatsOutput.
type OutputMetaOut struct {
	OutputID  string `json:"output_id"`
	Command   string `json:"command"`
	SizeBytes int64  `json:"size_bytes"`
	LineCount int    `json:"line_count"`
}

// HookStatsOut holds hook activity counts in StatsOutput.
type HookStatsOut struct {
	DangerousBlocked int `json:"dangerous_blocked"`
	RedirectedToMCP  int `json:"redirected_to_mcp"`
	EventsCaptured   int `json:"events_captured"`
}

// StatsHandler implements the ctx_stats MCP tool.
type StatsHandler struct {
	cfg         *config.Config
	st          store.Store
	projectPath string
	serverStart time.Time
}

// NewStatsHandler returns a StatsHandler wired to the given dependencies.
func NewStatsHandler(cfg *config.Config, st store.Store, projectPath string, serverStart time.Time) *StatsHandler {
	return &StatsHandler{cfg: cfg, st: st, projectPath: projectPath, serverStart: serverStart}
}

// Handle processes a ctx_stats request.
func (h *StatsHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input StatsInput) (*mcp.CallToolResult, StatsOutput, error) {
	scope := input.Scope
	if scope == "" {
		scope = "session"
	}

	since, err := resolveSince(scope, h.serverStart)
	if err != nil {
		return nil, StatsOutput{}, err
	}

	stats, err := h.st.GetStats(ctx, h.projectPath, since)
	if err != nil {
		return nil, StatsOutput{}, fmt.Errorf("fetching stats: %w", err)
	}

	estPerOutput := int64(h.cfg.Summary.HeadLines*80 + h.cfg.Summary.TailLines*80 + 200)
	estimatedSummaryBytes := estPerOutput * int64(stats.OutputsStored)

	savingPercent := 0.0
	if stats.RawBytes > 0 {
		saved := stats.RawBytes - estimatedSummaryBytes
		if saved < 0 {
			saved = 0
		}
		savingPercent = float64(saved) / float64(stats.RawBytes) * 100
	}

	// Compute adherence score (0–100).
	nativeTotal := stats.NativeShellCount + stats.NativeReadCount
	ctxTotal := stats.CtxExecuteCount + stats.CtxReadFileCount
	total := nativeTotal + ctxTotal
	adherenceScore := 0.0
	if total > 0 {
		adherenceScore = float64(ctxTotal) / float64(total) * 100
	}

	adherenceNote := adherenceNote(adherenceScore, total)

	out := StatsOutput{
		Scope:                 scope,
		OutputsStored:         stats.OutputsStored,
		RawBytes:              stats.RawBytes,
		EstimatedSummaryBytes: estimatedSummaryBytes,
		SavingPercent:         savingPercent,
		AvgDurationMs:         stats.AvgDurationMs,
		HookStats: HookStatsOut{
			DangerousBlocked: stats.DangerousBlocked,
			RedirectedToMCP:  stats.RedirectedToMCP,
			EventsCaptured:   stats.EventsCaptured,
		},
		AdherenceScore:   adherenceScore,
		NativeShellCount: stats.NativeShellCount,
		NativeReadCount:  stats.NativeReadCount,
		CtxExecuteCount:  stats.CtxExecuteCount,
		CtxReadFileCount: stats.CtxReadFileCount,
		AdherenceNote:    adherenceNote,
	}
	for _, c := range stats.TopCommands {
		out.TopCommands = append(out.TopCommands, CommandStatOut{
			Command: c.Command, Count: c.Count, TotalBytes: c.TotalBytes,
		})
	}
	for _, o := range stats.LargestOutputs {
		out.LargestOutputs = append(out.LargestOutputs, OutputMetaOut{
			OutputID: o.OutputID, Command: o.Command,
			SizeBytes: o.SizeBytes, LineCount: o.LineCount,
		})
	}
	return nil, out, nil
}

// adherenceNote returns a plain-English assessment of the current adherence score.
// When total is 0, no note is returned (not enough data yet).
func adherenceNote(score float64, total int) string {
	if total == 0 {
		return ""
	}
	switch {
	case score >= 90:
		return "✅ Excellent adherence. ctx-saver tools are being used consistently."
	case score >= 70:
		return "👍 Good adherence. Some native tool usage detected — review tool descriptions if this is Copilot."
	case score >= 50:
		return "⚠️  Mixed adherence. Context window is at risk. Re-read ctx_session_init rules."
	default:
		return "🚨 Low adherence. Native tools dominating — session may fail early. " +
			"Call ctx_session_init to refresh rules; ensure .github/copilot-instructions.md is present."
	}
}

// resolveSince maps a scope string to the earliest time.Time to include.
// Returns an error for unrecognised scopes.
func resolveSince(scope string, serverStart time.Time) (time.Time, error) {
	switch scope {
	case "session":
		return serverStart, nil
	case "today":
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
	case "7d":
		return time.Now().AddDate(0, 0, -7), nil
	case "all":
		return time.Time{}, nil
	default:
		return time.Time{}, fmt.Errorf("invalid scope %q — must be: session | today | 7d | all", scope)
	}
}
