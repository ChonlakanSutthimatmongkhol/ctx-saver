package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/freshness"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// SessionInitInput is the typed input for ctx_session_init.
type SessionInitInput struct {
	Task string `json:"task,omitempty" jsonschema:"optional task scope. When set, recent_decisions include only notes for this task; when empty, only unscoped notes are returned."`
}

// SessionInitOutput is the typed output for ctx_session_init.
type SessionInitOutput struct {
	ProjectPath      string                 `json:"project_path"`
	ProjectRules     string                 `json:"project_rules"`
	RecentEvents     []RecentEventEntry     `json:"recent_events,omitempty"`
	RecentDecisions  []DecisionDigest       `json:"recent_decisions,omitempty"`
	CachedOutputs    CachedOutputSummary    `json:"cached_outputs"`
	CachedFiles      []CachedFileEntry      `json:"cached_files,omitempty"`
	ActiveConfig     ActiveConfigSummary    `json:"active_config"`
	FreshnessPolicy  FreshnessPolicySummary `json:"freshness_policy"`
	NextActionHint   string                 `json:"next_action_hint,omitempty"`
	ProjectKnowledge string                 `json:"project_knowledge,omitempty"`
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

// CachedFileEntry is one cached file (reference read) surfaced at session start
// so the agent can reuse it via ctx_search/ctx_get_full without re-reading.
type CachedFileEntry struct {
	Path          string `json:"path"`
	OutputID      string `json:"output_id"`
	SHA256Short   string `json:"sha256,omitempty"`          // first 12 hex chars
	StaleLevel    string `json:"stale_level"`               // fresh|aging|stale|critical
	AgoHuman      string `json:"ago"`
	SizeBytes     int64  `json:"size_bytes"`
	ChangedOnDisk bool   `json:"changed_on_disk,omitempty"` // true = file edited/removed since cache; re-read before use
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
func (h *SessionInitHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input SessionInitInput) (*mcp.CallToolResult, SessionInitOutput, error) {
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

	// Populate cached files (reference reads) — best-effort, limit 10.
	if files, ferr := h.st.ListCachedFiles(ctx, h.projectPath, 10); ferr == nil {
		now := time.Now()
		for _, f := range files {
			fi := freshness.NewFreshnessInfo(f.SourceKind, f.RefreshedAt, f.TTLSeconds, now)
			entry := CachedFileEntry{
				Path:       f.Path,
				OutputID:   f.OutputID,
				StaleLevel: fi.StaleLevel,
				AgoHuman:   humanAgeShort(now.Sub(f.RefreshedAt)),
				SizeBytes:  f.SizeBytes,
			}
			if f.SourceHash != "" {
				if len(f.SourceHash) >= 12 {
					entry.SHA256Short = f.SourceHash[:12]
				} else {
					entry.SHA256Short = f.SourceHash
				}
				// Cheap verification: re-hash the file now. A mismatch or a
				// missing/unreadable file means the cache must not be trusted.
				if cur, herr := freshness.FileSHA256(f.Path); herr != nil || cur != f.SourceHash {
					entry.ChangedOnDisk = true
				}
			}
			out.CachedFiles = append(out.CachedFiles, entry)
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
	task := strings.TrimSpace(input.Task)
	decisions, derr := h.st.ListDecisions(ctx, store.ListDecisionsOptions{
		ProjectPath:   h.projectPath,
		Scope:         "7d",
		MinImportance: "normal",
		Limit:         10,
		Task:          &task,
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
	case len(out.CachedFiles) > 0 && !anyChangedOnDisk(out.CachedFiles):
		out.NextActionHint = "Cached files are listed in cached_files — use ctx_search/ctx_get_full with their output_id instead of re-reading fresh files."
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

	// Inject project-knowledge.md if present (capped at 8 KB to keep response lean).
	knowledgePath := filepath.Join(h.projectPath, ".ctx-saver", "project-knowledge.md")
	if raw, rerr := os.ReadFile(knowledgePath); rerr == nil {
		const maxKnowledgeBytes = 8 * 1024
		if len(raw) > maxKnowledgeBytes {
			raw = append(raw[:maxKnowledgeBytes], []byte("\n…(truncated)")...)
		}
		out.ProjectKnowledge = string(raw)
	}

	recordToolCall(ctx, h.st, h.projectPath, "ctx_session_init", "", "", "session_init")
	return nil, out, nil
}

// anyChangedOnDisk reports whether any cached file was edited or removed since
// it was cached (so the agent should not be told to blindly reuse them).
func anyChangedOnDisk(files []CachedFileEntry) bool {
	for _, f := range files {
		if f.ChangedOnDisk {
			return true
		}
	}
	return false
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
1. Commands with potentially large output (build/test/kubectl/curl/etc.) → ctx_execute
2. Files > 50 lines → ctx_read_file, NOT readFile
3. Before re-running: check ctx_stats(view=outputs) / ctx_search / ctx_get_section for cached results
4. Verify every ~20 turns: ctx_stats → missed_large_outputs should be 0
5. Dangerous commands (rm -rf, curl|bash, eval) are blocked by PreToolUse hook
Sanctioned native use: git write/admin commands, reads of files you edit, pwd/whoami/echo/date.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`
