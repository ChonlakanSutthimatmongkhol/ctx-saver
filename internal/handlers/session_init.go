package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// SessionInitInput is the typed input for ctx_session_init (no parameters).
type SessionInitInput struct{}

// SessionInitOutput is the typed output for ctx_session_init.
type SessionInitOutput struct {
	ProjectPath      string                 `json:"project_path"`
	ProjectRules     string                 `json:"project_rules"`
	RecentEvents     []RecentEventEntry     `json:"recent_events,omitempty"`
	RecentDecisions  []DecisionDigest       `json:"recent_decisions,omitempty"`
	CachedOutputs    CachedOutputSummary    `json:"cached_outputs"`
	ActiveConfig     ActiveConfigSummary    `json:"active_config"`
	FreshnessPolicy  FreshnessPolicySummary `json:"freshness_policy"`
	NextActionHint   string                 `json:"next_action_hint,omitempty"`
	ServerVersion    string                 `json:"server_version"`
	SessionStartTime time.Time              `json:"session_start_time"`
}

// FreshnessPolicySummary describes how to interpret stale_level values returned
// by retrieval tools (ctx_search, ctx_stats view=outputs, ctx_get_full, ctx_outline,
// ctx_get_section). Full policy lives here; each tool description references this
// field instead of repeating it.
type FreshnessPolicySummary struct {
	StaleLevels       []string          `json:"stale_levels"`
	Actions           map[string]string `json:"actions"`
	RefreshKeywordsTH []string          `json:"refresh_keywords_th"`
	RefreshKeywordsEN []string          `json:"refresh_keywords_en"`
}

// DecisionDigest is a compact view of one decision for session_init injection.
type DecisionDigest struct {
	DecisionID string   `json:"decision_id"`
	Text       string   `json:"text"`
	Tags       []string `json:"tags,omitempty"`
	AgoHuman   string   `json:"ago"`
	Importance string   `json:"importance"`
}

// RecentEventEntry describes one past tool call.
type RecentEventEntry struct {
	AgoSeconds int64  `json:"ago_seconds"`
	Summary    string `json:"summary"`
}

// CachedOutputSummary holds a snapshot of what is already stored.
type CachedOutputSummary struct {
	TotalOutputs      int           `json:"total_outputs"`
	TotalSizeBytes    int64         `json:"total_size_bytes"`
	TopCommands       []CommandRank `json:"top_commands,omitempty"`
	RetentionDaysLeft int           `json:"retention_days_left"`
}

// CommandRank is a command with its run count.
type CommandRank struct {
	Command string `json:"command"`
	Count   int    `json:"count"`
}

// ActiveConfigSummary surfaces key config flags for the agent.
type ActiveConfigSummary struct {
	Sandbox            string `json:"sandbox"`
	DedupEnabled       bool   `json:"dedup_enabled"`
	DedupWindowMinutes int    `json:"dedup_window_minutes"`
	SmartFormatEnabled bool   `json:"smart_format_enabled"`
}

// SessionInitHandler handles ctx_session_init.
type SessionInitHandler struct {
	cfg           *config.Config
	st            store.Store
	projectPath   string
	serverStart   time.Time
	serverVersion string
}

// NewSessionInitHandler constructs a SessionInitHandler with the given dependencies.
func NewSessionInitHandler(cfg *config.Config, st store.Store, projectPath string, serverStart time.Time, serverVersion string) *SessionInitHandler {
	return &SessionInitHandler{
		cfg:           cfg,
		st:            st,
		projectPath:   projectPath,
		serverStart:   serverStart,
		serverVersion: serverVersion,
	}
}

// Handle processes a ctx_session_init request.
func (h *SessionInitHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, _ SessionInitInput) (*mcp.CallToolResult, SessionInitOutput, error) {
	out := SessionInitOutput{
		ProjectPath:      h.projectPath,
		ProjectRules:     sessionRulesText,
		ServerVersion:    h.serverVersion,
		SessionStartTime: h.serverStart,
		ActiveConfig: ActiveConfigSummary{
			Sandbox:            h.cfg.Sandbox.Type,
			DedupEnabled:       h.cfg.Dedup.Enabled,
			DedupWindowMinutes: h.cfg.Dedup.WindowMinutes,
			SmartFormatEnabled: h.cfg.Summary.SmartFormat,
		},
		FreshnessPolicy: FreshnessPolicySummary{
			StaleLevels: []string{"fresh", "aging", "stale", "critical"},
			Actions: map[string]string{
				"fresh":    "use as-is",
				"aging":    "use as-is",
				"stale":    "warn user; offer ctx_execute refresh",
				"critical": "DO NOT use; require user_confirmation_required prompt",
			},
			RefreshKeywordsTH: []string{"ล่าสุด", "ปัจจุบัน"},
			RefreshKeywordsEN: []string{"current", "latest", "now"},
		},
	}

	// Populate cached outputs from stats over the last 7 days.
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	stats, err := h.st.GetStats(ctx, h.projectPath, sevenDaysAgo)
	if err == nil && stats != nil {
		out.CachedOutputs = CachedOutputSummary{
			TotalOutputs:      stats.OutputsStored,
			TotalSizeBytes:    stats.RawBytes,
			RetentionDaysLeft: h.cfg.Storage.RetentionDays,
		}
		for i, c := range stats.TopCommands {
			if i >= 5 {
				break
			}
			out.CachedOutputs.TopCommands = append(out.CachedOutputs.TopCommands, CommandRank{
				Command: c.Command,
				Count:   c.Count,
			})
		}
	}

	// Populate recent events, deduplicating by tool + summary prefix.
	events, err := h.st.ListProjectSessionEvents(ctx, h.projectPath, 20)
	if err == nil && len(events) > 0 {
		now := time.Now()
		seen := make(map[string]struct{})
		// Iterate newest-first (events slice is newest-last, so we reverse).
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			key := e.ToolName + ":" + truncateStr(e.Summary, 40)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out.RecentEvents = append(out.RecentEvents, RecentEventEntry{
				AgoSeconds: int64(now.Sub(e.CreatedAt).Seconds()),
				Summary:    fmt.Sprintf("[%s] %s", e.ToolName, e.Summary),
			})
			if len(out.RecentEvents) >= 10 {
				break
			}
		}
	}

	// Inject recent decisions (last 10, normal+high importance from past 7 days).
	decisions, derr := h.st.ListDecisions(ctx, store.ListDecisionsOptions{
		ProjectPath:   h.projectPath,
		Scope:         "7d",
		MinImportance: "normal",
		Limit:         10,
	})
	if derr == nil {
		now := time.Now()
		for _, d := range decisions {
			out.RecentDecisions = append(out.RecentDecisions, DecisionDigest{
				DecisionID: d.DecisionID,
				Text:       d.Text,
				Tags:       d.Tags,
				AgoHuman:   humanAgeShort(now.Sub(d.CreatedAt)),
				Importance: d.Importance,
			})
		}
	}

	// Choose a next-action hint.
	switch {
	case len(out.RecentEvents) > 0:
		out.NextActionHint = "Recent session activity found. Check ctx_stats(view=outputs) to reuse cached results, or ctx_stats to verify adherence_score."
	case out.CachedOutputs.TotalOutputs > 0:
		out.NextActionHint = "Cached outputs exist but no recent activity. Use ctx_stats(view=outputs) to explore what is stored."
	default:
		out.NextActionHint = "Fresh project. Use ctx_execute for your first command to seed the cache."
	}
	if len(out.RecentDecisions) > 0 {
		out.NextActionHint += fmt.Sprintf(" You have %d recent architectural decisions logged — review them to understand prior reasoning.", len(out.RecentDecisions))
	}

	recordToolCall(ctx, h.st, h.projectPath, "ctx_session_init", "", "", "session_init")
	return nil, out, nil
}

// truncateStr returns s truncated to max bytes, appending "…" when truncated.
// It walks back to a valid UTF-8 rune boundary before slicing.
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && s[max]&0xC0 == 0x80 { // skip continuation bytes
		max--
	}
	return s[:max] + "…"
}

// sessionRulesText is the condensed rule block returned on ctx_session_init.
// Kept short to avoid consuming context on every session start.
const sessionRulesText = `━━━ ctx-saver SESSION RULES ━━━
1. Commands (build/test/git/kubectl/curl/etc.) → ctx_execute, NOT runInTerminal/Shell/Bash
2. Files > 50 lines → ctx_read_file, NOT readFile
3. Before re-running: check ctx_stats(view=outputs) / ctx_search / ctx_get_section for cached results
4. Verify every ~20 turns: ctx_stats → saving_percent and adherence_score should be > 80%
5. Dangerous commands (rm -rf, curl|bash, eval) are blocked by PreToolUse hook
Exception: pwd / whoami / echo / date may use native terminal.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`
