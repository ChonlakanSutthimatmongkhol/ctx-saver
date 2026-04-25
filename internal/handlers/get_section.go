package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/freshness"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary"
)

// GetSectionInput is the typed input for ctx_get_section.
type GetSectionInput struct {
	OutputID    string `json:"output_id"              jsonschema:"ID of the stored output"`
	Heading     string `json:"heading"                jsonschema:"heading text to match (case-insensitive)"`
	Partial     bool   `json:"partial,omitempty"      jsonschema:"allow substring match on heading (default: false)"`
	AcceptStale bool   `json:"accept_stale,omitempty" jsonschema:"set true to bypass freshness confirmation gate"`
}

// GetSectionOutput is the typed output for ctx_get_section.
type GetSectionOutput struct {
	OutputID                string                  `json:"output_id"`
	Heading                 string                  `json:"heading"`
	StartLine               int                     `json:"start_line,omitempty"`
	EndLine                 int                     `json:"end_line,omitempty"`
	Lines                   []string                `json:"lines,omitempty"`
	LineCount               int                     `json:"line_count,omitempty"`
	Found                   bool                    `json:"found"`
	Freshness               freshness.FreshnessInfo `json:"freshness"`
	UserConfirmationRequired bool                   `json:"user_confirmation_required,omitempty"`
	UserConfirmationPrompt  string                  `json:"user_confirmation_prompt,omitempty"`
}

// GetSectionHandler handles the ctx_get_section MCP tool.
type GetSectionHandler struct {
	st          store.Store
	projectPath string
	sb          sandbox.Sandbox
	fc          config.FreshnessConfig
}

// NewGetSectionHandler creates a GetSectionHandler.
func NewGetSectionHandler(st store.Store, projectPath string) *GetSectionHandler {
	return &GetSectionHandler{st: st, projectPath: projectPath}
}

// WithFreshness attaches a sandbox and freshness config for auto-refresh support.
func (h *GetSectionHandler) WithFreshness(sb sandbox.Sandbox, fc config.FreshnessConfig) *GetSectionHandler {
	h.sb = sb
	h.fc = fc
	return h
}

// Handle implements the ctx_get_section tool.
func (h *GetSectionHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input GetSectionInput) (*mcp.CallToolResult, GetSectionOutput, error) {
	if input.OutputID == "" {
		return nil, GetSectionOutput{}, fmt.Errorf("output_id must not be empty")
	}
	if strings.TrimSpace(input.Heading) == "" {
		return nil, GetSectionOutput{}, fmt.Errorf("heading must not be empty")
	}

	out, err := h.st.Get(ctx, input.OutputID)
	if err != nil {
		return nil, GetSectionOutput{}, fmt.Errorf("getting output: %w", err)
	}

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
	fi := freshness.NewFreshnessInfo(out.SourceKind, out.RefreshedAt, out.TTLSeconds, time.Now())
	lines := strings.Split(strings.TrimRight(out.FullOutput, "\n"), "\n")
	start, end, found := summary.FindSection(lines, input.Heading, input.Partial)
	if !found {
		recordToolCall(ctx, h.st, h.projectPath, "ctx_get_section", input.OutputID+"#"+input.Heading, "", "get_section: "+input.Heading)
		return nil, GetSectionOutput{
			OutputID:                input.OutputID,
			Heading:                 input.Heading,
			Found:                   false,
			Freshness:               fi,
			UserConfirmationRequired: userConfirmRequired,
			UserConfirmationPrompt:  userConfirmPrompt,
		}, nil
	}

	selected := lines[start-1 : end]
	recordToolCall(ctx, h.st, h.projectPath, "ctx_get_section", input.OutputID+"#"+input.Heading, "", "get_section: "+input.Heading)
	return nil, GetSectionOutput{
		OutputID:                input.OutputID,
		Heading:                 input.Heading,
		StartLine:               start,
		EndLine:                 end,
		Lines:                   selected,
		LineCount:               len(selected),
		Found:                   true,
		Freshness:               fi,
		UserConfirmationRequired: userConfirmRequired,
		UserConfirmationPrompt:  userConfirmPrompt,
	}, nil
}
