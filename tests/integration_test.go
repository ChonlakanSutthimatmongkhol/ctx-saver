package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/sandbox"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// testDataPath returns the absolute path to a file in tests/testdata/.
func testDataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

// newTestDeps creates a temporary store, a subprocess sandbox, and default config
// with a small threshold so large outputs are always stored.
func newTestDeps(t *testing.T) (*config.Config, sandbox.Sandbox, *store.SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	projectPath := t.TempDir()

	cfg := config.Default()
	cfg.Storage.DataDir = dir
	cfg.Summary.AutoIndexThresholdBytes = 512 // force storage for most outputs in tests
	cfg.Summary.HeadLines = 5
	cfg.Summary.TailLines = 2

	st, err := store.NewSQLiteStore(dir, projectPath)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	sb := sandbox.NewSubprocess(cfg.DenyCommands)
	return cfg, sb, st, projectPath
}

// ── ctx_execute tests ──────────────────────────────────────────────────────

func TestCtxExecute_SmallOutput_ReturnedDirectly(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)
	cfg.Summary.AutoIndexThresholdBytes = 10240 // make threshold large so echo returns directly

	h := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)
	_, out, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "echo hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello\n", out.DirectOutput)
	assert.Empty(t, out.OutputID, "small output should not be stored")
}

func TestCtxExecute_LargeOutput_Stored(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)

	h := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)
	_, out, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "cat " + testDataPath("large_log.txt"),
		Intent:   "read large log",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.OutputID, "large output should be stored")
	assert.NotEmpty(t, out.Summary, "summary should be returned")
	assert.Contains(t, out.SearchHint, out.OutputID)
	assert.Greater(t, out.Stats.Lines, 100)
}

func TestCtxExecute_DenyListBlocks(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)
	cfg.DenyCommands = []string{"sudo *"}
	sb = sandbox.NewSubprocess(cfg.DenyCommands)

	h := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)
	_, _, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "sudo rm -rf /tmp/x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deny list")
}

func TestCtxExecute_NonZeroExitCode(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)
	h := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)

	_, out, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "echo 'some output'; exit 1",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, out.Stats.ExitCode)
}

// ── ctx_read_file tests ────────────────────────────────────────────────────

func TestCtxReadFile_LargeFile_Stored(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)
	h := handlers.NewReadFileHandler(cfg, sb, st, proj, proj)

	_, out, err := h.Handle(context.Background(), nil, handlers.ReadFileInput{
		Path: testDataPath("large_log.txt"),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.OutputID)
	assert.NotEmpty(t, out.Summary)
}

func TestCtxReadFile_WithProcessScript(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)
	cfg.Summary.AutoIndexThresholdBytes = 10240 // keep small jq result direct
	h := handlers.NewReadFileHandler(cfg, sb, st, proj, proj)

	_, out, err := h.Handle(context.Background(), nil, handlers.ReadFileInput{
		Path:          testDataPath("jira_output.json"),
		ProcessScript: "python3 -c \"import json,sys; d=json.load(sys.stdin); print(d['total'])\"",
		Language:      "shell",
	})
	require.NoError(t, err)
	assert.Equal(t, "150\n", out.DirectOutput)
}

func TestCtxReadFile_MissingFile_ReturnsError(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)
	h := handlers.NewReadFileHandler(cfg, sb, st, proj, proj)

	_, _, err := h.Handle(context.Background(), nil, handlers.ReadFileInput{
		Path: "/nonexistent/path/file.txt",
	})
	require.Error(t, err)
}

// ── ctx_search tests ───────────────────────────────────────────────────────

func TestCtxSearch_FindsStoredContent(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)

	// First store a large output.
	execH := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)
	_, execOut, err := execH.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "cat " + testDataPath("large_log.txt"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, execOut.OutputID)

	// Now search for a term we know is in the log.
	searchH := handlers.NewSearchHandler(st, proj, nil)
	_, searchOut, err := searchH.Handle(context.Background(), nil, handlers.SearchInput{
		Queries:            []string{"connection pool exhausted"},
		OutputID:           execOut.OutputID,
		MaxResultsPerQuery: 3,
	})
	require.NoError(t, err)
	require.Len(t, searchOut.Results, 1)
	assert.NotEmpty(t, searchOut.Results[0].Matches, "expected match for 'connection pool exhausted'")
}

func TestCtxSearch_MultipleQueriesInParallel(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)

	execH := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)
	_, _, err := execH.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "cat " + testDataPath("large_log.txt"),
	})
	require.NoError(t, err)

	searchH := handlers.NewSearchHandler(st, proj, nil)
	start := time.Now()
	_, out, err := searchH.Handle(context.Background(), nil, handlers.SearchInput{
		Queries: []string{"ERROR", "timeout", "connection"},
	})
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Len(t, out.Results, 3)
	_ = elapsed // parallel queries should be fast; not strictly asserting timing
}

// ── ctx_list_outputs tests ─────────────────────────────────────────────────

func TestCtxListOutputs_ReturnsStoredOutputs(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)

	execH := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)
	for i := 0; i < 3; i++ {
		// seq 1 500 produces ~2 KB, well above the 512-byte test threshold.
		_, _, err := execH.Handle(context.Background(), nil, handlers.ExecuteInput{
			Language: "shell",
			Code:     "seq 1 500",
		})
		require.NoError(t, err)
	}

	listH := handlers.NewListHandler(st, proj)
	_, out, err := listH.Handle(context.Background(), nil, handlers.ListInput{})
	require.NoError(t, err)
	assert.Len(t, out.Outputs, 3)
}

// ── ctx_get_full tests ─────────────────────────────────────────────────────

func TestCtxGetFull_ReturnsAllLines(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)
	execH := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)

	_, execOut, err := execH.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "cat " + testDataPath("large_log.txt"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, execOut.OutputID)

	getH := handlers.NewGetFullHandler(st)
	_, full, err := getH.Handle(context.Background(), nil, handlers.GetFullInput{
		OutputID: execOut.OutputID,
	})
	require.NoError(t, err)
	assert.Greater(t, full.TotalLines, 1000)
	assert.Equal(t, full.TotalLines, full.Returned)
}

func TestCtxGetFull_LineRange(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)
	execH := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)

	_, execOut, err := execH.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "seq 1 1000",
	})
	require.NoError(t, err)
	require.NotEmpty(t, execOut.OutputID)

	getH := handlers.NewGetFullHandler(st)
	_, full, err := getH.Handle(context.Background(), nil, handlers.GetFullInput{
		OutputID:  execOut.OutputID,
		LineRange: []int{10, 20},
	})
	require.NoError(t, err)
	assert.Equal(t, 11, full.Returned) // lines 10..20 inclusive
	assert.Equal(t, "10", strings.TrimSpace(full.Lines[0]))
	assert.Equal(t, "20", strings.TrimSpace(full.Lines[10]))
}

func TestCtxGetFull_NotFound(t *testing.T) {
	_, _, st, _ := newTestDeps(t)
	h := handlers.NewGetFullHandler(st)
	_, _, err := h.Handle(context.Background(), nil, handlers.GetFullInput{OutputID: "out_doesnotexist"})
	require.Error(t, err)
}

// ── token saving benchmark (informational) ────────────────────────────────

func TestTokenSaving_LargeLog(t *testing.T) {
	cfg, sb, st, proj := newTestDeps(t)
	cfg.Summary.HeadLines = 20
	cfg.Summary.TailLines = 5

	data, err := os.ReadFile(testDataPath("large_log.txt"))
	require.NoError(t, err)

	execH := handlers.NewExecuteHandler(cfg, sb, st, proj, proj)
	_, out, err := execH.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "cat " + testDataPath("large_log.txt"),
	})
	require.NoError(t, err)

	rawBytes := len(data)
	summaryBytes := len(out.Summary)
	savingPct := float64(rawBytes-summaryBytes) / float64(rawBytes) * 100

	t.Logf("Large log: raw=%d bytes, summary=%d bytes, saving=%.1f%%", rawBytes, summaryBytes, savingPct)
	assert.GreaterOrEqual(t, savingPct, 50.0, "expected at least 50%% token saving")
}
