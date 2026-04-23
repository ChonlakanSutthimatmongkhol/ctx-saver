package handlers_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// ── Mocks ─────────────────────────────────────────────────────────────────

type mockSandbox struct {
	output   []byte
	exitCode int
	err      error
}

func (m *mockSandbox) Execute(_ context.Context, _ sandbox.ExecuteRequest) (sandbox.Result, error) {
	if m.err != nil {
		return sandbox.Result{}, m.err
	}
	return sandbox.Result{
		Output:   m.output,
		ExitCode: m.exitCode,
		Duration: time.Millisecond,
	}, nil
}

type mockStore struct {
	saved     []*store.Output
	saveErr   error
	getErr    error
	listMeta  []*store.OutputMeta
	listErr   error
	matches   []*store.Match
	searchErr error
}

func (m *mockStore) Save(_ context.Context, o *store.Output) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, o)
	return nil
}

func (m *mockStore) Get(_ context.Context, id string) (*store.Output, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for _, o := range m.saved {
		if o.OutputID == id {
			return o, nil
		}
	}
	return nil, fmt.Errorf("store: output %q not found", id)
}

func (m *mockStore) List(_ context.Context, _ string, _ int) ([]*store.OutputMeta, error) {
	return m.listMeta, m.listErr
}

func (m *mockStore) Search(_ context.Context, _, _ string, _ int) ([]*store.Match, error) {
	return m.matches, m.searchErr
}

func (m *mockStore) Cleanup(_ context.Context, _ string, _ int) error { return nil }
func (m *mockStore) Close() error                                     { return nil }

func (m *mockStore) SaveSessionEvent(_ context.Context, _ *store.SessionEvent) error {
	return nil
}
func (m *mockStore) ListSessionEvents(_ context.Context, _ string, _ int) ([]*store.SessionEvent, error) {
	return nil, nil
}
func (m *mockStore) ListProjectSessionEvents(_ context.Context, _ string, _ int) ([]*store.SessionEvent, error) {
	return nil, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────

func defaultCfg() *config.Config {
	cfg := config.Default()
	cfg.Summary.AutoIndexThresholdBytes = 512
	cfg.Summary.HeadLines = 5
	cfg.Summary.TailLines = 2
	cfg.Sandbox.TimeoutSeconds = 10
	return cfg
}

func largeOutput(n int) []byte {
	return []byte(strings.Repeat("x\n", n))
}

// ── ExecuteHandler tests ──────────────────────────────────────────────────

func TestExecuteHandler_EmptyCode_Error(t *testing.T) {
	h := handlers.NewExecuteHandler(defaultCfg(), &mockSandbox{}, &mockStore{}, "/proj", "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "code must not be empty")
}

func TestExecuteHandler_SmallOutput_ReturnedDirectly(t *testing.T) {
	cfg := defaultCfg()
	cfg.Summary.AutoIndexThresholdBytes = 10240 // large threshold → small output
	sb := &mockSandbox{output: []byte("hello\n")}
	st := &mockStore{}

	h := handlers.NewExecuteHandler(cfg, sb, st, "/proj", "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "echo hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello\n", out.DirectOutput)
	assert.Empty(t, out.OutputID)
	assert.Empty(t, st.saved)
}

func TestExecuteHandler_LargeOutput_StoredAndSummarised(t *testing.T) {
	cfg := defaultCfg()
	// 600 bytes > 512 threshold
	bigOut := largeOutput(300)
	sb := &mockSandbox{output: bigOut}
	st := &mockStore{}

	h := handlers.NewExecuteHandler(cfg, sb, st, "/proj", "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "seq 300",
		Intent:   "test large output",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.OutputID)
	assert.NotEmpty(t, out.Summary)
	assert.Empty(t, out.DirectOutput)
	assert.Len(t, st.saved, 1)
	assert.Equal(t, "test large output", st.saved[0].Intent)
}

func TestExecuteHandler_NonZeroExitCode_Succeeds(t *testing.T) {
	sb := &mockSandbox{output: []byte("error output\n"), exitCode: 1}
	st := &mockStore{}

	h := handlers.NewExecuteHandler(defaultCfg(), sb, st, "/proj", "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "exit 1",
	})
	require.NoError(t, err) // non-zero exit is not a handler error
	assert.Equal(t, 1, out.Stats.ExitCode)
}

func TestExecuteHandler_SandboxError_ReturnsError(t *testing.T) {
	sb := &mockSandbox{err: fmt.Errorf("sandbox boom")}
	h := handlers.NewExecuteHandler(defaultCfg(), sb, &mockStore{}, "/proj", "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "something",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox boom")
}

func TestExecuteHandler_StoreError_ReturnsError(t *testing.T) {
	cfg := defaultCfg()
	sb := &mockSandbox{output: largeOutput(300)}
	st := &mockStore{saveErr: fmt.Errorf("disk full")}

	h := handlers.NewExecuteHandler(cfg, sb, st, "/proj", "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "big cmd",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

func TestExecuteHandler_OutputExceedsMaxSize_Error(t *testing.T) {
	cfg := defaultCfg()
	cfg.Storage.MaxOutputSizeMB = 0 // effectively 0 MB max → any output is too big
	sb := &mockSandbox{output: []byte("some output")}

	h := handlers.NewExecuteHandler(cfg, sb, &mockStore{}, "/proj", "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language: "shell",
		Code:     "echo hi",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds max_output_size_mb")
}

func TestExecuteHandler_CustomSummaryLines(t *testing.T) {
	cfg := defaultCfg()
	sb := &mockSandbox{output: largeOutput(300)}
	st := &mockStore{}

	h := handlers.NewExecuteHandler(cfg, sb, st, "/proj", "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.ExecuteInput{
		Language:     "shell",
		Code:         "seq 300",
		SummaryLines: 3,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.Summary)
}

// ── ReadFileHandler tests ─────────────────────────────────────────────────

func TestReadFileHandler_EmptyPath_Error(t *testing.T) {
	h := handlers.NewReadFileHandler(defaultCfg(), &mockSandbox{}, &mockStore{}, "/proj", "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.ReadFileInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path must not be empty")
}

func TestReadFileHandler_MissingFile_Error(t *testing.T) {
	h := handlers.NewReadFileHandler(defaultCfg(), &mockSandbox{}, &mockStore{}, "/proj", "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.ReadFileInput{
		Path: "/nonexistent/file.txt",
	})
	require.Error(t, err)
}

func TestReadFileHandler_SmallFile_ReturnedDirectly(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "small.txt")
	require.NoError(t, os.WriteFile(f, []byte("hello file\n"), 0600))

	cfg := defaultCfg()
	cfg.Summary.AutoIndexThresholdBytes = 10240 // large threshold
	st := &mockStore{}
	h := handlers.NewReadFileHandler(cfg, &mockSandbox{}, st, "/proj", dir)

	_, out, err := h.Handle(context.Background(), nil, handlers.ReadFileInput{Path: f})
	require.NoError(t, err)
	assert.Equal(t, "hello file\n", out.DirectOutput)
	assert.Empty(t, out.OutputID)
}

func TestReadFileHandler_LargeFile_Stored(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("line of content\n", 50) // ~800 bytes > 512
	f := filepath.Join(dir, "large.txt")
	require.NoError(t, os.WriteFile(f, []byte(content), 0600))

	st := &mockStore{}
	h := handlers.NewReadFileHandler(defaultCfg(), &mockSandbox{}, st, "/proj", dir)

	_, out, err := h.Handle(context.Background(), nil, handlers.ReadFileInput{Path: f})
	require.NoError(t, err)
	assert.NotEmpty(t, out.OutputID)
	assert.Len(t, st.saved, 1)
}

// ── SearchHandler tests ───────────────────────────────────────────────────

func TestSearchHandler_EmptyQueries_Error(t *testing.T) {
	h := handlers.NewSearchHandler(&mockStore{}, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.SearchInput{Queries: nil})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queries must not be empty")
}

func TestSearchHandler_ReturnsMatches(t *testing.T) {
	st := &mockStore{
		matches: []*store.Match{
			{OutputID: "out_abc", Line: 5, Snippet: "found it", Score: 1.0},
		},
	}
	h := handlers.NewSearchHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.SearchInput{
		Queries: []string{"found"},
	})
	require.NoError(t, err)
	require.Len(t, out.Results, 1)
	assert.Equal(t, "found", out.Results[0].Query)
	assert.Len(t, out.Results[0].Matches, 1)
	assert.Equal(t, "out_abc", out.Results[0].Matches[0].OutputID)
}

func TestSearchHandler_MultipleQueries_AllReturned(t *testing.T) {
	st := &mockStore{matches: []*store.Match{}}
	h := handlers.NewSearchHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.SearchInput{
		Queries: []string{"q1", "q2", "q3"},
	})
	require.NoError(t, err)
	assert.Len(t, out.Results, 3)
}

func TestSearchHandler_StoreError_ReturnsError(t *testing.T) {
	st := &mockStore{searchErr: fmt.Errorf("fts broken")}
	h := handlers.NewSearchHandler(st, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.SearchInput{
		Queries: []string{"q"},
	})
	require.Error(t, err)
}

func TestSearchHandler_DefaultMaxResults(t *testing.T) {
	st := &mockStore{matches: []*store.Match{}}
	h := handlers.NewSearchHandler(st, "/proj")
	// MaxResultsPerQuery = 0 → should default to 5, not error
	_, out, err := h.Handle(context.Background(), nil, handlers.SearchInput{
		Queries:            []string{"anything"},
		MaxResultsPerQuery: 0,
	})
	require.NoError(t, err)
	assert.Len(t, out.Results, 1)
}

// ── ListHandler tests ─────────────────────────────────────────────────────

func TestListHandler_EmptyStore_ReturnsEmptyList(t *testing.T) {
	h := handlers.NewListHandler(&mockStore{}, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.ListInput{})
	require.NoError(t, err)
	assert.Empty(t, out.Outputs)
}

func TestListHandler_ReturnsEntries(t *testing.T) {
	now := time.Now().UTC()
	st := &mockStore{
		listMeta: []*store.OutputMeta{
			{OutputID: "out_1", Command: "go test ./...", CreatedAt: now, SizeBytes: 1024, LineCount: 80},
			{OutputID: "out_2", Command: "make build", CreatedAt: now, SizeBytes: 512, LineCount: 10},
		},
	}
	h := handlers.NewListHandler(st, "/proj")
	_, out, err := h.Handle(context.Background(), nil, handlers.ListInput{Limit: 10})
	require.NoError(t, err)
	require.Len(t, out.Outputs, 2)
	assert.Equal(t, "out_1", out.Outputs[0].OutputID)
	assert.Equal(t, "go test ./...", out.Outputs[0].Command)
	assert.Equal(t, int64(1024), out.Outputs[0].SizeBytes)
}

func TestListHandler_StoreError_ReturnsError(t *testing.T) {
	st := &mockStore{listErr: fmt.Errorf("db locked")}
	h := handlers.NewListHandler(st, "/proj")
	_, _, err := h.Handle(context.Background(), nil, handlers.ListInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db locked")
}

// ── GetFullHandler tests ──────────────────────────────────────────────────

func TestGetFullHandler_EmptyOutputID_Error(t *testing.T) {
	h := handlers.NewGetFullHandler(&mockStore{})
	_, _, err := h.Handle(context.Background(), nil, handlers.GetFullInput{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output_id must not be empty")
}

func TestGetFullHandler_NotFound_Error(t *testing.T) {
	st := &mockStore{getErr: fmt.Errorf("store: output \"out_xyz\" not found")}
	h := handlers.NewGetFullHandler(st)
	_, _, err := h.Handle(context.Background(), nil, handlers.GetFullInput{OutputID: "out_xyz"})
	require.Error(t, err)
}

func TestGetFullHandler_AllLines(t *testing.T) {
	content := "line1\nline2\nline3\n"
	st := &mockStore{
		saved: []*store.Output{
			{OutputID: "out_abc", FullOutput: content},
		},
	}
	h := handlers.NewGetFullHandler(st)
	_, out, err := h.Handle(context.Background(), nil, handlers.GetFullInput{OutputID: "out_abc"})
	require.NoError(t, err)
	assert.Equal(t, 3, out.TotalLines)
	assert.Equal(t, 3, out.Returned)
	assert.Equal(t, "line1", out.Lines[0])
}

func TestGetFullHandler_LineRange(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	content := strings.Join(lines, "\n") + "\n"
	st := &mockStore{
		saved: []*store.Output{
			{OutputID: "out_range", FullOutput: content},
		},
	}
	h := handlers.NewGetFullHandler(st)
	_, out, err := h.Handle(context.Background(), nil, handlers.GetFullInput{
		OutputID:  "out_range",
		LineRange: []int{5, 10},
	})
	require.NoError(t, err)
	assert.Equal(t, 6, out.Returned)
	assert.Equal(t, "line5", out.Lines[0])
	assert.Equal(t, "line10", out.Lines[5])
}

func TestGetFullHandler_LineRange_StartAfterEnd_Error(t *testing.T) {
	st := &mockStore{
		saved: []*store.Output{
			{OutputID: "out_bad", FullOutput: "a\nb\nc\n"},
		},
	}
	h := handlers.NewGetFullHandler(st)
	_, _, err := h.Handle(context.Background(), nil, handlers.GetFullInput{
		OutputID:  "out_bad",
		LineRange: []int{10, 5},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be <=")
}

func TestGetFullHandler_LineRange_WrongLength_Error(t *testing.T) {
	st := &mockStore{
		saved: []*store.Output{
			{OutputID: "out_bad2", FullOutput: "a\nb\n"},
		},
	}
	h := handlers.NewGetFullHandler(st)
	_, _, err := h.Handle(context.Background(), nil, handlers.GetFullInput{
		OutputID:  "out_bad2",
		LineRange: []int{1},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly 2 elements")
}

func TestGetFullHandler_LineRange_ClampedToTotalLines(t *testing.T) {
	st := &mockStore{
		saved: []*store.Output{
			{OutputID: "out_clamp", FullOutput: "a\nb\nc\n"},
		},
	}
	h := handlers.NewGetFullHandler(st)
	_, out, err := h.Handle(context.Background(), nil, handlers.GetFullInput{
		OutputID:  "out_clamp",
		LineRange: []int{2, 999}, // end beyond total → clamped
	})
	require.NoError(t, err)
	assert.Equal(t, 2, out.Returned) // lines 2 and 3
}
