package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/freshness"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// StatsInput is the input for the ctx_stats MCP tool.
type StatsInput struct {
	// Discriminator: "stats" (default) returns adherence + summary metrics; "outputs" returns full list of stored outputs.
	View string `json:"view,omitempty"`

	// Stats fields (used when view="stats" or omitted).
	Scope string `json:"scope,omitempty" jsonschema:"session | today | 7d | all (default: session)"`

	// Outputs list fields (used when view="outputs").
	Limit       int  `json:"limit,omitempty"        jsonschema:"maximum number of outputs to return (default: 50)"`
	AcceptStale bool `json:"accept_stale,omitempty" jsonschema:"set true to suppress freshness warnings in results"`
}

// StatsOutput is the response from ctx_stats (discriminated by View field).
type StatsOutput struct {
	View string `json:"view"`

	// Stats view fields (when View == "stats").
	Scope                 string           `json:"scope,omitempty"`
	OutputsStored         int              `json:"outputs_stored,omitempty"`
	RawBytes              int64            `json:"raw_bytes,omitempty"`
	EstimatedSummaryBytes int64            `json:"estimated_summary_bytes,omitempty"`
	SavingPercent         float64          `json:"saving_percent,omitempty"`
	AvgDurationMs         int64            `json:"avg_duration_ms,omitempty"`
	TopCommands           []CommandStatOut `json:"top_commands,omitempty"`
	LargestOutputs        []OutputMetaOut  `json:"largest_outputs,omitempty"`
	HookStats             HookStatsOut     `json:"hook_stats,omitempty"`

	// Adherence fields — how consistently ctx-saver tools are being used.
	AdherenceScore   float64 `json:"adherence_score,omitempty"`
	NativeShellCount int     `json:"native_shell_count,omitempty"`
	NativeReadCount  int     `json:"native_read_count,omitempty"`
	CtxExecuteCount  int     `json:"ctx_execute_count,omitempty"`
	CtxReadFileCount int     `json:"ctx_read_file_count,omitempty"`
	AdherenceNote    string  `json:"adherence_note,omitempty"`

	// Outputs view field (when View == "outputs").
	Outputs []OutputEntry `json:"outputs,omitempty"`
}

// OutputEntry is one row in the outputs list (view="outputs").
type OutputEntry struct {
	OutputID  string                  `json:"output_id"`
	Command   string                  `json:"command"`
	CreatedAt string                  `json:"created_at"`
	SizeBytes int64                   `json:"size_bytes"`
	Lines     int                     `json:"lines"`
	Freshness freshness.FreshnessInfo `json:"freshness"`
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

// Handle dispatches to handleStats or handleListOutputs based on the view field.
// Backward compat: omitted view defaults to "stats".
func (h *StatsHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input StatsInput) (*mcp.CallToolResult, StatsOutput, error) {
	view := input.View
	if view == "" {
		view = "stats"
	}

	switch view {
	case "stats":
		return h.handleStats(ctx, input)
	case "outputs":
		return h.handleListOutputs(ctx, input)
	default:
		return nil, StatsOutput{}, fmt.Errorf("unknown view %q (expected 'stats' or 'outputs')", view)
	}
}

func (h *StatsHandler) handleStats(ctx context.Context, input StatsInput) (*mcp.CallToolResult, StatsOutput, error) {
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
		View:                  "stats",
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
	recordToolCall(ctx, h.st, h.projectPath, "ctx_stats", input.Scope, "", "stats: "+input.Scope)
	return nil, out, nil
}

func (h *StatsHandler) handleListOutputs(ctx context.Context, input StatsInput) (*mcp.CallToolResult, StatsOutput, error) {
	metas, err := h.st.List(ctx, h.projectPath, input.Limit)
	if err != nil {
		return nil, StatsOutput{}, fmt.Errorf("listing outputs: %w", err)
	}

	now := time.Now()
	entries := make([]OutputEntry, 0, len(metas))
	for _, m := range metas {
		entries = append(entries, OutputEntry{
			OutputID:  m.OutputID,
			Command:   m.Command,
			CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
			SizeBytes: m.SizeBytes,
			Lines:     m.LineCount,
			Freshness: freshness.NewFreshnessInfo(m.SourceKind, m.RefreshedAt, m.TTLSeconds, now),
		})
	}

	recordToolCall(ctx, h.st, h.projectPath, "ctx_stats", "", "", "stats outputs")
	return nil, StatsOutput{View: "outputs", Outputs: entries}, nil
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
