package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pmezard/go-difflib/difflib"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/freshness"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// GetFullInput is the typed input for ctx_get_full.
type GetFullInput struct {
	OutputID    string `json:"output_id"              jsonschema:"ID of the output to retrieve"`
	LineRange   []int  `json:"line_range,omitempty"   jsonschema:"optional [start, end] line range (1-based, inclusive); omit to retrieve all lines"`
	AcceptStale bool   `json:"accept_stale,omitempty" jsonschema:"set true to bypass freshness confirmation gate and use cached data regardless of age"`
	DiffAgainst string `json:"diff_against,omitempty" jsonschema:"output_id of an older run to diff against; returns unified diff instead of lines"`
}

// GetFullOutput is the typed output for ctx_get_full.
type GetFullOutput struct {
	OutputID                 string                  `json:"output_id"`
	Lines                    []string                `json:"lines"`
	TotalLines               int                     `json:"total_lines"`
	Returned                 int                     `json:"returned"`
	Freshness                freshness.FreshnessInfo `json:"freshness"`
	UserConfirmationRequired bool                    `json:"user_confirmation_required,omitempty"`
	UserConfirmationPrompt   string                  `json:"user_confirmation_prompt,omitempty"`
	DiffAgainst              string                  `json:"diff_against,omitempty"`
	DiffLines                []string                `json:"diff_lines,omitempty"`
	LinesAdded               int                     `json:"lines_added,omitempty"`
	LinesRemoved             int                     `json:"lines_removed,omitempty"`
	DiffNote                 string                  `json:"diff_note,omitempty"`
}

// GetFullHandler handles the ctx_get_full MCP tool.
type GetFullHandler struct {
	st          store.Store
	projectPath string
	sb          sandbox.Sandbox
	fc          config.FreshnessConfig
}

// NewGetFullHandler creates a GetFullHandler.
func NewGetFullHandler(st store.Store, projectPath string) *GetFullHandler {
	return &GetFullHandler{st: st, projectPath: projectPath}
}

// WithFreshness attaches a sandbox and freshness config for auto-refresh support.
func (h *GetFullHandler) WithFreshness(sb sandbox.Sandbox, fc config.FreshnessConfig) *GetFullHandler {
	h.sb = sb
	h.fc = fc
	return h
}

// Handle implements the ctx_get_full tool.
func (h *GetFullHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input GetFullInput) (*mcp.CallToolResult, GetFullOutput, error) {
	if input.OutputID == "" {
		return nil, GetFullOutput{}, fmt.Errorf("output_id must not be empty")
	}
	if input.DiffAgainst != "" && len(input.LineRange) != 0 {
		return nil, GetFullOutput{}, fmt.Errorf("line_range and diff_against are mutually exclusive")
	}

	out, err := h.st.Get(ctx, input.OutputID)
	if err != nil {
		return nil, GetFullOutput{}, fmt.Errorf("retrieving output: %w", err)
	}

	// Resolve freshness and act unless the caller explicitly accepts stale data.
	var userConfirmRequired bool
	var userConfirmPrompt string
	if !input.AcceptStale {
		switch freshness.Resolve(out.SourceKind, out.RefreshedAt, h.fc).Action {
		case "auto_refresh":
			out = refreshOutput(ctx, h.st, h.sb, "", out)
		case "ask_user":
			userConfirmRequired = true
			userConfirmPrompt = "This output is over 7 days old and may be severely outdated. Reply 'use cache' to proceed with cached data, or 'refresh' to re-run the command via ctx_execute before continuing."
		}
	}

	if input.DiffAgainst != "" {
		against, getErr := h.st.Get(ctx, input.DiffAgainst)
		if getErr != nil {
			return nil, GetFullOutput{}, fmt.Errorf("retrieving diff_against output %q: %w", input.DiffAgainst, getErr)
		}
		diffLines, added, removed, note, diffErr := unifiedOutputDiff(against, out)
		if diffErr != nil {
			return nil, GetFullOutput{}, diffErr
		}
		fi := freshness.NewFreshnessInfo(out.SourceKind, out.RefreshedAt, out.TTLSeconds, time.Now())
		recordToolCall(
			ctx, h.st, h.projectPath, "ctx_get_full",
			input.OutputID+"#diff:"+input.DiffAgainst, "", "get_full diff: "+input.OutputID,
		)
		return nil, GetFullOutput{
			OutputID:                 input.OutputID,
			TotalLines:               outputLineCount(out.FullOutput),
			Returned:                 len(diffLines),
			Freshness:                fi,
			UserConfirmationRequired: userConfirmRequired,
			UserConfirmationPrompt:   userConfirmPrompt,
			DiffAgainst:              input.DiffAgainst,
			DiffLines:                diffLines,
			LinesAdded:               added,
			LinesRemoved:             removed,
			DiffNote:                 note,
		}, nil
	}

	// Split into lines (strip trailing newline first to avoid a ghost empty line).
	allLines := strings.Split(strings.TrimRight(out.FullOutput, "\n"), "\n")
	totalLines := len(allLines)

	start := 1
	end := totalLines

	if len(input.LineRange) == 2 {
		start = input.LineRange[0]
		end = input.LineRange[1]
		if start < 1 {
			start = 1
		}
		if end > totalLines {
			end = totalLines
		}
		if start > end {
			return nil, GetFullOutput{}, fmt.Errorf("line_range start (%d) must be <= end (%d)", start, end)
		}
	} else if len(input.LineRange) != 0 {
		return nil, GetFullOutput{}, fmt.Errorf("line_range must have exactly 2 elements [start, end], got %d", len(input.LineRange))
	}

	selected := allLines[start-1 : end]
	fi := freshness.NewFreshnessInfo(out.SourceKind, out.RefreshedAt, out.TTLSeconds, time.Now())
	recordToolCall(ctx, h.st, h.projectPath, "ctx_get_full", input.OutputID, "", "get_full: "+input.OutputID)
	return nil, GetFullOutput{
		OutputID:                 input.OutputID,
		Lines:                    selected,
		TotalLines:               totalLines,
		Returned:                 len(selected),
		Freshness:                fi,
		UserConfirmationRequired: userConfirmRequired,
		UserConfirmationPrompt:   userConfirmPrompt,
	}, nil
}

func unifiedOutputDiff(against, current *store.Output) ([]string, int, int, string, error) {
	note := ""
	if against.Command != current.Command {
		note = "commands differ; compare results with care"
	}
	if against.FullOutput == current.FullOutput {
		return []string{}, 0, 0, appendDiffNote(note, "outputs are identical"), nil
	}

	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(against.FullOutput),
		B:        difflib.SplitLines(current.FullOutput),
		FromFile: against.OutputID,
		ToFile:   current.OutputID,
		Context:  3,
	})
	if err != nil {
		return nil, 0, 0, "", fmt.Errorf("generating unified diff: %w", err)
	}
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	added, removed := 0, 0
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "@@"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}

	if len(lines) > 400 {
		omitted := len(lines) - 300
		truncated := make([]string, 0, 300)
		truncated = append(truncated, lines[:200]...)
		truncated = append(truncated, lines[len(lines)-100:]...)
		lines = truncated
		note = appendDiffNote(
			note,
			fmt.Sprintf("diff truncated: %d line(s) omitted; use line_range on either output to inspect a focused region", omitted),
		)
	}
	return lines, added, removed, note, nil
}

func appendDiffNote(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "; " + addition
}

func outputLineCount(output string) int {
	if output == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(output, "\n"), "\n"))
}
