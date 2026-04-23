package handlers

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	OutputID string   `json:"output_id"`
	Line     int      `json:"line"`
	Snippet  string   `json:"snippet"`
	Score    float64  `json:"score"`
	Context  []string `json:"context,omitempty"` // surrounding lines when context_lines > 0
}

// QueryResult groups matches for one query.
type QueryResult struct {
	Query   string        `json:"query"`
	Matches []SearchMatch `json:"matches"`
}

// SearchOutput is the typed output for ctx_search.
type SearchOutput struct {
	Results []QueryResult `json:"results"`
}

// SearchHandler handles the ctx_search MCP tool.
type SearchHandler struct {
	st          store.Store
	projectPath string
}

// NewSearchHandler creates a SearchHandler.
func NewSearchHandler(st store.Store, projectPath string) *SearchHandler {
	return &SearchHandler{st: st, projectPath: projectPath}
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

	type result struct {
		query   string
		matches []SearchMatch
		err     error
	}

	ch := make(chan result, len(input.Queries))
	var wg sync.WaitGroup

	for _, q := range input.Queries {
		wg.Add(1)
		go func(query string) {
			defer wg.Done()
			matches, err := h.st.Search(ctx, query, input.OutputID, maxResults)
			if err != nil {
				ch <- result{query: query, err: err}
				return
			}
			sm := make([]SearchMatch, 0, len(matches))
			for _, m := range matches {
				sm = append(sm, SearchMatch{
					OutputID: m.OutputID,
					Line:     m.Line,
					Snippet:  m.Snippet,
					Score:    m.Score,
				})
			}
			ch <- result{query: query, matches: sm}
		}(q)
	}

	// Close channel once all goroutines finish.
	go func() {
		wg.Wait()
		close(ch)
	}()

	// Preserve query order by building a map first.
	resultMap := make(map[string][]SearchMatch, len(input.Queries))
	var firstErr error
	for r := range ch {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		resultMap[r.query] = r.matches
	}
	if firstErr != nil {
		return nil, SearchOutput{}, fmt.Errorf("searching: %w", firstErr)
	}

	// Attach surrounding context lines if requested.
	if input.ContextLines > 0 {
		// Fetch each unique output once.
		outputLines := make(map[string][]string)
		for _, matches := range resultMap {
			for _, m := range matches {
				if _, ok := outputLines[m.OutputID]; ok {
					continue
				}
				out, err := h.st.Get(ctx, m.OutputID)
				if err != nil {
					continue
				}
				outputLines[m.OutputID] = strings.Split(strings.TrimRight(out.FullOutput, "\n"), "\n")
			}
		}
		for q, matches := range resultMap {
			for i, m := range matches {
				lines, ok := outputLines[m.OutputID]
				if !ok {
					continue
				}
				start := m.Line - 1 - input.ContextLines
				if start < 0 {
					start = 0
				}
				end := m.Line + input.ContextLines
				if end > len(lines) {
					end = len(lines)
				}
				matches[i].Context = lines[start:end]
			}
			resultMap[q] = matches
		}
	}

	ordered := make([]QueryResult, 0, len(input.Queries))
	for _, q := range input.Queries {
		ordered = append(ordered, QueryResult{
			Query:   q,
			Matches: resultMap[q],
		})
	}

	return nil, SearchOutput{Results: ordered}, nil
}
