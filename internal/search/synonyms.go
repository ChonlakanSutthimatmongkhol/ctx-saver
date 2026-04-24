// Package search provides query expansion helpers for ctx_search.
package search

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed builtin_synonyms.yaml
var builtinSynonymsYAML []byte

// SynonymTable holds keyword → expansions mapping.
// Safe for concurrent read after construction.
type SynonymTable struct {
	entries map[string][]string
}

// Load returns a SynonymTable merged from builtin + projectDir/.ctx-saver-synonyms.yaml
// if present. Errors reading the override file are returned; missing file is silently ignored.
func Load(projectDir string) (*SynonymTable, error) {
	t := &SynonymTable{entries: make(map[string][]string)}
	if err := t.mergeYAML(builtinSynonymsYAML); err != nil {
		return nil, fmt.Errorf("loading builtin synonyms: %w", err)
	}
	if projectDir != "" {
		overridePath := filepath.Join(projectDir, ".ctx-saver-synonyms.yaml")
		data, err := os.ReadFile(overridePath)
		if err == nil {
			if mergeErr := t.mergeYAML(data); mergeErr != nil {
				return nil, fmt.Errorf("loading %s: %w", overridePath, mergeErr)
			}
		}
		// Missing file is not an error.
	}
	return t, nil
}

func (t *SynonymTable) mergeYAML(data []byte) error {
	var raw map[string][]string
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}
	for k, v := range raw {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		t.entries[key] = v
	}
	return nil
}

// Expand returns the original query plus any synonyms for it.
// Case is preserved in the original; synonyms appended in their declared form.
// Result is deduplicated and order-stable.
func (t *SynonymTable) Expand(query string) []string {
	key := strings.ToLower(strings.TrimSpace(query))
	out := []string{query}
	syns, ok := t.entries[key]
	if !ok {
		return out
	}
	seen := map[string]struct{}{key: {}}
	for _, s := range syns {
		sLower := strings.ToLower(s)
		if _, dup := seen[sLower]; dup {
			continue
		}
		seen[sLower] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ExpandAll expands each query and returns the deduplicated union.
func (t *SynonymTable) ExpandAll(queries []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, q := range queries {
		for _, exp := range t.Expand(q) {
			key := strings.ToLower(exp)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, exp)
		}
	}
	return out
}
