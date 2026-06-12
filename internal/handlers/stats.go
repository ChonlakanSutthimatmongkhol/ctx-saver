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
	Scope                 string           `json:"scope"`
	OutputsStored         int              `json:"outputs_stored"`
	RawBytes              int64            `json:"raw_bytes"`
	EstimatedSummaryBytes int64            `json:"estimated_summary_bytes"`
	EstimatedTokensSaved  int64            `json:"estimated_tokens_saved"`
	SavingPercent         float64          `json:"saving_percent"`
	RawTokens             int64            `json:"raw_tokens"`
	ResponseTokens        int64            `json:"response_tokens"`
	TokensSaved           int64            `json:"tokens_saved"`
	TokenSavingPercent    float64          `json:"token_saving_percent"`
	Tokenizer             string           `json:"tokenizer"`
	TokenizedOutputs      int              `json:"tokenized_outputs"`
	UntokenizedOutputs    int              `json:"untokenized_outputs"`
	AvgDurationMs         int64            `json:"avg_duration_ms"`
	TopCommands           []CommandStatOut `json:"top_commands,omitempty"`
	LargestOutputs        []OutputMetaOut  `json:"largest_outputs,omitempty"`
	HookStats             HookStatsOut     `json:"hook_stats"`

	// Adherence fields — how consistently ctx-saver tools are being used.
	AdherenceScore     float64 `json:"adherence_score"`
	NativeShellCount   int     `json:"native_shell_count"`
	NativeReadCount    int     `json:"native_read_count"`
	CtxExecuteCount    int     `json:"ctx_execute_count"`
	CtxReadFileCount   int     `json:"ctx_read_file_count"`
	AdherenceNote      string  `json:"adherence_note,omitempty"`
	MissedLargeOutputs int     `json:"missed_large_outputs"`
	MissedLargeBytes   int64   `json:"missed_large_bytes"`
	SanctionedReads    int     `json:"sanctioned_reads"`
	SavingsNote        string  `json:"savings_note,omitempty"`

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
	adherence, err := h.st.GetAdherenceStats(
		ctx,
		h.projectPath,
		since,
		h.cfg.Summary.AutoIndexThresholdBytes,
	)
	if err != nil {
		return nil, StatsOutput{}, fmt.Errorf("fetching adherence stats: %w", err)
	}

	tokensSaved := stats.RawTokens - stats.ResponseTokens
	if tokensSaved < 0 {
		tokensSaved = 0
	}
	tokenSavingPercent := 0.0
	if stats.RawTokens > 0 {
		tokenSavingPercent = float64(tokensSaved) / float64(stats.RawTokens) * 100
	}

	// Compute adherence score (0–100).
	nativeTotal := adherence.NativeShellCount + adherence.NativeReadCount
	ctxTotal := adherence.CtxExecuteCount + adherence.CtxReadFileCount
	total := nativeTotal + ctxTotal
	adherenceScore := 0.0
	if total > 0 {
		adherenceScore = float64(ctxTotal) / float64(total) * 100
	}

	out := StatsOutput{
		View:                  "stats",
		Scope:                 scope,
		OutputsStored:         stats.OutputsStored,
		RawBytes:              stats.RawBytes,
		EstimatedSummaryBytes: stats.ResponseBytes,
		EstimatedTokensSaved:  tokensSaved,
		SavingPercent:         tokenSavingPercent,
		RawTokens:             stats.RawTokens,
		ResponseTokens:        stats.ResponseTokens,
		TokensSaved:           tokensSaved,
		TokenSavingPercent:    tokenSavingPercent,
		Tokenizer:             stats.Tokenizer,
		TokenizedOutputs:      stats.TokenizedOutputs,
		UntokenizedOutputs:    stats.UntokenizedOutputs,
		AvgDurationMs:         stats.AvgDurationMs,
		HookStats: HookStatsOut{
			DangerousBlocked: stats.DangerousBlocked,
			RedirectedToMCP:  stats.RedirectedToMCP,
			EventsCaptured:   stats.EventsCaptured,
		},
		AdherenceScore:     adherenceScore,
		NativeShellCount:   adherence.NativeShellCount,
		NativeReadCount:    adherence.NativeReadCount,
		CtxExecuteCount:    adherence.CtxExecuteCount,
		CtxReadFileCount:   adherence.CtxReadFileCount,
		AdherenceNote:      adherenceNote(adherence),
		MissedLargeOutputs: adherence.MissedLargeOutputs,
		MissedLargeBytes:   adherence.MissedLargeBytes,
		SanctionedReads:    adherence.SanctionedReads,
	}
	if stats.OutputsStored == 0 {
		out.SavingsNote = "No outputs exceeded auto_index_threshold in this scope — nothing required summarizing. A zero here is normal for edit/commit sessions."
	} else if stats.TokenizedOutputs == 0 {
		out.SavingsNote = "Stored outputs predate exact token accounting; they are listed in untokenized_outputs and excluded from token savings."
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

// adherenceNote assesses context-window health. Severity is driven by missed
// large outputs rather than raw native-tool counts.
func adherenceNote(stats *store.AdherenceStats) string {
	total := stats.CtxExecuteCount + stats.CtxReadFileCount +
		stats.NativeShellCount + stats.NativeReadCount
	if total == 0 {
		return ""
	}
	switch {
	case stats.MissedLargeOutputs == 0:
		if stats.NativeShellCount+stats.NativeReadCount >
			stats.CtxExecuteCount+stats.CtxReadFileCount {
			return "✅ Healthy. Native tool usage is high but no large outputs leaked into context — typical for edit/git-heavy sessions."
		}
		return "✅ Excellent. ctx-saver tools used consistently; no large outputs leaked."
	case stats.MissedLargeOutputs <= 2:
		return fmt.Sprintf(
			"⚠️  %d large output(s) (%s) went through native tools — route those through ctx_execute/ctx_read_file next time.",
			stats.MissedLargeOutputs,
			humanBytes(stats.MissedLargeBytes),
		)
	default:
		return fmt.Sprintf(
			"🚨 %d large outputs (%s total) bypassed ctx-saver — context window at risk. Call ctx_session_init to refresh rules.",
			stats.MissedLargeOutputs,
			humanBytes(stats.MissedLargeBytes),
		)
	}
}

func humanBytes(size int64) string {
	const unit = int64(1024)
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := unit, 0
	for n := size / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGT"[exp])
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
