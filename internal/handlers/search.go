package handlers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/freshness"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/search"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// SearchInput is the typed input for ctx_search.
type SearchInput struct {
	Queries            []string `json:"queries"                         jsonschema:"list of search queries to run against indexed outputs"`
	OutputID           string   `json:"output_id,omitempty"             jsonschema:"optional output ID to limit search to a specific output"`
	MaxResultsPerQuery int      `json:"max_results_per_query,omitempty" jsonschema:"maximum results per query (default: 5)"`
	ContextLines       int      `json:"context_lines,omitempty"         jsonschema:"lines of surrounding context to include before/after each match, like grep -C (default: 0)"`
}

// SearchMatch is a single FTS hit.
type SearchMatch struct {
	OutputID  string                   `json:"output_id"`
	Line      int                      `json:"line"`
	Snippet   string                   `json:"snippet"`
	Score     float64                  `json:"score"`
	Context   []string                 `json:"context,omitempty"` // surrounding lines when context_lines > 0
	Freshness *freshness.FreshnessInfo `json:"freshness,omitempty"`
}

// QueryResult groups matches for one query.
type QueryResult struct {
	Query   string        `json:"query"`
	Matches []SearchMatch `json:"matches"`
}

// SearchOutput is the typed output for ctx_search.
type SearchOutput struct {
	Results         []QueryResult `json:"results"`
	SearchMode      string        `json:"search_mode,omitempty"      jsonschema:"fts5 | like_fallback — indicates which backend served the query"`
	ExpandedQueries []string      `json:"expanded_queries,omitempty" jsonschema:"all queries used after synonym expansion"`
}

// SearchHandler handles the ctx_search MCP tool.
type SearchHandler struct {
	st          store.Store
	projectPath string
	synonyms    *search.SynonymTable
}

// NewSearchHandler creates a SearchHandler.
func NewSearchHandler(st store.Store, projectPath string, syns *search.SynonymTable) *SearchHandler {
	return &SearchHandler{st: st, projectPath: projectPath, synonyms: syns}
}

// Handle implements the ctx_search tool.
// All queries are executed in parallel via goroutines.
func (h *SearchHandler) Handle(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	if len(input.Queries) == 0 {
		return nil, SearchOutput{}, fmt.Errorf("queries must not be empty")
	}

	maxResults := input.MaxResultsPerQuery
	if maxResults <= 0 {
		maxResults = 5
	}

	// Expand queries with synonyms when a table is available.
	expandedQueries := input.Queries
	if h.synonyms != nil {
		expandedQueries = h.synonyms.ExpandAll(input.Queries)
	}

	type result struct {
		query   string
		matches []SearchMatch
		mode    string // "fts5" | "like_fallback"
		err     error
	}

	ch := make(chan result, len(expandedQueries))
	var wg sync.WaitGroup

	for _, q := range expandedQueries {
		wg.Add(1)
		go func(query string) {
			defer wg.Done()
			matches, err := h.st.Search(ctx, query, input.OutputID, maxResults)
			if err != nil {
				ch <- result{query: query, err: err}
				return
			}
			sm := make([]SearchMatch, 0, len(matches))
			mode := "fts5"
			for _, m := range matches {
				sm = append(sm, SearchMatch{
					OutputID: m.OutputID,
					Line:     m.Line,
					Snippet:  m.Snippet,
					Score:    m.Score,
				})
				if m.Mode == "like_fallback" {
					mode = "like_fallback"
				}
			}
			ch <- result{query: query, matches: sm, mode: mode}
		}(q)
	}

	// Close channel once all goroutines finish.
	go func() {
		wg.Wait()
		close(ch)
	}()

	// Preserve query order by building a map first.
	resultMap := make(map[string][]SearchMatch, len(expandedQueries))
	searchMode := "fts5"
	var firstErr error
	for r := range ch {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		resultMap[r.query] = r.matches
		if r.mode == "like_fallback" {
			searchMode = "like_fallback"
		}
	}
	if firstErr != nil {
		return nil, SearchOutput{}, fmt.Errorf("searching: %w", firstErr)
	}

	// Fetch each unique output once for freshness info and optional context lines.
	type outputData struct {
		lines     []string
		freshness freshness.FreshnessInfo
	}
	outputCache := make(map[string]outputData)
	now := time.Now()
	for _, matches := range resultMap {
		for _, m := range matches {
			if _, ok := outputCache[m.OutputID]; ok {
				continue
			}
			out, err := h.st.Get(ctx, m.OutputID)
			if err != nil {
				continue
			}
			outputCache[m.OutputID] = outputData{
				lines:     strings.Split(strings.TrimRight(out.FullOutput, "\n"), "\n"),
				freshness: freshness.NewFreshnessInfo(out.SourceKind, out.RefreshedAt, out.TTLSeconds, now),
			}
		}
	}
	for q, matches := range resultMap {
		for i, m := range matches {
			od, ok := outputCache[m.OutputID]
			if !ok {
				continue
			}
			fi := od.freshness
			matches[i].Freshness = &fi
			if input.ContextLines > 0 {
				start := m.Line - 1 - input.ContextLines
				if start < 0 {
					start = 0
				}
				end := m.Line + input.ContextLines
				if end > len(od.lines) {
					end = len(od.lines)
				}
				matches[i].Context = od.lines[start:end]
			}
		}
		resultMap[q] = matches
	}

	ordered := make([]QueryResult, 0, len(expandedQueries))
	for _, q := range expandedQueries {
		ordered = append(ordered, QueryResult{
			Query:   q,
			Matches: resultMap[q],
		})
	}

	recordToolCall(ctx, h.st, h.projectPath, "ctx_search", strings.Join(input.Queries, ", "), "", "search: "+strings.Join(input.Queries, ", "))
	return nil, SearchOutput{Results: ordered, SearchMode: searchMode, ExpandedQueries: expandedQueries}, nil
}
