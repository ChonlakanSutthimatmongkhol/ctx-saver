package search_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_BuiltinOnly(t *testing.T) {
	// Empty projectDir — only builtin synonyms.
	table, err := search.Load("")
	require.NoError(t, err)
	require.NotNil(t, table)
	// Builtin should contain api_path.
	expanded := table.Expand("api_path")
	assert.Greater(t, len(expanded), 1, "api_path should expand to multiple terms")
}

func TestLoad_WithOverride_Adds(t *testing.T) {
	dir := t.TempDir()
	yaml := "custom_term: [foo, bar]\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".ctx-saver-synonyms.yaml"), []byte(yaml), 0600))

	table, err := search.Load(dir)
	require.NoError(t, err)
	expanded := table.Expand("custom_term")
	assert.Equal(t, []string{"custom_term", "foo", "bar"}, expanded)
}

func TestLoad_WithOverride_Replaces(t *testing.T) {
	dir := t.TempDir()
	// Override api_path to only have one synonym.
	yaml := "api_path: [my_endpoint]\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".ctx-saver-synonyms.yaml"), []byte(yaml), 0600))

	table, err := search.Load(dir)
	require.NoError(t, err)
	expanded := table.Expand("api_path")
	assert.Equal(t, []string{"api_path", "my_endpoint"}, expanded)
}

func TestLoad_MissingOverride(t *testing.T) {
	// Directory exists but no override file — should not error.
	table, err := search.Load(t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, table)
}

func TestLoad_BadYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".ctx-saver-synonyms.yaml"), []byte("key: [unclosed"), 0600))

	_, err := search.Load(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".ctx-saver-synonyms.yaml")
}

func TestExpand_Known(t *testing.T) {
	table, err := search.Load("")
	require.NoError(t, err)

	expanded := table.Expand("authentication")
	assert.Contains(t, expanded, "authentication") // original preserved
	assert.Contains(t, expanded, "auth")
	assert.Contains(t, expanded, "jwt")
}

func TestExpand_Unknown(t *testing.T) {
	table, err := search.Load("")
	require.NoError(t, err)

	expanded := table.Expand("completely_unknown_term_xyz")
	assert.Equal(t, []string{"completely_unknown_term_xyz"}, expanded)
}

func TestExpand_CaseInsensitive(t *testing.T) {
	table, err := search.Load("")
	require.NoError(t, err)

	// "API_Path" should match "api_path" key.
	expanded := table.Expand("API_Path")
	assert.Greater(t, len(expanded), 1)
}

func TestExpandAll_Dedup(t *testing.T) {
	dir := t.TempDir()
	// Two keys that share a synonym.
	yaml := "a_term: [shared, unique_a]\nb_term: [shared, unique_b]\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".ctx-saver-synonyms.yaml"), []byte(yaml), 0600))

	table, err := search.Load(dir)
	require.NoError(t, err)

	expanded := table.ExpandAll([]string{"a_term", "b_term"})
	// "shared" should appear only once.
	count := 0
	for _, e := range expanded {
		if e == "shared" {
			count++
		}
	}
	assert.Equal(t, 1, count, "shared synonym must appear exactly once")
}

func TestExpandAll_PreservesOriginalOrder(t *testing.T) {
	table, err := search.Load("")
	require.NoError(t, err)

	expanded := table.ExpandAll([]string{"request", "response"})
	assert.Equal(t, "request", expanded[0])
}
